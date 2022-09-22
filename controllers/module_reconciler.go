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
	"strings"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/auth"
	"github.com/kubernetes-sigs/kernel-module-management/internal/build"
	"github.com/kubernetes-sigs/kernel-module-management/internal/daemonset"
	"github.com/kubernetes-sigs/kernel-module-management/internal/filter"
	"github.com/kubernetes-sigs/kernel-module-management/internal/metrics"
	"github.com/kubernetes-sigs/kernel-module-management/internal/module"
	"github.com/kubernetes-sigs/kernel-module-management/internal/rbac"
	"github.com/kubernetes-sigs/kernel-module-management/internal/registry"
	"github.com/kubernetes-sigs/kernel-module-management/internal/sign"
	"github.com/kubernetes-sigs/kernel-module-management/internal/statusupdater"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// ModuleReconciler reconciles a Module object
type ModuleReconciler struct {
	client.Client

	buildAPI         build.Manager
	signAPI          sign.SignManager
	rbacAPI          rbac.RBACCreator
	daemonAPI        daemonset.DaemonSetCreator
	kernelAPI        module.KernelMapper
	metricsAPI       metrics.Metrics
	registry         registry.Registry
	filter           *filter.Filter
	statusUpdaterAPI statusupdater.ModuleStatusUpdater
}

func NewModuleReconciler(
	client client.Client,
	buildAPI build.Manager,
	signAPI sign.SignManager,
	rbacAPI rbac.RBACCreator,
	daemonAPI daemonset.DaemonSetCreator,
	kernelAPI module.KernelMapper,
	metricsAPI metrics.Metrics,
	filter *filter.Filter,
	registry registry.Registry,
	statusUpdaterAPI statusupdater.ModuleStatusUpdater) *ModuleReconciler {
	return &ModuleReconciler{
		Client:           client,
		buildAPI:         buildAPI,
		signAPI:          signAPI,
		rbacAPI:          rbacAPI,
		daemonAPI:        daemonAPI,
		kernelAPI:        kernelAPI,
		metricsAPI:       metricsAPI,
		filter:           filter,
		registry:         registry,
		statusUpdaterAPI: statusUpdaterAPI,
	}
}

//+kubebuilder:rbac:groups=kmm.sigs.k8s.io,resources=modules,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=kmm.sigs.k8s.io,resources=modules/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=create;delete;get;list;patch;watch
//+kubebuilder:rbac:groups="core",resources=nodes,verbs=get;list;watch
//+kubebuilder:rbac:groups="core",resources=secrets,verbs=get;list;watch
//+kubebuilder:rbac:groups="core",resources=configmaps,verbs=get;list
//+kubebuilder:rbac:groups="core",resources=serviceaccounts,verbs=create;delete;get;list;patch;watch
//+kubebuilder:rbac:groups="batch",resources=jobs,verbs=create;list;watch;delete

// Reconcile lists all nodes and looks for kernels that match its mappings.
// For each mapping that matches at least one node in the cluster, it creates a DaemonSet running the container image
// on the nodes with a compatible kernel.
func (r *ModuleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	res := ctrl.Result{}

	logger := log.FromContext(ctx)

	mod, err := r.getRequestedModule(ctx, req.NamespacedName)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			logger.Info("Module deleted")
			return ctrl.Result{}, nil
		}

		return res, fmt.Errorf("failed to get the requested %s KMMO CR: %w", req.NamespacedName, err)
	}

	r.setKMMOMetrics(ctx)

	if mod.Spec.ModuleLoader.ServiceAccountName == "" {
		if err := r.rbacAPI.CreateModuleLoaderServiceAccount(ctx, *mod); err != nil {
			return res, fmt.Errorf("could not create module-loader's ServiceAccount: %w", err)
		}
	}
	if mod.Spec.DevicePlugin != nil && mod.Spec.DevicePlugin.ServiceAccountName == "" {
		if err := r.rbacAPI.CreateDevicePluginServiceAccount(ctx, *mod); err != nil {
			return res, fmt.Errorf("could not create device-plugin's ServiceAccount: %w", err)
		}
	}

	targetedNodes, err := r.getNodesListBySelector(ctx, mod)
	if err != nil {
		return res, fmt.Errorf("could get targeted nodes for module %s: %w", mod.Name, err)
	}

	mappings, nodesWithMapping, err := r.getRelevantKernelMappingsAndNodes(ctx, mod, targetedNodes)
	if err != nil {
		return res, fmt.Errorf("could get kernel mappings and nodes for modules %s: %w", mod.Name, err)
	}

	dsByKernelVersion, err := r.daemonAPI.ModuleDaemonSetsByKernelVersion(ctx, mod.Name, mod.Namespace)
	if err != nil {
		return res, fmt.Errorf("could get DaemonSets for module %s: %v", mod.Name, err)
	}

	for kernelVersion, m := range mappings {
		requeue, err := r.handleBuild(ctx, mod, m, kernelVersion, m.ContainerImage)
		if err != nil {
			return res, fmt.Errorf("failed to handle build for kernel version %s: %w", kernelVersion, err)
		}
		if requeue {
			logger.Info("Build requires a requeue; skipping handling driver container for now", "kernelVersion", kernelVersion, "image", m)
			res.Requeue = true
			continue
		}

		signrequeue, err := r.handleSigning(ctx, mod, m, kernelVersion, m.ContainerImage)
		if err != nil {
			return res, fmt.Errorf("failed to handle signing for kernel version %s: %v", kernelVersion, err)
		}
		if signrequeue {
			logger.Info("Signing requires a requeue; skipping handling driver container for now", "kernelVersion", kernelVersion, "image", m)
			res.Requeue = true
			continue
		}

		err = r.handleDriverContainer(ctx, mod, m, dsByKernelVersion, kernelVersion)
		if err != nil {
			return res, fmt.Errorf("failed to handle driver container for kernel version %s: %v", kernelVersion, err)
		}
	}

	logger.Info("Handle device plugin")
	err = r.handleDevicePlugin(ctx, mod)
	if err != nil {
		return res, fmt.Errorf("could handle device plugin: %w", err)
	}

	logger.Info("Run garbage collection")
	err = r.garbageCollect(ctx, mod, mappings, dsByKernelVersion)
	if err != nil {
		return res, fmt.Errorf("failed to run garbage collection: %v", err)
	}

	err = r.statusUpdaterAPI.ModuleUpdateStatus(ctx, mod, nodesWithMapping, targetedNodes, dsByKernelVersion)
	if err != nil {
		return res, fmt.Errorf("failed to update status of the module: %w", err)
	}

	logger.Info("Reconcile loop finished successfully")

	return res, nil
}

