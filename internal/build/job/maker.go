package job

import (
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	batchv1 "k8s.io/api/batch/v1"
)

//go:generate mockgen -source=maker.go -package=job -destination=mock_maker.go

type Maker interface {
	MakeJob(mod kmmv1beta1.Module, buildConfig *kmmv1beta1.Build, targetKernel, previousImage string, containerImage string, pushImage bool) (*batchv1.Job, error)
	GetName() string
	ShouldRun(mod *kmmv1beta1.Module, km *kmmv1beta1.KernelMapping) bool
}
