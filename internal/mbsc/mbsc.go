package mbsc

import (
	"context"
	"fmt"
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"

	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

//go:generate mockgen -source=mbsc.go -package=mbsc -destination=mock_mbsc.go

type MBSC interface {
	Get(ctx context.Context, name, namespace string) (*kmmv1beta1.ModuleBuildSignConfig, error)
	CreateOrPatch(ctx context.Context, name string, namespace string, moduleImageSpec *kmmv1beta1.ModuleImageSpec,
		imageRepoSecret *v1.LocalObjectReference, owner metav1.Object) error
}

type mbsc struct {
	client client.Client
	scheme *runtime.Scheme
}

func New(client client.Client, scheme *runtime.Scheme) MBSC {
	return &mbsc{
		client: client,
		scheme: scheme,
	}
}

func (m *mbsc) Get(ctx context.Context, name, namespace string) (*kmmv1beta1.ModuleBuildSignConfig, error) {
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

func (m *mbsc) CreateOrPatch(ctx context.Context,
	name string,
	namespace string,
	moduleImageSpec *kmmv1beta1.ModuleImageSpec,
	imageRepoSecret *v1.LocalObjectReference,
	owner metav1.Object) error {
	mbscObj := &kmmv1beta1.ModuleBuildSignConfig{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
	}

	_, err := controllerutil.CreateOrPatch(ctx, m.client, mbscObj, func() error {
		setModuleImageSpec(mbscObj, moduleImageSpec)
		mbscObj.Spec.ImageRepoSecret = imageRepoSecret
		return controllerutil.SetOwnerReference(owner, mbscObj, m.scheme)
	})
	return err
}

func setModuleImageSpec(mbscObj *kmmv1beta1.ModuleBuildSignConfig, moduleImageSpec *kmmv1beta1.ModuleImageSpec) {
	for i, imageSpec := range mbscObj.Spec.Images {
		if imageSpec.Image == moduleImageSpec.Image {
			mbscObj.Spec.Images[i] = *moduleImageSpec
			return
		}
	}
	mbscObj.Spec.Images = append(mbscObj.Spec.Images, *moduleImageSpec)
}
