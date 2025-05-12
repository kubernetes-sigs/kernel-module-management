package resource

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"os"
	"strings"
	"text/template"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/api"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	"github.com/kubernetes-sigs/kernel-module-management/internal/module"
	"github.com/mitchellh/hashstructure/v2"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

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

func (rm *resourceManager) buildSpec(mld *api.ModuleLoaderData, destinationImg string, pushImage bool) v1.PodSpec {

	buildConfig := mld.Build

	args := containerArgs(mld, destinationImg, mld.Build.BaseImageRegistryTLS, pushImage)
	overrides := []kmmv1beta1.BuildArg{
		{Name: "KERNEL_VERSION", Value: mld.KernelVersion},
		{Name: "KERNEL_FULL_VERSION", Value: mld.KernelVersion},
		{Name: "MOD_NAME", Value: mld.Name},
		{Name: "MOD_NAMESPACE", Value: mld.Namespace},
	}
	buildArgs := rm.combiner.ApplyBuildArgOverrides(
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

	volumes, volumeMounts := makeBuildResourceVolumesAndVolumeMounts(*buildConfig, mld.ImageRepoSecret)

	return v1.PodSpec{
		Containers: []v1.Container{
			{
				Args:         args,
				Name:         "kaniko",
				Image:        kanikoImage,
				VolumeMounts: volumeMounts,
			},
		},
		RestartPolicy: v1.RestartPolicyNever,
		Volumes:       volumes,
		NodeSelector:  selector,
		Tolerations:   mld.Tolerations,
	}
}

func signSpec(mld *api.ModuleLoaderData, destinationImg string, pushImage bool) v1.PodSpec {

	signConfig := mld.Sign
	args := containerArgs(mld, destinationImg, signConfig.UnsignedImageRegistryTLS, pushImage)
	volumes, volumeMounts := makeSignResourceVolumesAndVolumeMounts(signConfig, mld.ImageRepoSecret)

	return v1.PodSpec{
		Containers: []v1.Container{
			{
				Args:         args,
				Name:         "kaniko",
				Image:        os.Getenv("RELATED_IMAGE_BUILD"),
				VolumeMounts: volumeMounts,
			},
		},
		RestartPolicy: v1.RestartPolicyNever,
		Volumes:       volumes,
		NodeSelector:  mld.Selector,
		Tolerations:   mld.Tolerations,
	}
}

func containerArgs(mld *api.ModuleLoaderData, destinationImg string,
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

func (rm *resourceManager) getBuildHashAnnotationValue(ctx context.Context, configMapName, namespace string,
	buildSpec *v1.PodSpec) (uint64, error) {

	dockerfileCM := &v1.ConfigMap{}
	namespacedName := types.NamespacedName{Name: configMapName, Namespace: namespace}
	if err := rm.client.Get(ctx, namespacedName, dockerfileCM); err != nil {
		return 0, fmt.Errorf("failed to get dockerfile ConfigMap %s: %v", namespacedName, err)
	}
	dockerfile, ok := dockerfileCM.Data[constants.DockerfileCMKey]
	if !ok {
		return 0, fmt.Errorf("invalid Dockerfile ConfigMap %s format, %s key is missing", namespacedName, constants.DockerfileCMKey)
	}

	dataToHash := struct {
		BuildSpec  *v1.PodSpec
		Dockerfile string
	}{
		BuildSpec:  buildSpec,
		Dockerfile: dockerfile,
	}
	hashValue, err := hashstructure.Hash(dataToHash, hashstructure.FormatV2, nil)
	if err != nil {
		return 0, fmt.Errorf("could not hash build's spec template and dockefile: %v", err)
	}

	return hashValue, nil
}

func (rm *resourceManager) getSignHashAnnotationValue(ctx context.Context, privateSecret, publicSecret, namespace string,
	signConfig []byte, signSpec *v1.PodSpec) (uint64, error) {

	privateKeyData, err := rm.getSecretData(ctx, privateSecret, constants.PrivateSignDataKey, namespace)
	if err != nil {
		return 0, fmt.Errorf("failed to get private secret %s for signing: %v", privateSecret, err)
	}
	publicKeyData, err := rm.getSecretData(ctx, publicSecret, constants.PublicSignDataKey, namespace)
	if err != nil {
		return 0, fmt.Errorf("failed to get public secret %s for signing: %v", publicSecret, err)
	}

	dataToHash := struct {
		SignSpec       *v1.PodSpec
		PrivateKeyData []byte
		PublicKeyData  []byte
		SignConfig     []byte
	}{
		SignSpec:       signSpec,
		PrivateKeyData: privateKeyData,
		PublicKeyData:  publicKeyData,
		SignConfig:     signConfig,
	}
	hashValue, err := hashstructure.Hash(dataToHash, hashstructure.FormatV2, nil)
	if err != nil {
		return 0, fmt.Errorf("could not hash sign's spec template and signing keys: %v", err)
	}

	return hashValue, nil
}

func (rm *resourceManager) getSecretData(ctx context.Context, secretName, secretDataKey, namespace string) ([]byte, error) {
	secret := v1.Secret{}
	namespacedName := types.NamespacedName{Name: secretName, Namespace: namespace}
	err := rm.client.Get(ctx, namespacedName, &secret)
	if err != nil {
		return nil, fmt.Errorf("failed to get Secret %s: %v", namespacedName, err)
	}
	data, ok := secret.Data[secretDataKey]
	if !ok {
		return nil, fmt.Errorf("invalid Secret %s format, %s key is missing", namespacedName, secretDataKey)
	}
	return data, nil
}

func resourceLabels(modName, targetKernel string, resourceType kmmv1beta1.BuildOrSignAction) map[string]string {

	labels := moduleKernelLabels(modName, targetKernel, resourceType)

	labels["app.kubernetes.io/name"] = "kmm"
	labels["app.kubernetes.io/component"] = string(resourceType)
	labels["app.kubernetes.io/part-of"] = "kmm"

	return labels
}

func filterResourcesByOwner(resources []v1.Pod, owner metav1.Object) []v1.Pod {
	ownedResources := []v1.Pod{}
	for _, obj := range resources {
		if metav1.IsControlledBy(&obj, owner) {
			ownedResources = append(ownedResources, obj)
		}
	}
	return ownedResources
}

func moduleKernelLabels(moduleName, targetKernel string, resourceType kmmv1beta1.BuildOrSignAction) map[string]string {
	labels := moduleLabels(moduleName, resourceType)
	labels[constants.TargetKernelTarget] = targetKernel
	return labels
}

func moduleLabels(moduleName string, resourceType kmmv1beta1.BuildOrSignAction) map[string]string {
	return map[string]string{
		constants.ModuleNameLabel: moduleName,
		constants.ResourceType:    string(resourceType),
	}
}

func (rm *resourceManager) getResources(ctx context.Context, namespace string, labels map[string]string) ([]v1.Pod, error) {
	resourceList := v1.PodList{}
	opts := []client.ListOption{
		client.MatchingLabels(labels),
		client.InNamespace(namespace),
	}
	if err := rm.client.List(ctx, &resourceList, opts...); err != nil {
		return nil, fmt.Errorf("could not list resources: %v", err)
	}

	return resourceList.Items, nil
}

func (rm *resourceManager) makeBuildTemplate(ctx context.Context, mld *api.ModuleLoaderData, owner metav1.Object,
	pushImage bool) (metav1.Object, error) {

	// if build AND sign are specified, then we will build an intermediate image
	// and let sign produce the one specified in its targetImage
	containerImage := mld.ContainerImage
	if module.ShouldBeSigned(mld) {
		containerImage = module.IntermediateImageName(mld.Name, mld.Namespace, containerImage)
	}

	buildSpec := rm.buildSpec(mld, containerImage, pushImage)
	buildSpecHash, err := rm.getBuildHashAnnotationValue(
		ctx,
		mld.Build.DockerfileConfigMap.Name,
		mld.Namespace,
		&buildSpec,
	)
	if err != nil {
		return nil, fmt.Errorf("could not hash resource's definitions: %v", err)
	}

	build := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: mld.Name + "-build-",
			Namespace:    mld.Namespace,
			Labels:       resourceLabels(mld.Name, mld.KernelNormalizedVersion, kmmv1beta1.BuildImage),
			Annotations:  map[string]string{constants.ResourceHashAnnotation: fmt.Sprintf("%d", buildSpecHash)},
			Finalizers:   []string{constants.GCDelayFinalizer, constants.JobEventFinalizer},
		},
		Spec: buildSpec,
	}

	if err := controllerutil.SetControllerReference(owner, build, rm.scheme); err != nil {
		return nil, fmt.Errorf("could not set the owner reference: %v", err)
	}

	return build, nil
}

