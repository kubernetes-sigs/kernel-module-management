package module

import (
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
)

func GetRelevantTLSOptions(mod *kmmv1beta1.Module, km *kmmv1beta1.KernelMapping) *kmmv1beta1.TLSOptions {
	if km.RegistryTLS != nil {
		return km.RegistryTLS
	}
	return mod.Spec.ModuleLoader.Container.RegistryTLS
}
