package mbsc

import (
	"context"
	"fmt"
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

//go:generate mockgen -source=mbsc.go -package=mbsc -destination=mock_mbsc.go

type MBSC interface {
	SetModuleImageSpec(mbscObj *kmmv1beta1.ModuleBuildSignConfig, moduleImageSpec *kmmv1beta1.ModuleImageSpec)
	GetMBSC(ctx context.Context, name, namespace string) (*kmmv1beta1.ModuleBuildSignConfig, error)
}

type mbsc struct {
	client client.Client
}

func NewMBSC(client client.Client) MBSC {
	return &mbsc{
		client: client,
	}
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

func (m *mbsc) GetMBSC(ctx context.Context, name, namespace string) (*kmmv1beta1.ModuleBuildSignConfig, error) {
	mbsc := kmmv1beta1.ModuleBuildSignConfig{}
	err := m.client.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, &mbsc)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get ModuleBuildSignConfig object %s/%s: %v", namespace, name, err)
	}
	return &mbsc, nil
}
