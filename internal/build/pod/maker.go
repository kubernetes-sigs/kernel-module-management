package pod

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/mitchellh/hashstructure/v2"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/api"
	"github.com/kubernetes-sigs/kernel-module-management/internal/build"
	"github.com/kubernetes-sigs/kernel-module-management/internal/buildsign/pod"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	"github.com/kubernetes-sigs/kernel-module-management/internal/module"
)

const (
	dockerfileVolumeName = "dockerfile"
)

//go:generate mockgen -source=maker.go -package=pod -destination=mock_maker.go

type Maker interface {
	MakePodTemplate(
		ctx context.Context,
		mld *api.ModuleLoaderData,
		owner metav1.Object,
		pushImage bool) (*v1.Pod, error)
}

type maker struct {
	client              client.Client
	helper              build.Helper
	buildSignPodManager pod.BuildSignPodManager
	scheme              *runtime.Scheme
}

type hashData struct {
	Dockerfile string
	PodSpec    *v1.PodSpec
}

func NewMaker(
	client client.Client,
	helper build.Helper,
	buildSignPodManager pod.BuildSignPodManager,
	scheme *runtime.Scheme) Maker {
	return &maker{
		client:              client,
		helper:              helper,
		buildSignPodManager: buildSignPodManager,
		scheme:              scheme,
	}
}

func (m *maker) MakePodTemplate(
	ctx context.Context,
	mld *api.ModuleLoaderData,
	owner metav1.Object,
	pushImage bool) (*v1.Pod, error) {

	// if build AND sign are specified, then we will build an intermediate image
	// and let sign produce the one specified in its targetImage
	containerImage := mld.ContainerImage
	if module.ShouldBeSigned(mld) {
		containerImage = module.IntermediateImageName(mld.Name, mld.Namespace, containerImage)
	}

	podSpec := m.podSpec(mld, containerImage, pushImage)
	podSpecHash, err := m.getHashAnnotationValue(
		ctx,
		mld.Build.DockerfileConfigMap.Name,
		mld.Namespace,
		&podSpec,
	)
	if err != nil {
		return nil, fmt.Errorf("could not hash pod's definitions: %v", err)
	}

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: mld.Name + "-build-",
			Namespace:    mld.Namespace,
			Labels:       m.buildSignPodManager.PodLabels(mld.Name, mld.KernelNormalizedVersion, PodTypeBuild),
			Annotations:  map[string]string{constants.PodHashAnnotation: fmt.Sprintf("%d", podSpecHash)},
			Finalizers:   []string{constants.GCDelayFinalizer, constants.JobEventFinalizer},
		},
		Spec: podSpec,
	}

	if err := controllerutil.SetControllerReference(owner, pod, m.scheme); err != nil {
		return nil, fmt.Errorf("could not set the owner reference: %v", err)
	}

	return pod, nil
}

func (m *maker) podSpec(mld *api.ModuleLoaderData, containerImage string, pushImage bool) v1.PodSpec {

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
				Args:         m.containerArgs(buildConfig, mld, containerImage, pushImage),
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

func (m *maker) containerArgs(
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
	buildArgs := m.helper.ApplyBuildArgOverrides(
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

func (m *maker) getHashAnnotationValue(ctx context.Context, configMapName, namespace string, podSpec *v1.PodSpec) (uint64, error) {
	dockerfileCM := &corev1.ConfigMap{}
	namespacedName := types.NamespacedName{Name: configMapName, Namespace: namespace}
	if err := m.client.Get(ctx, namespacedName, dockerfileCM); err != nil {
		return 0, fmt.Errorf("failed to get dockerfile ConfigMap %s: %v", namespacedName, err)
	}
	data, ok := dockerfileCM.Data[constants.DockerfileCMKey]
	if !ok {
		return 0, fmt.Errorf("invalid Dockerfile ConfigMap %s format, %s key is missing", namespacedName, constants.DockerfileCMKey)
	}

	return getHashValue(podSpec, data)
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

func getHashValue(podSpec *v1.PodSpec, dockerfile string) (uint64, error) {
	dataToHash := hashData{
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
