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
	ApplyMIC(ctx context.Context, name, ns string, images []kmmv1beta1.ModuleImageSpec,
		imageRepoSecret *v1.LocalObjectReference, owner metav1.Object) error
}

type micImpl struct {
	client client.Client
	scheme *runtime.Scheme
}

func NewModuleImagesConfigAPI(client client.Client, scheme *runtime.Scheme) MIC {
	return &micImpl{
		client: client,
		scheme: scheme,
	}
}

func (mici *micImpl) ApplyMIC(ctx context.Context, name, ns string, images []kmmv1beta1.ModuleImageSpec,
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
