package pod

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"os"
	"text/template"

	"github.com/mitchellh/hashstructure/v2"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/kubernetes-sigs/kernel-module-management/internal/api"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	"github.com/kubernetes-sigs/kernel-module-management/internal/module"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
)

type TemplateData struct {
	FilesToSign   []string
	SignImage     string
	UnsignedImage string
}

//go:embed templates
var templateFS embed.FS

var tmpl = template.Must(
	template.ParseFS(templateFS, "templates/Dockerfile.gotmpl"),
)

//go:generate mockgen -source=signer.go -package=pod -destination=mock_signer.go

type Signer interface {
	MakePodTemplate(
		ctx context.Context,
		mld *api.ModuleLoaderData,
		owner metav1.Object,
		pushImage bool,
	) (*v1.Pod, error)
}

type signHashData struct {
	PrivateKeyData []byte
	PublicKeyData  []byte
	SignConfig     []byte
	PodSpec        *v1.PodSpec
}

type signer struct {
	client              client.Client
	scheme              *runtime.Scheme
	buildSignPodManager BuildSignPodManager
}

func NewSigner(
	client client.Client,
	scheme *runtime.Scheme,
	buildSignPodManager BuildSignPodManager) Signer {
	return &signer{
		client:              client,
		scheme:              scheme,
		buildSignPodManager: buildSignPodManager,
	}
}

func (s *signer) MakePodTemplate(
	ctx context.Context,
	mld *api.ModuleLoaderData,
	owner metav1.Object,
	pushImage bool) (*v1.Pod, error) {

	signConfig := mld.Sign

	var buf bytes.Buffer

	td := TemplateData{
		FilesToSign: mld.Sign.FilesToSign,
		SignImage:   os.Getenv("RELATED_IMAGE_SIGN"),
	}

	imageToSign := ""
	if module.ShouldBeBuilt(mld) {
		imageToSign = module.IntermediateImageName(mld.Name, mld.Namespace, mld.ContainerImage)
	}

	args := make([]string, 0)

	if pushImage {
		args = append(args, "--destination", mld.ContainerImage)

		if mld.RegistryTLS.Insecure {
			args = append(args, "--insecure")
		}
		if mld.RegistryTLS.InsecureSkipTLSVerify {
			args = append(args, "--skip-tls-verify")
		}
	} else {
		args = append(args, "-no-push")
	}

	if imageToSign != "" {
		td.UnsignedImage = imageToSign
	} else if signConfig.UnsignedImage != "" {
		td.UnsignedImage = signConfig.UnsignedImage
	} else {
		return nil, fmt.Errorf("no image to sign given")
	}

	if signConfig.UnsignedImageRegistryTLS.Insecure {
		args = append(args, "--insecure-pull")
	}

	if signConfig.UnsignedImageRegistryTLS.InsecureSkipTLSVerify {
		args = append(args, "--skip-tls-verify-pull")
	}

	volumes := []v1.Volume{
		utils.MakeSecretVolume(signConfig.KeySecret, "key", "key.pem"),
		utils.MakeSecretVolume(signConfig.CertSecret, "cert", "cert.pem"),
	}

	volumeMounts := []v1.VolumeMount{
		utils.MakeSecretVolumeMount(signConfig.CertSecret, "/run/secrets/cert", true),
		utils.MakeSecretVolumeMount(signConfig.KeySecret, "/run/secrets/key", true),
	}

	const (
		dockerfileAnnotationKey = "dockerfile"
		dockerfileVolumeName    = "dockerfile"
	)

	dockerfileVolume := v1.Volume{
		Name: dockerfileVolumeName,
		VolumeSource: v1.VolumeSource{
			DownwardAPI: &v1.DownwardAPIVolumeSource{
				Items: []v1.DownwardAPIVolumeFile{
					{
						Path: "Dockerfile",
						FieldRef: &v1.ObjectFieldSelector{
							FieldPath: fmt.Sprintf("metadata.annotations['%s']", dockerfileAnnotationKey),
						},
					},
				},
			},
		},
	}

	volumes = append(volumes, dockerfileVolume)

	volumeMounts = append(
		volumeMounts,
		v1.VolumeMount{
			Name:      dockerfileVolumeName,
			ReadOnly:  true,
			MountPath: "/workspace",
		},
	)

	if secretRef := mld.ImageRepoSecret; secretRef != nil {
		volumes = append(
			volumes,
			utils.MakeSecretVolume(secretRef, ".dockerconfigjson", "config.json"),
		)

		volumeMounts = append(
			volumeMounts,
			utils.MakeSecretVolumeMount(secretRef, "/kaniko/.docker", true),
		)
	}

	if err := tmpl.Execute(&buf, td); err != nil {
		return nil, fmt.Errorf("could not execute template: %v", err)
	}

	podSpec := v1.PodSpec{
		Containers: []v1.Container{
			{
				Name:         "kaniko",
				Image:        os.Getenv("RELATED_IMAGE_BUILD"),
				Args:         args,
				VolumeMounts: volumeMounts,
			},
		},
		RestartPolicy: v1.RestartPolicyNever,
		Volumes:       volumes,
		NodeSelector:  mld.Selector,
		Tolerations:   mld.Tolerations,
	}

	podSpecHash, err := s.getHashAnnotationValue(ctx, signConfig.KeySecret.Name,
		signConfig.CertSecret.Name, mld.Namespace, buf.Bytes(), &podSpec)
	if err != nil {
		return nil, fmt.Errorf("could not hash pod's definitions: %v", err)
	}

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: mld.Name + "-sign-",
			Namespace:    mld.Namespace,
			Labels:       s.buildSignPodManager.PodLabels(mld.Name, mld.KernelNormalizedVersion, PodTypeSign),
			Annotations: map[string]string{
				constants.PodHashAnnotation: fmt.Sprintf("%d", podSpecHash),
				dockerfileAnnotationKey:     buf.String(),
			},
			Finalizers: []string{constants.GCDelayFinalizer, constants.JobEventFinalizer},
		},
		Spec: podSpec,
	}

	if err = controllerutil.SetControllerReference(owner, pod, s.scheme); err != nil {
		return nil, fmt.Errorf("could not set the owner reference: %v", err)
	}

	return pod, nil
}

