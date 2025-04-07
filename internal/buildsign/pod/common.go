package pod

import (
	"context"
	"fmt"
	"os"
	"strings"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/api"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	"github.com/mitchellh/hashstructure/v2"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

const dockerfileVolumeName = "dockerfile"

func (pm *buildSignPodManager) podSpec(mld *api.ModuleLoaderData, containerImage string, pushImage bool) v1.PodSpec {

	buildConfig := mld.Build
	kanikoImage := os.Getenv("RELATED_IMAGE_BUILD")

	if buildConfig.KanikoParams != nil && buildConfig.KanikoParams.Tag != "" {
		if idx := strings.IndexAny(kanikoImage, "@:"); idx != -1 {
			kanikoImage = kanikoImage[0:idx]
		}

		kanikoImage += ":" + buildConfig.KanikoParams.Tag
	}

	selector := mld.Selector
	if len(mld.Build.Selector) != 0 {
		selector = mld.Build.Selector
	}

	return v1.PodSpec{
		Containers: []v1.Container{
			{
				Args:         pm.containerArgs(buildConfig, mld, containerImage, pushImage),
				Name:         "kaniko",
				Image:        kanikoImage,
				VolumeMounts: volumeMounts(mld.ImageRepoSecret, buildConfig),
			},
		},
		RestartPolicy: v1.RestartPolicyNever,
		Volumes:       volumes(mld.ImageRepoSecret, buildConfig),
		NodeSelector:  selector,
		Tolerations:   mld.Tolerations,
	}
}

func (pm *buildSignPodManager) containerArgs(
	buildConfig *kmmv1beta1.Build,
	mld *api.ModuleLoaderData,
	containerImage string,
	pushImage bool) []string {

	args := []string{}
	if pushImage {
		args = append(args, "--destination", containerImage)
	} else {
		args = append(args, "--no-push")
	}

	overrides := []kmmv1beta1.BuildArg{
		{Name: "KERNEL_VERSION", Value: mld.KernelVersion},
		{Name: "KERNEL_FULL_VERSION", Value: mld.KernelVersion},
		{Name: "MOD_NAME", Value: mld.Name},
		{Name: "MOD_NAMESPACE", Value: mld.Namespace},
	}
	buildArgs := pm.combiner.ApplyBuildArgOverrides(
		buildConfig.BuildArgs,
		overrides...,
	)

	for _, ba := range buildArgs {
		args = append(args, "--build-arg", fmt.Sprintf("%s=%s", ba.Name, ba.Value))
	}

	if buildConfig.BaseImageRegistryTLS.Insecure {
		args = append(args, "--insecure-pull")
	}

	if buildConfig.BaseImageRegistryTLS.InsecureSkipTLSVerify {
		args = append(args, "--skip-tls-verify-pull")
	}

	if pushImage {
		if mld.RegistryTLS.Insecure {
			args = append(args, "--insecure")
		}

		if mld.RegistryTLS.InsecureSkipTLSVerify {
			args = append(args, "--skip-tls-verify")
		}
	}

	return args
}

func (pm *buildSignPodManager) getHashAnnotationValue(ctx context.Context, configMapName, namespace string,
	podSpec *v1.PodSpec) (uint64, error) {

	dockerfileCM := &corev1.ConfigMap{}
	namespacedName := types.NamespacedName{Name: configMapName, Namespace: namespace}
	if err := pm.client.Get(ctx, namespacedName, dockerfileCM); err != nil {
		return 0, fmt.Errorf("failed to get dockerfile ConfigMap %s: %v", namespacedName, err)
	}
	data, ok := dockerfileCM.Data[constants.DockerfileCMKey]
	if !ok {
		return 0, fmt.Errorf("invalid Dockerfile ConfigMap %s format, %s key is missing", namespacedName, constants.DockerfileCMKey)
	}

	return getBuildHashValue(podSpec, data)
}

func volumes(imageRepoSecret *v1.LocalObjectReference, buildConfig *kmmv1beta1.Build) []v1.Volume {
	volumes := []v1.Volume{dockerfileVolume(dockerfileVolumeName, buildConfig.DockerfileConfigMap)}
	if imageRepoSecret != nil {
		volumes = append(volumes, makeImagePullSecretVolume(imageRepoSecret))
	}
	volumes = append(volumes, makeBuildSecretVolumes(buildConfig.Secrets)...)
	return volumes
}

func volumeMounts(imageRepoSecret *v1.LocalObjectReference, buildConfig *kmmv1beta1.Build) []v1.VolumeMount {
	volumeMounts := []v1.VolumeMount{dockerfileVolumeMount(dockerfileVolumeName)}
	if imageRepoSecret != nil {
		volumeMounts = append(volumeMounts, makeImagePullSecretVolumeMount(imageRepoSecret))
	}
	volumeMounts = append(volumeMounts, makeBuildSecretVolumeMounts(buildConfig.Secrets)...)
	return volumeMounts
}

func dockerfileVolume(name string, dockerfileConfigMap *v1.LocalObjectReference) v1.Volume {
	return v1.Volume{
		Name: name,
		VolumeSource: v1.VolumeSource{
			ConfigMap: &v1.ConfigMapVolumeSource{
				LocalObjectReference: *dockerfileConfigMap,
				Items: []v1.KeyToPath{
					{
						Key:  constants.DockerfileCMKey,
						Path: "Dockerfile",
					},
				},
			},
		},
	}
}

func dockerfileVolumeMount(name string) v1.VolumeMount {
	return v1.VolumeMount{
		Name:      name,
		ReadOnly:  true,
		MountPath: "/workspace",
	}
}

func getBuildHashValue(podSpec *v1.PodSpec, dockerfile string) (uint64, error) {
	dataToHash := struct {
		Dockerfile string
		PodSpec    *v1.PodSpec
	}{
		Dockerfile: dockerfile,
		PodSpec:    podSpec,
	}
	hashValue, err := hashstructure.Hash(dataToHash, hashstructure.FormatV2, nil)
	if err != nil {
		return 0, fmt.Errorf("could not hash pod's spec template and dockefile: %v", err)
	}
	return hashValue, nil
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
