package buildsign

import (
	"context"
	"errors"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/api"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type Status string

const (
	StatusCompleted  Status = "completed"
	StatusCreated    Status = "created"
	StatusInProgress Status = "in progress"
	StatusFailed     Status = "failed"
)

var ErrNoMatchingBuildSignResource = errors.New("no matching build or sign resource")

//go:generate mockgen -source=resourcemanager.go -package=buildsign -destination=mock_resourcemanager.go

type ResourceManager interface {
	MakeResourceTemplate(ctx context.Context, mld *api.ModuleLoaderData, owner metav1.Object, pushImage bool,
		resourceType kmmv1beta1.BuildOrSignAction) (metav1.Object, error)
	CreateResource(ctx context.Context, template metav1.Object) error
	DeleteResource(ctx context.Context, obj metav1.Object) error
	GetResourceByKernel(ctx context.Context, name, namespace, targetKernel string, resourceType kmmv1beta1.BuildOrSignAction,
		owner metav1.Object) (metav1.Object, error)
	GetResourceStatus(obj metav1.Object) (Status, error)
	IsResourceChanged(existingObj metav1.Object, newObj metav1.Object) (bool, error)
	GetModuleResources(ctx context.Context, modName, namespace string, resourceType kmmv1beta1.BuildOrSignAction,
		owner metav1.Object) ([]metav1.Object, error)
	HasResourcesCompletedSuccessfully(ctx context.Context, obj metav1.Object) (bool, error)
}
