package controllers

import (
	"context"
	"errors"
	"fmt"
	"strings"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/api"
	"github.com/kubernetes-sigs/kernel-module-management/internal/filter"
	"github.com/kubernetes-sigs/kernel-module-management/internal/module"
	"github.com/kubernetes-sigs/kernel-module-management/internal/nmc"
	"github.com/kubernetes-sigs/kernel-module-management/internal/registry"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

//+kubebuilder:rbac:groups="core",resources=nodes,verbs=get;watch
//+kubebuilder:rbac:groups=kmm.sigs.x-k8s.io,resources=nodemodulesconfigs,verbs=get;list;watch;patch;create

const (
	ModuleNMCReconcilerName = "ModuleNMCReconciler"
)

type ModuleNMCReconciler struct {
	kernelAPI   module.KernelMapper
	filter      *filter.Filter
	reconHelper moduleNMCReconcilerHelperAPI
}

func NewModuleNMCReconciler(client client.Client,
	kernelAPI module.KernelMapper,
	registryAPI registry.Registry,
	nmcHelper nmc.Helper,
	filter *filter.Filter) *ModuleNMCReconciler {
	reconHelper := newModuleNMCReconcilerHelper(client, registryAPI, nmcHelper)
	return &ModuleNMCReconciler{
		kernelAPI:   kernelAPI,
		filter:      filter,
		reconHelper: reconHelper,
	}
}

func (mnr *ModuleNMCReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// get the reconciled module
	logger := log.FromContext(ctx)

	logger.Info("Starting Module-NMS reconcilation", "module name and namespace", req.NamespacedName)

	mod, err := mnr.reconHelper.getRequestedModule(ctx, req.NamespacedName)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			logger.Info("Module deleted")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to get the requested %s Module: %v", req.NamespacedName, err)
	}

	// get all nodes
	nodes, err := mnr.reconHelper.getNodesList(ctx)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get nodes list: %v", err)
	}

	errs := make([]error, 0, len(nodes))
	for _, node := range nodes {
		kernelVersion := strings.TrimSuffix(node.Status.NodeInfo.KernelVersion, "+")
		mld, err := mnr.kernelAPI.GetModuleLoaderDataForKernel(mod, kernelVersion)
		if err != nil && !errors.Is(err, module.ErrNoMatchingKernelMapping) {
			logger.Info(utils.WarnString(fmt.Sprintf("internal errors while fetching kernel mapping for version %s: %v", kernelVersion, err)))
			errs = append(errs, err)
			continue
		}
		shouldBeOnNode, err := mnr.reconHelper.shouldModuleRunOnNode(node, mld)
		if err != nil {
			logger.Info(utils.WarnString(fmt.Sprintf("failed to determine if module %s/%s should be on node %s: %v", mld.Namespace, mld.Name, node.Name, err)))
			errs = append(errs, err)
			continue
		}
		if shouldBeOnNode {
			err = mnr.reconHelper.enableModuleOnNode(ctx, mld, node.Name, kernelVersion)
		} else {
			err = mnr.reconHelper.disableModuleOnNode(ctx, mod.Namespace, mod.Name, node.Name)
		}
		errs = append(errs, err)
	}

	err = errors.Join(errs...)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to reconcile module %s/%s with nodes: %v", mod.Namespace, mod.Name, err)
	}
	return ctrl.Result{}, nil
}

//go:generate mockgen -source=module_nmc_reconciler.go -package=controllers -destination=mock_module_nmc_reconciler.go moduleNMCReconcilerHelperAPI

type moduleNMCReconcilerHelperAPI interface {
	getRequestedModule(ctx context.Context, namespacedName types.NamespacedName) (*kmmv1beta1.Module, error)
	getNodesList(ctx context.Context) ([]v1.Node, error)
	shouldModuleRunOnNode(node v1.Node, mld *api.ModuleLoaderData) (bool, error)
	enableModuleOnNode(ctx context.Context, mld *api.ModuleLoaderData, nodeName, kernelVersion string) error
	disableModuleOnNode(ctx context.Context, modNamespace, modName, nodeName string) error
}

type moduleNMCReconcilerHelper struct {
	client      client.Client
	registryAPI registry.Registry
	nmcHelper   nmc.Helper
}

func newModuleNMCReconcilerHelper(client client.Client, registryAPI registry.Registry, nmcHelper nmc.Helper) moduleNMCReconcilerHelperAPI {
	return &moduleNMCReconcilerHelper{
		client:      client,
		registryAPI: registryAPI,
		nmcHelper:   nmcHelper,
	}
}

