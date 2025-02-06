/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"errors"
	"fmt"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/mbsc"
	"github.com/kubernetes-sigs/kernel-module-management/internal/mic"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	MICReconcilerName = "MICReconciler"

	moduleImageLabelKey = "kmm.node.kubernetes.io/module-image-config"
	imageLabelKey       = "kmm.node.kubernetes.io/module-image"

	pullerContainerName = "puller"
)

// micReconciler reconciles a MIC (moduleimagesconfig) object
type micReconciler struct {
	micReconHelper micReconcilerHelper
	podHelper      pullPodManager
}

func NewMICReconciler(client client.Client, micAPI mic.MIC, mbscAPI mbsc.MBSC, scheme *runtime.Scheme) *micReconciler {
	podHelper := newPullPodManager(client, scheme)
	micReconHelper := newMICReconcilerHelper(client, podHelper, micAPI, mbscAPI, scheme)
	return &micReconciler{
		micReconHelper: micReconHelper,
		podHelper:      podHelper,
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *micReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kmmv1beta1.ModuleImagesConfig{}).
		Owns(&v1.Pod{}).
		Owns(&kmmv1beta1.ModuleBuildSignConfig{}).
		Named(MICReconcilerName).
		Complete(
			reconcile.AsReconciler[*kmmv1beta1.ModuleImagesConfig](mgr.GetClient(), r),
		)
}

func (r *micReconciler) Reconcile(ctx context.Context, micObj *kmmv1beta1.ModuleImagesConfig) (ctrl.Result, error) {
	res := ctrl.Result{}
	if micObj.GetDeletionTimestamp() != nil {
		// [TODO] delete check image and build/sign pods
		return res, nil
	}

	pods, err := r.podHelper.listImagesPullPods(ctx, micObj)
	if err != nil {
		return res, fmt.Errorf("failed to get the image pods for mic %s: %v", micObj.Name, err)
	}

	err = r.micReconHelper.updateStatusByPullPods(ctx, micObj, pods)
	if err != nil {
		return res, fmt.Errorf("failed tp update the status for MIC %s based on pull pods: %v", micObj.Name, err)
	}

	err = r.micReconHelper.updateStatusByMBSC(ctx, micObj)
	if err != nil {
		return res, fmt.Errorf("failed tp update the status for MIC %s based on builds: %v", micObj.Name, err)
	}

	err = r.micReconHelper.processImagesSpecs(ctx, micObj, pods)
	if err != nil {
		return res, fmt.Errorf("failed to process images spec: %v", err)
	}
	return res, nil
}

//go:generate mockgen -source=mic_reconciler.go -package=controllers -destination=mock_mic_reconciler.go micReconcilerHelper
type micReconcilerHelper interface {
	updateStatusByPullPods(ctx context.Context, micObj *kmmv1beta1.ModuleImagesConfig, pods []v1.Pod) error
	updateStatusByMBSC(ctx context.Context, micObj *kmmv1beta1.ModuleImagesConfig) error
	processImagesSpecs(ctx context.Context, micObj *kmmv1beta1.ModuleImagesConfig, pullPods []v1.Pod) error
}

type micReconcilerHelperImpl struct {
	client     client.Client
	podHelper  pullPodManager
	micHelper  mic.MIC
	mbscHelper mbsc.MBSC
	scheme     *runtime.Scheme
}

func newMICReconcilerHelper(client client.Client,
	pullPodHelper pullPodManager,
	micAPI mic.MIC,
	mbscAPI mbsc.MBSC,
	scheme *runtime.Scheme) micReconcilerHelper {
	return &micReconcilerHelperImpl{
		client:     client,
		podHelper:  pullPodHelper,
		mbscHelper: mbscAPI,
		micHelper:  micAPI,
		scheme:     scheme,
	}
}

