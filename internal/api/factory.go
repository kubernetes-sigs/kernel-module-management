package api

import (
	"errors"
	"fmt"
	"regexp"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	buildutils "github.com/kubernetes-sigs/kernel-module-management/internal/build/utils"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
)

//go:generate mockgen -source=factory.go -package=api -destination=mock_factory.go ModuleLoaderDataFactory,moduleLoaderDataFactoryHelper

type ModuleLoaderDataFactory interface {
	FromModule(mod *kmmv1beta1.Module, kernelVersion string) (*ModuleLoaderData, error)
}

type moduleLoaderDataFactory struct {
	helper moduleLoaderDataFactoryHelper
}

func NewModuleLoaderDataFactory() ModuleLoaderDataFactory {
	return &moduleLoaderDataFactory{
		helper: &moduleLoaderDataFactoryHelperImpl{
			sectionGetter: &sectionGetterImpl{},
		},
	}
}

func (f *moduleLoaderDataFactory) FromModule(mod *kmmv1beta1.Module, kernelVersion string) (*ModuleLoaderData, error) {
	mappings := mod.Spec.ModuleLoader.Container.KernelMappings
	foundMapping, err := f.helper.findKernelMapping(mappings, kernelVersion)
	if err != nil {
		return nil, fmt.Errorf("failed to find mapping for kernel %s: %v", kernelVersion, err)
	}
	mld, err := f.helper.prepareModuleLoaderData(foundMapping, mod, kernelVersion)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare module loader data for kernel %s: %v", kernelVersion, err)
	}

	err = f.helper.replaceTemplates(mld)
	if err != nil {
		return nil, fmt.Errorf("failed to replace templates in module loader data for kernel %s: %v", kernelVersion, err)
	}
	return mld, nil
}

type moduleLoaderDataFactoryHelper interface {
	findKernelMapping(mappings []kmmv1beta1.KernelMapping, kernelVersion string) (*kmmv1beta1.KernelMapping, error)
	prepareModuleLoaderData(mapping *kmmv1beta1.KernelMapping, mod *kmmv1beta1.Module, kernelVersion string) (*ModuleLoaderData, error)
	replaceTemplates(mld *ModuleLoaderData) error
}

type moduleLoaderDataFactoryHelperImpl struct {
	sectionGetter sectionGetter
}

func (kh *moduleLoaderDataFactoryHelperImpl) findKernelMapping(mappings []kmmv1beta1.KernelMapping, kernelVersion string) (*kmmv1beta1.KernelMapping, error) {
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

	return nil, errors.New("no suitable mapping found")
}

func (kh *moduleLoaderDataFactoryHelperImpl) prepareModuleLoaderData(mapping *kmmv1beta1.KernelMapping, mod *kmmv1beta1.Module, kernelVersion string) (*ModuleLoaderData, error) {
	var err error

	mld := &ModuleLoaderData{}
	// prepare the build
	if mapping.Build != nil || mod.Spec.ModuleLoader.Container.Build != nil {
		mld.Build = kh.sectionGetter.GetRelevantBuild(mod.Spec.ModuleLoader.Container.Build, mapping.Build)
	}

	// prepare the sign
	if mapping.Sign != nil || mod.Spec.ModuleLoader.Container.Sign != nil {
		mld.Sign, err = kh.sectionGetter.GetRelevantSign(mod.Spec.ModuleLoader.Container.Sign, mapping.Sign, kernelVersion)
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

	mld.KernelVersion = kernelVersion
	mld.Name = mod.Name
	mld.Namespace = mod.Namespace
	mld.ImageRepoSecret = mod.Spec.ImageRepoSecret
	mld.Selector = mod.Spec.Selector
	mld.ServiceAccountName = mod.Spec.ModuleLoader.ServiceAccountName
	mld.Modprobe = mod.Spec.ModuleLoader.Container.Modprobe
	mld.Owner = mod

	return mld, nil
}

func (kh *moduleLoaderDataFactoryHelperImpl) replaceTemplates(mld *ModuleLoaderData) error {
	osConfigEnvVars := utils.KernelComponentsAsEnvVars(mld.KernelVersion)
	osConfigEnvVars = append(osConfigEnvVars, "MOD_NAME="+mld.Name, "MOD_NAMESPACE="+mld.Namespace)

	replacedContainerImage, err := utils.ReplaceInTemplates(osConfigEnvVars, mld.ContainerImage)
	if err != nil {
		return fmt.Errorf("failed to substitute templates in the ContainerImage field: %v", err)
	}
	mld.ContainerImage = replacedContainerImage[0]

	return nil
}

type sectionGetter interface {
	GetRelevantBuild(moduleBuild *kmmv1beta1.Build, mappingBuild *kmmv1beta1.Build) *kmmv1beta1.Build
	GetRelevantSign(moduleSign *kmmv1beta1.Sign, mappingSign *kmmv1beta1.Sign, kernel string) (*kmmv1beta1.Sign, error)
}

type sectionGetterImpl struct{}

func (sg *sectionGetterImpl) GetRelevantBuild(moduleBuild *kmmv1beta1.Build, mappingBuild *kmmv1beta1.Build) *kmmv1beta1.Build {
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

	buildConfig.BuildArgs = buildutils.ApplyBuildArgOverrides(buildConfig.BuildArgs, mappingBuild.BuildArgs...)

	// [TODO] once MGMT-10832 is consolidated, this code must be revisited. We will decide which
	// secret and how to use, and if we need to take care of repeated secrets names
	buildConfig.Secrets = append(buildConfig.Secrets, mappingBuild.Secrets...)
	return buildConfig
}

func (sg *sectionGetterImpl) GetRelevantSign(moduleSign *kmmv1beta1.Sign, mappingSign *kmmv1beta1.Sign, kernel string) (*kmmv1beta1.Sign, error) {
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

	osConfigEnvVars := utils.KernelComponentsAsEnvVars(kernel)
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
