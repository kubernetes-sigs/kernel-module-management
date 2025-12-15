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
	"github.com/kubernetes-sigs/kernel-module-management/internal/pod"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const MICReconcilerName = "MICReconciler"

// micReconciler reconciles a MIC (moduleimagesconfig) object
type micReconciler struct {
	micReconHelper micReconcilerHelper
	imagePullerAPI pod.ImagePuller
}

func NewMICReconciler(client client.Client, micAPI mic.MIC, mbscAPI mbsc.MBSC, imagePullerAPI pod.ImagePuller,
	scheme *runtime.Scheme) *micReconciler {

	micReconHelper := newMICReconcilerHelper(client, imagePullerAPI, micAPI, mbscAPI, scheme)
	return &micReconciler{
		micReconHelper: micReconHelper,
		imagePullerAPI: imagePullerAPI,
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

	triggerChanged, err := r.micReconHelper.handleImageRebuildTriggerGeneration(ctx, micObj)
	if err != nil {
		return res, fmt.Errorf("failed to handle ImageRebuildTriggerGeneration for MIC %s: %v", micObj.Name, err)
	}
	if triggerChanged {
		return ctrl.Result{Requeue: true}, nil
	}

	pods, err := r.imagePullerAPI.ListPullPods(ctx, micObj.Name, micObj.Namespace)
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
	handleImageRebuildTriggerGeneration(ctx context.Context, micObj *kmmv1beta1.ModuleImagesConfig) (bool, error)
	updateStatusByPullPods(ctx context.Context, micObj *kmmv1beta1.ModuleImagesConfig, pods []v1.Pod) error
	updateStatusByMBSC(ctx context.Context, micObj *kmmv1beta1.ModuleImagesConfig) error
	processImagesSpecs(ctx context.Context, micObj *kmmv1beta1.ModuleImagesConfig, pullPods []v1.Pod) error
}

type micReconcilerHelperImpl struct {
	client         client.Client
	imagePullerAPI pod.ImagePuller
	micHelper      mic.MIC
	mbscHelper     mbsc.MBSC
	scheme         *runtime.Scheme
}

func newMICReconcilerHelper(client client.Client,
	imagePullerAPI pod.ImagePuller,
	micAPI mic.MIC,
	mbscAPI mbsc.MBSC,
	scheme *runtime.Scheme) micReconcilerHelper {

	return &micReconcilerHelperImpl{
		client:         client,
		imagePullerAPI: imagePullerAPI,
		mbscHelper:     mbscAPI,
		micHelper:      micAPI,
		scheme:         scheme,
	}
}

func (mrhi *micReconcilerHelperImpl) handleImageRebuildTriggerGeneration(ctx context.Context, micObj *kmmv1beta1.ModuleImagesConfig) (bool, error) {
	logger := ctrl.LoggerFrom(ctx).WithValues("mic name", micObj.Name)

	specTrigger := micObj.Spec.ImageRebuildTriggerGeneration
	statusTrigger := micObj.Status.ImageRebuildTriggerGeneration

	// Compare *int pointers: both nil, or both non-nil and equal values
	if (specTrigger == nil && statusTrigger == nil) ||
		(specTrigger != nil && statusTrigger != nil && *specTrigger == *statusTrigger) {
		return false, nil
	}

	logger.Info("ImageRebuildTriggerGeneration changed, clearing all image statuses to trigger rebuild",
		"old", statusTrigger, "new", specTrigger)

	if err := mrhi.mbscHelper.Delete(ctx, micObj.Name, micObj.Namespace); err != nil {
		return false, fmt.Errorf("failed to delete MBSC after ImageRebuildTriggerGeneration change: %v", err)
	}

	patchFrom := client.MergeFrom(micObj.DeepCopy())

	// Clearing all image statuses
	micObj.Status.ImagesStates = []kmmv1beta1.ModuleImageState{}
	micObj.Status.ImageRebuildTriggerGeneration = specTrigger

	if err := mrhi.client.Status().Patch(ctx, micObj, patchFrom); err != nil {
		return false, fmt.Errorf("failed to patch MIC status after ImageRebuildTriggerGeneration change: %v", err)
	}

	return true, nil
}

func (mrhi *micReconcilerHelperImpl) updateStatusByPullPods(ctx context.Context, micObj *kmmv1beta1.ModuleImagesConfig, pods []v1.Pod) error {
	logger := ctrl.LoggerFrom(ctx).WithValues("mic name", micObj.Name)
	if len(pods) == 0 {
		return nil
	}

	podsToDelete := make([]v1.Pod, 0, len(pods))
	patchFrom := client.MergeFrom(micObj.DeepCopy())

	for _, p := range pods {
		image := mrhi.imagePullerAPI.GetPullPodImage(p)
		imageSpec := mrhi.micHelper.GetModuleImageSpec(micObj, image)
		logger := logger.WithValues("image", image)
		if imageSpec == nil {
			logger.Info(utils.WarnString("image not present in spec during updateStatusByPods, deleting pod"))
			podsToDelete = append(podsToDelete, p)
			continue
		}
		podStatus := mrhi.imagePullerAPI.GetPullPodStatus(&p)
		switch podStatus {
		case pod.PullImageFailed:
			switch {
			case imageSpec.Build != nil:
				logger.Info("pull pod failed, build exists, setting status to kmmv1beta1.ImageNeedsBuilding")
				mrhi.micHelper.SetImageStatus(micObj, image, kmmv1beta1.ImageNeedsBuilding)
			case imageSpec.Sign != nil:
				logger.Info("pull pod failed, build does not exist, sign exists, setting status to kmmv1beta1.ImageNeedsSigning")
				mrhi.micHelper.SetImageStatus(micObj, image, kmmv1beta1.ImageNeedsSigning)
			case imageSpec.SkipWaitMissingImage:
				logger.Info("pull pod failed, SkipWaitMissingImage was set, setting status to kmmv1beta1.ImageDoesNotExist")
				mrhi.micHelper.SetImageStatus(micObj, image, kmmv1beta1.ImageDoesNotExist)
			default:
				logger.Info(utils.WarnString("failed pod without build or sign spec, shoud not have happened"))
			}
			podsToDelete = append(podsToDelete, p)

		case pod.PullImageSuccess:
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
		err = mrhi.imagePullerAPI.DeletePod(ctx, &pod)
		errs = append(errs, err)
	}

	return errors.Join(errs...)
}

func (mrhi *micReconcilerHelperImpl) updateStatusByMBSC(ctx context.Context, micObj *kmmv1beta1.ModuleImagesConfig) error {
	logger := ctrl.LoggerFrom(ctx).WithValues("mic name", micObj.Name)
	mbsc, err := mrhi.mbscHelper.Get(ctx, micObj.Name, micObj.Namespace)
	if err != nil {
		return fmt.Errorf("failed to get ModuleBuildSignConfig object %s/%s: %v", micObj.Namespace, micObj.Name, err)
	}

	if mbsc == nil {
		return nil
	}

	patchFrom := client.MergeFrom(micObj.DeepCopy())
	for _, mbscImageState := range mbsc.Status.Images {
		micImageSpec := mrhi.micHelper.GetModuleImageSpec(micObj, mbscImageState.Image)
		if micImageSpec == nil {
			// spec for image does not exists in MIC, meaning it was already deleted - ignore status
			continue
		}

		mbscStatus := mbscImageState.Status
		mbscAction := mbscImageState.Action

		switch {
		case mbscStatus == kmmv1beta1.ActionFailure:
			// any failure (build or sign) - image does not exists
			logger.Info("mbsc status failed, updating mic image to DoesNotExist")
			mrhi.micHelper.SetImageStatus(micObj, mbscImageState.Image, kmmv1beta1.ImageDoesNotExist)
		case mbscStatus == kmmv1beta1.ActionSuccess && mbscAction == kmmv1beta1.SignImage:
			// sign action succeeded - image exists, nothing more to do
			logger.Info("mbsc status success and action as sign, updating mic image to Exists")
			mrhi.micHelper.SetImageStatus(micObj, mbscImageState.Image, kmmv1beta1.ImageExists)
		case mbscStatus == kmmv1beta1.ActionSuccess && micImageSpec.Sign != nil:
			// build succeeded and sign exists - image needs to be signed
			logger.Info("mbsc status success and sign section exists, updating mic image to NeedsSigning")
			mrhi.micHelper.SetImageStatus(micObj, mbscImageState.Image, kmmv1beta1.ImageNeedsSigning)
		case mbscStatus == kmmv1beta1.ActionSuccess:
			// build succeeded, no sign - image exists
			logger.Info("mbsc status success and no sign section exists, updating mic image to Exists")
			mrhi.micHelper.SetImageStatus(micObj, mbscImageState.Image, kmmv1beta1.ImageExists)
		}
	}

	return mrhi.client.Status().Patch(ctx, micObj, patchFrom)
}

func (mrhi *micReconcilerHelperImpl) processImagesSpecs(ctx context.Context, micObj *kmmv1beta1.ModuleImagesConfig, pullPods []v1.Pod) error {
	errs := make([]error, len(micObj.Spec.Images))
	for _, imageSpec := range micObj.Spec.Images {
		imageState := mrhi.micHelper.GetImageState(micObj, imageSpec.Image)

		var err error
		switch imageState {
		case "":
			// image State is not set: either new image or pull pod is still running
			if mrhi.imagePullerAPI.GetPullPodForImage(pullPods, imageSpec.Image) == nil {
				// no pull pod- create it, otherwise we wait for it to finish
				oneTimePod := imageSpec.Build != nil || imageSpec.Sign != nil || imageSpec.SkipWaitMissingImage
				err := mrhi.imagePullerAPI.CreatePullPod(ctx,
					micObj.Name,
					micObj.Namespace,
					imageSpec.Image,
					oneTimePod,
					micObj.Spec.ImageRepoSecret,
					micObj.Spec.ImagePullPolicy,
					micObj)
				errs = append(errs, err)
			}
		case kmmv1beta1.ImageDoesNotExist:
			if imageSpec.Build == nil && imageSpec.Sign == nil {
				break
			}
			fallthrough
		case kmmv1beta1.ImageNeedsBuilding:
			// image needs to be built - patching MBSC
			err = mrhi.mbscHelper.CreateOrPatch(ctx, micObj, &imageSpec, kmmv1beta1.BuildImage)
		case kmmv1beta1.ImageNeedsSigning:
			// image needs to be signed - patching MBSC
			err = mrhi.mbscHelper.CreateOrPatch(ctx, micObj, &imageSpec, kmmv1beta1.SignImage)
		}
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}
