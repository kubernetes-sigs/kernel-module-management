package module

import (
	"errors"
	"fmt"
	"regexp"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/api"
	"github.com/kubernetes-sigs/kernel-module-management/internal/kernel"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
)

var ErrNoMatchingKernelMapping = errors.New("kernel mapping not found")

//go:generate mockgen -source=kernelmapper.go -package=module -destination=mock_kernelmapper.go KernelMapper,kernelMapperHelperAPI

type KernelMapper interface {
	GetModuleLoaderDataForKernel(mod *kmmv1beta1.Module, kernelVersion string) (*api.ModuleLoaderData, error)
}

type kernelMapper struct {
	helper kernelMapperHelperAPI
}

func NewKernelMapper(buildArgOverrider BuildArgOverrider) KernelMapper {
	return &kernelMapper{
		helper: newKernelMapperHelper(buildArgOverrider),
	}
}

func (k *kernelMapper) GetModuleLoaderDataForKernel(mod *kmmv1beta1.Module, kernelVersion string) (*api.ModuleLoaderData, error) {
	mappings := mod.Spec.ModuleLoader.Container.KernelMappings
	foundMapping, err := k.helper.findKernelMapping(mappings, kernelVersion)
	if err != nil {
		return nil, fmt.Errorf("failed to find mapping for kernel %s: %w", kernelVersion, err)
	}
	mld, err := k.helper.prepareModuleLoaderData(foundMapping, mod, kernelVersion)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare module loader data for kernel %s: %v", kernelVersion, err)
	}

	err = k.helper.replaceTemplates(mld)
	if err != nil {
		return nil, fmt.Errorf("failed to replace templates in module loader data for kernel %s: %v", kernelVersion, err)
	}
	return mld, nil
}

type kernelMapperHelperAPI interface {
	findKernelMapping(mappings []kmmv1beta1.KernelMapping, kernelVersion string) (*kmmv1beta1.KernelMapping, error)
	prepareModuleLoaderData(mapping *kmmv1beta1.KernelMapping, mod *kmmv1beta1.Module, kernelVersion string) (*api.ModuleLoaderData, error)
	replaceTemplates(mld *api.ModuleLoaderData) error
	getRelevantBuild(moduleBuild *kmmv1beta1.Build, mappingBuild *kmmv1beta1.Build) *kmmv1beta1.Build
	getRelevantSign(moduleSign *kmmv1beta1.Sign, mappingSign *kmmv1beta1.Sign, kernel string) (*kmmv1beta1.Sign, error)
}

type kernelMapperHelper struct {
	buildArgOverrider BuildArgOverrider
}

func newKernelMapperHelper(buildArgOverrider BuildArgOverrider) kernelMapperHelperAPI {
	return &kernelMapperHelper{
		buildArgOverrider: buildArgOverrider,
	}
}

func (kh *kernelMapperHelper) findKernelMapping(mappings []kmmv1beta1.KernelMapping, kernelVersion string) (*kmmv1beta1.KernelMapping, error) {
	for _, m := range mappings {
		if m.Literal != "" && m.Literal == kernelVersion {
			return &m, nil
		}

		if m.Regexp == "" {
			continue
		}

		if matches, err := regexp.MatchString(m.Regexp, kernelVersion); err != nil {
			return nil, fmt.Errorf("could not match regexp %q against kernel %q: %v", m.Regexp, kernelVersion, err)
		} else if matches {
			return &m, nil
		}
	}

	return nil, ErrNoMatchingKernelMapping
}