func (r *ModuleReconciler) getRelevantKernelMappingsAndNodes(ctx context.Context,
	mod *kmmv1beta1.Module,
	targetedNodes []v1.Node) (map[string]*kmmv1beta1.KernelMapping, []v1.Node, error) {

	mappings := make(map[string]*kmmv1beta1.KernelMapping)
	logger := log.FromContext(ctx)

	nodes := make([]v1.Node, 0, len(targetedNodes))

	for _, node := range targetedNodes {
		osConfig := r.kernelAPI.GetNodeOSConfig(&node)
		kernelVersion := strings.TrimSuffix(node.Status.NodeInfo.KernelVersion, "+")

		nodeLogger := logger.WithValues(
			"node", node.Name,
			"kernel version", kernelVersion,
		)

		if image, ok := mappings[kernelVersion]; ok {
			nodes = append(nodes, node)
			nodeLogger.V(1).Info("Using cached image", "image", image)
			continue
		}

		m, err := r.kernelAPI.FindMappingForKernel(mod.Spec.ModuleLoader.Container.KernelMappings, kernelVersion)
		if err != nil {
			nodeLogger.Info("no suitable container image found; skipping node")
			continue
		}

		m, err = r.kernelAPI.PrepareKernelMapping(m, osConfig)
		if err != nil {
			nodes = append(nodes, node)
			nodeLogger.Info("failed to substitute the template variables in the mapping", "error", err)
			continue
		}

		nodeLogger.V(1).Info("Found a valid mapping",
			"image", m.ContainerImage,
			"build", m.Build != nil,
		)

		mappings[kernelVersion] = m
		nodes = append(nodes, node)
	}
	return mappings, nodes, nil
}

func (r *ModuleReconciler) getNodesListBySelector(ctx context.Context, mod *kmmv1beta1.Module) ([]v1.Node, error) {
	logger := log.FromContext(ctx)
	logger.V(1).Info("Listing nodes", "selector", mod.Spec.Selector)

	selectedNodes := v1.NodeList{}
	opt := client.MatchingLabels(mod.Spec.Selector)
	if err := r.Client.List(ctx, &selectedNodes, opt); err != nil {
		logger.Error(err, "Could not list nodes")
		return nil, fmt.Errorf("could not list nodes: %v", err)
	}
	nodes := make([]v1.Node, 0, len(selectedNodes.Items))

	for _, node := range selectedNodes.Items {
		if isNodeSchedulable(&node) {
			nodes = append(nodes, node)
		}
	}
	return nodes, nil
}