func (s *signer) getHashAnnotationValue(ctx context.Context, privateSecret, publicSecret, namespace string,
	signConfig []byte, podSpec *v1.PodSpec) (uint64, error) {

	privateKeyData, err := s.getSecretData(ctx, privateSecret, constants.PrivateSignDataKey, namespace)
	if err != nil {
		return 0, fmt.Errorf("failed to get private secret %s for signing: %v", privateSecret, err)
	}
	publicKeyData, err := s.getSecretData(ctx, publicSecret, constants.PublicSignDataKey, namespace)
	if err != nil {
		return 0, fmt.Errorf("failed to get public secret %s for signing: %v", publicSecret, err)
	}

	return getSignHashValue(podSpec, publicKeyData, privateKeyData, signConfig)
}

func (s *signer) getSecretData(ctx context.Context, secretName, secretDataKey, namespace string) ([]byte, error) {
	secret := v1.Secret{}
	namespacedName := types.NamespacedName{Name: secretName, Namespace: namespace}
	err := s.client.Get(ctx, namespacedName, &secret)
	if err != nil {
		return nil, fmt.Errorf("failed to get Secret %s: %v", namespacedName, err)
	}
	data, ok := secret.Data[secretDataKey]
	if !ok {
		return nil, fmt.Errorf("invalid Secret %s format, %s key is missing", namespacedName, secretDataKey)
	}
	return data, nil
}

func getSignHashValue(podSpec *v1.PodSpec, publicKeyData, privateKeyData, signConfig []byte) (uint64, error) {
	dataToHash := signHashData{
		PrivateKeyData: privateKeyData,
		PublicKeyData:  publicKeyData,
		SignConfig:     signConfig,
		PodSpec:        podSpec,
	}
	hashValue, err := hashstructure.Hash(dataToHash, hashstructure.FormatV2, nil)
	if err != nil {
		return 0, fmt.Errorf("could not hash pod's spec template and dockefile: %v", err)
	}
	return hashValue, nil
}
