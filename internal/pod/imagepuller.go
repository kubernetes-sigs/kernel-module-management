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

type PullPodStatus string

const (
	PullImageFailed        PullPodStatus = "pullFailed"
	PullImageSuccess       PullPodStatus = "pullSuccess"
	PullImageInProcess     PullPodStatus = "pullInProcess"
	PullImageUnexpectedErr PullPodStatus = "unexpectedError"

	imagePullBackOffReason = "ImagePullBackOff"
	errImagePullReason     = "ErrImagePull"

	moduleImageLabelKey = "kmm.node.kubernetes.io/module-image-config"
	pullPodTypeLabelKey = "kmm.node.kubernetes.io/pull-pod-type"

	pullerContainerName = "puller"

	pullPodTypeOneTime  = "one-time-pull"
	pullPodUntilSuccess = "until-success"
)

//go:generate mockgen -source=imagepuller.go -package=pod -destination=mock_imagepuller.go

type ImagePuller interface {
	CreatePullPod(ctx context.Context, imageSpec *kmmv1beta1.ModuleImageSpec, micObj *kmmv1beta1.ModuleImagesConfig) error
	DeletePod(ctx context.Context, pod *v1.Pod) error
	ListPullPods(ctx context.Context, micObj *kmmv1beta1.ModuleImagesConfig) ([]v1.Pod, error)
	GetPullPodForImage(pods []v1.Pod, image string) *v1.Pod
	GetPullPodImage(pod v1.Pod) string
	GetPullPodStatus(pod *v1.Pod) PullPodStatus
}

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

	pullPodTypeLabeValue := pullPodUntilSuccess
	if imageSpec.Build != nil || imageSpec.Sign != nil {
		pullPodTypeLabeValue = pullPodTypeOneTime
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
				pullPodTypeLabelKey: pullPodTypeLabeValue,
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
			RestartPolicy:    v1.RestartPolicyNever,
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

	hl := client.HasLabels{pullPodTypeLabelKey}
	ml := client.MatchingLabels{moduleImageLabelKey: micObj.Name}

	ctrl.LoggerFrom(ctx).WithValues("mic name", micObj.Name).V(1).Info("Listing mic image Pods")

	if err := ipi.client.List(ctx, &pl, client.InNamespace(micObj.Namespace), hl, ml); err != nil {
		return nil, fmt.Errorf("could not list mic image pods for mic %s: %v", micObj.Name, err)
	}

	return pl.Items, nil
}

func (ipi *imagePullerImpl) GetPullPodForImage(pods []v1.Pod, image string) *v1.Pod {

	for i, pod := range pods {
		if image == pod.Spec.Containers[0].Image {
			return &pods[i]
		}
	}
	return nil
}

func (ipi *imagePullerImpl) GetPullPodImage(pod v1.Pod) string {
	return pod.Spec.Containers[0].Image
}

func (ipi *imagePullerImpl) GetPullPodStatus(pod *v1.Pod) PullPodStatus {
	switch pod.Status.Phase {
	case v1.PodSucceeded:
		return PullImageSuccess
	case v1.PodFailed, v1.PodUnknown:
		return PullImageUnexpectedErr
	case v1.PodRunning:
		return PullImageInProcess
	case v1.PodPending:
		// no container statuses yet, the pod is just starting to pull images
		if pod.Status.ContainerStatuses == nil {
			return PullImageInProcess
		}

		// no wating status, the pull process is still in progress
		if pod.Status.ContainerStatuses[0].State.Waiting == nil {
			return PullImageInProcess
		}

		pullPodType := pod.GetLabels()[pullPodTypeLabelKey]
		// if pod is targeted to wait till the end - return the InProgress
		if pullPodType == pullPodUntilSuccess {
			return PullImageInProcess
		}

		if waitingReason := pod.Status.ContainerStatuses[0].State.Waiting.Reason; waitingReason == imagePullBackOffReason || waitingReason == errImagePullReason {
			return PullImageFailed
		}
	}

	return PullImageUnexpectedErr
}
