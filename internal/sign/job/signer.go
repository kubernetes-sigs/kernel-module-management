package signjob

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"os"
	"text/template"

	"github.com/mitchellh/hashstructure"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/kubernetes-sigs/kernel-module-management/internal/api"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
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

//go:generate mockgen -source=signer.go -package=signjob -destination=mock_signer.go

type Signer interface {
	MakeJobTemplate(
		ctx context.Context,
		mld *api.ModuleLoaderData,
		labels map[string]string,
		imageToSign string,
		pushImage bool,
		owner metav1.Object,
	) (*batchv1.Job, error)
}

type hashData struct {
	PrivateKeyData []byte
	PublicKeyData  []byte
	PodTemplate    *v1.PodTemplateSpec
}

type signer struct {
	client    client.Client
	scheme    *runtime.Scheme
	jobHelper utils.JobHelper
}

func NewSigner(
	client client.Client,
	scheme *runtime.Scheme,
	jobHelper utils.JobHelper) Signer {
	return &signer{
		client:    client,
		scheme:    scheme,
		jobHelper: jobHelper,
	}
}

func (s *signer) MakeJobTemplate(
	ctx context.Context,
	mld *api.ModuleLoaderData,
	labels map[string]string,
	imageToSign string,
	pushImage bool,
	owner metav1.Object) (*batchv1.Job, error) {

	signConfig := mld.Sign

	var buf bytes.Buffer

	td := TemplateData{
		FilesToSign: mld.Sign.FilesToSign,
		SignImage:   os.Getenv("RELATED_IMAGES_SIGN"),
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

	specTemplate := v1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{dockerfileAnnotationKey: buf.String()},
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name:         "kaniko",
					Image:        os.Getenv("RELATED_IMAGES_BUILD"),
					Args:         args,
					VolumeMounts: volumeMounts,
				},
			},
			RestartPolicy: v1.RestartPolicyNever,
			Volumes:       volumes,
			NodeSelector:  mld.Selector,
		},
	}

	specTemplateHash, err := s.getHashAnnotationValue(ctx, signConfig.KeySecret.Name, signConfig.CertSecret.Name, mld.Namespace, &specTemplate)
	if err != nil {
		return nil, fmt.Errorf("could not hash job's definitions: %v", err)
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: mld.Name + "-sign-",
			Namespace:    mld.Namespace,
			Labels:       labels,
			Annotations:  map[string]string{constants.JobHashAnnotation: fmt.Sprintf("%d", specTemplateHash)},
		},
		Spec: batchv1.JobSpec{
			Completions:  pointer.Int32(1),
			Template:     specTemplate,
			BackoffLimit: pointer.Int32(0),
		},
	}

	if err = controllerutil.SetControllerReference(owner, job, s.scheme); err != nil {
		return nil, fmt.Errorf("could not set the owner reference: %v", err)
	}

	return job, nil
}

func (s *signer) getHashAnnotationValue(ctx context.Context, privateSecret, publicSecret, namespace string, podTemplate *v1.PodTemplateSpec) (uint64, error) {
	privateKeyData, err := s.getSecretData(ctx, privateSecret, constants.PrivateSignDataKey, namespace)
	if err != nil {
		return 0, fmt.Errorf("failed to get private secret %s for signing: %v", privateSecret, err)
	}
	publicKeyData, err := s.getSecretData(ctx, publicSecret, constants.PublicSignDataKey, namespace)
	if err != nil {
		return 0, fmt.Errorf("failed to get public secret %s for signing: %v", publicSecret, err)
	}

	return getHashValue(podTemplate, publicKeyData, privateKeyData)
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

func getHashValue(podTemplate *v1.PodTemplateSpec, publicKeyData, privateKeyData []byte) (uint64, error) {
	dataToHash := hashData{
		PrivateKeyData: privateKeyData,
		PublicKeyData:  publicKeyData,
		PodTemplate:    podTemplate,
	}
	hashValue, err := hashstructure.Hash(dataToHash, nil)
	if err != nil {
		return 0, fmt.Errorf("could not hash job's spec template and dockefile: %v", err)
	}
	return hashValue, nil
}
