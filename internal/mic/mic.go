package mic

import (
	"context"
	"fmt"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

//go:generate mockgen -source=mic.go -package=mic -destination=mock_mic.go

type MIC interface {
	CreateOrPatch(ctx context.Context, name, ns string, images []kmmv1beta1.ModuleImageSpec,
		imageRepoSecret *v1.LocalObjectReference, pullPolicy v1.PullPolicy, pushBuiltImage bool,
		imageRebuildTriggerGeneration *int, owner metav1.Object) error
	Get(ctx context.Context, name, ns string) (*kmmv1beta1.ModuleImagesConfig, error)
	GetModuleImageSpec(micObj *kmmv1beta1.ModuleImagesConfig, image string) *kmmv1beta1.ModuleImageSpec
	SetImageStatus(micObj *kmmv1beta1.ModuleImagesConfig, image string, status kmmv1beta1.ImageState)
	GetImageState(micObj *kmmv1beta1.ModuleImagesConfig, image string) kmmv1beta1.ImageState
	DoAllImagesExist(micObj *kmmv1beta1.ModuleImagesConfig) bool
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
	imageRepoSecret *v1.LocalObjectReference, pullPolicy v1.PullPolicy, pushBuiltImage bool,
	imageRebuildTriggerGeneration *int, owner metav1.Object) error {

	logger := log.FromContext(ctx)

	mic := &kmmv1beta1.ModuleImagesConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
	}

	images = filterDuplicateImages(images)

	opRes, err := controllerutil.CreateOrPatch(ctx, mici.client, mic, func() error {

		mic.Spec = kmmv1beta1.ModuleImagesConfigSpec{
			Images:                        images,
			ImageRepoSecret:               imageRepoSecret,
			ImagePullPolicy:               pullPolicy,
			PushBuiltImage:                pushBuiltImage,
			ImageRebuildTriggerGeneration: imageRebuildTriggerGeneration,
		}

		return controllerutil.SetControllerReference(owner, mic, mici.scheme)
	})
	if err != nil {
		return fmt.Errorf("failed to create or patch %s/%s: %v", ns, name, err)
	}

	logger.Info("Applied MIC", "name", name, "namespace", ns, "result", opRes)

	return nil
}

func (mici *micImpl) Get(ctx context.Context, name, ns string) (*kmmv1beta1.ModuleImagesConfig, error) {

	var micObj kmmv1beta1.ModuleImagesConfig
	if err := mici.client.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, &micObj); err != nil {
		return nil, fmt.Errorf("could not get ModuleImagesConfig %s: %v", name, err)
	}

	return &micObj, nil
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

func (mici *micImpl) DoAllImagesExist(micObj *kmmv1beta1.ModuleImagesConfig) bool {

	imagesStates := map[string]kmmv1beta1.ImageState{}
	for _, img := range micObj.Status.ImagesStates {
		imagesStates[img.Image] = img.Status
	}

	for _, img := range micObj.Spec.Images {
		// Status isn't "ImageExists" or status isn't set at all
		if imagesStates[img.Image] != kmmv1beta1.ImageExists {
			return false
		}
	}

	return true
}

func filterDuplicateImages(images []kmmv1beta1.ModuleImageSpec) []kmmv1beta1.ModuleImageSpec {
	imagesSet := sets.New[string]()
	filteredImages := make([]kmmv1beta1.ModuleImageSpec, 0, len(images))
	for _, image := range images {
		if !imagesSet.Has(image.Image) {
			imagesSet.Insert(image.Image)
			filteredImages = append(filteredImages, image)
		}
	}
	return filteredImages
}
