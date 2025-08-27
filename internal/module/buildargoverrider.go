package module

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/util/sets"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
)

//go:generate mockgen -source=buildargoverrider.go -package=module -destination=mock_buildargoverrider.go

type BuildArgOverrider interface {
	ApplyBuildArgOverrides(args []kmmv1beta1.BuildArg, overrides ...kmmv1beta1.BuildArg) []kmmv1beta1.BuildArg
	FormatBuildArgs(buildArgs []kmmv1beta1.BuildArg) string
}

type buildArgOverrider struct{}

func NewBuildArgOverrider() BuildArgOverrider {
	return &buildArgOverrider{}
}

func (c *buildArgOverrider) ApplyBuildArgOverrides(args []kmmv1beta1.BuildArg, overrides ...kmmv1beta1.BuildArg) []kmmv1beta1.BuildArg {
	overridesMap := make(map[string]kmmv1beta1.BuildArg, len(overrides))

	for _, o := range overrides {
		overridesMap[o.Name] = o
	}

	unusedOverrides := sets.StringKeySet(overridesMap)

	for i := 0; i < len(args); i++ {
		argName := args[i].Name

		if o, ok := overridesMap[argName]; ok {
			args[i] = o
			unusedOverrides.Delete(argName)
		}
	}

	for _, overrideName := range unusedOverrides.List() {
		args = append(args, overridesMap[overrideName])
	}

	return args
}

func (c *buildArgOverrider) FormatBuildArgs(buildArgs []kmmv1beta1.BuildArg) string {
	args := []string{}
	for _, ba := range buildArgs {
		args = append(args, "--build-arg", fmt.Sprintf("%s=%s", ba.Name, ba.Value))
	}
	return strings.Join(args, " ")
}
