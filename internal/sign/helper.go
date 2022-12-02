package sign

import (
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
)

//go:generate mockgen -source=helper.go -package=sign -destination=mock_helper.go

type Helper interface {
	GetRelevantSign(modSpec kmmv1beta1.ModuleSpec, km kmmv1beta1.KernelMapping) *kmmv1beta1.Sign
}

type helper struct{}

func NewSignerHelper() Helper {
	return &helper{}
}

func (m *helper) GetRelevantSign(modSpec kmmv1beta1.ModuleSpec, km kmmv1beta1.KernelMapping) *kmmv1beta1.Sign {
	if modSpec.ModuleLoader.Container.Sign == nil {
		// km.Sign cannot be nil in case mod.Sign is nil, checked above
		return km.Sign.DeepCopy()
	}

	if km.Sign == nil {
		return modSpec.ModuleLoader.Container.Sign.DeepCopy()
	}

	signConfig := modSpec.ModuleLoader.Container.Sign.DeepCopy()

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

	return signConfig
}