func (mrhi *micReconcilerHelperImpl) updateStatusByPullPods(ctx context.Context, micObj *kmmv1beta1.ModuleImagesConfig, pods []v1.Pod) error {
	logger := ctrl.LoggerFrom(ctx)
	if len(pods) == 0 {
		return nil
	}

	podsToDelete := make([]v1.Pod, 0, len(pods))
	patchFrom := client.MergeFrom(micObj.DeepCopy())

	for _, p := range pods {
		image := p.Labels[imageLabelKey]
		imageSpec := mrhi.micHelper.GetModuleImageSpec(micObj, image)
		if imageSpec == nil {
			logger.Info(utils.WarnString("image not present in spec during updateStatusByPods, deleting pod"), "mic", micObj.Name, "image", image)
			podsToDelete = append(podsToDelete, p)
			continue
		}
		phase := p.Status.Phase
		switch phase {
		case v1.PodFailed:
			if imageSpec.Build != nil || imageSpec.Sign != nil {
				logger.Info("failed pod with build or sign spec, updating image status to NeedsBulding")
				mrhi.micHelper.SetImageStatus(micObj, image, kmmv1beta1.ImageNeedsBuilding)
			} else {
				logger.Info(utils.WarnString("failed pod without build or sign spec, shoud not have happened"), "image", image)
			}
			podsToDelete = append(podsToDelete, p)
		case v1.PodSucceeded:
			logger.Info("successful pod, updating image status to ImageExists")
			mrhi.micHelper.SetImageStatus(micObj, image, kmmv1beta1.ImageExists)
			podsToDelete = append(podsToDelete, p)
		}
	}
	// patch the status in the MIC object
	err := mrhi.client.Status().Patch(ctx, micObj, patchFrom)
	if err != nil {
		return fmt.Errorf("failed to patch the status of mic %s: %v", micObj.Name, err)
	}

	// deleting pods only after MIC status was patched successfully.otherwise, in the next recon loop
	// we won't have pods to calculate the status
	errs := make([]error, 0, len(podsToDelete))
	for _, pod := range podsToDelete {
		err = mrhi.podHelper.deletePod(ctx, &pod)
		errs = append(errs, err)
	}

	return errors.Join(errs...)
}

func (mrhi *micReconcilerHelperImpl) updateStatusByMBSC(ctx context.Context, micObj *kmmv1beta1.ModuleImagesConfig) error {
	mbsc, err := mrhi.mbscHelper.Get(ctx, micObj.Name, micObj.Namespace)
	if err != nil {
		return fmt.Errorf("failed to get ModuleBuildSignConfig object %s/%s: %v", micObj.Namespace, micObj.Name, err)
	}

	if mbsc == nil {
		return nil
	}

	patchFrom := client.MergeFrom(micObj.DeepCopy())
	for _, imageState := range mbsc.Status.ImagesStates {
		imageSpec := mrhi.micHelper.GetModuleImageSpec(micObj, imageState.Image)
		if imageSpec == nil {
			// image not found in spec, ignore
			continue
		}
		micImageStatus := kmmv1beta1.ImageExists
		if imageState.Status == kmmv1beta1.ImageBuildFailed {
			micImageStatus = kmmv1beta1.ImageDoesNotExist
		}
		mrhi.micHelper.SetImageStatus(micObj, imageState.Image, micImageStatus)
	}

	return mrhi.client.Status().Patch(ctx, micObj, patchFrom)
}

