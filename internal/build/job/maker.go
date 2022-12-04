package job

import (
	"context"
	"fmt"

	"github.com/mitchellh/hashstructure"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/build"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	"github.com/kubernetes-sigs/kernel-module-management/internal/module"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
)

const (
	dockerfileVolumeName = "dockerfile"
)

//go:generate mockgen -source=maker.go -package=job -destination=mock_maker.go

type Maker interface {
	MakeJobTemplate(
		ctx context.Context,
		mod kmmv1beta1.Module,
		km kmmv1beta1.KernelMapping,
		targetKernel string,
		owner metav1.Object,
		pushImage bool) (*batchv1.Job, error)
}

type maker struct {
	client    client.Client
	helper    build.Helper
	jobHelper utils.JobHelper
	scheme    *runtime.Scheme
}

type hashData struct {
	Dockerfile  string
	PodTemplate *v1.PodTemplateSpec
}

func NewMaker(
	client client.Client,
	helper build.Helper,
	jobHelper utils.JobHelper,
	scheme *runtime.Scheme) Maker {
	return &maker{
		client:    client,
		helper:    helper,
		jobHelper: jobHelper,
		scheme:    scheme,
	}
}

func (m *maker) MakeJobTemplate(
	ctx context.Context,
	mod kmmv1beta1.Module,
	km kmmv1beta1.KernelMapping,
	targetKernel string,
	owner metav1.Object,
	pushImage bool) (*batchv1.Job, error) {

	buildConfig := m.helper.GetRelevantBuild(mod.Spec, km)

	containerImage := km.ContainerImage

	// if build AND sign are specified, then we will build an intermediate image
	// and let sign produce the one specified in its targetImage
	if module.ShouldBeSigned(mod.Spec, km) {
		containerImage = module.IntermediateImageName(mod.Name, mod.Namespace, containerImage)
	}

	registryTLS := module.TLSOptions(mod.Spec, km)
	specTemplate := m.specTemplate(
		mod.Spec,
		buildConfig,
		targetKernel,
		containerImage,
		registryTLS,
		pushImage)

	specTemplateHash, err := m.getHashAnnotationValue(ctx, buildConfig.DockerfileConfigMap.Name, mod.Namespace, &specTemplate)
	if err != nil {
		return nil, fmt.Errorf("could not hash job's definitions: %v", err)
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: mod.Name + "-build-",
			Namespace:    mod.Namespace,
			Labels:       m.jobHelper.JobLabels(mod.Name, targetKernel, utils.JobTypeBuild),
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

func (m *maker) specTemplate(
	modSpec kmmv1beta1.ModuleSpec,
	buildConfig *kmmv1beta1.Build,
	targetKernel string,
	containerImage string,
	registryTLS *kmmv1beta1.TLSOptions,
	pushImage bool) v1.PodTemplateSpec {

	kanikoImageTag := "latest"
	if buildConfig.KanikoParams != nil && buildConfig.KanikoParams.Tag != "" {
		kanikoImageTag = buildConfig.KanikoParams.Tag
	}

	return v1.PodTemplateSpec{
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Args:         m.containerArgs(buildConfig, targetKernel, containerImage, registryTLS, pushImage),
					Name:         "kaniko",
					Image:        "gcr.io/kaniko-project/executor:" + kanikoImageTag,
					VolumeMounts: volumeMounts(modSpec, buildConfig),
				},
			},
			NodeSelector:  modSpec.Selector,
			RestartPolicy: v1.RestartPolicyOnFailure,
			Volumes:       volumes(modSpec, buildConfig),
		},
	}
}

func (m *maker) containerArgs(
	buildConfig *kmmv1beta1.Build,
	targetKernel string,
	containerImage string,
	registryTLS *kmmv1beta1.TLSOptions,
	pushImage bool) []string {

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

	if buildConfig.BaseImageRegistryTLS.Insecure {
		args = append(args, "--insecure-pull")
	}

	if buildConfig.BaseImageRegistryTLS.InsecureSkipTLSVerify {
		args = append(args, "--skip-tls-verify-pull")
	}

	if pushImage {
		if registryTLS.Insecure {
			args = append(args, "--insecure")
		}

		if registryTLS.InsecureSkipTLSVerify {
			args = append(args, "--skip-tls-verify")
		}
	}

	return args
}

func (m *maker) getHashAnnotationValue(ctx context.Context, configMapName, namespace string, podTemplate *v1.PodTemplateSpec) (uint64, error) {
	dockerfileCM := &corev1.ConfigMap{}
	namespacedName := types.NamespacedName{Name: configMapName, Namespace: namespace}
	if err := m.client.Get(ctx, namespacedName, dockerfileCM); err != nil {
		return 0, fmt.Errorf("failed to get dockerfile ConfigMap %s: %v", namespacedName, err)
	}
	data, ok := dockerfileCM.Data[constants.DockerfileCMKey]
	if !ok {
		return 0, fmt.Errorf("invalid Dockerfile ConfigMap %s format, %s key is missing", namespacedName, constants.DockerfileCMKey)
	}

	return getHashValue(podTemplate, data)
}

func volumes(modSpec kmmv1beta1.ModuleSpec, buildConfig *kmmv1beta1.Build) []v1.Volume {
	volumes := []v1.Volume{dockerfileVolume(dockerfileVolumeName, buildConfig.DockerfileConfigMap)}
	if irs := modSpec.ImageRepoSecret; irs != nil {
		volumes = append(volumes, makeImagePullSecretVolume(irs))
	}
	volumes = append(volumes, makeBuildSecretVolumes(buildConfig.Secrets)...)
	return volumes
}

func volumeMounts(modSpec kmmv1beta1.ModuleSpec, buildConfig *kmmv1beta1.Build) []v1.VolumeMount {
	volumeMounts := []v1.VolumeMount{dockerfileVolumeMount(dockerfileVolumeName)}
	if irs := modSpec.ImageRepoSecret; irs != nil {
		volumeMounts = append(volumeMounts, makeImagePullSecretVolumeMount(irs))
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

func getHashValue(podTemplate *v1.PodTemplateSpec, dockerfile string) (uint64, error) {
	dataToHash := hashData{
		Dockerfile:  dockerfile,
		PodTemplate: podTemplate,
	}
	hashValue, err := hashstructure.Hash(dataToHash, nil)
	if err != nil {
		return 0, fmt.Errorf("could not hash job's spec template and dockefile: %v", err)
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
