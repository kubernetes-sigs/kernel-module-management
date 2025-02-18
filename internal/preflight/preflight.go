package preflight

import (
	"context"
	"fmt"
	"slices"

	"github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/api/v1beta2"
	"github.com/kubernetes-sigs/kernel-module-management/internal/api"
	"github.com/kubernetes-sigs/kernel-module-management/internal/auth"
	"github.com/kubernetes-sigs/kernel-module-management/internal/build"
	"github.com/kubernetes-sigs/kernel-module-management/internal/module"
	"github.com/kubernetes-sigs/kernel-module-management/internal/pod"
	"github.com/kubernetes-sigs/kernel-module-management/internal/registry"
	"github.com/kubernetes-sigs/kernel-module-management/internal/sign"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
	"k8s.io/apimachinery/pkg/types"

	ctrlruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const VerificationStatusReasonVerified = "Verification successful (%s), this Module will not be verified again in this Preflight CR"

//go:generate mockgen -source=preflight.go -package=preflight -destination=mock_preflight_api.go PreflightAPI, preflightHelperAPI

type PreflightAPI interface {
	PreflightUpgradeCheck(ctx context.Context, pv *v1beta2.PreflightValidation, mod *v1beta1.Module) (bool, string)
}

func NewPreflightAPI(
	client client.Client,
	buildAPI build.Manager,
	signAPI sign.SignManager,
	registryAPI registry.Registry,
	statusUpdater StatusUpdater,
	kernelAPI module.KernelMapper) PreflightAPI {
	helper := newPreflightHelper(client, buildAPI, signAPI, registryAPI)
	return &preflight{
		kernelAPI:     kernelAPI,
		statusUpdater: statusUpdater,
		helper:        helper,
	}
}

type preflight struct {
	kernelAPI     module.KernelMapper
	statusUpdater StatusUpdater
	helper        preflightHelperAPI
}

func (p *preflight) PreflightUpgradeCheck(ctx context.Context, pv *v1beta2.PreflightValidation, mod *v1beta1.Module) (bool, string) {
	log := ctrlruntime.LoggerFrom(ctx)
	kernelVersion := pv.Spec.KernelVersion
	mld, err := p.kernelAPI.GetModuleLoaderDataForKernel(mod, kernelVersion)
	if err != nil {
		return false, fmt.Sprintf("failed to process kernel mapping in the module %s for kernel version %s", mod.Name, kernelVersion)
	}

	nsn := mld.NamespacedName()

	err = p.statusUpdater.SetVerificationStage(ctx, pv, nsn, v1beta1.VerificationStageImage)
	if err != nil {
		log.Info(utils.WarnString("failed to update the stage of Module CR in preflight to image stage"), "module", nsn, "error", err)
	}

	verified, msg := p.helper.verifyImage(ctx, mld)
	if verified {
		return true, msg
	}

	shouldBuild := module.ShouldBeBuilt(mld)
	shouldSign := module.ShouldBeSigned(mld)

	if shouldBuild {
		err = p.statusUpdater.SetVerificationStage(ctx, pv, nsn, v1beta1.VerificationStageBuild)
		if err != nil {
			log.Info(utils.WarnString("failed to update the stage of Module CR in preflight to build stage"), "module", nsn, "error", err)
		}

		verified, msg = p.helper.verifyBuild(ctx, pv, mld)
		if !verified {
			return false, msg
		}
	}

	if shouldSign {
		err = p.statusUpdater.SetVerificationStage(ctx, pv, nsn, v1beta1.VerificationStageSign)
		if err != nil {
			log.Info(utils.WarnString("failed to update the stage of Module CR in preflight to sign stage"), "module", nsn, "error", err)
		}
		verified, msg = p.helper.verifySign(ctx, pv, mld)
		if !verified {
			return false, msg
		}
	}
	return verified, msg
}

type preflightHelperAPI interface {
	verifyImage(ctx context.Context, mld *api.ModuleLoaderData) (bool, string)
	verifyBuild(ctx context.Context, pv *v1beta2.PreflightValidation, mld *api.ModuleLoaderData) (bool, string)
	verifySign(ctx context.Context, pv *v1beta2.PreflightValidation, mld *api.ModuleLoaderData) (bool, string)
}

type preflightHelper struct {
	client      client.Client
	registryAPI registry.Registry
	buildAPI    build.Manager
	signAPI     sign.SignManager
}

func newPreflightHelper(client client.Client, buildAPI build.Manager, signAPI sign.SignManager, registryAPI registry.Registry) preflightHelperAPI {
	return &preflightHelper{
		client:      client,
		buildAPI:    buildAPI,
		signAPI:     signAPI,
		registryAPI: registryAPI,
	}
}

func (p *preflightHelper) verifyImage(ctx context.Context, mld *api.ModuleLoaderData) (bool, string) {
	log := ctrlruntime.LoggerFrom(ctx)
	image := mld.ContainerImage
	moduleFileName := mld.Modprobe.ModuleName + ".ko"
	baseDir := mld.Modprobe.DirName
	kernelVersion := mld.KernelVersion

	registryAuthGetter := auth.NewRegistryAuthGetterFrom(p.client, mld)
	digests, repoConfig, err := p.registryAPI.GetLayersDigests(ctx, image, mld.RegistryTLS, registryAuthGetter)
	if err != nil {
		log.Info("image layers inaccessible, image probably does not exists", "module name", mld.Name, "image", image)
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

func (p *preflightHelper) verifyBuild(ctx context.Context, pv *v1beta2.PreflightValidation, mld *api.ModuleLoaderData) (bool, string) {
	log := ctrlruntime.LoggerFrom(ctx)
	// at this stage we know that eiher mapping Build or Container build are defined
	buildStatus, err := p.buildAPI.Sync(ctx, mld, pv.Spec.PushBuiltImage, pv)
	if err != nil {
		return false, fmt.Sprintf("Failed to verify build for module %s, kernel version %s, error %s", mld.Name, pv.Spec.KernelVersion, err)
	}

	if buildStatus == pod.StatusCompleted {
		msg := "build compiles"
		if pv.Spec.PushBuiltImage {
			msg += " and image pushed"
		}
		log.Info("build for module during preflight has been build successfully", "module", mld.Name)
		return true, fmt.Sprintf(VerificationStatusReasonVerified, msg)
	}
	return false, "Waiting for build verification"
}

func (p *preflightHelper) verifySign(ctx context.Context, pv *v1beta2.PreflightValidation, mld *api.ModuleLoaderData) (bool, string) {
	log := ctrlruntime.LoggerFrom(ctx)

	previousImage := ""
	if module.ShouldBeBuilt(mld) {
		previousImage = module.IntermediateImageName(mld.Name, mld.Namespace, mld.ContainerImage)
	}

	// at this stage we know that eiher mapping Sign or Container sign are defined
	signStatus, err := p.signAPI.Sync(ctx, mld, previousImage, pv.Spec.PushBuiltImage, pv)
	if err != nil {
		return false, fmt.Sprintf("Failed to verify signing for module %s, kernel version %s, error %s", mld.Name, pv.Spec.KernelVersion, err)
	}

	if signStatus == pod.StatusCompleted {
		msg := "sign completes"
		if pv.Spec.PushBuiltImage {
			msg += " and image pushed"
		}
		log.Info("build for module during preflight has been build successfully", "module", mld.Name)
		return true, fmt.Sprintf(VerificationStatusReasonVerified, msg)
	}
	return false, "Waiting for sign verification"
}

func FindModuleStatus(statuses []v1beta2.PreflightValidationModuleStatus, nsn types.NamespacedName) (*v1beta2.PreflightValidationModuleStatus, bool) {
	i := slices.IndexFunc(statuses, func(status v1beta2.PreflightValidationModuleStatus) bool {
		return status.Namespace == nsn.Namespace && status.Name == nsn.Name
	})

	if i != -1 {
		return &statuses[i], true
	}

	return nil, false
}
