package build

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
)

type Status string

const (
	StatusCompleted  = "completed"
	StatusCreated    = "created"
	StatusInProgress = "in progress"
)

type Result struct {
	Requeue bool
	Status  Status
}

//go:generate mockgen -source=manager.go -package=build -destination=mock_manager.go

type Manager interface {
	GarbageCollect(ctx context.Context, modName, namespace string, owner metav1.Object) ([]string, error)

	ShouldSync(
		ctx context.Context,
		mod kmmv1beta1.Module,
		m kmmv1beta1.KernelMapping) (bool, error)

	Sync(
		ctx context.Context,
		mod kmmv1beta1.Module,
		m kmmv1beta1.KernelMapping,
		targetKernel string,
		pushImage bool,
		owner metav1.Object) (Result, error)
}
