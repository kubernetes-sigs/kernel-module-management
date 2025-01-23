package mbsc

import (
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	v1 "k8s.io/api/core/v1"
)

//go:generate mockgen -source=helper.go -package=mbsc -destination=mock_helper.go

type Helper interface {
	SetModuleImageSpec(mbscObj *kmmv1beta1.ModuleBuildSignConfig, imageSpec *kmmv1beta1.ModuleImageSpec, repoSecret *v1.LocalObjectReference)
}

type helper struct {
}

func NewHelper() Helper {
	return &helper{}
}

func (h *helper) SetModuleImageSpec(mbscObj *kmmv1beta1.ModuleBuildSignConfig, micImageSpec *kmmv1beta1.ModuleImageSpec, repoSecret *v1.LocalObjectReference) {
	mbscObj.Spec.ImageRepoSecret = repoSecret
	for i, imageSpec := range mbscObj.Spec.Images {
		if imageSpec.Image == micImageSpec.Image {
			mbscObj.Spec.Images[i] = *micImageSpec
			return
		}
	}
	mbscObj.Spec.Images = append(mbscObj.Spec.Images, *micImageSpec)
}
