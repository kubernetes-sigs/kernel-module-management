package preflight

import (
	"context"
	"fmt"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/auth"
	"github.com/kubernetes-sigs/kernel-module-management/internal/build"
	"github.com/kubernetes-sigs/kernel-module-management/internal/module"
	"github.com/kubernetes-sigs/kernel-module-management/internal/registry"
	"github.com/kubernetes-sigs/kernel-module-management/internal/statusupdater"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"

	ctrlruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	VerificationStatusReasonBuildConfigPresent = "Verification successful, all driver-containers have paired BuildConfigs in the recipe"
	VerificationStatusReasonNoDaemonSet        = "Verification successful, no driver-container present in the recipe"
	VerificationStatusReasonUnknown            = "Verification has not started yet"
	VerificationStatusReasonVerified           = "Verification successful (%s), this Module will not be verified again in this Preflight CR"
)

//go:generate mockgen -source=preflight.go -package=preflight -destination=mock_preflight_api.go PreflightAPI, preflightHelperAPI

type PreflightAPI interface {
	PreflightUpgradeCheck(ctx context.Context, pv *kmmv1beta1.PreflightValidation, mod *kmmv1beta1.Module) (bool, string)
}

func NewPreflightAPI(
	client client.Client,
	buildAPI build.Manager,
	registryAPI registry.Registry,
	statusUpdater statusupdater.PreflightStatusUpdater,
	kernelAPI module.KernelMapper) PreflightAPI {
	helper := newPreflightHelper(client, buildAPI, registryAPI)
	return &preflight{
		kernelAPI:     kernelAPI,
		statusUpdater: statusUpdater,
		helper:        helper,
	}
}

type preflight struct {
	kernelAPI     module.KernelMapper
	statusUpdater statusupdater.PreflightStatusUpdater
	helper        preflightHelperAPI
}

func (p *preflight) PreflightUpgradeCheck(ctx context.Context, pv *kmmv1beta1.PreflightValidation, mod *kmmv1beta1.Module) (bool, string) {
	log := ctrlruntime.LoggerFrom(ctx)
	kernelVersion := pv.Spec.KernelVersion
	mapping, err := p.kernelAPI.FindMappingForKernel(mod.Spec.ModuleLoader.Container.KernelMappings, kernelVersion)
	if err != nil {
		return false, fmt.Sprintf("Failed to find kernel mapping in the module %s for kernel version %s", mod.Name, kernelVersion)
	}

	osConfig := module.NodeOSConfig{KernelFullVersion: kernelVersion}
	mapping, err = p.kernelAPI.PrepareKernelMapping(mapping, &osConfig)
	if err != nil {
		return false, fmt.Sprintf("Failed to substitute template in kernel mapping in the module %s for kernel version %s", mod.Name, kernelVersion)
	}

	err = p.statusUpdater.PreflightSetVerificationStage(ctx, pv, mod.Name, kmmv1beta1.VerificationStageImage)
	if err != nil {
		log.Info(utils.WarnString("failed to update the stage of Module CR in preflight to image stage"), "module", mod.Name, "error", err)
	}

	imageVerified, msg := p.helper.verifyImage(ctx, mapping, mod, kernelVersion)
	if imageVerified || (mapping.Build == nil && mod.Spec.ModuleLoader.Container.Build == nil) {
		return imageVerified, msg
	}

	err = p.statusUpdater.PreflightSetVerificationStage(ctx, pv, mod.Name, kmmv1beta1.VerificationStageBuild)
	if err != nil {
		log.Info(utils.WarnString("failed to update the stage of Module CR in preflight to build stage"), "module", mod.Name, "error", err)
	}

	return p.helper.verifyBuild(ctx, pv, mapping, mod)
}

type preflightHelperAPI interface {
	verifyImage(ctx context.Context, mapping *kmmv1beta1.KernelMapping, mod *kmmv1beta1.Module, kernelVersion string) (bool, string)
	verifyBuild(ctx context.Context, pv *kmmv1beta1.PreflightValidation, mapping *kmmv1beta1.KernelMapping, mod *kmmv1beta1.Module) (bool, string)
}

type preflightHelper struct {
	client      client.Client
	registryAPI registry.Registry
	buildAPI    build.Manager
}

func newPreflightHelper(client client.Client, buildAPI build.Manager, registryAPI registry.Registry) preflightHelperAPI {
	return &preflightHelper{
		client:      client,
		buildAPI:    buildAPI,
		registryAPI: registryAPI,
	}
}

func (p *preflightHelper) verifyImage(ctx context.Context, mapping *kmmv1beta1.KernelMapping, mod *kmmv1beta1.Module, kernelVersion string) (bool, string) {
	log := ctrlruntime.LoggerFrom(ctx)
	image := mapping.ContainerImage
	moduleFileName := mod.Spec.ModuleLoader.Container.Modprobe.ModuleName + ".ko"
	baseDir := mod.Spec.ModuleLoader.Container.Modprobe.DirName

	tlsOptions := module.GetRelevantTLSOptions(mod, mapping)
	registryAuthGetter := auth.NewRegistryAuthGetterFrom(p.client, mod)
	digests, repoConfig, err := p.registryAPI.GetLayersDigests(ctx, image, tlsOptions, registryAuthGetter)
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
		if p.registryAPI.VerifyModuleExists(layer, baseDir, kernelVersion, moduleFileName) {
			return true, fmt.Sprintf(VerificationStatusReasonVerified, "image accessible and verified")
		}
		log.V(1).Info("module is not present in the current layer", "image", image, "module file name", moduleFileName, "kernel", kernelVersion, "dir", baseDir)
	}

	log.Info("driver for kernel is not present in the image", "baseDir", baseDir, "kernel", kernelVersion, "moduleFileName", moduleFileName, "image", image)
	return false, fmt.Sprintf("image %s does not contain kernel module for kernel %s on any layer", image, kernelVersion)
}

func (p *preflightHelper) verifyBuild(ctx context.Context,
	pv *kmmv1beta1.PreflightValidation,
	mapping *kmmv1beta1.KernelMapping,
	mod *kmmv1beta1.Module) (bool, string) {
	// at this stage we know that eiher mapping Build or Container build are defined
	buildRes, err := p.buildAPI.Sync(ctx, *mod, *mapping, pv.Spec.KernelVersion, mapping.ContainerImage, pv.Spec.PushBuiltImage)
	if err != nil {
		return false, fmt.Sprintf("Failed to verify build for module %s, kernel version %s, error %s", mod.Name, pv.Spec.KernelVersion, err)
	}

	if buildRes.Status == build.StatusCompleted {
		msg := "build compiles"
		if pv.Spec.PushBuiltImage {
			msg += " and image pushed"
		}
		return true, fmt.Sprintf(VerificationStatusReasonVerified, msg)
	}
	return false, "Waiting for build verification"
}
