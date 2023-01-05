package sign

import (
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
)

//go:generate mockgen -source=helper.go -package=sign -destination=mock_helper.go

type Helper interface {
	GetRelevantSign(modSpec kmmv1beta1.ModuleSpec, km kmmv1beta1.KernelMapping, kernel string) (*kmmv1beta1.Sign, error)
}

type helper struct {
}

func NewSignerHelper() Helper {
	return &helper{}
}

func (m *helper) GetRelevantSign(modSpec kmmv1beta1.ModuleSpec, km kmmv1beta1.KernelMapping, kernel string) (*kmmv1beta1.Sign, error) {
	var signConfig *kmmv1beta1.Sign
	if modSpec.ModuleLoader.Container.Sign == nil {
		// km.Sign cannot be nil in case mod.Sign is nil, checked above
		signConfig = km.Sign.DeepCopy()
	} else if km.Sign == nil {
		signConfig = modSpec.ModuleLoader.Container.Sign.DeepCopy()
	} else {
		signConfig = modSpec.ModuleLoader.Container.Sign.DeepCopy()

		if km.Sign.UnsignedImage != "" {
			signConfig.UnsignedImage = km.Sign.UnsignedImage
		}

		if km.Sign.KeySecret != nil {
			signConfig.KeySecret = km.Sign.KeySecret
		}
		if km.Sign.CertSecret != nil {
			signConfig.CertSecret = km.Sign.CertSecret
		}
		//append (not overwrite) any files in the km to the defaults
		signConfig.FilesToSign = append(signConfig.FilesToSign, km.Sign.FilesToSign...)
	}

	osConfigEnvVars := utils.KernelComponentsAsEnvVars(kernel)
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