func (r *ModuleReconciler) handleBuild(ctx context.Context,
	mod *kmmv1beta1.Module,
	km *kmmv1beta1.KernelMapping,
	kernelVersion string,
	containerImage string) (bool, error) {

	//if theres no build specified skip this section
	if !r.willBuild(mod, km) {
		return false, nil
	}
	//if build is specified AND sign is specified then we want to build an intermediate image
	// and let sign produce the one specified in containerImage
	if r.willSign(mod, km) {
		containerImage = r.appendToTag(containerImage, mod.Namespace+"_"+mod.Name+"_kmm_unsigned")
	}

	// build is specified and containerImage is either the final image, or the intermediate image tag depending on if sign is defined
	// either way if containerImage exists we can skip building it
	exists, err := r.checkImageExists(ctx, mod, km, containerImage)
	if err != nil {
		return false, fmt.Errorf("failed to check existence of image %s for kernel %s: %w", containerImage, kernelVersion, err)
	}
	if exists {
		return false, nil
	}

	logger := log.FromContext(ctx).WithValues("kernel version", kernelVersion, "image", containerImage)
	buildCtx := log.IntoContext(ctx, logger)

	buildRes, err := r.buildAPI.Sync(buildCtx, *mod, *km, kernelVersion, containerImage, true)
	if err != nil {
		return false, fmt.Errorf("could not synchronize the build: %w", err)
	}

	switch buildRes.Status {
	case build.StatusCreated:
		r.metricsAPI.SetCompletedStage(mod.Name, mod.Namespace, kernelVersion, metrics.BuildStage, false)
	case build.StatusCompleted:
		r.metricsAPI.SetCompletedStage(mod.Name, mod.Namespace, kernelVersion, metrics.BuildStage, true)
	}

	return buildRes.Requeue, nil
}

// is sign defined in the CR
func (r *ModuleReconciler) willSign(mod *kmmv1beta1.Module, km *kmmv1beta1.KernelMapping) bool {
	if mod.Spec.ModuleLoader.Container.Sign == nil && km.Sign == nil {
		return false
	}
	return true
}

// is build defined in the CR
func (r *ModuleReconciler) willBuild(mod *kmmv1beta1.Module, km *kmmv1beta1.KernelMapping) bool {
	if mod.Spec.ModuleLoader.Container.Build == nil && km.Build == nil {
		return false
	}
	return true
}

// append tag to the image name cleanly, avoiding messing up the name or getting "name:-tag"
func (r *ModuleReconciler) appendToTag(name string, tag string) string {
	if strings.Contains(name, ":") {
		return name + "_" + tag
	} else {
		return name + ":" + tag
	}

}

func (r *ModuleReconciler) handleSigning(ctx context.Context,
	mod *kmmv1beta1.Module,
	km *kmmv1beta1.KernelMapping,
	kernelVersion string,
	containerImage string) (bool, error) {

	// if sign is not specified skip
	if !r.willSign(mod, km) {
		return false, nil
	}

	// if its already built, skip
	exists, err := r.checkImageExists(ctx, mod, km, containerImage)
	if err != nil {
		return false, fmt.Errorf("failed to check existence of image %s for kernel %s: %w", containerImage, kernelVersion, err)
	}
	if exists {
		return false, nil
	}

	// if we need to sign AND we've built, then we must have built the intermediate image so must figure out its name
	previousImage := ""
	if r.willBuild(mod, km) {
		previousImage = r.appendToTag(containerImage, mod.Namespace+"_"+mod.Name+"_kmm_unsigned")
		if err != nil {
			return false, err
		}
	}

	logger := log.FromContext(ctx).WithValues("kernel version", kernelVersion, "image", containerImage)
	signCtx := log.IntoContext(ctx, logger)

	signRes, err := r.signAPI.Sync(signCtx, *mod, *km, kernelVersion, previousImage, containerImage, true)
	if err != nil {
		return false, fmt.Errorf("could not synchronize the signing: %w", err)
	}

	switch signRes.Status {
	case utils.StatusCreated:
		r.metricsAPI.SetCompletedStage(mod.Name, mod.Namespace, kernelVersion, metrics.SignStage, false)
	case utils.StatusCompleted:
		r.metricsAPI.SetCompletedStage(mod.Name, mod.Namespace, kernelVersion, metrics.SignStage, true)
	}

	return signRes.Requeue, nil
}

func (r *ModuleReconciler) checkImageExists(ctx context.Context, mod *kmmv1beta1.Module, km *kmmv1beta1.KernelMapping, imageName string) (bool, error) {
	registryAuthGetter := auth.NewRegistryAuthGetterFrom(r.Client, mod)
	tlsOptions := module.GetRelevantTLSOptions(mod, km)
	imageAvailable, err := r.registry.ImageExists(ctx, imageName, tlsOptions, registryAuthGetter)
	if err != nil {
		return false, fmt.Errorf("could not check if the image is available: %v", err)
	}
	return imageAvailable, nil
}

