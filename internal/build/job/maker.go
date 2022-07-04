package job

import (
	"fmt"

	ootov1alpha1 "github.com/qbarrand/oot-operator/api/v1alpha1"
	"github.com/qbarrand/oot-operator/internal/build"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

//go:generate mockgen -source=maker.go -package=job -destination=mock_maker.go

type Maker interface {
	MakeJob(mod ootov1alpha1.Module, buildConfig *ootov1alpha1.Build, targetKernel, containerImage string) (*batchv1.Job, error)
}

type maker struct {
	helper build.Helper
	scheme *runtime.Scheme
}

func NewMaker(helper build.Helper, scheme *runtime.Scheme) Maker {
	return &maker{helper: helper, scheme: scheme}
}

func (m *maker) MakeJob(mod ootov1alpha1.Module, buildConfig *ootov1alpha1.Build, targetKernel, containerImage string) (*batchv1.Job, error) {
	args := []string{"--destination", containerImage}

	buildArgs := m.helper.ApplyBuildArgOverrides(
		buildConfig.BuildArgs,
		ootov1alpha1.BuildArg{Name: "KERNEL_VERSION", Value: targetKernel},
	)

	for _, ba := range buildArgs {
		args = append(args, "--build-arg", fmt.Sprintf("%s=%s", ba.Name, ba.Value))
	}

	if buildConfig.Pull.Insecure {
		args = append(args, "--insecure-pull")
	}

	if buildConfig.Push.Insecure {
		args = append(args, "--insecure")
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

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: mod.Name + "-build-",
			Namespace:    mod.Namespace,
			Labels:       labels(mod, targetKernel),
		},
		Spec: batchv1.JobSpec{
			Completions: pointer.Int32(1),
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{"Dockerfile": buildConfig.Dockerfile},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Args:         args,
							Name:         "kaniko",
							Image:        "gcr.io/kaniko-project/executor:latest",
							VolumeMounts: append([]v1.VolumeMount{dockerFileVolumeMount}, MakeSecretVolumeMounts(buildConfig.Secrets)...),
						},
					},
					RestartPolicy: v1.RestartPolicyOnFailure,
					Volumes:       append([]v1.Volume{dockerFileVolume}, MakeSecretVolumes(buildConfig.Secrets)...),
				},
			},
		},
	}

	if err := controllerutil.SetControllerReference(&mod, job, m.scheme); err != nil {
		return nil, fmt.Errorf("could not set the owner reference: %v", err)
	}

	return job, nil
}

func MakeSecretVolumes(secretRefs []v1.LocalObjectReference) []v1.Volume {
	volumes := make([]v1.Volume, 0, len(secretRefs))

	for _, secretRef := range secretRefs {
		vol := v1.Volume{
			Name: volumeNameFromSecretRef(secretRef),
			VolumeSource: v1.VolumeSource{
				Secret: &v1.SecretVolumeSource{SecretName: secretRef.Name},
			},
		}

		volumes = append(volumes, vol)
	}

	return volumes
}

func MakeSecretVolumeMounts(secretRefs []v1.LocalObjectReference) []v1.VolumeMount {
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
