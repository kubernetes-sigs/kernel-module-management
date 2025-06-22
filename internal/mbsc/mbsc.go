package mbsc

import (
	"context"
	"fmt"
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"

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
	CreateOrPatch(ctx context.Context, micObj *kmmv1beta1.ModuleImagesConfig,
		moduleImageSpec *kmmv1beta1.ModuleImageSpec, action kmmv1beta1.BuildOrSignAction) error
	GetImageSpec(mbscObj *kmmv1beta1.ModuleBuildSignConfig, image string) *kmmv1beta1.ModuleBuildSignSpec
	SetImageStatus(mbscObj *kmmv1beta1.ModuleBuildSignConfig, image string, action kmmv1beta1.BuildOrSignAction, status kmmv1beta1.BuildOrSignStatus)
	GetImageStatus(mbscObj *kmmv1beta1.ModuleBuildSignConfig, image string, action kmmv1beta1.BuildOrSignAction) kmmv1beta1.BuildOrSignStatus
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

func (m *mbsc) CreateOrPatch(ctx context.Context, micObj *kmmv1beta1.ModuleImagesConfig,
	moduleImageSpec *kmmv1beta1.ModuleImageSpec, action kmmv1beta1.BuildOrSignAction) error {
	mbscObj := &kmmv1beta1.ModuleBuildSignConfig{
		ObjectMeta: metav1.ObjectMeta{Name: micObj.Name, Namespace: micObj.Namespace},
	}

	_, err := controllerutil.CreateOrPatch(ctx, m.client, mbscObj, func() error {
		setModuleImageSpec(mbscObj, moduleImageSpec, action)
		mbscObj.Spec.ImageRepoSecret = micObj.Spec.ImageRepoSecret
		mbscObj.Spec.PushBuiltImage = micObj.Spec.PushBuiltImage
		return controllerutil.SetControllerReference(micObj, mbscObj, m.scheme)
	})
	return err
}

func (m *mbsc) GetImageSpec(mbscObj *kmmv1beta1.ModuleBuildSignConfig, image string) *kmmv1beta1.ModuleBuildSignSpec {
	for _, imageSpec := range mbscObj.Spec.Images {
		if imageSpec.Image == image {
			return &imageSpec
		}
	}
	return nil
}

func (m *mbsc) SetImageStatus(mbscObj *kmmv1beta1.ModuleBuildSignConfig, image string, action kmmv1beta1.BuildOrSignAction, status kmmv1beta1.BuildOrSignStatus) {
	imageState := kmmv1beta1.BuildSignImageState{
		Image:  image,
		Action: action,
		Status: status,
	}
	for i, imageStatus := range mbscObj.Status.Images {
		if imageStatus.Image == image {
			mbscObj.Status.Images[i] = imageState
			return
		}
	}
	mbscObj.Status.Images = append(mbscObj.Status.Images, imageState)
}

func (m *mbsc) GetImageStatus(mbscObj *kmmv1beta1.ModuleBuildSignConfig, image string, action kmmv1beta1.BuildOrSignAction) kmmv1beta1.BuildOrSignStatus {
	for _, imageState := range mbscObj.Status.Images {
		if imageState.Image == image && imageState.Action == action {
			return imageState.Status
		}
	}
	return kmmv1beta1.BuildOrSignStatus("")
}

func setModuleImageSpec(mbscObj *kmmv1beta1.ModuleBuildSignConfig, moduleImageSpec *kmmv1beta1.ModuleImageSpec, action kmmv1beta1.BuildOrSignAction) {
	specEntry := kmmv1beta1.ModuleBuildSignSpec{
		ModuleImageSpec: *moduleImageSpec,
		Action:          action,
	}
	for i, imageSpec := range mbscObj.Spec.Images {
		if imageSpec.Image == moduleImageSpec.Image {
			mbscObj.Spec.Images[i] = specEntry
			return
		}
	}
	mbscObj.Spec.Images = append(mbscObj.Spec.Images, specEntry)
}
