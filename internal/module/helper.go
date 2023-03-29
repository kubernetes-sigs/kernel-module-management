package module

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kubernetes-sigs/kernel-module-management/internal/api"
	"github.com/kubernetes-sigs/kernel-module-management/internal/auth"
	"github.com/kubernetes-sigs/kernel-module-management/internal/registry"
)

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

	tlsOptions := mld.RegistryTLS
	exists, err := reg.ImageExists(ctx, imageName, tlsOptions, registryAuthGetter)
	if err != nil {
		return false, fmt.Errorf("could not check if the image is available: %v", err)
	}

	return exists, nil
}
