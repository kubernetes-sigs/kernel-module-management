package buildsign

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/api"
)

//go:generate mockgen -source=manager.go -package=buildsign -destination=mock_manager.go

type Manager interface {
	GetStatus(ctx context.Context, name, namespace, kernelVersion string,
		action kmmv1beta1.BuildOrSignAction, owner metav1.Object) (kmmv1beta1.BuildOrSignStatus, error)
	Sync(ctx context.Context, mld *api.ModuleLoaderData, pushImage bool, action kmmv1beta1.BuildOrSignAction, owner metav1.Object) error
	GarbageCollect(ctx context.Context, name, namespace string, action kmmv1beta1.BuildOrSignAction, owner metav1.Object) ([]string, error)
}
