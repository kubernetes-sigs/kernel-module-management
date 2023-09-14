package controllers

import (
	"context"
	"errors"
	"fmt"
	"strings"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/api"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	"github.com/kubernetes-sigs/kernel-module-management/internal/filter"
	"github.com/kubernetes-sigs/kernel-module-management/internal/module"
	"github.com/kubernetes-sigs/kernel-module-management/internal/nmc"
	"github.com/kubernetes-sigs/kernel-module-management/internal/registry"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
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
	actionDelete            = "delete"
	actionAdd               = "add"
)

type schedulingData struct {
	action string
	mld    *api.ModuleLoaderData
	node   *v1.Node
}

type ModuleNMCReconciler struct {
	filter      *filter.Filter
	reconHelper moduleNMCReconcilerHelperAPI
}

func NewModuleNMCReconciler(client client.Client,
	kernelAPI module.KernelMapper,
	registryAPI registry.Registry,
	nmcHelper nmc.Helper,
	filter *filter.Filter,
	scheme *runtime.Scheme) *ModuleNMCReconciler {
	reconHelper := newModuleNMCReconcilerHelper(client, kernelAPI, registryAPI, nmcHelper, scheme)
	return &ModuleNMCReconciler{
		filter:      filter,
		reconHelper: reconHelper,
	}
}

