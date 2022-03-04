package test

import (
	ootov1beta1 "github.com/qbarrand/oot-operator/api/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
)

func TestScheme() (*runtime.Scheme, error) {
	s := runtime.NewScheme()

	if err := scheme.AddToScheme(s); err != nil {
		return nil, err
	}

	if err := ootov1beta1.AddToScheme(s); err != nil {
		return nil, err
	}

	return s, nil
}
