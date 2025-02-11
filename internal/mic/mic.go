package mic

import (
	"context"
	"fmt"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

//go:generate mockgen -source=mic.go -package=mic -destination=mock_mic.go

type MIC interface {
	CreateOrPatch(ctx context.Context, name, ns string, images []kmmv1beta1.ModuleImageSpec,
		imageRepoSecret *v1.LocalObjectReference, owner metav1.Object) error
	GetModuleImageSpec(micObj *kmmv1beta1.ModuleImagesConfig, image string) *kmmv1beta1.ModuleImageSpec
	SetImageStatus(micObj *kmmv1beta1.ModuleImagesConfig, image string, status kmmv1beta1.ImageState)
	GetImageState(micObj *kmmv1beta1.ModuleImagesConfig, image string) kmmv1beta1.ImageState
}

type micImpl struct {
	client client.Client
	scheme *runtime.Scheme
}

func New(client client.Client, scheme *runtime.Scheme) MIC {
	return &micImpl{
		client: client,
		scheme: scheme,
	}
}

func (mici *micImpl) CreateOrPatch(ctx context.Context, name, ns string, images []kmmv1beta1.ModuleImageSpec,
	imageRepoSecret *v1.LocalObjectReference, owner metav1.Object) error {

	logger := log.FromContext(ctx)

	mic := &kmmv1beta1.ModuleImagesConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
	}

	opRes, err := controllerutil.CreateOrPatch(ctx, mici.client, mic, func() error {

		mic.Spec = kmmv1beta1.ModuleImagesConfigSpec{
			Images:          images,
			ImageRepoSecret: imageRepoSecret,
		}

		return controllerutil.SetOwnerReference(owner, mic, mici.scheme)
	})
	if err != nil {
		return fmt.Errorf("failed to create or patch %s/%s: %v", ns, name, err)
	}

	logger.Info("Applied MIC", "name", name, "namespace", ns, "result", opRes)

	return nil
}

func (mici *micImpl) GetModuleImageSpec(micObj *kmmv1beta1.ModuleImagesConfig, image string) *kmmv1beta1.ModuleImageSpec {
	for _, imageSpec := range micObj.Spec.Images {
		if imageSpec.Image == image {
			return &imageSpec
		}
	}
	return nil
}

func (mici *micImpl) SetImageStatus(micObj *kmmv1beta1.ModuleImagesConfig, image string, status kmmv1beta1.ImageState) {
	imageState := kmmv1beta1.ModuleImageState{
		Image:  image,
		Status: status,
	}
	for i, imageStatus := range micObj.Status.ImagesStates {
		if imageStatus.Image == image {
			micObj.Status.ImagesStates[i] = imageState
			return
		}
	}
	micObj.Status.ImagesStates = append(micObj.Status.ImagesStates, imageState)
}

func (mici *micImpl) GetImageState(micObj *kmmv1beta1.ModuleImagesConfig, image string) kmmv1beta1.ImageState {
	for _, imageState := range micObj.Status.ImagesStates {
		if imageState.Image == image {
			return imageState.Status
		}
	}
	return ""
}
