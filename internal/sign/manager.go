package sign

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kubernetes-sigs/kernel-module-management/internal/api"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
)

//go:generate mockgen -source=manager.go -package=sign -destination=mock_manager.go

type SignManager interface {
	GarbageCollect(ctx context.Context, modName, namespace string, owner metav1.Object) ([]string, error)

	ShouldSync(ctx context.Context, mld *api.ModuleLoaderData) (bool, error)

	Sync(
		ctx context.Context,
		mld *api.ModuleLoaderData,
		imageToSign string,
		pushImage bool,
		owner metav1.Object) (utils.Status, error)
}
