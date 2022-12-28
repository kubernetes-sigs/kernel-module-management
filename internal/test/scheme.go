package test

import (
	hubv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api-hub/v1beta1"
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workv1 "open-cluster-management.io/api/work/v1"
)

func TestScheme() (*runtime.Scheme, error) {
	s := runtime.NewScheme()

	funcs := []func(s *runtime.Scheme) error{
		scheme.AddToScheme,
		kmmv1beta1.AddToScheme,
		hubv1beta1.AddToScheme,
		clusterv1.Install,
		workv1.Install,
	}

	for _, f := range funcs {
		if err := f(s); err != nil {
			return nil, err
		}
	}

	return s, nil
}
