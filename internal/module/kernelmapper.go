package module

import (
	"errors"
	"fmt"
	"regexp"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/build"
	"github.com/kubernetes-sigs/kernel-module-management/internal/sign"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
)

//go:generate mockgen -source=kernelmapper.go -package=module -destination=mock_kernelmapper.go KernelMapper,kernelMapperHelperAPI

type KernelMapper interface {
	GetMergedMappingForKernel(modSpec *kmmv1beta1.ModuleSpec, kernelVersion string) (*kmmv1beta1.KernelMapping, error)
}

type kernelMapper struct {
	helper kernelMapperHelperAPI
}

func NewKernelMapper(buildHelper build.Helper, signHelper sign.Helper) KernelMapper {
	return &kernelMapper{
		helper: newKernelMapperHelper(buildHelper, signHelper),
	}
}

func (k *kernelMapper) GetMergedMappingForKernel(modSpec *kmmv1beta1.ModuleSpec, kernelVersion string) (*kmmv1beta1.KernelMapping, error) {
	mappings := modSpec.ModuleLoader.Container.KernelMappings
	foundMapping, err := k.helper.findKernelMapping(mappings, kernelVersion)
	if err != nil {
		return nil, fmt.Errorf("failed to find mapping for kernel %s: %v", kernelVersion, err)
	}
	mapping := foundMapping.DeepCopy()
	err = k.helper.mergeMappingData(mapping, modSpec, kernelVersion)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare module loader data for kernel %s: %v", kernelVersion, err)
	}

	err = k.helper.replaceTemplates(mapping, kernelVersion)
	if err != nil {
		return nil, fmt.Errorf("failed to replace templates in module loader data for kernel %s: %v", kernelVersion, err)
	}
	return mapping, nil
}

type kernelMapperHelperAPI interface {
	findKernelMapping(mappings []kmmv1beta1.KernelMapping, kernelVersion string) (*kmmv1beta1.KernelMapping, error)
	mergeMappingData(mapping *kmmv1beta1.KernelMapping, modSpec *kmmv1beta1.ModuleSpec, kernelVersion string) error
	replaceTemplates(mapping *kmmv1beta1.KernelMapping, kernelVersion string) error
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

func (kh *kernelMapperHelper) mergeMappingData(mapping *kmmv1beta1.KernelMapping, modSpec *kmmv1beta1.ModuleSpec, kernelVersion string) error {
	var err error

	// prepare the build
	if mapping.Build != nil || modSpec.ModuleLoader.Container.Build != nil {
		mapping.Build = kh.buildHelper.GetRelevantBuild(modSpec.ModuleLoader.Container.Build, mapping.Build)
	}

	// prepare the sign
	if mapping.Sign != nil || modSpec.ModuleLoader.Container.Sign != nil {
		mapping.Sign, err = kh.signHelper.GetRelevantSign(modSpec.ModuleLoader.Container.Sign, mapping.Sign, kernelVersion)
		if err != nil {
			return fmt.Errorf("failed to get the relevant Sign configuration for kernel %s: %v", kernelVersion, err)
		}
	}

	// prepare TLS options
	if mapping.RegistryTLS == nil {
		mapping.RegistryTLS = &modSpec.ModuleLoader.Container.RegistryTLS
	}

	//prepare container image
	if mapping.ContainerImage == "" {
		mapping.ContainerImage = modSpec.ModuleLoader.Container.ContainerImage
	}

	return nil
}

func (kh *kernelMapperHelper) replaceTemplates(mapping *kmmv1beta1.KernelMapping, kernelVersion string) error {
	osConfigEnvVars := utils.KernelComponentsAsEnvVars(kernelVersion)
	replacedContainerImage, err := utils.ReplaceInTemplates(osConfigEnvVars, mapping.ContainerImage)
	if err != nil {
		return fmt.Errorf("failed to substitute templates in the ContainerImage field: %v", err)
	}
	mapping.ContainerImage = replacedContainerImage[0]

	return nil
}
