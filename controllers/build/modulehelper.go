package build

import (
	"github.com/qbarrand/oot-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/util/sets"
)

//go:generate mockgen -source=modulehelper.go -package=build -destination=mock_modulehelper.go

type ModuleHelper interface {
	ApplyBuildArgOverrides(args []v1alpha1.BuildArg, overrides ...v1alpha1.BuildArg) []v1alpha1.BuildArg
}

type moduleHelper struct{}

func NewModuleHelper() ModuleHelper {
	return moduleHelper{}
}

func (moduleHelper) ApplyBuildArgOverrides(args []v1alpha1.BuildArg, overrides ...v1alpha1.BuildArg) []v1alpha1.BuildArg {
	overridesMap := make(map[string]v1alpha1.BuildArg, len(overrides))

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
