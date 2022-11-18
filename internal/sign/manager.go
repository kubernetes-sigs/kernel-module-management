package sign

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
)

//go:generate mockgen -source=manager.go -package=sign -destination=mock_manager.go

type SignManager interface {
	ShouldSync(
		ctx context.Context,
		mod kmmv1beta1.Module,
		m kmmv1beta1.KernelMapping) (bool, error)

	Sync(
		ctx context.Context,
		mod kmmv1beta1.Module,
		m kmmv1beta1.KernelMapping,
		targetKernel string,
		imageToSign string,
		pushImage bool,
		owner metav1.Object) (utils.Result, error)
}
