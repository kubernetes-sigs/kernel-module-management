package sign

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"text/template"

	"github.com/kubernetes-sigs/kernel-module-management/internal/api"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	"github.com/kubernetes-sigs/kernel-module-management/internal/imgbuild"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
	"github.com/mitchellh/hashstructure/v2"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

var (
	//go:embed templates
	templateFS embed.FS

	tmpl = template.Must(
		template.ParseFS(templateFS, "templates/Dockerfile.gotmpl"),
	)
)

type (
	TemplateData struct {
		FilesToSign   []string
		SignImage     string
		UnsignedImage string
	}

	jobMaker struct {
		buildImage string
		client     client.Client
		jobHelper  imgbuild.JobHelper
		scheme     *runtime.Scheme
		signImage  string
	}
)

var _ imgbuild.JobMaker = &jobMaker{}

func NewJobMaker(client client.Client, jobHelper imgbuild.JobHelper, buildImage, signImage string, scheme *runtime.Scheme) imgbuild.JobMaker {
	return &jobMaker{
		buildImage: buildImage,
		client:     client,
		jobHelper:  jobHelper,
		scheme:     scheme,
		signImage:  signImage,
	}
}

func (s *jobMaker) MakeJob(ctx context.Context, mld *api.ModuleLoaderData, owner metav1.Object, pushImage bool) (*batchv1.Job, error) {

	signConfig := mld.Sign

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

	var buf bytes.Buffer

	unsignedImage, err := mld.UnsignedImage()
	if err != nil {
		return nil, fmt.Errorf("could not determine the unsigned image: %v", err)
	}

	td := TemplateData{
		FilesToSign:   mld.Sign.FilesToSign,
		SignImage:     s.signImage,
		UnsignedImage: unsignedImage,
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
					Image:        s.buildImage,
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
			Labels:       s.jobHelper.JobLabels(mld.Name, mld.KernelVersion, imgbuild.JobTypeSign),
			Annotations:  map[string]string{imgbuild.JobHashAnnotation: fmt.Sprintf("%d", specTemplateHash)},
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

func (s *jobMaker) getHashAnnotationValue(ctx context.Context, privateSecret, publicSecret, namespace string, podTemplate *v1.PodTemplateSpec) (uint64, error) {
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

func (s *jobMaker) getSecretData(ctx context.Context, secretName, secretDataKey, namespace string) ([]byte, error) {
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

type hashData struct {
	PrivateKeyData []byte
	PublicKeyData  []byte
	PodTemplate    *v1.PodTemplateSpec
}

func getHashValue(podTemplate *v1.PodTemplateSpec, publicKeyData, privateKeyData []byte) (uint64, error) {
	dataToHash := hashData{
		PrivateKeyData: privateKeyData,
		PublicKeyData:  publicKeyData,
		PodTemplate:    podTemplate,
	}
	hashValue, err := hashstructure.Hash(dataToHash, hashstructure.FormatV2, nil)
	if err != nil {
		return 0, fmt.Errorf("could not hash job's spec template and dockefile: %v", err)
	}
	return hashValue, nil
}
