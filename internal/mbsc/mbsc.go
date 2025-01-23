package mbsc

import (
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
)

//go:generate mockgen -source=mbsc.go -package=mbsc -destination=mock_mbsc.go

type MBSC interface {
	SetModuleImageSpec(mbscObj *kmmv1beta1.ModuleBuildSignConfig, moduleImageSpec *kmmv1beta1.ModuleImageSpec)
}

type mbsc struct{}

func NewMBSC() MBSC {
	return &mbsc{}
}

func (m *mbsc) SetModuleImageSpec(mbscObj *kmmv1beta1.ModuleBuildSignConfig, moduleImageSpec *kmmv1beta1.ModuleImageSpec) {
	for i, imageSpec := range mbscObj.Spec.Images {
		if imageSpec.Image == moduleImageSpec.Image {
			mbscObj.Spec.Images[i] = *moduleImageSpec
			return
		}
	}
	mbscObj.Spec.Images = append(mbscObj.Spec.Images, *moduleImageSpec)
}