func (mnr *ModuleNMCReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	logger.Info("Starting Module-NMS reconcilation", "module name and namespace", req.NamespacedName)

	mod, err := mnr.reconHelper.getRequestedModule(ctx, req.NamespacedName)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			logger.Info("Module deleted, nothing to do")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to get the requested %s Module: %v", req.NamespacedName, err)
	}
	if mod.GetDeletionTimestamp() != nil {
		//Module is being deleted
		err = mnr.reconHelper.finalizeModule(ctx, mod)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to finalize %s Module: %v", req.NamespacedName, err)
		}
		return ctrl.Result{}, nil
	}

	err = mnr.reconHelper.setFinalizer(ctx, mod)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to set finalizer on %s Module: %v", req.NamespacedName, err)
	}

	// get nodes targeted by selector
	targetedNodes, err := mnr.reconHelper.getNodesListBySelector(ctx, mod)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get list of nodes by selector: %v", err)
	}

	currentNMCs, err := mnr.reconHelper.getNMCsByModuleSet(ctx, mod)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get NMCs for %s Module: %v", req.NamespacedName, err)
	}

	sdMap, prepareErrs := mnr.reconHelper.prepareSchedulingData(ctx, mod, targetedNodes, currentNMCs)
	errs := make([]error, 0, len(sdMap)+1)
	errs = append(errs, prepareErrs...)

	for nodeName, sd := range sdMap {
		if sd.action == actionAdd {
			err = mnr.reconHelper.enableModuleOnNode(ctx, sd.mld, sd.node)
		}
		if sd.action == actionDelete {
			err = mnr.reconHelper.disableModuleOnNode(ctx, mod.Namespace, mod.Name, nodeName)
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
	setFinalizer(ctx context.Context, mod *kmmv1beta1.Module) error
	finalizeModule(ctx context.Context, mod *kmmv1beta1.Module) error
	getRequestedModule(ctx context.Context, namespacedName types.NamespacedName) (*kmmv1beta1.Module, error)
	getNodesListBySelector(ctx context.Context, mod *kmmv1beta1.Module) ([]v1.Node, error)
	getNMCsByModuleSet(ctx context.Context, mod *kmmv1beta1.Module) (sets.Set[string], error)
	prepareSchedulingData(ctx context.Context, mod *kmmv1beta1.Module, targetedNodes []v1.Node, currentNMCs sets.Set[string]) (map[string]schedulingData, []error)
	enableModuleOnNode(ctx context.Context, mld *api.ModuleLoaderData, node *v1.Node) error
	disableModuleOnNode(ctx context.Context, modNamespace, modName, nodeName string) error
}

type moduleNMCReconcilerHelper struct {
	client      client.Client
	kernelAPI   module.KernelMapper
	registryAPI registry.Registry
	nmcHelper   nmc.Helper
	scheme      *runtime.Scheme
}

func newModuleNMCReconcilerHelper(
	client client.Client,
	kernelAPI module.KernelMapper,
	registryAPI registry.Registry,
	nmcHelper nmc.Helper,
	scheme *runtime.Scheme) moduleNMCReconcilerHelperAPI {
	return &moduleNMCReconcilerHelper{
		client:      client,
		kernelAPI:   kernelAPI,
		registryAPI: registryAPI,
		nmcHelper:   nmcHelper,
		scheme:      scheme,
	}
}

func (mnrh *moduleNMCReconcilerHelper) setFinalizer(ctx context.Context, mod *kmmv1beta1.Module) error {
	if controllerutil.ContainsFinalizer(mod, constants.ModuleFinalizer) {
		return nil
	}

	logger := log.FromContext(ctx)
	logger.Info("Adding finalizer", "module name", mod.Name, "module namespace", mod.Namespace)

	modCopy := mod.DeepCopy()
	controllerutil.AddFinalizer(mod, constants.ModuleFinalizer)
	return mnrh.client.Patch(ctx, mod, client.MergeFrom(modCopy))
}

func (mnrh *moduleNMCReconcilerHelper) getRequestedModule(ctx context.Context, namespacedName types.NamespacedName) (*kmmv1beta1.Module, error) {
	mod := kmmv1beta1.Module{}

	if err := mnrh.client.Get(ctx, namespacedName, &mod); err != nil {
		return nil, fmt.Errorf("failed to get Module %s: %w", namespacedName, err)
	}
	return &mod, nil
}

func (mnrh *moduleNMCReconcilerHelper) finalizeModule(ctx context.Context, mod *kmmv1beta1.Module) error {
	nmcList := kmmv1beta1.NodeModulesConfigList{}
	err := mnrh.client.List(ctx, &nmcList)
	if err != nil {
		return fmt.Errorf("failed to list NMCs in the cluster: %v", err)
	}

	errs := make([]error, 0, len(nmcList.Items))
	for _, nmc := range nmcList.Items {
		err = mnrh.removeModuleFromNMC(ctx, &nmc, mod.Namespace, mod.Name)
		errs = append(errs, err)
	}

	err = errors.Join(errs...)
	if err != nil {
		return fmt.Errorf("failed to remove %s/%s module from some of NMCs: %v", mod.Namespace, mod.Name, err)
	}

	// remove finalizer
	modCopy := mod.DeepCopy()
	controllerutil.RemoveFinalizer(mod, constants.ModuleFinalizer)

	return mnrh.client.Patch(ctx, mod, client.MergeFrom(modCopy))
}

func (mnrh *moduleNMCReconcilerHelper) getNodesListBySelector(ctx context.Context, mod *kmmv1beta1.Module) ([]v1.Node, error) {
	logger := log.FromContext(ctx)
	logger.V(1).Info("Listing nodes", "selector", mod.Spec.Selector)

	selectedNodes := v1.NodeList{}
	opt := client.MatchingLabels(mod.Spec.Selector)
	if err := mnrh.client.List(ctx, &selectedNodes, opt); err != nil {
		return nil, fmt.Errorf("could not list nodes: %v", err)
	}
	nodes := make([]v1.Node, 0, len(selectedNodes.Items))

	for _, node := range selectedNodes.Items {
		if utils.IsNodeSchedulable(&node) {
			nodes = append(nodes, node)
		}
	}
	return nodes, nil
}

func (mnrh *moduleNMCReconcilerHelper) getNMCsByModuleSet(ctx context.Context, mod *kmmv1beta1.Module) (sets.Set[string], error) {
	nmcNamesList, err := mnrh.getNMCsNamesForModule(ctx, mod)
	if err != nil {
		return nil, fmt.Errorf("failed to get list of %s/%s module's NMC for map: %v", mod.Namespace, mod.Name, err)
	}

	return sets.New[string](nmcNamesList...), nil
}

func (mnrh *moduleNMCReconcilerHelper) getNMCsNamesForModule(ctx context.Context, mod *kmmv1beta1.Module) ([]string, error) {
	logger := log.FromContext(ctx)
	moduleNMCLabel := utils.GetModuleNMCLabel(mod.Namespace, mod.Name)
	logger.V(1).Info("Listing nmcs", "selector", moduleNMCLabel)
	selectedNMCs := kmmv1beta1.NodeModulesConfigList{}
	opt := client.MatchingLabels(map[string]string{moduleNMCLabel: ""})
	if err := mnrh.client.List(ctx, &selectedNMCs, opt); err != nil {
		return nil, fmt.Errorf("could not list NMCs: %v", err)
	}
	result := make([]string, len(selectedNMCs.Items))
	for i := range selectedNMCs.Items {
		result[i] = selectedNMCs.Items[i].Name
	}
	return result, nil
}

// prepareSchedulingData prepare data needed to scheduling enable/disable module per node
// in case there is an error during handling one of the nodes, function continues to the next node
// It returns the map of scheduling data per successfully processed node, and slice of errors
// per unsuccessfuly processed nodes
func (mnrh *moduleNMCReconcilerHelper) prepareSchedulingData(ctx context.Context,
	mod *kmmv1beta1.Module,
	targetedNodes []v1.Node,
	currentNMCs sets.Set[string]) (map[string]schedulingData, []error) {

	logger := log.FromContext(ctx)
	result := make(map[string]schedulingData)
	errs := make([]error, 0, len(targetedNodes))
	for _, node := range targetedNodes {
		kernelVersion := strings.TrimSuffix(node.Status.NodeInfo.KernelVersion, "+")
		mld, err := mnrh.kernelAPI.GetModuleLoaderDataForKernel(mod, kernelVersion)
		if err != nil && !errors.Is(err, module.ErrNoMatchingKernelMapping) {
			// deleting earlier, so as not to change NMC in case we failed to determine mld
			currentNMCs.Delete(node.Name)
			logger.Info(utils.WarnString(fmt.Sprintf("internal errors while fetching kernel mapping for version %s: %v", kernelVersion, err)))
			errs = append(errs, err)
			continue
		}
		result[node.Name] = prepareNodeSchedulingData(&node, mld, currentNMCs)
		currentNMCs.Delete(node.Name)
	}
	for _, nmcName := range currentNMCs.UnsortedList() {
		result[nmcName] = schedulingData{action: actionDelete}
	}
	return result, errs
}

func (mnrh *moduleNMCReconcilerHelper) enableModuleOnNode(ctx context.Context, mld *api.ModuleLoaderData, node *v1.Node) error {
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
		KernelVersion:        mld.KernelVersion,
		ContainerImage:       mld.ContainerImage,
		InTreeModuleToRemove: mld.InTreeModuleToRemove,
		Modprobe:             mld.Modprobe,
	}

	if tls := mld.RegistryTLS; tls != nil {
		moduleConfig.InsecurePull = tls.Insecure || tls.InsecureSkipTLSVerify
	}

	nmc := &kmmv1beta1.NodeModulesConfig{
		ObjectMeta: metav1.ObjectMeta{Name: node.Name},
	}

	opRes, err := controllerutil.CreateOrPatch(ctx, mnrh.client, nmc, func() error {
		err = mnrh.nmcHelper.SetModuleConfig(nmc, mld, &moduleConfig)
		if err != nil {
			return err
		}
		return controllerutil.SetOwnerReference(node, nmc, mnrh.scheme)
	})

	if err != nil {
		return fmt.Errorf("failed to enable module %s/%s in NMC %s: %v", mld.Namespace, mld.Name, node.Name, err)
	}
	logger.Info("Enable module in NMC", "name", mld.Name, "namespace", mld.Namespace, "node", node.Name, "result", opRes)
	return nil
}

