package imgbuild

import (
	"context"

	"github.com/kubernetes-sigs/kernel-module-management/internal/api"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type Status string

const (
	StatusCompleted  Status = "completed"
	StatusCreated    Status = "created"
	StatusInProgress Status = "in progress"
	StatusFailed     Status = "failed"
)

//go:generate mockgen -source=types.go -package=imgbuild -destination=mock_interfaces.go

type JobMaker interface {
	MakeJob(ctx context.Context, mld *api.ModuleLoaderData, owner metav1.Object, pushImage bool) (*batchv1.Job, error)
}

type JobManager interface {
	GarbageCollect(ctx context.Context, modName, namespace string, owner metav1.Object) ([]string, error)
	ShouldSync(ctx context.Context, mld *api.ModuleLoaderData) (bool, error)
	Sync(ctx context.Context, mld *api.ModuleLoaderData, pushImage bool, owner metav1.Object) (Status, error)
}
