package worker

import (
	"context"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
)

//go:generate mockgen -source=imagemounter.go -package=worker -destination=mock_imagemounter.go ImageMounter

type ImageMounter interface {
	MountImage(ctx context.Context, imageName string, cfg *kmmv1beta1.ModuleConfig) (string, error)
}
