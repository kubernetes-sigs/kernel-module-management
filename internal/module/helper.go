package module

import (
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
)

func GetRelevantPullOptions(mod *kmmv1beta1.Module, km *kmmv1beta1.KernelMapping) *kmmv1beta1.PullOptions {
	if km.Pull != nil {
		return km.Pull
	}
	return mod.Spec.ModuleLoader.Container.Pull
}
