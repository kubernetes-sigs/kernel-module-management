package build

import (
	"context"

	ootov1alpha1 "github.com/qbarrand/oot-operator/api/v1alpha1"
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
	Sync(ctx context.Context, mod ootov1alpha1.Module, m ootov1alpha1.KernelMapping, targetKernel string) (Result, error)
}
