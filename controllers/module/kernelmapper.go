package module

import (
	"errors"
	"fmt"
	"regexp"

	ootov1alpha1 "github.com/qbarrand/oot-operator/api/v1alpha1"
)

//go:generate mockgen -source=kernelmapper.go -package=module -destination=mock_kernelmapper.go

type KernelMapper interface {
	FindMappingForKernel(mappings []ootov1alpha1.KernelMapping, kernelVersion string) (*ootov1alpha1.KernelMapping, error)
}

type kernelMapper struct{}

func NewKernelMapper() KernelMapper {
	return &kernelMapper{}
}

// FindMappingForKernel tries to match kernelVersion against mappings. It returns the first mapping that has a Literal
// field equal to kernelVersion or a Regexp field that matches kernelVersion.
func (kernelMapper) FindMappingForKernel(mappings []ootov1alpha1.KernelMapping, kernelVersion string) (*ootov1alpha1.KernelMapping, error) {
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
