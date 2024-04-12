package utils

import (
	"context"
	"errors"
	"fmt"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
)

type Status string

const (
	PodTypeBuild = "build"
	PodTypeSign  = "sign"

	StatusCompleted  = "completed"
	StatusCreated    = "created"
	StatusInProgress = "in progress"
	StatusFailed     = "failed"
)

var ErrNoMatchingPod = errors.New("no matching pod")

//go:generate mockgen -source=podhelper.go -package=utils -destination=mock_podhelper.go

type PodHelper interface {
	IsPodChanged(existingPod *v1.Pod, newPod *v1.Pod) (bool, error)
	PodLabels(modName string, targetKernel string, podType string) map[string]string
	GetModulePodByKernel(ctx context.Context, modName, namespace, targetKernel, podType string, owner metav1.Object) (*v1.Pod, error)
	GetModulePods(ctx context.Context, modName, namespace, podType string, owner metav1.Object) ([]v1.Pod, error)
	DeletePod(ctx context.Context, pod *v1.Pod) error
	CreatePod(ctx context.Context, podSpec *v1.Pod) error
	GetPodStatus(pod *v1.Pod) (Status, error)
}

type podHelper struct {
	client client.Client
}

func NewPodHelper(client client.Client) PodHelper {
	return &podHelper{
		client: client,
	}
}

func (ph *podHelper) IsPodChanged(existingPod *v1.Pod, newPod *v1.Pod) (bool, error) {
	existingAnnotations := existingPod.GetAnnotations()
	newAnnotations := newPod.GetAnnotations()
	if existingAnnotations == nil {
		return false, fmt.Errorf("annotations are not present in the existing pod %s", existingPod.Name)
	}
	if existingAnnotations[constants.PodHashAnnotation] == newAnnotations[constants.PodHashAnnotation] {
		return false, nil
	}
	return true, nil
}

func (ph *podHelper) PodLabels(modName string, targetKernel string, podType string) map[string]string {
	labels := moduleKernelLabels(modName, targetKernel, podType)

	labels["app.kubernetes.io/name"] = "kmm"
	labels["app.kubernetes.io/component"] = podType
	labels["app.kubernetes.io/part-of"] = "kmm"

	return labels
}

func (ph *podHelper) GetModulePodByKernel(ctx context.Context, modName, namespace, targetKernel, podType string, owner metav1.Object) (*v1.Pod, error) {
	matchLabels := moduleKernelLabels(modName, targetKernel, podType)
	pods, err := ph.getPods(ctx, namespace, matchLabels)
	if err != nil {
		return nil, fmt.Errorf("failed to get module %s, pods by kernel %s: %v", modName, targetKernel, err)
	}

	// filter pods by owner, since they could have been created by the preflight
	// when checking that specific module
	moduleOwnedPods := filterPodsByOwner(pods, owner)
	numFoundPods := len(moduleOwnedPods)
	if numFoundPods == 0 {
		return nil, ErrNoMatchingPod
	} else if numFoundPods > 1 {
		return nil, fmt.Errorf("expected 0 or 1 %s pod, got %d", podType, numFoundPods)
	}

	return &moduleOwnedPods[0], nil
}

func (ph *podHelper) GetModulePods(ctx context.Context, modName, namespace, podType string, owner metav1.Object) ([]v1.Pod, error) {
	matchLabels := moduleLabels(modName, podType)
	pods, err := ph.getPods(ctx, namespace, matchLabels)
	if err != nil {
		return nil, fmt.Errorf("failed to get pods for module %s, namespace %s: %v", modName, namespace, err)
	}

	// filter pods by owner, since they could have been created by the preflight
	// when checking that specific module
	moduleOwnedPods := filterPodsByOwner(pods, owner)
	return moduleOwnedPods, nil
}

func (ph *podHelper) DeletePod(ctx context.Context, pod *v1.Pod) error {
	opts := []client.DeleteOption{
		client.PropagationPolicy(metav1.DeletePropagationBackground),
	}
	err := ph.client.Delete(ctx, pod, opts...)
	if err != nil {
		return err
	}
	return nil
}

func (ph *podHelper) CreatePod(ctx context.Context, pod *v1.Pod) error {
	err := ph.client.Create(ctx, pod)
	if err != nil {
		return err
	}
	return nil
}

// GetPodStatus returns the status of a Pod, whether the latter is in progress or not and
// whether there was an error or not
func (ph *podHelper) GetPodStatus(pod *v1.Pod) (Status, error) {
	switch pod.Status.Phase {
	case v1.PodSucceeded:
		return StatusCompleted, nil
	case v1.PodRunning, v1.PodPending:
		return StatusInProgress, nil
	case v1.PodFailed:
		return StatusFailed, nil
	default:
		return "", fmt.Errorf("unknown status: %v", pod.Status)
	}
}

func (ph *podHelper) getPods(ctx context.Context, namespace string, labels map[string]string) ([]v1.Pod, error) {
	podList := v1.PodList{}
	opts := []client.ListOption{
		client.MatchingLabels(labels),
		client.InNamespace(namespace),
	}
	if err := ph.client.List(ctx, &podList, opts...); err != nil {
		return nil, fmt.Errorf("could not list pods: %v", err)
	}

	return podList.Items, nil
}

func moduleKernelLabels(moduleName, targetKernel, podType string) map[string]string {
	labels := moduleLabels(moduleName, podType)
	labels[constants.TargetKernelTarget] = targetKernel
	return labels
}

func moduleLabels(moduleName, podType string) map[string]string {
	return map[string]string{
		constants.ModuleNameLabel: moduleName,
		constants.PodType:         podType,
	}
}

func filterPodsByOwner(pods []v1.Pod, owner metav1.Object) []v1.Pod {
	ownedPods := []v1.Pod{}
	for _, pod := range pods {
		if metav1.IsControlledBy(&pod, owner) {
			ownedPods = append(ownedPods, pod)
		}
	}
	return ownedPods
}
