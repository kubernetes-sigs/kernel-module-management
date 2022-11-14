package job

import (
	"fmt"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/build"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
	"github.com/mitchellh/hashstructure"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

//go:generate mockgen -source=maker.go -package=job -destination=mock_maker.go

type Maker interface {
	MakeJobTemplate(mod kmmv1beta1.Module, buildConfig *kmmv1beta1.Build, targetKernel, containerImage string, pushImage bool) (*batchv1.Job, error)
}

type maker struct {
	helper    build.Helper
	jobHelper utils.JobHelper
	scheme    *runtime.Scheme
}

func NewMaker(helper build.Helper, jobHelper utils.JobHelper, scheme *runtime.Scheme) Maker {
	return &maker{
		helper:    helper,
		jobHelper: jobHelper,
		scheme:    scheme,
	}
}

func (m *maker) MakeJobTemplate(mod kmmv1beta1.Module, buildConfig *kmmv1beta1.Build, targetKernel, containerImage string, pushImage bool) (*batchv1.Job, error) {
	args := []string{}
	if pushImage {
		args = append(args, "--destination", containerImage)
	} else {
		args = append(args, "--no-push")
	}

	buildArgs := m.helper.ApplyBuildArgOverrides(
		buildConfig.BuildArgs,
		kmmv1beta1.BuildArg{Name: "KERNEL_VERSION", Value: targetKernel},
	)

	for _, ba := range buildArgs {
		args = append(args, "--build-arg", fmt.Sprintf("%s=%s", ba.Name, ba.Value))
	}

	if buildConfig.Pull.Insecure {
		args = append(args, "--insecure-pull")
	}

	if buildConfig.Pull.InsecureSkipTLSVerify {
		args = append(args, "--skip-tls-verify-pull")
	}

	if buildConfig.Push.Insecure {
		args = append(args, "--insecure")
	}

	if buildConfig.Push.InsecureSkipTLSVerify {
		args = append(args, "--skip-tls-verify")
	}

	const dockerfileVolumeName = "dockerfile"

	dockerFileVolume := v1.Volume{
		Name: dockerfileVolumeName,
		VolumeSource: v1.VolumeSource{
			DownwardAPI: &v1.DownwardAPIVolumeSource{
				Items: []v1.DownwardAPIVolumeFile{
					{
						Path:     "Dockerfile",
						FieldRef: &v1.ObjectFieldSelector{FieldPath: "metadata.annotations['Dockerfile']"},
					},
				},
			},
		},
	}

	dockerFileVolumeMount := v1.VolumeMount{
		Name:      dockerfileVolumeName,
		ReadOnly:  true,
		MountPath: "/workspace",
	}

	volumes := []v1.Volume{dockerFileVolume}
	volumeMounts := []v1.VolumeMount{dockerFileVolumeMount}
	if irs := mod.Spec.ImageRepoSecret; irs != nil {
		volumes = append(volumes, makeImagePullSecretVolume(irs))
		volumeMounts = append(volumeMounts, makeImagePullSecretVolumeMount(irs))
	}
	volumes = append(volumes, makeBuildSecretVolumes(buildConfig.Secrets)...)
	volumeMounts = append(volumeMounts, makeBuildSecretVolumeMounts(buildConfig.Secrets)...)

	kanikoImageTag := "latest"
	if buildConfig.KanikoParams != nil && buildConfig.KanikoParams.Tag != "" {
		kanikoImageTag = buildConfig.KanikoParams.Tag
	}

	specTemplate := v1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{"Dockerfile": buildConfig.Dockerfile},
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Args:         args,
					Name:         "kaniko",
					Image:        "gcr.io/kaniko-project/executor:" + kanikoImageTag,
					VolumeMounts: volumeMounts,
				},
			},
			NodeSelector:  mod.Spec.Selector,
			RestartPolicy: v1.RestartPolicyOnFailure,
			Volumes:       volumes,
		},
	}
	specTemplateHash, err := hashstructure.Hash(specTemplate, nil)
	if err != nil {
		return nil, fmt.Errorf("could not hash job's spec template: %v", err)
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: mod.Name + "-build-",
			Namespace:    mod.Namespace,
			Labels:       m.jobHelper.JobLabels(mod, targetKernel, utils.JobTypeBuild),
			Annotations:  map[string]string{constants.JobHashAnnotation: fmt.Sprintf("%d", specTemplateHash)},
		},
		Spec: batchv1.JobSpec{
			Completions: pointer.Int32(1),
			Template:    specTemplate,
		},
	}

	if err := controllerutil.SetControllerReference(&mod, job, m.scheme); err != nil {
		return nil, fmt.Errorf("could not set the owner reference: %v", err)
	}

	return job, nil
}

func makeImagePullSecretVolume(secretRef *v1.LocalObjectReference) v1.Volume {

	if secretRef == nil {
		return v1.Volume{}
	}

	return v1.Volume{
		Name: volumeNameFromSecretRef(*secretRef),
		VolumeSource: v1.VolumeSource{
			Secret: &v1.SecretVolumeSource{
				SecretName: secretRef.Name,
				Items: []v1.KeyToPath{
					{
						Key:  v1.DockerConfigJsonKey,
						Path: "config.json",
					},
				},
			},
		},
	}
}

func makeImagePullSecretVolumeMount(secretRef *v1.LocalObjectReference) v1.VolumeMount {

	if secretRef == nil {
		return v1.VolumeMount{}
	}

	return v1.VolumeMount{
		Name:      volumeNameFromSecretRef(*secretRef),
		ReadOnly:  true,
		MountPath: "/kaniko/.docker",
	}
}

func makeBuildSecretVolumes(secretRefs []v1.LocalObjectReference) []v1.Volume {

	volumes := make([]v1.Volume, 0, len(secretRefs))

	for _, secretRef := range secretRefs {
		vol := v1.Volume{
			Name: volumeNameFromSecretRef(secretRef),
			VolumeSource: v1.VolumeSource{
				Secret: &v1.SecretVolumeSource{
					SecretName: secretRef.Name,
				},
			},
		}

		volumes = append(volumes, vol)
	}

	return volumes
}

func makeBuildSecretVolumeMounts(secretRefs []v1.LocalObjectReference) []v1.VolumeMount {

	secretVolumeMounts := make([]v1.VolumeMount, 0, len(secretRefs))

	for _, secretRef := range secretRefs {
		volMount := v1.VolumeMount{
			Name:      volumeNameFromSecretRef(secretRef),
			ReadOnly:  true,
			MountPath: "/run/secrets/" + secretRef.Name,
		}

		secretVolumeMounts = append(secretVolumeMounts, volMount)
	}

	return secretVolumeMounts
}

func volumeNameFromSecretRef(ref v1.LocalObjectReference) string {
	return "secret-" + ref.Name
}
