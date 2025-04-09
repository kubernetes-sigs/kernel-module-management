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
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
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

func (bspm *buildSignPodManager) podSpec(mld *api.ModuleLoaderData, containerImage string, pushImage bool) v1.PodSpec {

	buildConfig := mld.Build

	args := bspm.containerArgs(mld, containerImage, mld.Build.BaseImageRegistryTLS, pushImage)
	overrides := []kmmv1beta1.BuildArg{
		{Name: "KERNEL_VERSION", Value: mld.KernelVersion},
		{Name: "KERNEL_FULL_VERSION", Value: mld.KernelVersion},
		{Name: "MOD_NAME", Value: mld.Name},
		{Name: "MOD_NAMESPACE", Value: mld.Namespace},
	}
	buildArgs := bspm.combiner.ApplyBuildArgOverrides(
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

func (bspm *buildSignPodManager) containerArgs(mld *api.ModuleLoaderData, destinationImg string,
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

func (bspm *buildSignPodManager) getBuildHashAnnotationValue(ctx context.Context, configMapName, namespace string,
	podSpec *v1.PodSpec) (uint64, error) {

	dockerfileCM := &v1.ConfigMap{}
	namespacedName := types.NamespacedName{Name: configMapName, Namespace: namespace}
	if err := bspm.client.Get(ctx, namespacedName, dockerfileCM); err != nil {
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

func (bspm *buildSignPodManager) getSignHashAnnotationValue(ctx context.Context, privateSecret, publicSecret, namespace string,
	signConfig []byte, podSpec *v1.PodSpec) (uint64, error) {

	privateKeyData, err := bspm.getSecretData(ctx, privateSecret, constants.PrivateSignDataKey, namespace)
	if err != nil {
		return 0, fmt.Errorf("failed to get private secret %s for signing: %v", privateSecret, err)
	}
	publicKeyData, err := bspm.getSecretData(ctx, publicSecret, constants.PublicSignDataKey, namespace)
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

func (bspm *buildSignPodManager) getSecretData(ctx context.Context, secretName, secretDataKey, namespace string) ([]byte, error) {
	secret := v1.Secret{}
	namespacedName := types.NamespacedName{Name: secretName, Namespace: namespace}
	err := bspm.client.Get(ctx, namespacedName, &secret)
	if err != nil {
		return nil, fmt.Errorf("failed to get Secret %s: %v", namespacedName, err)
	}
	data, ok := secret.Data[secretDataKey]
	if !ok {
		return nil, fmt.Errorf("invalid Secret %s format, %s key is missing", namespacedName, secretDataKey)
	}
	return data, nil
}
