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
	"github.com/kubernetes-sigs/kernel-module-management/internal/api"
	"github.com/kubernetes-sigs/kernel-module-management/internal/buildsign"
	"github.com/kubernetes-sigs/kernel-module-management/internal/kernel"
	"github.com/kubernetes-sigs/kernel-module-management/internal/mbsc"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
	v1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const MBSCReconcilerName = "MBSCReconciler"

// mbscReconciler reconciles a ModuleBuldSignConfig object
type mbscReconciler struct {
	reconHelperAPI mbscReconcilerHelperAPI
}

func NewMBSCReconciler(
	client client.Client,
	buildSignAPI buildsign.Manager,
	mbscAPI mbsc.MBSC,
) *mbscReconciler {
	reconHelperAPI := newMBSCReconcilerHelper(client, buildSignAPI, mbscAPI)
	return &mbscReconciler{
		reconHelperAPI: reconHelperAPI,
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *mbscReconciler) SetupWithManager(mgr ctrl.Manager, kernelLabel string) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kmmv1beta1.ModuleBuildSignConfig{}).
		Owns(&v1.Pod{}).
		Named(MBSCReconcilerName).
		Complete(
			reconcile.AsReconciler[*kmmv1beta1.ModuleBuildSignConfig](mgr.GetClient(), r),
		)
}

//+kubebuilder:rbac:groups=kmm.sigs.x-k8s.io,resources=modulebuildsignconfigs,verbs=get;list;watch;update;patch;create
//+kubebuilder:rbac:groups=kmm.sigs.x-k8s.io,resources=modulebuildsignconfigs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups="core",resources=secrets,verbs=get;list;watch
//+kubebuilder:rbac:groups="core",resources=configmaps,verbs=get;list;watch
//+kubebuilder:rbac:groups="core",resources=pods,verbs=create;list;watch;delete

func (r *mbscReconciler) Reconcile(ctx context.Context, mbscObj *kmmv1beta1.ModuleBuildSignConfig) (ctrl.Result, error) {
	res := ctrl.Result{}

	logger := log.FromContext(ctx)

	if mbscObj.GetDeletionTimestamp() != nil {
		// [TODO] delete build/sign pods
		return res, nil
	}

	err := r.reconHelperAPI.updateStatus(ctx, mbscObj)
	if err != nil {
		return res, fmt.Errorf("failed to update MSBC %s status based on the result of the builds: %v", mbscObj.Name, err)
	}

	err = r.reconHelperAPI.processImagesSpecs(ctx, mbscObj)
	if err != nil {
		return res, fmt.Errorf("failed to process images of MSBC %s: %v", mbscObj.Name, err)
	}

	logger.Info("run garbage collector for build/sign pods")
	err = r.reconHelperAPI.garbageCollect(ctx, mbscObj)
	if err != nil {
		return res, fmt.Errorf("failed to run garbage collector for MBSC %s: %v", mbscObj.Name, err)
	}

	return res, nil
}

//go:generate mockgen -source=mbsc_reconciler.go -package=controllers -destination=mock_mbsc_reconciler.go mbscReconcilerHelperAPI

type mbscReconcilerHelperAPI interface {
	updateStatus(ctx context.Context, mbscObj *kmmv1beta1.ModuleBuildSignConfig) error
	processImagesSpecs(ctx context.Context, mbscObj *kmmv1beta1.ModuleBuildSignConfig) error
	garbageCollect(ctx context.Context, mbscObj *kmmv1beta1.ModuleBuildSignConfig) error
}

type mbscReconcilerHelper struct {
	client       client.Client
	buildSignAPI buildsign.Manager
	mbscAPI      mbsc.MBSC
}

func newMBSCReconcilerHelper(client client.Client, buildSignAPI buildsign.Manager, mbscAPI mbsc.MBSC) mbscReconcilerHelperAPI {
	return &mbscReconcilerHelper{
		client:       client,
		buildSignAPI: buildSignAPI,
		mbscAPI:      mbscAPI,
	}
}

