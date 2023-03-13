package build

import (
	"context"
	"fmt"

	"github.com/kubernetes-sigs/kernel-module-management/internal/build/utils"
	"github.com/kubernetes-sigs/kernel-module-management/internal/imgbuild"
	"github.com/mitchellh/hashstructure/v2"
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
	"github.com/kubernetes-sigs/kernel-module-management/internal/api"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils/image"
)

const (
	dockerfileVolumeName = "dockerfile"
	kanikoContainerName  = "kaniko"
)

type buildMaker struct {
	builderImage string
	client       client.Client
	jobHelper    imgbuild.JobHelper
	scheme       *runtime.Scheme
}

var _ imgbuild.JobMaker = &buildMaker{}

func NewJobMaker(client client.Client, builderImage string, jobHelper imgbuild.JobHelper, scheme *runtime.Scheme) imgbuild.JobMaker {
	return &buildMaker{
		builderImage: builderImage,
		client:       client,
		jobHelper:    jobHelper,
		scheme:       scheme,
	}
}

func (m *buildMaker) MakeJob(
	ctx context.Context,
	mld *api.ModuleLoaderData,
	owner metav1.Object,
	pushImage bool) (*batchv1.Job, error) {
	// if build AND sign are specified, then we will build an intermediate image
	// and let sign produce the one specified in its targetImage
	containerImage, err := mld.BuildDestinationImage()
	if err != nil {
		return nil, fmt.Errorf("could not generate the destination image for the build: %v", err)
	}

	specTemplate, err := m.specTemplate(mld, containerImage, pushImage)
	if err != nil {
		return nil, fmt.Errorf("could not generate then spec template: %v", err)
	}

	specTemplateHash, err := m.getHashAnnotationValue(
		ctx,
		mld.Build.DockerfileConfigMap.Name,
		mld.Namespace,
		&specTemplate,
	)
	if err != nil {
		return nil, fmt.Errorf("could not hash job's definitions: %v", err)
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: mld.Name + "-build-",
			Namespace:    mld.Namespace,
			Labels:       m.jobHelper.JobLabels(mld.Name, mld.KernelVersion, imgbuild.JobTypeBuild),
			Annotations:  map[string]string{imgbuild.JobHashAnnotation: fmt.Sprintf("%d", specTemplateHash)},
		},
		Spec: batchv1.JobSpec{
			Completions:  pointer.Int32(1),
			BackoffLimit: pointer.Int32(0),
			Template:     specTemplate,
		},
	}

	if err = controllerutil.SetControllerReference(owner, job, m.scheme); err != nil {
		return nil, fmt.Errorf("could not set the owner reference: %v", err)
	}

	return job, nil
}

func (m *buildMaker) specTemplate(mld *api.ModuleLoaderData, containerImage string, pushImage bool) (v1.PodTemplateSpec, error) {

	buildConfig := mld.Build
	kanikoImage := m.builderImage

	if buildConfig.KanikoParams != nil && buildConfig.KanikoParams.Tag != "" {
		var err error

		kanikoImage, err = image.SetTag(kanikoImage, buildConfig.KanikoParams.Tag)
		if err != nil {
			return v1.PodTemplateSpec{}, fmt.Errorf("could not generate the kaniko image name: %v", err)
		}
	}

	podSpec := v1.PodTemplateSpec{
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Args:         m.containerArgs(buildConfig, mld, containerImage, pushImage),
					Name:         kanikoContainerName,
					Image:        kanikoImage,
					VolumeMounts: volumeMounts(mld.ImageRepoSecret, buildConfig),
				},
			},
			NodeSelector:  mld.Selector,
			RestartPolicy: v1.RestartPolicyNever,
			Volumes:       volumes(mld.ImageRepoSecret, buildConfig),
		},
	}

	return podSpec, nil
}

func (m *buildMaker) containerArgs(
	buildConfig *kmmv1beta1.Build,
	mld *api.ModuleLoaderData,
	containerImage string,
	pushImage bool) []string {

	args := []string{}
	if pushImage {
		args = append(args, "--destination", containerImage)

		if mld.RegistryTLS.Insecure {
			args = append(args, "--insecure")
		}

		if mld.RegistryTLS.InsecureSkipTLSVerify {
			args = append(args, "--skip-tls-verify")
		}
	} else {
		args = append(args, "--no-push")
	}

	overrides := []kmmv1beta1.BuildArg{
		{Name: "KERNEL_VERSION", Value: mld.KernelVersion},
		{Name: "MOD_NAME", Value: mld.Name},
		{Name: "MOD_NAMESPACE", Value: mld.Namespace},
	}
	buildArgs := utils.ApplyBuildArgOverrides(
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

	return args
}

func (m *buildMaker) getHashAnnotationValue(ctx context.Context, configMapName, namespace string, podTemplate *v1.PodTemplateSpec) (uint64, error) {
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

type buildHashData struct {
	Dockerfile  string
	PodTemplate *v1.PodTemplateSpec
}

func getHashValue(podTemplate *v1.PodTemplateSpec, dockerfile string) (uint64, error) {
	dataToHash := buildHashData{
		Dockerfile:  dockerfile,
		PodTemplate: podTemplate,
	}
	hashValue, err := hashstructure.Hash(dataToHash, hashstructure.FormatV2, nil)
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
