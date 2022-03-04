package module

import (
	"errors"
	"fmt"
	"regexp"

	ootov1beta1 "github.com/qbarrand/oot-operator/api/v1beta1"
)

//go:generate mockgen -source=kernelmapper.go -package=module -destination=mock_kernelmapper.go

type KernelMapper interface {
	FindImageForKernel(mappings []ootov1beta1.KernelMapping, kernelVersion string) (string, error)
}

type kernelMapper struct{}

func NewKernelMapper() KernelMapper {
	return &kernelMapper{}
}

// FindImageForKernel tries to match kernelVersion against mappings. It returns the contents of ContainerImage for the
// first mapping that has a Literal field equal to kernelVersion or a Regexp field that matches kernelVersion.
func (kernelMapper) FindImageForKernel(mappings []ootov1beta1.KernelMapping, kernelVersion string) (string, error) {
	for _, m := range mappings {
		if m.Literal != "" && m.Literal == kernelVersion {
			return m.ContainerImage, nil
		}

		if m.Regexp == "" {
			continue
		}

		if matches, err := regexp.MatchString(m.Regexp, kernelVersion); err != nil {
			return "", fmt.Errorf("could not match regexp %q against kernel %q: %v", m.Regexp, kernelVersion, err)
		} else if matches {
			return m.ContainerImage, nil
		}
	}

	return "", errors.New("no suitable mapping found")
}
