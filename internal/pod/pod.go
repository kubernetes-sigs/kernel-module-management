package pod

import (
	"context"
	"fmt"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

//go:generate mockgen -source=pod.go -package=pod -destination=mock_pod.go

type ImagePuller interface {
	CreatePullPod(ctx context.Context, imageSpec *kmmv1beta1.ModuleImageSpec, micObj *kmmv1beta1.ModuleImagesConfig) error
	DeletePod(ctx context.Context, pod *v1.Pod) error
	ListPullPods(ctx context.Context, micObj *kmmv1beta1.ModuleImagesConfig) ([]v1.Pod, error)
	GetPullPodForImage(pods []v1.Pod, image string) *v1.Pod
	GetPullPodImage(pod v1.Pod) string
}

const (
	moduleImageLabelKey = "kmm.node.kubernetes.io/module-image-config"
	imageLabelKey       = "kmm.node.kubernetes.io/module-image"
	pullerContainerName = "puller"
)

type imagePullerImpl struct {
	client client.Client
	scheme *runtime.Scheme
}

func NewImagePuller(client client.Client, scheme *runtime.Scheme) ImagePuller {
	return &imagePullerImpl{
		client: client,
		scheme: scheme,
	}
}

func (ipi *imagePullerImpl) CreatePullPod(ctx context.Context, imageSpec *kmmv1beta1.ModuleImageSpec,
	micObj *kmmv1beta1.ModuleImagesConfig) error {

	restartPolicy := v1.RestartPolicyOnFailure
	if imageSpec.Build != nil || imageSpec.Sign != nil {
		restartPolicy = v1.RestartPolicyNever
	}

	imagePullSecrets := []v1.LocalObjectReference{}
	if micObj.Spec.ImageRepoSecret != nil {
		imagePullSecrets = []v1.LocalObjectReference{*micObj.Spec.ImageRepoSecret}
	}

	pullPod := v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: micObj.Name + "-pull-pod-",
			Namespace:    micObj.Namespace,
			Labels: map[string]string{
				moduleImageLabelKey: micObj.Name,
				imageLabelKey:       imageSpec.Image,
			},
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name:    pullerContainerName,
					Image:   imageSpec.Image,
					Command: []string{"/bin/sh", "-c", "exit 0"},
				},
			},
			RestartPolicy:    restartPolicy,
			ImagePullSecrets: imagePullSecrets,
		},
	}

	err := ctrl.SetControllerReference(micObj, &pullPod, ipi.scheme)
	if err != nil {
		return fmt.Errorf("failed to set MIC object %s as owner on pullPod for image %s: %v", micObj.Name, imageSpec.Image, err)
	}

	return ipi.client.Create(ctx, &pullPod)
}

func (ipi *imagePullerImpl) DeletePod(ctx context.Context, pod *v1.Pod) error {

	return deletePod(ipi.client, ctx, pod)
}

func (ipi *imagePullerImpl) ListPullPods(ctx context.Context, micObj *kmmv1beta1.ModuleImagesConfig) ([]v1.Pod, error) {

	pl := v1.PodList{}

	hl := client.HasLabels{imageLabelKey}
	ml := client.MatchingLabels{moduleImageLabelKey: micObj.Name}

	ctrl.LoggerFrom(ctx).WithValues("mic name", micObj.Name).V(1).Info("Listing mic image Pods")

	if err := ipi.client.List(ctx, &pl, client.InNamespace(micObj.Namespace), hl, ml); err != nil {
		return nil, fmt.Errorf("could not list mic image pods for mic %s: %v", micObj.Name, err)
	}

	return pl.Items, nil
}

func (ipi *imagePullerImpl) GetPullPodForImage(pods []v1.Pod, image string) *v1.Pod {

	for i, pod := range pods {
		if image == pod.Labels[imageLabelKey] {
			return &pods[i]
		}
	}
	return nil
}

func (ipi *imagePullerImpl) GetPullPodImage(pod v1.Pod) string {
	return pod.Labels[imageLabelKey]
}

func deletePod(clnt client.Client, ctx context.Context, pod *v1.Pod) error {

	logger := ctrl.LoggerFrom(ctx)

	if pod.DeletionTimestamp != nil {
		logger.Info("DeletionTimestamp set, pod is already in deletion", "pod", pod.Name)
		return nil
	}

	if err := clnt.Delete(ctx, pod); client.IgnoreNotFound(err) != nil {
		return fmt.Errorf("failed to delete pull pod %s/%s: %v", pod.Namespace, pod.Name, err)
	}

	return nil
}
