package module

import (
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"

	"github.com/a8m/envsubst/parse"
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/build"
	"github.com/kubernetes-sigs/kernel-module-management/internal/sign"
	v1 "k8s.io/api/core/v1"
)

const (
	kernelVersionMajorIdx = 0
	kernelVersionMinorIdx = 1
	kernelVersionPatchIdx = 2
)

type NodeOSConfig struct {
	KernelFullVersion  string `subst:"KERNEL_FULL_VERSION"`
	KernelVersionMMP   string `subst:"KERNEL_XYZ"`
	KernelVersionMajor string `subst:"KERNEL_X"`
	KernelVersionMinor string `subst:"KERNEL_Y"`
	KernelVersionPatch string `subst:"KERNEL_Z"`
}

//go:generate mockgen -source=kernelmapper.go -package=module -destination=mock_kernelmapper.go

type KernelMapper interface {
	FindMappingForKernel(modSpec *kmmv1beta1.ModuleSpec, kernelVersion string) (*kmmv1beta1.KernelMapping, error)
	GetNodeOSConfig(node *v1.Node) *NodeOSConfig
	GetNodeOSConfigFromKernelVersion(kernelVersion string) *NodeOSConfig
	PrepareKernelMapping(mapping *kmmv1beta1.KernelMapping, osConfig *NodeOSConfig) (*kmmv1beta1.KernelMapping, error)
}

type kernelMapper struct {
	buildHelper build.Helper
	signHelper  sign.Helper
}

func NewKernelMapper(buildHelper build.Helper, signHelper sign.Helper) KernelMapper {
	return &kernelMapper{
		buildHelper: buildHelper,
		signHelper:  signHelper,
	}
}

// FindMappingForKernel tries to match kernelVersion against mappings. It returns the first mapping that has a Literal
// field equal to kernelVersion or a Regexp field that matches kernelVersion.
func (k *kernelMapper) FindMappingForKernel(modSpec *kmmv1beta1.ModuleSpec, kernelVersion string) (*kmmv1beta1.KernelMapping, error) {
	mappings := modSpec.ModuleLoader.Container.KernelMappings
	foundMapping, err := k.findKernelMapping(mappings, kernelVersion)
	if err != nil {
		return nil, fmt.Errorf("failed to find mapping for kernel %s: %v", kernelVersion, err)
	}
	mapping := foundMapping.DeepCopy()

	// prepare the build for the mapping
	if mapping.Build != nil || modSpec.ModuleLoader.Container.Build != nil {
		mapping.Build = k.buildHelper.GetRelevantBuild(modSpec.ModuleLoader.Container.Build, mapping.Build)
	}

	// prepare the sign for the mapping
	if mapping.Sign != nil || modSpec.ModuleLoader.Container.Sign != nil {
		mapping.Sign, err = k.signHelper.GetRelevantSign(modSpec.ModuleLoader.Container.Sign, mapping.Sign, kernelVersion)
		if err != nil {
			return nil, fmt.Errorf("failed to get the relevant Sign configuration for kernel %s: %v", kernelVersion, err)
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
	return mapping, nil
}

func (k *kernelMapper) findKernelMapping(mappings []kmmv1beta1.KernelMapping, kernelVersion string) (*kmmv1beta1.KernelMapping, error) {
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

func (k *kernelMapper) GetNodeOSConfig(node *v1.Node) *NodeOSConfig {
	return k.GetNodeOSConfigFromKernelVersion(node.Status.NodeInfo.KernelVersion)
}

func (k *kernelMapper) GetNodeOSConfigFromKernelVersion(kernelVersion string) *NodeOSConfig {
	osConfig := NodeOSConfig{}

	osConfigFieldsList := regexp.MustCompile("[.,-]").Split(kernelVersion, -1)

	osConfig.KernelFullVersion = kernelVersion
	osConfig.KernelVersionMMP = strings.Join(osConfigFieldsList[:kernelVersionPatchIdx+1], ".")
	osConfig.KernelVersionMajor = osConfigFieldsList[kernelVersionMajorIdx]
	osConfig.KernelVersionMinor = osConfigFieldsList[kernelVersionMinorIdx]
	osConfig.KernelVersionPatch = osConfigFieldsList[kernelVersionPatchIdx]

	return &osConfig
}

func (k *kernelMapper) PrepareKernelMapping(mapping *kmmv1beta1.KernelMapping, osConfig *NodeOSConfig) (*kmmv1beta1.KernelMapping, error) {
	osConfigStrings := k.prepareOSConfigList(*osConfig)

	parser := parse.New("mapping", osConfigStrings, &parse.Restrictions{})

	substContainerImage, err := parser.Parse(mapping.ContainerImage)
	if err != nil {
		return nil, fmt.Errorf("failed to substitute the os config into ContainerImage field: %w", err)
	}

	substMapping := mapping.DeepCopy()
	substMapping.ContainerImage = substContainerImage

	return substMapping, nil
}

func (k *kernelMapper) prepareOSConfigList(osConfig NodeOSConfig) []string {
	t := reflect.TypeOf(osConfig)
	v := reflect.ValueOf(osConfig)

	varList := make([]string, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		varList[i] = t.Field(i).Tag.Get("subst") + "=" + v.Field(i).String()
	}
	return varList
}
