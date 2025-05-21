package buildsign

import (
	"context"
	"errors"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/api"
	"github.com/kubernetes-sigs/kernel-module-management/internal/kernel"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

//go:generate mockgen -source=manager.go -package=buildsign -destination=mock_manager.go

type Manager interface {
	GetStatus(ctx context.Context, name, namespace, kernelVersion string,
		action kmmv1beta1.BuildOrSignAction, owner metav1.Object) (kmmv1beta1.BuildOrSignStatus, error)
	Sync(ctx context.Context, mld *api.ModuleLoaderData, pushImage bool, action kmmv1beta1.BuildOrSignAction, owner metav1.Object) error
	GarbageCollect(ctx context.Context, name, namespace string, action kmmv1beta1.BuildOrSignAction, owner metav1.Object) ([]string, error)
}

type manager struct {
	client          client.Client
	resourceManager ResourceManager
}

func NewManager(client client.Client, resourceManager ResourceManager, scheme *runtime.Scheme) Manager {
	return &manager{
		client:          client,
		resourceManager: resourceManager,
	}
}

func (m *manager) GetStatus(ctx context.Context, name, namespace, kernelVersion string,
	action kmmv1beta1.BuildOrSignAction, owner metav1.Object) (kmmv1beta1.BuildOrSignStatus, error) {

	normalizedKernel := kernel.NormalizeVersion(kernelVersion)
	foundResource, err := m.resourceManager.GetResourceByKernel(ctx, name, namespace, normalizedKernel, action, owner)
	if err != nil {
		if !errors.Is(err, ErrNoMatchingBuildSignResource) {
			return kmmv1beta1.BuildOrSignStatus(""), fmt.Errorf("failed to get resource %s/%s, action %s: %v",
				namespace, name, action, err)
		}
		return kmmv1beta1.BuildOrSignStatus(""), nil
	}
	status, err := m.resourceManager.GetResourceStatus(foundResource)
	if err != nil {
		return kmmv1beta1.BuildOrSignStatus(""), fmt.Errorf("failed to get status for the resource %s/%s, action %s: %v",
			foundResource.GetNamespace(), foundResource.GetName(), action, err)
	}
	switch status {
	case StatusCompleted:
		return kmmv1beta1.ActionSuccess, nil
	case StatusFailed:
		return kmmv1beta1.ActionFailure, nil
	}

	// any other status means the resource is still not finished, returning empty status
	return kmmv1beta1.BuildOrSignStatus(""), nil
}

func (m *manager) Sync(ctx context.Context, mld *api.ModuleLoaderData, pushImage bool, action kmmv1beta1.BuildOrSignAction,
	owner metav1.Object) error {

	logger := log.FromContext(ctx)
	var (
		resourceTemplate metav1.Object
		err              error
	)

	logger.Info("Building or Signing in-cluster")

	resourceTemplate, err = m.resourceManager.MakeResourceTemplate(ctx, mld, owner, pushImage, action)
	if err != nil {
		return fmt.Errorf("could not make the resource's template template: %v", err)
	}

	resource, err := m.resourceManager.GetResourceByKernel(ctx, mld.Name, mld.Namespace, mld.KernelNormalizedVersion,
		action, owner)

	if err != nil {
		if !errors.Is(err, ErrNoMatchingBuildSignResource) {
			return fmt.Errorf("error getting the %s resource: %v", action, err)
		}

		logger.Info("Creating resource")
		err = m.resourceManager.CreateResource(ctx, resourceTemplate)
		if err != nil {
			return fmt.Errorf("could not create resource: %v", err)
		}

		return nil
	}

	changed, err := m.resourceManager.IsResourceChanged(resource, resourceTemplate)
	if err != nil {
		return fmt.Errorf("could not determine if the resource has changed: %v", err)
	}

	if changed {
		logger.Info("The module's spec has been changed, deleting the current resource so a new one can be created",
			"name", resource.GetName(), "action", action)
		err = m.resourceManager.DeleteResource(ctx, resource)
		if err != nil {
			logger.Info(utils.WarnString(fmt.Sprintf("failed to delete %s resource %s: %v", action, resource.GetName(), err)))
		}
	}

	return nil
}

func (m *manager) GarbageCollect(ctx context.Context, name, namespace string, action kmmv1beta1.BuildOrSignAction,
	owner metav1.Object) ([]string, error) {

	resources, err := m.resourceManager.GetModuleResources(ctx, name, namespace, action, owner)
	if err != nil {
		return nil, fmt.Errorf("failed to get %s resources for mbsc %s/%s: %v", action, namespace, name, err)
	}

	logger := log.FromContext(ctx)
	errs := make([]error, 0, len(resources))
	deleteResourceNames := make([]string, 0, len(resources))
	for _, obj := range resources {
		isPhaseSucceed, err := m.resourceManager.HasResourcesCompletedSuccessfully(ctx, obj)
		if err != nil {
			return nil, fmt.Errorf("failed to check if resource %s succeeded: %v", obj.GetName(), err)
		}
		if isPhaseSucceed {
			err = m.resourceManager.DeleteResource(ctx, obj)
			errs = append(errs, err)
			if err != nil {
				logger.Info(utils.WarnString(
					fmt.Sprintf("failed to delete %s resource %s in garbage collection: %v", action, obj.GetName(), err),
				))
				continue
			}
			deleteResourceNames = append(deleteResourceNames, obj.GetName())
		}
	}
	return deleteResourceNames, errors.Join(errs...)
}
