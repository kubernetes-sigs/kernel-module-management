package module

import (
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"

	"github.com/a8m/envsubst/parse"
	kmmv1beta1 "github.com/qbarrand/oot-operator/api/v1beta1"
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
	FindMappingForKernel(mappings []kmmv1beta1.KernelMapping, kernelVersion string) (*kmmv1beta1.KernelMapping, error)
	GetNodeOSConfig(node *v1.Node) *NodeOSConfig
	PrepareKernelMapping(mapping *kmmv1beta1.KernelMapping, osConfig *NodeOSConfig) (*kmmv1beta1.KernelMapping, error)
}

type kernelMapper struct{}

func NewKernelMapper() KernelMapper {
	return &kernelMapper{}
}

// FindMappingForKernel tries to match kernelVersion against mappings. It returns the first mapping that has a Literal
// field equal to kernelVersion or a Regexp field that matches kernelVersion.
func (k *kernelMapper) FindMappingForKernel(mappings []kmmv1beta1.KernelMapping, kernelVersion string) (*kmmv1beta1.KernelMapping, error) {
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
	osConfig := NodeOSConfig{}

	osConfigFieldsList := regexp.MustCompile("[.,-]").Split(node.Status.NodeInfo.KernelVersion, -1)

	osConfig.KernelFullVersion = node.Status.NodeInfo.KernelVersion
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