func (mrh *mbscReconcilerHelper) updateStatus(ctx context.Context, mbscObj *kmmv1beta1.ModuleBuildSignConfig) error {
	errs := make([]error, 0, len(mbscObj.Spec.Images))
	patchFrom := client.MergeFrom(mbscObj.DeepCopy())
	for _, imageSpec := range mbscObj.Spec.Images {
		status, err := mrh.buildSignAPI.GetStatus(ctx, mbscObj.Name, mbscObj.Namespace, imageSpec.ModuleImageSpec.KernelVersion,
			imageSpec.Action, &mbscObj.ObjectMeta)
		if err != nil || status == kmmv1beta1.BuildOrSignStatus("") {
			// either we could not get the status or the status is empty
			errs = append(errs, err)
			continue
		}
		mrh.mbscAPI.SetImageStatus(mbscObj, imageSpec.Image, imageSpec.Action, status)
	}

	err := mrh.client.Status().Patch(ctx, mbscObj, patchFrom)
	errs = append(errs, err)
	return errors.Join(errs...)
}

func (mrh *mbscReconcilerHelper) processImagesSpecs(ctx context.Context, mbscObj *kmmv1beta1.ModuleBuildSignConfig) error {
	logger := log.FromContext(ctx)
	errs := make([]error, 0, len(mbscObj.Spec.Images))
	for _, imageSpec := range mbscObj.Spec.Images {
		imageStatus := mrh.mbscAPI.GetImageStatus(mbscObj, imageSpec.Image, imageSpec.Action)
		if imageStatus == kmmv1beta1.ActionSuccess {
			// in case action succeeded - skip to the next image. Otherwise, action had not been handled yet, or is being handled, or already failed.
			// in that case the Sync API will take care of what is needed to be done
			continue
		}
		mld := createMLD(mbscObj, &imageSpec.ModuleImageSpec)
		err := mrh.buildSignAPI.Sync(ctx, mld, true, imageSpec.Action, &mbscObj.ObjectMeta)
		if err != nil {
			errs = append(errs, err)
			logger.Info(utils.WarnString("sync for image %s, action %s failed: %v"), imageSpec.Image, imageSpec.Action, err)
		}
	}
	return errors.Join(errs...)
}

func (mrh *mbscReconcilerHelper) garbageCollect(ctx context.Context, mbscObj *kmmv1beta1.ModuleBuildSignConfig) error {
	logger := log.FromContext(ctx)

	// Garbage collect for successfully finished build pods
	deleted, err := mrh.buildSignAPI.GarbageCollect(ctx, mbscObj.Name, mbscObj.Namespace, kmmv1beta1.BuildImage, &mbscObj.ObjectMeta)
	if err != nil {
		return fmt.Errorf("could not garbage collect build objects: %v", err)
	}

	logger.Info("Garbage-collected Build objects", "names", deleted)

	// Garbage collect for successfully finished sign pods
	deleted, err = mrh.buildSignAPI.GarbageCollect(ctx, mbscObj.Name, mbscObj.Namespace, kmmv1beta1.SignImage, &mbscObj.ObjectMeta)
	if err != nil {
		return fmt.Errorf("could not garbage collect sign objects: %v", err)
	}

	logger.Info("Garbage-collected Sign objects", "names", deleted)

	return nil
}

func createMLD(mbscObj *kmmv1beta1.ModuleBuildSignConfig, imageSpec *kmmv1beta1.ModuleImageSpec) *api.ModuleLoaderData {
	return &api.ModuleLoaderData{
		Name:                    mbscObj.Name,
		Namespace:               mbscObj.Namespace,
		ContainerImage:          imageSpec.Image,
		Build:                   imageSpec.Build,
		Sign:                    imageSpec.Sign,
		Owner:                   mbscObj,
		KernelVersion:           imageSpec.KernelVersion,
		KernelNormalizedVersion: kernel.NormalizeVersion(imageSpec.KernelVersion),
		ImageRepoSecret:         mbscObj.Spec.ImageRepoSecret,
		RegistryTLS:             imageSpec.RegistryTLS,
	}
}
