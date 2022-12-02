package build

import (
	"k8s.io/apimachinery/pkg/util/sets"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
)

//go:generate mockgen -source=helper.go -package=build -destination=mock_helper.go

type Helper interface {
	ApplyBuildArgOverrides(args []kmmv1beta1.BuildArg, overrides ...kmmv1beta1.BuildArg) []kmmv1beta1.BuildArg
	GetRelevantBuild(modSpec kmmv1beta1.ModuleSpec, km kmmv1beta1.KernelMapping) *kmmv1beta1.Build
}

type helper struct{}

func NewHelper() Helper {
	return &helper{}
}

func (m *helper) ApplyBuildArgOverrides(args []kmmv1beta1.BuildArg, overrides ...kmmv1beta1.BuildArg) []kmmv1beta1.BuildArg {
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

func (m *helper) GetRelevantBuild(modSpec kmmv1beta1.ModuleSpec, km kmmv1beta1.KernelMapping) *kmmv1beta1.Build {
	if modSpec.ModuleLoader.Container.Build == nil {
		return km.Build.DeepCopy()
	}

	if km.Build == nil {
		return modSpec.ModuleLoader.Container.Build.DeepCopy()
	}

	buildConfig := modSpec.ModuleLoader.Container.Build.DeepCopy()
	buildConfig.DockerfileConfigMap = km.Build.DockerfileConfigMap

	buildConfig.BuildArgs = m.ApplyBuildArgOverrides(buildConfig.BuildArgs, km.Build.BuildArgs...)

	// [TODO] once MGMT-10832 is consolidated, this code must be revisited. We will decide which
	// secret and how to use, and if we need to take care of repeated secrets names
	buildConfig.Secrets = append(buildConfig.Secrets, km.Build.Secrets...)
	return buildConfig
}