func (rm *resourceManager) makeSignTemplate(ctx context.Context, mld *api.ModuleLoaderData, owner metav1.Object,
	pushImage bool) (metav1.Object, error) {

	signConfig := mld.Sign

	var buf bytes.Buffer

	td := TemplateData{
		FilesToSign: mld.Sign.FilesToSign,
		SignImage:   os.Getenv("RELATED_IMAGE_SIGN"),
	}

	imageToSign := ""
	if module.ShouldBeBuilt(mld) {
		imageToSign = module.IntermediateImageName(mld.Name, mld.Namespace, mld.ContainerImage)
	}

	if imageToSign != "" {
		td.UnsignedImage = imageToSign
	} else if signConfig.UnsignedImage != "" {
		td.UnsignedImage = signConfig.UnsignedImage
	} else {
		return nil, fmt.Errorf("no image to sign given")
	}

	if err := tmpl.Execute(&buf, td); err != nil {
		return nil, fmt.Errorf("could not execute template: %v", err)
	}

	signSpec := signSpec(mld, mld.ContainerImage, pushImage)
	signSpecHash, err := rm.getSignHashAnnotationValue(ctx, signConfig.KeySecret.Name,
		signConfig.CertSecret.Name, mld.Namespace, buf.Bytes(), &signSpec)
	if err != nil {
		return nil, fmt.Errorf("could not hash resource's definitions: %v", err)
	}

	sign := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: mld.Name + "-sign-",
			Namespace:    mld.Namespace,
			Labels:       resourceLabels(mld.Name, mld.KernelNormalizedVersion, kmmv1beta1.SignImage),
			Annotations: map[string]string{
				constants.ResourceHashAnnotation: fmt.Sprintf("%d", signSpecHash),
				dockerfileAnnotationKey:          buf.String(),
			},
			Finalizers: []string{constants.GCDelayFinalizer, constants.JobEventFinalizer},
		},
		Spec: signSpec,
	}

	if err = controllerutil.SetControllerReference(owner, sign, rm.scheme); err != nil {
		return nil, fmt.Errorf("could not set the owner reference: %v", err)
	}

	return sign, nil
}
