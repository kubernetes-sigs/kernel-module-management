package rbac

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
)

//go:generate mockgen -source=rbac.go -package=rbac -destination=mock_rbac.go

type RBACCreator interface {
	CreateModuleLoaderServiceAccount(ctx context.Context, mod kmmv1beta1.Module) error
	CreateDevicePluginServiceAccount(ctx context.Context, mod kmmv1beta1.Module) error
}

type rbacCreator struct {
	client client.Client
	scheme *runtime.Scheme
}

func NewCreator(client client.Client, scheme *runtime.Scheme) RBACCreator {
	return &rbacCreator{
		client: client,
		scheme: scheme,
	}
}

func (rc *rbacCreator) CreateModuleLoaderServiceAccount(ctx context.Context, mod kmmv1beta1.Module) error {
	logger := log.FromContext(ctx)

	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GenerateModuleLoaderServiceAccountName(mod),
			Namespace: mod.Namespace,
		},
	}

	opRes, err := controllerutil.CreateOrPatch(ctx, rc.client, sa, func() error {
		return controllerutil.SetControllerReference(&mod, sa, rc.scheme)
	})
	if err != nil {
		return fmt.Errorf("cound not create/patch ServiceAccount: %w", err)
	}
	logger.Info("Created module-loader's ServiceAccount", "name", sa.Name, "result", opRes)

	return nil
}

func (rc *rbacCreator) CreateDevicePluginServiceAccount(ctx context.Context, mod kmmv1beta1.Module) error {
	logger := log.FromContext(ctx)

	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GenerateDevicePluginServiceAccountName(mod),
			Namespace: mod.Namespace,
		},
	}

	opRes, err := controllerutil.CreateOrPatch(ctx, rc.client, sa, func() error {
		return controllerutil.SetControllerReference(&mod, sa, rc.scheme)
	})
	if err != nil {
		return fmt.Errorf("cound not create/patch ServiceAccount: %w", err)
	}
	logger.Info("Created device-plugin's ServiceAccount", "name", sa.Name, "result", opRes)

	return nil
}

func GenerateModuleLoaderServiceAccountName(mod kmmv1beta1.Module) string {
	return mod.Name + "-module-loader"
}

func GenerateDevicePluginServiceAccountName(mod kmmv1beta1.Module) string {
	return mod.Name + "-device-plugin"
}