func (r *ModuleReconciler) handleDriverContainer(ctx context.Context,
	mod *kmmv1beta1.Module,
	km *kmmv1beta1.KernelMapping,
	dsByKernelVersion map[string]*appsv1.DaemonSet,
	kernelVersion string) error {
	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Namespace: mod.Namespace},
	}

	logger := log.FromContext(ctx)
	if existingDS := dsByKernelVersion[kernelVersion]; existingDS != nil {
		logger.Info("updating existing driver container DS", "kernel version", kernelVersion, "image", km, "name", ds.Name)
		ds = existingDS
	} else {
		logger.Info("creating new driver container DS", "kernel version", kernelVersion, "image", km)
		ds.GenerateName = mod.Name + "-"
	}

	opRes, err := controllerutil.CreateOrPatch(ctx, r.Client, ds, func() error {
		return r.daemonAPI.SetDriverContainerAsDesired(ctx, ds, km.ContainerImage, *mod, kernelVersion)
	})

	if err == nil {
		if opRes == controllerutil.OperationResultCreated {
			r.metricsAPI.SetCompletedStage(mod.Name, mod.Namespace, kernelVersion, metrics.ModuleLoaderStage, false)
		}
		logger.Info("Reconciled Driver Container", "name", ds.Name, "result", opRes)
	}

	return err
}

func (r *ModuleReconciler) handleDevicePlugin(ctx context.Context, mod *kmmv1beta1.Module) error {
	if mod.Spec.DevicePlugin == nil {
		return nil
	}

	logger := log.FromContext(ctx)
	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Namespace: mod.Namespace},
	}
	name := mod.Name + "-device-plugin"
	ds.Name = name
	err := r.Client.Get(ctx, types.NamespacedName{Name: name, Namespace: mod.Namespace}, ds)
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to get the device plugin daemonset %s/%s: %w", name, mod.Namespace, err)
	}

	opRes, err := controllerutil.CreateOrPatch(ctx, r.Client, ds, func() error {
		return r.daemonAPI.SetDevicePluginAsDesired(ctx, ds, mod)
	})

	if err == nil {
		if opRes == controllerutil.OperationResultCreated {
			r.metricsAPI.SetCompletedStage(mod.Name, mod.Namespace, "", metrics.DevicePluginStage, false)
		}
		logger.Info("Reconciled Device Plugin", "name", ds.Name, "result", opRes)
	}

	return err
}

func (r *ModuleReconciler) garbageCollect(ctx context.Context,
	mod *kmmv1beta1.Module,
	mappings map[string]*kmmv1beta1.KernelMapping,
	existingDS map[string]*appsv1.DaemonSet) error {
	logger := log.FromContext(ctx)
	// Garbage collect old DaemonSets for which there are no nodes.
	validKernels := sets.StringKeySet(mappings)

	deleted, err := r.daemonAPI.GarbageCollect(ctx, existingDS, validKernels)
	if err != nil {
		return fmt.Errorf("could not garbage collect DaemonSets: %v", err)
	}

	logger.Info("Garbage-collected DaemonSets", "names", deleted)

	// Garbage collect for successfully finished build jobs
	deleted, err = r.buildAPI.GarbageCollect(ctx, *mod)
	if err != nil {
		return fmt.Errorf("could not garbage collect build objects: %v", err)
	}

	logger.Info("Garbage-collected Build objects", "names", deleted)

	return nil
}

func (r *ModuleReconciler) setKMMOMetrics(ctx context.Context) {
	logger := log.FromContext(ctx)

	mods := kmmv1beta1.ModuleList{}
	err := r.Client.List(ctx, &mods)
	if err != nil {
		logger.V(1).Info("failed to list KMMomodules for metrics", "error", err)
	}

	r.metricsAPI.SetExistingKMMOModules(len(mods.Items))
}

func (r *ModuleReconciler) getRequestedModule(ctx context.Context, namespacedName types.NamespacedName) (*kmmv1beta1.Module, error) {
	mod := kmmv1beta1.Module{}

	if err := r.Client.Get(ctx, namespacedName, &mod); err != nil {
		return nil, fmt.Errorf("failed to get the kmmo module %s: %w", namespacedName, err)
	}
	return &mod, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ModuleReconciler) SetupWithManager(mgr ctrl.Manager, kernelLabel string) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kmmv1beta1.Module{}).
		Owns(&appsv1.DaemonSet{}).
		Owns(&v1.ServiceAccount{}).
		Owns(&batchv1.Job{}).
		Watches(
			&source.Kind{Type: &v1.Node{}},
			handler.EnqueueRequestsFromMapFunc(r.filter.FindModulesForNode),
			builder.WithPredicates(
				r.filter.ModuleReconcilerNodePredicate(kernelLabel),
			),
		).
		Named("module").
		Complete(r)
}

func isNodeSchedulable(node *v1.Node) bool {
	for _, taint := range node.Spec.Taints {
		if taint.Effect == v1.TaintEffectNoSchedule {
			return false
		}
	}
	return true
}
