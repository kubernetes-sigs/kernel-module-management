package mic

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
	"sigs.k8s.io/controller-runtime/pkg/log"
)

//go:generate mockgen -source=mic.go -package=mic -destination=mock_mic.go

type ModuleImagesConfigAPI interface {
	HandleModuleImagesConfig(ctx context.Context, name, ns string, images []kmmv1beta1.ModuleImageSpec,
		imageRepoSecret *v1.LocalObjectReference, owner metav1.Object) error
}

type moduleImagesConfigAPI struct {
	client client.Client
	scheme *runtime.Scheme
}

func NewModuleImagesConfigAPI(client client.Client, scheme *runtime.Scheme) ModuleImagesConfigAPI {
	return &moduleImagesConfigAPI{
		client: client,
		scheme: scheme,
	}
}

func (mica *moduleImagesConfigAPI) HandleModuleImagesConfig(ctx context.Context, name, ns string,
	images []kmmv1beta1.ModuleImageSpec, imageRepoSecret *v1.LocalObjectReference, owner metav1.Object) error {

	logger := log.FromContext(ctx)

	mic := &kmmv1beta1.ModuleImagesConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
	}

	opRes, err := controllerutil.CreateOrPatch(ctx, mica.client, mic, func() error {

		gen, err := mica.getGeneration(ctx, name, ns)
		if err != nil {
			return fmt.Errorf("failed to get %s/%s MIC's generation: %v", ns, name, err)
		}

		mic.Spec = kmmv1beta1.ModuleImagesConfigSpec{
			Images:          images,
			ImageRepoSecret: imageRepoSecret,
			Generation:      gen,
		}

		return controllerutil.SetOwnerReference(owner, mic, mica.scheme)
	})
	if err != nil {
		return fmt.Errorf("failed to create or patch %s/%s: %v", ns, name, err)
	}

	logger.Info("Applied MIC", "name", name, "namespace", ns, "result", opRes)

	return nil
}

func (mica *moduleImagesConfigAPI) getGeneration(ctx context.Context, name, ns string) (int64, error) {

	mic := kmmv1beta1.ModuleImagesConfig{}
	err := mica.client.Get(ctx, types.NamespacedName{Name: name, Namespace: ns}, &mic)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("failed to get ModuleImagesConfig %s/%s: %v", ns, name, err)
	}

	return mic.Spec.Generation, nil
}
