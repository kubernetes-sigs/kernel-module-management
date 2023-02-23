package module

import (
	"errors"
	"fmt"
	"regexp"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/api"
	"github.com/kubernetes-sigs/kernel-module-management/internal/build"
	"github.com/kubernetes-sigs/kernel-module-management/internal/sign"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
)

//go:generate mockgen -source=kernelmapper.go -package=module -destination=mock_kernelmapper.go KernelMapper,kernelMapperHelperAPI

type KernelMapper interface {
	GetModuleLoaderDataForKernel(mod *kmmv1beta1.Module, kernelVersion string) (*api.ModuleLoaderData, error)
}

type kernelMapper struct {
	helper kernelMapperHelperAPI
}

func NewKernelMapper(buildHelper build.Helper, signHelper sign.Helper) KernelMapper {
	return &kernelMapper{
		helper: newKernelMapperHelper(buildHelper, signHelper),
	}
}

func (k *kernelMapper) GetModuleLoaderDataForKernel(mod *kmmv1beta1.Module, kernelVersion string) (*api.ModuleLoaderData, error) {
	mappings := mod.Spec.ModuleLoader.Container.KernelMappings
	foundMapping, err := k.helper.findKernelMapping(mappings, kernelVersion)
	if err != nil {
		return nil, fmt.Errorf("failed to find mapping for kernel %s: %v", kernelVersion, err)
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
}

type kernelMapperHelper struct {
	buildHelper build.Helper
	signHelper  sign.Helper
}

func newKernelMapperHelper(buildHelper build.Helper, signHelper sign.Helper) kernelMapperHelperAPI {
	return &kernelMapperHelper{
		buildHelper: buildHelper,
		signHelper:  signHelper,
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

	return nil, errors.New("no suitable mapping found")
}

func (kh *kernelMapperHelper) prepareModuleLoaderData(mapping *kmmv1beta1.KernelMapping, mod *kmmv1beta1.Module, kernelVersion string) (*api.ModuleLoaderData, error) {
	var err error

	mld := &api.ModuleLoaderData{}
	// prepare the build
	if mapping.Build != nil || mod.Spec.ModuleLoader.Container.Build != nil {
		mld.Build = kh.buildHelper.GetRelevantBuild(mod.Spec.ModuleLoader.Container.Build, mapping.Build)
	}

	// prepare the sign
	if mapping.Sign != nil || mod.Spec.ModuleLoader.Container.Sign != nil {
		mld.Sign, err = kh.signHelper.GetRelevantSign(mod.Spec.ModuleLoader.Container.Sign, mapping.Sign, kernelVersion)
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

func (kh *kernelMapperHelper) replaceTemplates(mld *api.ModuleLoaderData) error {
	osConfigEnvVars := utils.KernelComponentsAsEnvVars(mld.KernelVersion)
	osConfigEnvVars = append(osConfigEnvVars, "MOD_NAME="+mld.Name, "MOD_NAMESPACE="+mld.Namespace)

	replacedContainerImage, err := utils.ReplaceInTemplates(osConfigEnvVars, mld.ContainerImage)
	if err != nil {
		return fmt.Errorf("failed to substitute templates in the ContainerImage field: %v", err)
	}
	mld.ContainerImage = replacedContainerImage[0]

	return nil
}
