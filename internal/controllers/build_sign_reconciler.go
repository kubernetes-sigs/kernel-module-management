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
	"fmt"
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/api"
	"github.com/kubernetes-sigs/kernel-module-management/internal/build"
	"github.com/kubernetes-sigs/kernel-module-management/internal/filter"
	"github.com/kubernetes-sigs/kernel-module-management/internal/module"
	"github.com/kubernetes-sigs/kernel-module-management/internal/node"
	"github.com/kubernetes-sigs/kernel-module-management/internal/sign"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
	v1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"strings"
)

const BuildSignReconcilerName = "BuildSignReconciler"

// ModuleReconciler reconciles a Module object
type BuildSignReconciler struct {
	filter         *filter.Filter
	reconHelperAPI buildSignReconcilerHelperAPI
	nodeAPI        node.Node
}

func NewBuildSignReconciler(
	client client.Client,
	buildAPI build.Manager,
	signAPI sign.SignManager,
	kernelAPI module.KernelMapper,
	filter *filter.Filter,
	nodeAPI node.Node,
) *BuildSignReconciler {
	reconHelperAPI := newBuildSignReconcilerHelper(client, buildAPI, signAPI, kernelAPI)
	return &BuildSignReconciler{
		reconHelperAPI: reconHelperAPI,
		filter:         filter,
		nodeAPI:        nodeAPI,
	}
}

//+kubebuilder:rbac:groups=kmm.sigs.x-k8s.io,resources=modules,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=kmm.sigs.x-k8s.io,resources=modules/status,verbs=get;update;patch
//+kubebuilder:rbac:groups="core",resources=nodes,verbs=get;list;watch
//+kubebuilder:rbac:groups="core",resources=secrets,verbs=get;list;watch
//+kubebuilder:rbac:groups="core",resources=configmaps,verbs=get;list;watch
//+kubebuilder:rbac:groups="core",resources=pods,verbs=create;list;watch;delete

// Reconcile lists all nodes and looks for kernels that match its mappings.
// For each mapping that matches at least one node in the cluster, it creates a DaemonSet running the container image
// on the nodes with a compatible kernel.
func (r *BuildSignReconciler) Reconcile(ctx context.Context, mod *kmmv1beta1.Module) (ctrl.Result, error) {
	res := ctrl.Result{}

	logger := log.FromContext(ctx)
	targetedNodes, err := r.nodeAPI.GetNodesListBySelector(ctx, mod.Spec.Selector, mod.Spec.Tolerations)
	if err != nil {
		return res, fmt.Errorf("could get targeted nodes for module %s: %w", mod.Name, err)
	}

	mldMappings, err := r.reconHelperAPI.getRelevantKernelMappings(ctx, mod, targetedNodes)
	if err != nil {
		return res, fmt.Errorf("could get kernel mappings for module %s: %w", mod.Name, err)
	}

	for kernelVersion, mld := range mldMappings {
		completedSuccessfully, err := r.reconHelperAPI.handleBuild(ctx, mld)
		if err != nil {
			return res, fmt.Errorf("failed to handle build for kernel version %s: %v", kernelVersion, err)
		}
		mldLogger := logger.WithValues(
			"kernel version", kernelVersion,
			"mld", mld,
		)
		if !completedSuccessfully {
			mldLogger.Info("Build has not finished successfully yet:skipping handling signing for now")
			continue
		}

		completedSuccessfully, err = r.reconHelperAPI.handleSigning(ctx, mld)
		if err != nil {
			return res, fmt.Errorf("failed to handle signing for kernel version %s: %v", kernelVersion, err)
		}
		if !completedSuccessfully {
			mldLogger.Info("Signing has not finished successfully yet")
		}
	}

	logger.Info("run garbage collector for build/sign pods")
	err = r.reconHelperAPI.garbageCollect(ctx, mod, mldMappings)
	if err != nil {
		return res, fmt.Errorf("failed to run garbage collection: %v", err)
	}

	logger.Info("Reconcile loop finished successfully")

	return res, nil
}

//go:generate mockgen -source=build_sign_reconciler.go -package=controllers -destination=mock_build_sign_reconciler.go buildSignReconcilerHelperAPI

type buildSignReconcilerHelperAPI interface {
	getRelevantKernelMappings(ctx context.Context, mod *kmmv1beta1.Module, targetedNodes []v1.Node) (map[string]*api.ModuleLoaderData, error)
	handleBuild(ctx context.Context, mld *api.ModuleLoaderData) (bool, error)
	handleSigning(ctx context.Context, mld *api.ModuleLoaderData) (bool, error)
	garbageCollect(ctx context.Context, mod *kmmv1beta1.Module, mldMappings map[string]*api.ModuleLoaderData) error
}

type buildSignReconcilerHelper struct {
	client    client.Client
	buildAPI  build.Manager
	signAPI   sign.SignManager
	kernelAPI module.KernelMapper
}