func (kh *kernelMapperHelper) prepareModuleLoaderData(mapping *kmmv1beta1.KernelMapping, mod *kmmv1beta1.Module, kernelVersion string) (*api.ModuleLoaderData, error) {
	var err error

	mld := &api.ModuleLoaderData{}
	// prepare the build
	if mapping.Build != nil || mod.Spec.ModuleLoader.Container.Build != nil {
		mld.Build = kh.getRelevantBuild(mod.Spec.ModuleLoader.Container.Build, mapping.Build)
	}

	// prepare the sign
	if mapping.Sign != nil || mod.Spec.ModuleLoader.Container.Sign != nil {
		mld.Sign, err = kh.getRelevantSign(mod.Spec.ModuleLoader.Container.Sign, mapping.Sign, kernelVersion)
		if err != nil {
			return nil, fmt.Errorf("failed to get the relevant Sign configuration for kernel %s: %v", kernelVersion, err)
		}
	}

	// prepare TLS options
	mld.RegistryTLS = mapping.RegistryTLS
	if mapping.RegistryTLS == nil {
		mld.RegistryTLS = &mod.Spec.ModuleLoader.Container.RegistryTLS
	}

	//prepare container image
	mld.ContainerImage = mapping.ContainerImage
	if mapping.ContainerImage == "" {
		mld.ContainerImage = mod.Spec.ModuleLoader.Container.ContainerImage
	}

	mld.InTreeModulesToRemove = mod.Spec.ModuleLoader.Container.InTreeModulesToRemove
	if mapping.InTreeModulesToRemove != nil {
		mld.InTreeModulesToRemove = mapping.InTreeModulesToRemove
	}

	// [TODO] remove this code after InTreeModuleToRemove deprecated field has been
	// removed from the CRD
	inTreeModuleToRemove := mod.Spec.ModuleLoader.Container.InTreeModuleToRemove //nolint:staticcheck
	if mapping.InTreeModuleToRemove != "" {                                      //nolint:staticcheck
		inTreeModuleToRemove = mapping.InTreeModuleToRemove //nolint:staticcheck
	}

	// webhook makes sure that InTreeModuleToRemove and InTreeModulesToRemove cannot both
	// be set, so if inTreeModuleToRemove is not empty, its value should be set into mld.InTreeModulesToRemove
	if inTreeModuleToRemove != "" {
		mld.InTreeModulesToRemove = []string{inTreeModuleToRemove}
	}

	mld.KernelVersion = kernelVersion
	mld.KernelNormalizedVersion = kernel.NormalizeVersion(kernelVersion)
	mld.Name = mod.Name
	mld.Namespace = mod.Namespace
	mld.ImageRepoSecret = mod.Spec.ImageRepoSecret
	mld.Selector = mod.Spec.Selector
	mld.Tolerations = mod.Spec.Tolerations
	mld.ServiceAccountName = mod.Spec.ModuleLoader.ServiceAccountName
	mld.Modprobe = mod.Spec.ModuleLoader.Container.Modprobe
	mld.ModuleVersion = mod.Spec.ModuleLoader.Container.Version
	mld.ImagePullPolicy = mod.Spec.ModuleLoader.Container.ImagePullPolicy
	mld.Owner = mod

	return mld, nil
}

func (kh *kernelMapperHelper) replaceTemplates(mld *api.ModuleLoaderData) error {
	osConfigEnvVars := utils.KernelComponentsAsEnvVars(mld.KernelNormalizedVersion)
	osConfigEnvVars = append(osConfigEnvVars, "MOD_NAME="+mld.Name, "MOD_NAMESPACE="+mld.Namespace)

	replacedContainerImage, err := utils.ReplaceInTemplates(osConfigEnvVars, mld.ContainerImage)
	if err != nil {
		return fmt.Errorf("failed to substitute templates in the ContainerImage field: %v", err)
	}
	mld.ContainerImage = replacedContainerImage[0]

	return nil
}

func (kh *kernelMapperHelper) getRelevantBuild(moduleBuild *kmmv1beta1.Build, mappingBuild *kmmv1beta1.Build) *kmmv1beta1.Build {
	if moduleBuild == nil {
		return mappingBuild.DeepCopy()
	}

	if mappingBuild == nil {
		return moduleBuild.DeepCopy()
	}

	buildConfig := moduleBuild.DeepCopy()
	if mappingBuild.DockerfileConfigMap != nil {
		buildConfig.DockerfileConfigMap = mappingBuild.DockerfileConfigMap
	}

	buildConfig.BuildArgs = kh.buildArgOverrider.ApplyBuildArgOverrides(buildConfig.BuildArgs, mappingBuild.BuildArgs...)

	buildConfig.Secrets = append(buildConfig.Secrets, mappingBuild.Secrets...)
	return buildConfig
}

func (kh *kernelMapperHelper) getRelevantSign(moduleSign *kmmv1beta1.Sign, mappingSign *kmmv1beta1.Sign, kernelVersion string) (*kmmv1beta1.Sign, error) {
	var signConfig *kmmv1beta1.Sign
	if moduleSign == nil {
		// km.Sign cannot be nil in case mod.Sign is nil, checked above
		signConfig = mappingSign.DeepCopy()
	} else if mappingSign == nil {
		signConfig = moduleSign.DeepCopy()
	} else {
		signConfig = moduleSign.DeepCopy()

		if mappingSign.UnsignedImage != "" {
			signConfig.UnsignedImage = mappingSign.UnsignedImage
		}

		if mappingSign.KeySecret != nil {
			signConfig.KeySecret = mappingSign.KeySecret
		}
		if mappingSign.CertSecret != nil {
			signConfig.CertSecret = mappingSign.CertSecret
		}
		//append (not overwrite) any files in the km to the defaults
		signConfig.FilesToSign = append(signConfig.FilesToSign, mappingSign.FilesToSign...)
	}

	osConfigEnvVars := utils.KernelComponentsAsEnvVars(
		kernel.NormalizeVersion(kernelVersion),
	)
	unsignedImage, err := utils.ReplaceInTemplates(osConfigEnvVars, signConfig.UnsignedImage)
	if err != nil {
		return nil, err
	}
	signConfig.UnsignedImage = unsignedImage[0]
	filesToSign, err := utils.ReplaceInTemplates(osConfigEnvVars, signConfig.FilesToSign...)
	if err != nil {
		return nil, err
	}
	signConfig.FilesToSign = filesToSign

	return signConfig, nil
}
