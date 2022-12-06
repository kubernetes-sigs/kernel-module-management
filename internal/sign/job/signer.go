package signjob

import (
	"context"
	"fmt"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	"github.com/kubernetes-sigs/kernel-module-management/internal/module"
	"github.com/kubernetes-sigs/kernel-module-management/internal/sign"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
	"github.com/mitchellh/hashstructure"
)

//go:generate mockgen -source=signer.go -package=signjob -destination=mock_signer.go

type Signer interface {
	MakeJobTemplate(
		ctx context.Context,
		mod kmmv1beta1.Module,
		km kmmv1beta1.KernelMapping,
		targetKernel string,
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
	helper    sign.Helper
	jobHelper utils.JobHelper
}

func NewSigner(
	client client.Client,
	scheme *runtime.Scheme,
	helper sign.Helper,
	jobHelper utils.JobHelper) Signer {
	return &signer{
		client:    client,
		scheme:    scheme,
		helper:    helper,
		jobHelper: jobHelper,
	}
}

func (m *signer) MakeJobTemplate(
	ctx context.Context,
	mod kmmv1beta1.Module,
	km kmmv1beta1.KernelMapping,
	targetKernel string,
	labels map[string]string,
	imageToSign string,
	pushImage bool,
	owner metav1.Object) (*batchv1.Job, error) {

	signConfig := m.helper.GetRelevantSign(mod.Spec, km)

	args := make([]string, 0)

	if pushImage {
		args = append(args, "-signedimage", km.ContainerImage)

		registryTLS := module.TLSOptions(mod.Spec, km)
		if registryTLS.Insecure {
			args = append(args, "--insecure")
		}
		if registryTLS.InsecureSkipTLSVerify {
			args = append(args, "--skip-tls-verify")
		}
	} else {
		args = append(args, "-no-push")
	}

	if imageToSign != "" {
		args = append(args, "-unsignedimage", imageToSign)
	} else if signConfig.UnsignedImage != "" {
		args = append(args, "-unsignedimage", signConfig.UnsignedImage)
	} else {
		return nil, fmt.Errorf("no image to sign given")
	}
	args = append(args, "-key", "/signingkey/key.priv")
	args = append(args, "-cert", "/signingcert/public.der")

	if len(signConfig.FilesToSign) > 0 {
		args = append(args, "-filestosign", strings.Join(signConfig.FilesToSign, ":"))
	}

	if signConfig.UnsignedImageRegistryTLS.Insecure {
		args = append(args, "--insecure-pull")
	}

	if signConfig.UnsignedImageRegistryTLS.InsecureSkipTLSVerify {
		args = append(args, "--skip-tls-verify-pull")
	}

	volumes := []v1.Volume{
		utils.MakeSecretVolume(signConfig.KeySecret, "key", "key.priv"),
		utils.MakeSecretVolume(signConfig.CertSecret, "cert", "public.der"),
	}
	volumeMounts := []v1.VolumeMount{
		utils.MakeSecretVolumeMount(signConfig.CertSecret, "/signingcert"),
		utils.MakeSecretVolumeMount(signConfig.KeySecret, "/signingkey"),
	}

	if mod.Spec.ImageRepoSecret != nil {
		args = append(args, "-pullsecret", "/docker_config/config.json")
		volumes = append(volumes, utils.MakeSecretVolume(mod.Spec.ImageRepoSecret, v1.DockerConfigJsonKey, "config.json"))
		volumeMounts = append(volumeMounts, utils.MakeSecretVolumeMount(mod.Spec.ImageRepoSecret, "/docker_config"))
	}

	specTemplate := v1.PodTemplateSpec{
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name:         "signimage",
					Image:        "quay.io/chrisp262/kmod-signer:latest",
					Args:         args,
					VolumeMounts: volumeMounts,
				},
			},
			RestartPolicy: v1.RestartPolicyOnFailure,
			Volumes:       volumes,
			NodeSelector:  mod.Spec.Selector,
		},
	}

	specTemplateHash, err := m.getHashAnnotationValue(ctx, signConfig.KeySecret.Name, signConfig.CertSecret.Name, mod.Namespace, &specTemplate)
	if err != nil {
		return nil, fmt.Errorf("could not hash job's definitions: %v", err)
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: mod.Name + "-sign-",
			Namespace:    mod.Namespace,
			Labels:       labels,
			Annotations:  map[string]string{constants.JobHashAnnotation: fmt.Sprintf("%d", specTemplateHash)},
		},
		Spec: batchv1.JobSpec{
			Completions: pointer.Int32(1),
			Template:    specTemplate,
		},
	}

	if err := controllerutil.SetControllerReference(owner, job, m.scheme); err != nil {
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