func newBuildSignReconcilerHelper(client client.Client,
	buildAPI build.Manager,
	signAPI sign.SignManager,
	kernelAPI module.KernelMapper) buildSignReconcilerHelperAPI {
	return &buildSignReconcilerHelper{
		client:    client,
		buildAPI:  buildAPI,
		signAPI:   signAPI,
		kernelAPI: kernelAPI,
	}
}
func (bsrh *buildSignReconcilerHelper) getRelevantKernelMappings(ctx context.Context,
	mod *kmmv1beta1.Module,
	targetedNodes []v1.Node) (map[string]*api.ModuleLoaderData, error) {

	mldMappings := make(map[string]*api.ModuleLoaderData)
	logger := log.FromContext(ctx)

	for _, node := range targetedNodes {
		kernelVersion := strings.TrimSuffix(node.Status.NodeInfo.KernelVersion, "+")

		nodeLogger := logger.WithValues(
			"node", node.Name,
			"kernel version", kernelVersion,
		)

		if mld, ok := mldMappings[kernelVersion]; ok {
			nodeLogger.V(1).Info("Using cached mld mapping", "mld", mld)
			continue
		}

		mld, err := bsrh.kernelAPI.GetModuleLoaderDataForKernel(mod, kernelVersion)
		if err != nil {
			nodeLogger.Error(err, "failed to get and process kernel mapping")
			continue
		}

		nodeLogger.V(1).Info("Found a valid mapping",
			"image", mld.ContainerImage,
			"build", mld.Build != nil,
		)

		mldMappings[kernelVersion] = mld
	}
	return mldMappings, nil
}

// handleBuild returns true if build is not needed or finished successfully
func (bsrh *buildSignReconcilerHelper) handleBuild(ctx context.Context, mld *api.ModuleLoaderData) (bool, error) {

	shouldSync, err := bsrh.buildAPI.ShouldSync(ctx, mld)
	if err != nil {
		return false, fmt.Errorf("could not check if build synchronization is needed: %w", err)
	}
	if !shouldSync {
		return true, nil
	}

	logger := log.FromContext(ctx).WithValues("kernel version", mld.KernelVersion, "image", mld.ContainerImage)
	buildCtx := log.IntoContext(ctx, logger)

	buildStatus, err := bsrh.buildAPI.Sync(buildCtx, mld, true, mld.Owner)
	if err != nil {
		return false, fmt.Errorf("could not synchronize the build: %w", err)
	}

	completedSuccessfully := false
	switch buildStatus {
	case utils.StatusCompleted:
		completedSuccessfully = true
	case utils.StatusFailed:
		logger.Info(utils.WarnString("Build pod has failed. If the fix is not in Module CR, then delete pod after the fix in order to restart the pod"))
	}

	return completedSuccessfully, nil
}

// handleSigning returns true if signing is not needed or finished successfully
func (bsrh *buildSignReconcilerHelper) handleSigning(ctx context.Context, mld *api.ModuleLoaderData) (bool, error) {
	shouldSync, err := bsrh.signAPI.ShouldSync(ctx, mld)
	if err != nil {
		return false, fmt.Errorf("cound not check if synchronization is needed: %w", err)
	}
	if !shouldSync {
		return true, nil
	}

	// if we need to sign AND we've built, then we must have built the intermediate image so must figure out its name
	previousImage := ""
	if module.ShouldBeBuilt(mld) {
		previousImage = module.IntermediateImageName(mld.Name, mld.Namespace, mld.ContainerImage)
	}

	logger := log.FromContext(ctx).WithValues("kernel version", mld.KernelVersion, "image", mld.ContainerImage)
	signCtx := log.IntoContext(ctx, logger)

	signStatus, err := bsrh.signAPI.Sync(signCtx, mld, previousImage, true, mld.Owner)
	if err != nil {
		return false, fmt.Errorf("could not synchronize the signing: %w", err)
	}

	completedSuccessfully := false
	switch signStatus {
	case utils.StatusCompleted:
		completedSuccessfully = true
	case utils.StatusFailed:
		logger.Info(utils.WarnString("Sign pod has failed. If the fix is not in Module CR, then delete pod after the fix in order to restart the pod"))
	}

	return completedSuccessfully, nil
}

func (bsrh *buildSignReconcilerHelper) garbageCollect(ctx context.Context,
	mod *kmmv1beta1.Module,
	mldMappings map[string]*api.ModuleLoaderData) error {
	logger := log.FromContext(ctx)

	// Garbage collect for successfully finished build pods
	deleted, err := bsrh.buildAPI.GarbageCollect(ctx, mod.Name, mod.Namespace, mod)
	if err != nil {
		return fmt.Errorf("could not garbage collect build objects: %v", err)
	}

	logger.Info("Garbage-collected Build objects", "names", deleted)

	// Garbage collect for successfully finished sign pods
	deleted, err = bsrh.signAPI.GarbageCollect(ctx, mod.Name, mod.Namespace, mod)
	if err != nil {
		return fmt.Errorf("could not garbage collect sign objects: %v", err)
	}

	logger.Info("Garbage-collected Sign objects", "names", deleted)

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *BuildSignReconciler) SetupWithManager(mgr ctrl.Manager, kernelLabel string) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kmmv1beta1.Module{}).
		Owns(&v1.Pod{}).
		Watches(
			&v1.Node{},
			handler.EnqueueRequestsFromMapFunc(r.filter.FindModulesForNode),
			builder.WithPredicates(
				r.filter.ModuleReconcilerNodePredicate(kernelLabel),
			),
		).
		Named(BuildSignReconcilerName).
		Complete(
			reconcile.AsReconciler[*kmmv1beta1.Module](mgr.GetClient(), r),
		)
}
