package module

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/auth"
	"github.com/kubernetes-sigs/kernel-module-management/internal/registry"
)

func TLSOptions(modSpec kmmv1beta1.ModuleSpec, km kmmv1beta1.KernelMapping) *kmmv1beta1.TLSOptions {
	if km.RegistryTLS != nil {
		return km.RegistryTLS
	}
	return &modSpec.ModuleLoader.Container.RegistryTLS
}

// AppendToTag adds the specified tag to the image name cleanly, i.e. by avoiding messing up
// the name or getting "name:-tag"
func AppendToTag(name string, tag string) string {
	separator := ":"
	if strings.Contains(name, ":") {
		separator = "_"
	}
	return name + separator + tag
}

// IntermediateImageName returns the image name of the pre-signed module image name
func IntermediateImageName(name, namespace, targetImage string) string {
	return AppendToTag(targetImage, namespace+"_"+name+"_kmm_unsigned")
}

// ShouldBeBuilt indicates whether the specified KernelMapping of the
// Module should be built or not.
func ShouldBeBuilt(modSpec kmmv1beta1.ModuleSpec, km kmmv1beta1.KernelMapping) bool {
	return modSpec.ModuleLoader.Container.Build != nil || km.Build != nil
}

// ShouldBeSigned indicates whether the specified KernelMapping of the
// Module should be signed or not.
func ShouldBeSigned(modSpec kmmv1beta1.ModuleSpec, km kmmv1beta1.KernelMapping) bool {
	return modSpec.ModuleLoader.Container.Sign != nil || km.Sign != nil
}

func ImageExists(
	ctx context.Context,
	client client.Client,
	reg registry.Registry,
	modSpec kmmv1beta1.ModuleSpec,
	namespace string,
	km kmmv1beta1.KernelMapping,
	imageName string) (bool, error) {

	var registryAuthGetter auth.RegistryAuthGetter
	if modSpec.ImageRepoSecret != nil {
		registryAuthGetter = auth.NewRegistryAuthGetter(client, types.NamespacedName{
			Name:      modSpec.ImageRepoSecret.Name,
			Namespace: namespace,
		})
	}

	tlsOptions := TLSOptions(modSpec, km)
	exists, err := reg.ImageExists(ctx, imageName, tlsOptions, registryAuthGetter)
	if err != nil {
		return false, fmt.Errorf("could not check if the image is available: %v", err)
	}

	return exists, nil
}
