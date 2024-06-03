package sign

import (
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/kernel"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
)

//go:generate mockgen -source=helper.go -package=sign -destination=mock_helper.go

type Helper interface {
	GetRelevantSign(moduleSign *kmmv1beta1.Sign, mappingSign *kmmv1beta1.Sign, kernel string) (*kmmv1beta1.Sign, error)
}

type helper struct {
}

func NewSignerHelper() Helper {
	return &helper{}
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
