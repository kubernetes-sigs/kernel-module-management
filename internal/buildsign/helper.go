package buildsign

import (
	"k8s.io/apimachinery/pkg/util/sets"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/kernel"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
)

//go:generate mockgen -source=helper.go -package=buildsign -destination=mock_helper.go

type Helper interface {
	ApplyBuildArgOverrides(args []kmmv1beta1.BuildArg, overrides ...kmmv1beta1.BuildArg) []kmmv1beta1.BuildArg
	GetRelevantBuild(moduleBuild *kmmv1beta1.Build, mappingBuild *kmmv1beta1.Build) *kmmv1beta1.Build
	GetRelevantSign(moduleSign *kmmv1beta1.Sign, mappingSign *kmmv1beta1.Sign, kernel string) (*kmmv1beta1.Sign, error)
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

func (m *helper) GetRelevantBuild(moduleBuild *kmmv1beta1.Build, mappingBuild *kmmv1beta1.Build) *kmmv1beta1.Build {
	if moduleBuild == nil {
		return mappingBuild.DeepCopy()
	}

	if mappingBuild == nil {
		return moduleBuild.DeepCopy()
	}

	buildConfig := moduleBuild.DeepCopy()
	if mappingBuild.DockerfileConfigMap != nil {
		buildConfig.DockerfileConfigMap = mappingBuild.DockerfileConfigMap
	}

	buildConfig.BuildArgs = m.ApplyBuildArgOverrides(buildConfig.BuildArgs, mappingBuild.BuildArgs...)

	buildConfig.Secrets = append(buildConfig.Secrets, mappingBuild.Secrets...)
	return buildConfig
}

func (m *helper) GetRelevantSign(moduleSign *kmmv1beta1.Sign, mappingSign *kmmv1beta1.Sign, kernelVersion string) (*kmmv1beta1.Sign, error) {
	var signConfig *kmmv1beta1.Sign
	if moduleSign == nil {
		// km.Sign cannot be nil in case mod.Sign is nil, checked above
		signConfig = mappingSign.DeepCopy()
	} else if mappingSign == nil {
		signConfig = moduleSign.DeepCopy()
	} else {
		signConfig = moduleSign.DeepCopy()

		if mappingSign.UnsignedImage != "" {
			signConfig.UnsignedImage = mappingSign.UnsignedImage
		}

		if mappingSign.KeySecret != nil {
			signConfig.KeySecret = mappingSign.KeySecret
		}
		if mappingSign.CertSecret != nil {
			signConfig.CertSecret = mappingSign.CertSecret
		}
		//append (not overwrite) any files in the km to the defaults
		signConfig.FilesToSign = append(signConfig.FilesToSign, mappingSign.FilesToSign...)
	}

	osConfigEnvVars := utils.KernelComponentsAsEnvVars(
		kernel.NormalizeVersion(kernelVersion),
	)
	unsignedImage, err := utils.ReplaceInTemplates(osConfigEnvVars, signConfig.UnsignedImage)
	if err != nil {
		return nil, err
	}
	signConfig.UnsignedImage = unsignedImage[0]
	filesToSign, err := utils.ReplaceInTemplates(osConfigEnvVars, signConfig.FilesToSign...)
	if err != nil {
		return nil, err
	}
	signConfig.FilesToSign = filesToSign

	return signConfig, nil
}