func (mnrh *moduleNMCReconcilerHelper) disableModuleOnNode(ctx context.Context, modNamespace, modName, nodeName string) error {
	nmc := &kmmv1beta1.NodeModulesConfig{
		ObjectMeta: metav1.ObjectMeta{Name: nodeName},
	}

	return mnrh.removeModuleFromNMC(ctx, nmc, modNamespace, modName)
}

func (mnrh *moduleNMCReconcilerHelper) removeModuleFromNMC(ctx context.Context, nmc *kmmv1beta1.NodeModulesConfig, modNamespace, modName string) error {
	logger := log.FromContext(ctx)
	opRes, err := controllerutil.CreateOrPatch(ctx, mnrh.client, nmc, func() error {
		return mnrh.nmcHelper.RemoveModuleConfig(nmc, modNamespace, modName)
	})

	if err != nil {
		return fmt.Errorf("failed to disable module %s/%s in NMC %s: %v", modNamespace, modName, nmc.Name, err)
	}

	logger.Info("Disabled module in NMC", "name", modName, "namespace", modNamespace, "NMC", nmc.Name, "result", opRes)
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

func prepareNodeSchedulingData(node *v1.Node, mld *api.ModuleLoaderData, currentNMCs sets.Set[string]) schedulingData {
	versionLabel := ""
	present := false
	if mld != nil {
		versionLabel, present = utils.GetNodeWorkerPodVersionLabel(node.GetLabels(), mld.Namespace, mld.Name)
	}
	switch {
	case mld == nil && currentNMCs.Has(node.Name):
		// mld missing, Module does not have mapping for node's kernel, NMC for the node exists
		return schedulingData{action: actionDelete}
	case mld.ModuleVersion == "":
		// mld exists, Version not define, should be running
		return schedulingData{action: actionAdd, mld: mld, node: node}
	case present && versionLabel == mld.ModuleVersion:
		// mld exists, version label defined and equal to Module's version, should be running
		return schedulingData{action: actionAdd, mld: mld, node: node}
	case present && versionLabel != mld.ModuleVersion:
		// mld exists, version label defined but not equal to Module's version, nothing needs to be changed in NMC (the previous version should run)
		return schedulingData{}
	case !present && mld.ModuleVersion != "":
		// mld exists, version label missing, Module's version defined, shoud not be running
		return schedulingData{action: actionDelete}
	}
	// nothing should be done
	return schedulingData{}
}