func (mnrh *moduleNMCReconcilerHelper) getRequestedModule(ctx context.Context, namespacedName types.NamespacedName) (*kmmv1beta1.Module, error) {
	mod := kmmv1beta1.Module{}

	if err := mnrh.client.Get(ctx, namespacedName, &mod); err != nil {
		return nil, fmt.Errorf("failed to get Module %s: %w", namespacedName, err)
	}
	return &mod, nil
}

func (mnrh *moduleNMCReconcilerHelper) getNodesList(ctx context.Context) ([]v1.Node, error) {
	nodes := v1.NodeList{}
	err := mnrh.client.List(ctx, &nodes)
	if err != nil {
		return nil, fmt.Errorf("failed to get list of nodes: %v", err)
	}
	return nodes.Items, nil
}

func (mnrh *moduleNMCReconcilerHelper) shouldModuleRunOnNode(node v1.Node, mld *api.ModuleLoaderData) (bool, error) {
	if mld == nil {
		return false, nil
	}

	nodeKernelVersion := strings.TrimSuffix(node.Status.NodeInfo.KernelVersion, "+")
	if nodeKernelVersion != mld.KernelVersion {
		return false, nil
	}

	if !utils.IsNodeSchedulable(&node) {
		return false, nil
	}

	return utils.IsObjectSelectedByLabels(node.GetLabels(), mld.Selector)
}

func (mnrh *moduleNMCReconcilerHelper) enableModuleOnNode(ctx context.Context, mld *api.ModuleLoaderData, nodeName, kernelVersion string) error {
	logger := log.FromContext(ctx)
	exists, err := module.ImageExists(ctx, mnrh.client, mnrh.registryAPI, mld, mld.Namespace, mld.ContainerImage)
	if err != nil {
		return fmt.Errorf("failed to verify is image %s exists: %v", mld.ContainerImage, err)
	}
	if !exists {
		// skip updating NMC, reconciliation will kick in once the build pod is completed
		return nil
	}
	moduleConfig := kmmv1beta1.ModuleConfig{
		KernelVersion:        kernelVersion,
		ContainerImage:       mld.ContainerImage,
		InTreeModuleToRemove: mld.InTreeModuleToRemove,
		Modprobe:             mld.Modprobe,
	}

	nmcObj := &kmmv1beta1.NodeModulesConfig{
		ObjectMeta: metav1.ObjectMeta{Name: nodeName},
	}

	opRes, err := controllerutil.CreateOrPatch(ctx, mnrh.client, nmcObj, func() error {
		return nmc.SetModuleConfig(nmcObj, mld.Namespace, mld.Name, &moduleConfig)
	})

	if err != nil {
		return fmt.Errorf("failed to enable module %s/%s in NMC %s: %v", mld.Namespace, mld.Name, nodeName, err)
	}
	logger.Info("Enable module in NMC", "name", mld.Name, "namespace", mld.Namespace, "node", nodeName, "result", opRes)
	return nil
}

func (mnrh *moduleNMCReconcilerHelper) disableModuleOnNode(ctx context.Context, modNamespace, modName, nodeName string) error {
	logger := log.FromContext(ctx)
	nmcObj, err := mnrh.nmcHelper.Get(ctx, nodeName)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			// NodeModulesConfig does not exists, module was never running on the node, we are good
			return nil
		}
		return fmt.Errorf("failed to get the NodeModulesConfig for node %s: %v", nodeName, err)
	}

	opRes, err := controllerutil.CreateOrPatch(ctx, mnrh.client, nmcObj, func() error {
		return nmc.RemoveModuleConfig(nmcObj, modNamespace, modName)
	})

	if err != nil {
		return fmt.Errorf("failed to diable module %s/%s in NMC %s: %v", modNamespace, modName, nodeName, err)
	}

	logger.Info("Disable module in NMC", "name", modName, "namespace", modNamespace, "node", nodeName, "result", opRes)
	return nil
}

func (mnr *ModuleNMCReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.
		NewControllerManagedBy(mgr).
		For(&kmmv1beta1.Module{}).
		Owns(&kmmv1beta1.NodeModulesConfig{}).
		Owns(&v1.Pod{}, builder.WithPredicates(filter.ModuleNMCReconcilePodPredicate())).
		Watches(
			&v1.Node{},
			handler.EnqueueRequestsFromMapFunc(mnr.filter.FindModulesForNMCNodeChange),
			builder.WithPredicates(
				filter.ModuleNMCReconcilerNodePredicate(),
			),
		).
		Named(ModuleNMCReconcilerName).
		Complete(mnr)
}
