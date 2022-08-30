package preflight

import (
	"context"
	"fmt"

	kmmv1beta1 "github.com/qbarrand/oot-operator/api/v1beta1"
	"github.com/qbarrand/oot-operator/internal/auth"
	"github.com/qbarrand/oot-operator/internal/module"
	"github.com/qbarrand/oot-operator/internal/registry"

	"k8s.io/apimachinery/pkg/types"
	ctrlruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	VerificationStatusReasonBuildConfigPresent = "Verification successful, all driver-containers have paired BuildConfigs in the recipe"
	VerificationStatusReasonNoDaemonSet        = "Verification successful, no driver-container present in the recipe"
	VerificationStatusReasonUnknown            = "Verification has not started yet"
	VerificationStatusReasonVerified           = "Verification successful, this Module would be verified again in this Preflight CR"
)

//go:generate mockgen -source=preflight.go -package=preflight -destination=mock_preflight_api.go

type PreflightAPI interface {
	PreflightUpgradeCheck(ctx context.Context, mod *kmmv1beta1.Module, kernelVersion string) (bool, string)
}

func NewPreflightAPI(
	client client.Client,
	registryAPI registry.Registry,
	kernelAPI module.KernelMapper) PreflightAPI {
	return &preflight{
		registryAPI: registryAPI,
		kernelAPI:   kernelAPI,
		client:      client,
	}
}

type preflight struct {
	client      client.Client
	registryAPI registry.Registry
	kernelAPI   module.KernelMapper
}

func (p *preflight) PreflightUpgradeCheck(ctx context.Context, mod *kmmv1beta1.Module, kernelVersion string) (bool, string) {
	mapping, err := p.kernelAPI.FindMappingForKernel(mod.Spec.ModuleLoader.Container.KernelMappings, kernelVersion)
	if err != nil {
		return false, fmt.Sprintf("Failed to find kernel mapping in the module %s for kernel version %s", mod.Name, kernelVersion)
	}

	osConfig := module.NodeOSConfig{KernelFullVersion: kernelVersion}
	mapping, err = p.kernelAPI.PrepareKernelMapping(mapping, &osConfig)
	if err != nil {
		return false, fmt.Sprintf("Failed to substitute template in kernel mapping in the module %s for kernel version %s", mod.Name, kernelVersion)
	}

	return p.verifyImage(ctx, mapping, mod, kernelVersion)
}

func (p *preflight) verifyImage(ctx context.Context, mapping *kmmv1beta1.KernelMapping, mod *kmmv1beta1.Module, kernelVersion string) (bool, string) {
	log := ctrlruntime.LoggerFrom(ctx)
	image := mapping.ContainerImage
	moduleName := mod.Spec.ModuleLoader.Container.Modprobe.ModuleName
	baseDir := mod.Spec.ModuleLoader.Container.Modprobe.DirName

	var registryAuthGetter auth.RegistryAuthGetter
	if mod.Spec.ImageRepoSecret != nil {
		namespacedName := types.NamespacedName{
			Name:      mod.Spec.ImageRepoSecret.Name,
			Namespace: mod.Namespace,
		}
		registryAuthGetter = auth.NewRegistryAuthGetter(p.client, namespacedName)
	}

	digests, repoConfig, err := p.registryAPI.GetLayersDigests(ctx, image, registryAuthGetter)
	if err != nil {
		log.Info("image layers inaccessible, image probably does not exists", "module name", mod.Name, "image", image)
		return false, fmt.Sprintf("image %s inaccessible or does not exists", image)
	}

	for i := len(digests) - 1; i >= 0; i-- {
		layer, err := p.registryAPI.GetLayerByDigest(digests[i], repoConfig)
		if err != nil {
			log.Info("layer from image inaccessible", "layer", digests[i], "repo", repoConfig, "image", image)
			return false, fmt.Sprintf("image %s, layer %s is inaccessible", image, digests[i])
		}

		// check kernel module file present in the directory of the kernel lib modules
		if p.registryAPI.VerifyModuleExists(layer, baseDir, kernelVersion, moduleName) {
			return true, VerificationStatusReasonVerified
		}
		log.V(1).Info("module is not present in the current layer", "image", image, "module name", moduleName, "kernel", kernelVersion, "dir", baseDir)
	}

	log.Info("driver for kernel is not present in the image", "kernel", kernelVersion, "image", image)
	return false, fmt.Sprintf("image %s does not contain kernel module for kernel %s on any layer", image, kernelVersion)
}
