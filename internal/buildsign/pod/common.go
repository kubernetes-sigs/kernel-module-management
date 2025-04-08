package pod

import (
	"context"
	"embed"
	"fmt"
	"os"
	"strings"
	"text/template"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/api"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	"github.com/mitchellh/hashstructure/v2"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

const dockerfileVolumeName = "dockerfile"

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

func (pm *buildSignPodManager) podSpec(mld *api.ModuleLoaderData, containerImage string, pushImage bool) v1.PodSpec {

	buildConfig := mld.Build

	args := pm.containerArgs(mld, containerImage, mld.Build.BaseImageRegistryTLS, pushImage)
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
				Args:         args,
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

func (pm *buildSignPodManager) containerArgs(mld *api.ModuleLoaderData, destinationImg string,
	tlsOptions kmmv1beta1.TLSOptions, pushImage bool) []string {

	args := []string{}

	if pushImage {
		args = append(args, "--destination", destinationImg)
		if mld.RegistryTLS.Insecure {
			args = append(args, "--insecure")
		}
		if mld.RegistryTLS.InsecureSkipTLSVerify {
			args = append(args, "--skip-tls-verify")
		}
	} else {
		args = append(args, "--no-push")
	}

	if tlsOptions.Insecure {
		args = append(args, "--insecure-pull")
	}

	if tlsOptions.InsecureSkipTLSVerify {
		args = append(args, "--skip-tls-verify-pull")
	}

	return args

}

func (pm *buildSignPodManager) getBuildHashAnnotationValue(ctx context.Context, configMapName, namespace string,
	podSpec *v1.PodSpec) (uint64, error) {

	dockerfileCM := &corev1.ConfigMap{}
	namespacedName := types.NamespacedName{Name: configMapName, Namespace: namespace}
	if err := pm.client.Get(ctx, namespacedName, dockerfileCM); err != nil {
		return 0, fmt.Errorf("failed to get dockerfile ConfigMap %s: %v", namespacedName, err)
	}
	dockerfile, ok := dockerfileCM.Data[constants.DockerfileCMKey]
	if !ok {
		return 0, fmt.Errorf("invalid Dockerfile ConfigMap %s format, %s key is missing", namespacedName, constants.DockerfileCMKey)
	}

	dataToHash := struct {
		PodSpec    *v1.PodSpec
		Dockerfile string
	}{
		PodSpec:    podSpec,
		Dockerfile: dockerfile,
	}
	hashValue, err := hashstructure.Hash(dataToHash, hashstructure.FormatV2, nil)
	if err != nil {
		return 0, fmt.Errorf("could not hash pod's spec template and dockefile: %v", err)
	}

	return hashValue, nil
}

func (pm *buildSignPodManager) getSignHashAnnotationValue(ctx context.Context, privateSecret, publicSecret, namespace string,
	signConfig []byte, podSpec *v1.PodSpec) (uint64, error) {

	privateKeyData, err := pm.getSecretData(ctx, privateSecret, constants.PrivateSignDataKey, namespace)
	if err != nil {
		return 0, fmt.Errorf("failed to get private secret %s for signing: %v", privateSecret, err)
	}
	publicKeyData, err := pm.getSecretData(ctx, publicSecret, constants.PublicSignDataKey, namespace)
	if err != nil {
		return 0, fmt.Errorf("failed to get public secret %s for signing: %v", publicSecret, err)
	}

	dataToHash := struct {
		PodSpec        *v1.PodSpec
		PrivateKeyData []byte
		PublicKeyData  []byte
		SignConfig     []byte
	}{
		PodSpec:        podSpec,
		PrivateKeyData: privateKeyData,
		PublicKeyData:  publicKeyData,
		SignConfig:     signConfig,
	}
	hashValue, err := hashstructure.Hash(dataToHash, hashstructure.FormatV2, nil)
	if err != nil {
		return 0, fmt.Errorf("could not hash pod's spec template and signing keys: %v", err)
	}

	return hashValue, nil
}

func (pm *buildSignPodManager) getSecretData(ctx context.Context, secretName, secretDataKey, namespace string) ([]byte, error) {
	secret := v1.Secret{}
	namespacedName := types.NamespacedName{Name: secretName, Namespace: namespace}
	err := pm.client.Get(ctx, namespacedName, &secret)
	if err != nil {
		return nil, fmt.Errorf("failed to get Secret %s: %v", namespacedName, err)
	}
	data, ok := secret.Data[secretDataKey]
	if !ok {
		return nil, fmt.Errorf("invalid Secret %s format, %s key is missing", namespacedName, secretDataKey)
	}
	return data, nil
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