func (mrhi *micReconcilerHelperImpl) processImagesSpecs(ctx context.Context, micObj *kmmv1beta1.ModuleImagesConfig, pullPods []v1.Pod) error {
	errs := make([]error, len(micObj.Spec.Images))
	for _, imageSpec := range micObj.Spec.Images {
		imageState := mrhi.micHelper.GetImageState(micObj, imageSpec.Image)

		switch imageState {
		case "":
			// image State is not set: either new image or pull pod is still running
			if mrhi.podHelper.getPullPodForImage(pullPods, imageSpec.Image) == nil {
				// no pull pod- create it, otherwise we wait for it to finish
				err := mrhi.podHelper.createPullPod(ctx, &imageSpec, micObj)
				errs = append(errs, err)
			}
		case kmmv1beta1.ImageDoesNotExist:
			if imageSpec.Build == nil && imageSpec.Sign == nil {
				break
			}
			fallthrough
		case kmmv1beta1.ImageNeedsBuilding:
			// image needs to be built - patching MBSC
			err := mrhi.mbscHelper.CreateOrPatch(ctx, micObj.Name, micObj.Namespace, &imageSpec, micObj.Spec.ImageRepoSecret, micObj)
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

//go:generate mockgen -source=mic_reconciler.go -package=controllers -destination=mock_mic_reconciler.go pullPodManager
type pullPodManager interface {
	listImagesPullPods(ctx context.Context, micObj *kmmv1beta1.ModuleImagesConfig) ([]v1.Pod, error)
	deletePod(ctx context.Context, pod *v1.Pod) error
	createPullPod(ctx context.Context, imageSpec *kmmv1beta1.ModuleImageSpec, micObj *kmmv1beta1.ModuleImagesConfig) error
	getPullPodForImage(pods []v1.Pod, image string) *v1.Pod
}

type pullPodManagerImpl struct {
	client client.Client
	scheme *runtime.Scheme
}

func newPullPodManager(client client.Client, scheme *runtime.Scheme) pullPodManager {
	return &pullPodManagerImpl{
		client: client,
		scheme: scheme,
	}
}

func (ppmi *pullPodManagerImpl) listImagesPullPods(ctx context.Context, micObj *kmmv1beta1.ModuleImagesConfig) ([]v1.Pod, error) {
	logger := ctrl.LoggerFrom(ctx).WithValues("mic name", micObj.Name)

	pl := v1.PodList{}

	hl := client.HasLabels{imageLabelKey}
	ml := client.MatchingLabels{moduleImageLabelKey: micObj.Name}

	logger.V(1).Info("Listing mic image Pods")

	if err := ppmi.client.List(ctx, &pl, client.InNamespace(micObj.Namespace), hl, ml); err != nil {
		return nil, fmt.Errorf("could not list mic image pods for mic %s: %v", micObj.Name, err)
	}

	return pl.Items, nil
}

func (ppmi *pullPodManagerImpl) deletePod(ctx context.Context, pod *v1.Pod) error {
	logger := ctrl.LoggerFrom(ctx)
	if pod.DeletionTimestamp != nil {
		logger.Info("DeletionTimestamp set, pod is already in deletion", "pod", pod.Name)
		return nil
	}
	if err := ppmi.client.Delete(ctx, pod); client.IgnoreNotFound(err) != nil {
		return fmt.Errorf("failed to delete pull pod %s/%s: %v", pod.Namespace, pod.Name, err)
	}
	return nil
}

func (ppmi *pullPodManagerImpl) createPullPod(ctx context.Context, imageSpec *kmmv1beta1.ModuleImageSpec, micObj *kmmv1beta1.ModuleImagesConfig) error {
	restartPolicy := v1.RestartPolicyOnFailure
	if imageSpec.Build != nil || imageSpec.Sign != nil {
		restartPolicy = v1.RestartPolicyNever
	}
	var imagePullSecrets []v1.LocalObjectReference
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

	err := ctrl.SetControllerReference(micObj, &pullPod, ppmi.scheme)
	if err != nil {
		return fmt.Errorf("failed to set MIC object %s as owner on pullPod for image %s: %v", micObj.Name, imageSpec.Image, err)
	}

	return ppmi.client.Create(ctx, &pullPod)
}

func (ppmi *pullPodManagerImpl) getPullPodForImage(pods []v1.Pod, image string) *v1.Pod {
	for i, pod := range pods {
		if image == pod.Labels[imageLabelKey] {
			return &pods[i]
		}
	}
	return nil
}
