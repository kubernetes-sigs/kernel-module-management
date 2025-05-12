package resource

import (
	"context"
	"errors"
	"fmt"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/api"
	"github.com/kubernetes-sigs/kernel-module-management/internal/buildsign"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	"github.com/kubernetes-sigs/kernel-module-management/internal/module"
)

const (
	dockerfileAnnotationKey = "dockerfile"
	dockerfileVolumeName    = "dockerfile"
)

type resourceManager struct {
	client            client.Client
	buildArgOverrider module.BuildArgOverrider
	scheme            *runtime.Scheme
}

func NewResourceManager(client client.Client, buildArgOverrider module.BuildArgOverrider,
	scheme *runtime.Scheme) buildsign.ResourceManager {

	return &resourceManager{
		client:            client,
		buildArgOverrider: buildArgOverrider,
		scheme:            scheme,
	}
}

func (rm *resourceManager) MakeResourceTemplate(ctx context.Context, mld *api.ModuleLoaderData, owner metav1.Object,
	pushImage bool, resourceType kmmv1beta1.BuildOrSignAction) (metav1.Object, error) {

	if resourceType == kmmv1beta1.BuildImage {
		return rm.makeBuildTemplate(ctx, mld, owner, pushImage)
	}
	return rm.makeSignTemplate(ctx, mld, owner, pushImage)
}

func (rm *resourceManager) CreateResource(ctx context.Context, obj metav1.Object) error {

	resource, ok := obj.(*v1.Pod)
	if !ok {
		return errors.New("the resource cannot be converted to the corect resource")
	}
	err := rm.client.Create(ctx, resource)
	if err != nil {
		return err
	}
	return nil
}
func (rm *resourceManager) DeleteResource(ctx context.Context, obj metav1.Object) error {

	resource, ok := obj.(*v1.Pod)
	if !ok {
		return errors.New("the resource cannot be converted to the correct resource")
	}
	opts := []client.DeleteOption{
		client.PropagationPolicy(metav1.DeletePropagationBackground),
	}
	return rm.client.Delete(ctx, resource, opts...)
}

func (rm *resourceManager) GetResourceByKernel(ctx context.Context, name, namespace, targetKernel string,
	resourceType kmmv1beta1.BuildOrSignAction, owner metav1.Object) (metav1.Object, error) {

	matchLabels := moduleKernelLabels(name, targetKernel, resourceType)
	resources, err := rm.getResources(ctx, namespace, matchLabels)
	if err != nil {
		return nil, fmt.Errorf("failed to get module %s, resources by kernel %s: %v", name, targetKernel, err)
	}

	// filter resources by owner, since they could have been created by the preflight
	// when checking that specific module
	moduleOwnedResources := filterResourcesByOwner(resources, owner)
	numFoundResources := len(moduleOwnedResources)
	if numFoundResources == 0 {
		return nil, buildsign.ErrNoMatchingBuildSignResource
	} else if numFoundResources > 1 {
		return nil, fmt.Errorf("expected 0 or 1 %s resources, got %d", resourceType, numFoundResources)
	}

	return &moduleOwnedResources[0], nil
}

// GetBuildSignResourceStatus returns the status of a Resource, whether the latter is in progress or not and
// whether there was an error or not
func (rm *resourceManager) GetResourceStatus(obj metav1.Object) (buildsign.Status, error) {

	resource, ok := obj.(*v1.Pod)
	if !ok {
		return "", errors.New("the existing resource cannot be converted to the corect resource")
	}
	switch resource.Status.Phase {
	case v1.PodSucceeded:
		return buildsign.StatusCompleted, nil
	case v1.PodRunning, v1.PodPending:
		return buildsign.StatusInProgress, nil
	case v1.PodFailed:
		return buildsign.StatusFailed, nil
	default:
		return "", fmt.Errorf("unknown status: %v", resource.Status)
	}
}

func (rm *resourceManager) IsResourceChanged(existingObj metav1.Object, newObj metav1.Object) (bool, error) {

	existingResource, ok := existingObj.(*v1.Pod)
	if !ok {
		return false, errors.New("the existing resource cannot be converted to the corect resource")
	}
	newResource, ok := newObj.(*v1.Pod)
	if !ok {
		return false, errors.New("the new resource cannot be converted to the corect resource")
	}

	existingAnnotations := existingResource.GetAnnotations()
	newAnnotations := newResource.GetAnnotations()
	if existingAnnotations == nil {
		return false, fmt.Errorf("annotations are not present in the existing resource %s", existingResource.Name)
	}
	if existingAnnotations[constants.ResourceHashAnnotation] == newAnnotations[constants.ResourceHashAnnotation] {
		return false, nil
	}
	return true, nil
}

func (rm *resourceManager) GetModuleResources(ctx context.Context, modName, namespace string,
	resourceType kmmv1beta1.BuildOrSignAction, owner metav1.Object) ([]metav1.Object, error) {

	matchLabels := moduleLabels(modName, resourceType)
	resources, err := rm.getResources(ctx, namespace, matchLabels)
	if err != nil {
		return nil, fmt.Errorf("failed to get resources for module %s, namespace %s: %v", modName, namespace, err)
	}

	// filter resources by owner, since they could have been created by the preflight
	// when checking that specific module
	moduleOwnedResources := filterResourcesByOwner(resources, owner)

	moduleOwnedObjects := []metav1.Object{}
	for _, mor := range moduleOwnedResources {
		moduleOwnedObjects = append(moduleOwnedObjects, &mor)
	}
	return moduleOwnedObjects, nil
}
func (rm *resourceManager) HasResourcesCompletedSuccessfully(ctx context.Context, obj metav1.Object) (bool, error) {

	resource, ok := obj.(*v1.Pod)
	if !ok {
		return false, errors.New("the existing resource cannot be converted to the corect resource")
	}

	return resource.Status.Phase == v1.PodSucceeded, nil
}
