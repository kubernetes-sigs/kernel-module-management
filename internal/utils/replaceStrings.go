package utils

import (
	"fmt"
	"github.com/a8m/envsubst/parse"
	"regexp"
	"strings"
)

const (
	kernelVersionMajorIdx = 0
	kernelVersionMinorIdx = 1
	kernelVersionPatchIdx = 2
)

func KernelComponentsAsEnvVars(kernel string) []string {
	osConfigFieldsList := regexp.MustCompile("[.,-]").Split(kernel, -1)

	envvars := []string{
		"KERNEL_FULL_VERSION=" + kernel,
		"KERNEL_VERSION=" + kernel,
		"KERNEL_XYZ=" + strings.Join(osConfigFieldsList[:kernelVersionPatchIdx+1], "."),
		"KERNEL_X=" + osConfigFieldsList[kernelVersionMajorIdx],
		"KERNEL_Y=" + osConfigFieldsList[kernelVersionMinorIdx],
		"KERNEL_Z=" + osConfigFieldsList[kernelVersionPatchIdx],
	}

	return envvars
}

func ReplaceInTemplates(envvars []string, templates ...string) ([]string, error) {
	parser := parse.New("mapping", envvars, &parse.Restrictions{})

	var replacedStrings []string
	for _, v := range templates {
		resultString, err := parser.Parse(v)
		if err != nil {
			return nil, fmt.Errorf("failed to substitute \"%s\" : %w", v, err)
		}
		replacedStrings = append(replacedStrings, resultString)
	}
	return replacedStrings, nil
}
