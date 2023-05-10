package module

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kubernetes-sigs/kernel-module-management/internal/api"
	"github.com/kubernetes-sigs/kernel-module-management/internal/auth"
	"github.com/kubernetes-sigs/kernel-module-management/internal/registry"
)

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

// ShouldBeBuilt indicates whether the specified ModuleLoaderData of the
// Module should be built or not.
func ShouldBeBuilt(mld *api.ModuleLoaderData) bool {
	return mld.Build != nil
}

// ShouldBeSigned indicates whether the specified ModuleLoaderData of the
// Module should be signed or not.
func ShouldBeSigned(mld *api.ModuleLoaderData) bool {
	return mld.Sign != nil
}

func ImageDigest(
	ctx context.Context,
	client client.Client,
	reg registry.Registry,
	mld *api.ModuleLoaderData,
	namespace string,
	imageName string) (string, error) {

	var registryAuthGetter auth.RegistryAuthGetter
	if mld.ImageRepoSecret != nil {
		registryAuthGetter = auth.NewRegistryAuthGetter(client, types.NamespacedName{
			Name:      mld.ImageRepoSecret.Name,
			Namespace: namespace,
		})
	}

	digest, err := reg.GetDigest(ctx, imageName, mld.RegistryTLS, registryAuthGetter)
	if err != nil {
		return "", fmt.Errorf("could not get image digest: %v", err)
	}

	return digest, nil
}

func ImageExists(
	ctx context.Context,
	client client.Client,
	reg registry.Registry,
	mld *api.ModuleLoaderData,
	namespace string,
	imageName string) (bool, error) {

	var registryAuthGetter auth.RegistryAuthGetter
	if mld.ImageRepoSecret != nil {
		registryAuthGetter = auth.NewRegistryAuthGetter(client, types.NamespacedName{
			Name:      mld.ImageRepoSecret.Name,
			Namespace: namespace,
		})
	}

	exists, err := reg.ImageExists(ctx, imageName, mld.RegistryTLS, registryAuthGetter)
	if err != nil {
		return false, fmt.Errorf("could not check if the image is available: %v", err)
	}

	return exists, nil
}
