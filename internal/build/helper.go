package build

import (
	"github.com/qbarrand/oot-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/util/sets"

	ootov1alpha1 "github.com/qbarrand/oot-operator/api/v1alpha1"
)

//go:generate mockgen -source=helper.go -package=build -destination=mock_helper.go

type Helper interface {
	ApplyBuildArgOverrides(args []v1alpha1.BuildArg, overrides ...v1alpha1.BuildArg) []v1alpha1.BuildArg
	GetRelevantBuild(mod ootov1alpha1.Module, km ootov1alpha1.KernelMapping) *ootov1alpha1.Build
}

type helper struct{}

func NewHelper() Helper {
	return &helper{}
}

func (m *helper) ApplyBuildArgOverrides(args []v1alpha1.BuildArg, overrides ...v1alpha1.BuildArg) []v1alpha1.BuildArg {
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

func (m *helper) GetRelevantBuild(mod ootov1alpha1.Module, km ootov1alpha1.KernelMapping) *ootov1alpha1.Build {
	if mod.Spec.Build == nil {
		// km.Build cannot be nil in case mod.Build is nil, checked above
		return km.Build.DeepCopy()
	}

	if km.Build == nil {
		return mod.Spec.Build.DeepCopy()
	}

	buildConfig := mod.Spec.Build.DeepCopy()
	if km.Build.Dockerfile != "" {
		buildConfig.Dockerfile = km.Build.Dockerfile
	}

	buildConfig.BuildArgs = m.ApplyBuildArgOverrides(buildConfig.BuildArgs, km.Build.BuildArgs...)

	// [TODO] once MGMT-10832 is consolidated, this code must be revisited. We will decide which
	// secret and how to use, and if we need to take care of repeated secrets names
	buildConfig.Secrets = append(buildConfig.Secrets, km.Build.Secrets...)
	return buildConfig
}
