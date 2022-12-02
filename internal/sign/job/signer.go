package signjob

import (
	"fmt"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/sign"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
)

//go:generate mockgen -source=signer.go -package=signjob -destination=mock_signer.go

type Signer interface {
	MakeJobTemplate(
		mod kmmv1beta1.Module,
		km kmmv1beta1.KernelMapping,
		targetKernel string,
		labels map[string]string,
		imageToSign string,
		pushImage bool,
		owner metav1.Object,
	) (*batchv1.Job, error)
}

type signer struct {
	scheme    *runtime.Scheme
	helper    sign.Helper
	jobHelper utils.JobHelper
}

func NewSigner(
	scheme *runtime.Scheme,
	helper sign.Helper,
	jobHelper utils.JobHelper) Signer {
	return &signer{
		scheme:    scheme,
		helper:    helper,
		jobHelper: jobHelper,
	}
}

func (m *signer) MakeJobTemplate(
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

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: mod.Name + "-sign-",
			Namespace:    mod.Namespace,
			Labels:       labels,
		},
		Spec: batchv1.JobSpec{
			Completions: pointer.Int32(1),
			Template: v1.PodTemplateSpec{
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
			},
		},
	}

	if err := controllerutil.SetControllerReference(owner, job, m.scheme); err != nil {
		return nil, fmt.Errorf("could not set the owner reference: %v", err)
	}

	return job, nil
}
