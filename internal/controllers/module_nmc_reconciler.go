package controllers

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/api"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	"github.com/kubernetes-sigs/kernel-module-management/internal/filter"
	"github.com/kubernetes-sigs/kernel-module-management/internal/meta"
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

	err = mnr.reconHelper.setFinalizerAndStatus(ctx, mod)
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

	err = mnr.reconHelper.moduleUpdateWorkerPodsStatus(ctx, mod, targetedNodes)
	errs = append(errs, err)

	err = errors.Join(errs...)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to reconcile module %s/%s config: %v", mod.Namespace, mod.Name, err)
	}
	return ctrl.Result{}, nil
}

func (mnr *ModuleNMCReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.
		NewControllerManagedBy(mgr).
		For(&kmmv1beta1.Module{}).
		Owns(&v1.Pod{}, builder.WithPredicates(filter.ModuleNMCReconcilePodPredicate())).
		Watches(
			&v1.Node{},
			handler.EnqueueRequestsFromMapFunc(mnr.filter.FindModulesForNMCNodeChange),
			builder.WithPredicates(
				filter.ModuleNMCReconcilerNodePredicate(),
			),
		).
		Watches(
			&kmmv1beta1.NodeModulesConfig{},
			handler.EnqueueRequestsFromMapFunc(filter.ListModulesForNMC),
		).
		Named(ModuleNMCReconcilerName).
		Complete(mnr)
}

//go:generate mockgen -source=module_nmc_reconciler.go -package=controllers -destination=mock_module_nmc_reconciler.go moduleNMCReconcilerHelperAPI

type moduleNMCReconcilerHelperAPI interface {
	setFinalizerAndStatus(ctx context.Context, mod *kmmv1beta1.Module) error
	finalizeModule(ctx context.Context, mod *kmmv1beta1.Module) error
	getRequestedModule(ctx context.Context, namespacedName types.NamespacedName) (*kmmv1beta1.Module, error)
	getNodesListBySelector(ctx context.Context, mod *kmmv1beta1.Module) ([]v1.Node, error)
	getNMCsByModuleSet(ctx context.Context, mod *kmmv1beta1.Module) (sets.Set[string], error)
	prepareSchedulingData(ctx context.Context, mod *kmmv1beta1.Module, targetedNodes []v1.Node, currentNMCs sets.Set[string]) (map[string]schedulingData, []error)
	enableModuleOnNode(ctx context.Context, mld *api.ModuleLoaderData, node *v1.Node) error
	disableModuleOnNode(ctx context.Context, modNamespace, modName, nodeName string) error
	moduleUpdateWorkerPodsStatus(ctx context.Context, mod *kmmv1beta1.Module, targetedNodes []v1.Node) error
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

func (mnrh *moduleNMCReconcilerHelper) setFinalizerAndStatus(ctx context.Context, mod *kmmv1beta1.Module) error {
	if controllerutil.ContainsFinalizer(mod, constants.ModuleFinalizer) {
		return nil
	}

	logger := log.FromContext(ctx)
	logger.Info("Adding finalizer", "module name", mod.Name, "module namespace", mod.Namespace)

	modCopy := mod.DeepCopy()
	controllerutil.AddFinalizer(mod, constants.ModuleFinalizer)
	err := mnrh.client.Patch(ctx, mod, client.MergeFrom(modCopy))
	if err != nil {
		return fmt.Errorf("failed to set finalizer for module %s/%s: %v", mod.Namespace, mod.Name, err)
	}

	//mod.Status.ModuleLoader.NodesMatchingSelectorNumber = 0
	//mod.Status.ModuleLoader.DesiredNumber = 0
	//mod.Status.ModuleLoader.AvailableNumber = 0

	return mnrh.client.Status().Update(ctx, mod)
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

	modNSN := types.NamespacedName{Namespace: mod.Namespace, Name: mod.Name}

	matchesConfigured := client.MatchingLabels{
		nmc.ModuleConfiguredLabel(mod.Namespace, mod.Name): "",
	}

	if err := mnrh.client.List(ctx, &nmcList, matchesConfigured); err != nil {
		return fmt.Errorf("failed to list NMCs with %s configured in the cluster: %v", modNSN, err)
	}

	errs := make([]error, 0, len(nmcList.Items))
	for _, nmc := range nmcList.Items {
		err := mnrh.removeModuleFromNMC(ctx, &nmc, mod.Namespace, mod.Name)
		errs = append(errs, err)
	}

	err := errors.Join(errs...)
	if err != nil {
		return fmt.Errorf("failed to remove %s module from some of NMCs: %v", modNSN, err)
	}

	// Now, list all NMCs that still have the Module loaded
	nmcList = kmmv1beta1.NodeModulesConfigList{}

	matchesInUse := client.MatchingLabels{
		nmc.ModuleInUseLabel(mod.Namespace, mod.Name): "",
	}

	if err := mnrh.client.List(ctx, &nmcList, matchesInUse); err != nil {
		return fmt.Errorf("failed to list NMCs with %s loaded in the cluster: %v", modNSN, err)
	}

	if l := len(nmcList.Items); l > 0 {
		ctrl.LoggerFrom(ctx).Info("Some NMCs still list the Module as in-use; not removing the finalizer", "count", l)
		return nil
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
	nmcList, err := mnrh.getNMCsForModule(ctx, mod)
	if err != nil {
		return nil, fmt.Errorf("failed to get list of %s/%s module's NMC for map: %v", mod.Namespace, mod.Name, err)
	}

	result := sets.New[string]()
	for _, nmc := range nmcList {
		result.Insert(nmc.Name)
	}
	return result, nil
}

func (mnrh *moduleNMCReconcilerHelper) getNMCsForModule(ctx context.Context, mod *kmmv1beta1.Module) ([]kmmv1beta1.NodeModulesConfig, error) {
	logger := log.FromContext(ctx)
	moduleNMCLabel := nmc.ModuleConfiguredLabel(mod.Namespace, mod.Name)
	logger.V(1).Info("Listing nmcs", "selector", moduleNMCLabel)
	selectedNMCs := kmmv1beta1.NodeModulesConfigList{}
	opt := client.MatchingLabels(map[string]string{moduleNMCLabel: ""})
	if err := mnrh.client.List(ctx, &selectedNMCs, opt); err != nil {
		return nil, fmt.Errorf("could not list NMCs: %v", err)
	}

	return selectedNMCs.Items, nil
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
		result[node.Name] = prepareNodeSchedulingData(node, mld, currentNMCs)
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

	nmcObj := &kmmv1beta1.NodeModulesConfig{
		ObjectMeta: metav1.ObjectMeta{Name: node.Name},
	}

	opRes, err := controllerutil.CreateOrPatch(ctx, mnrh.client, nmcObj, func() error {
		err = mnrh.nmcHelper.SetModuleConfig(nmcObj, mld, &moduleConfig)
		if err != nil {
			return err
		}

		meta.SetLabel(nmcObj, nmc.ModuleConfiguredLabel(mld.Namespace, mld.Name), "")
		meta.SetLabel(nmcObj, nmc.ModuleInUseLabel(mld.Namespace, mld.Name), "")

		return controllerutil.SetOwnerReference(node, nmcObj, mnrh.scheme)
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

func (mnrh *moduleNMCReconcilerHelper) removeModuleFromNMC(ctx context.Context, nmcObj *kmmv1beta1.NodeModulesConfig, modNamespace, modName string) error {
	logger := log.FromContext(ctx)
	opRes, err := controllerutil.CreateOrPatch(ctx, mnrh.client, nmcObj, func() error {
		if err := mnrh.nmcHelper.RemoveModuleConfig(nmcObj, modNamespace, modName); err != nil {
			return err
		}

		meta.RemoveLabel(nmcObj, nmc.ModuleConfiguredLabel(modNamespace, modName))

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to disable module %s/%s in NMC %s: %v", modNamespace, modName, nmcObj.Name, err)
	}

	logger.Info("Disabled module in NMC", "name", modName, "namespace", modNamespace, "NMC", nmcObj.Name, "result", opRes)
	return nil
}

func (mnrh *moduleNMCReconcilerHelper) moduleUpdateWorkerPodsStatus(ctx context.Context, mod *kmmv1beta1.Module, targetedNodes []v1.Node) error {
	logger := log.FromContext(ctx)
	// get nmcs with configured
	nmcs, err := mnrh.getNMCsForModule(ctx, mod)
	if err != nil {
		return fmt.Errorf("failed to get configured NMCs for module %s/%s: %v", mod.Namespace, mod.Name, err)
	}

	numAvailable := 0
	for _, nmc := range nmcs {
		modSpec, _ := mnrh.nmcHelper.GetModuleSpecEntry(&nmc, mod.Namespace, mod.Name)
		if modSpec == nil {
			logger.Info(utils.WarnString(
				fmt.Sprintf("module %s/%s spec is missing in NMC %s although config label is present", mod.Namespace, mod.Name, nmc.Name)))
			continue
		}
		modStatus := mnrh.nmcHelper.GetModuleStatusEntry(&nmc, mod.Namespace, mod.Name)
		if modStatus != nil && reflect.DeepEqual(modSpec.Config, modStatus.Config) {
			numAvailable += 1
		}
	}

	unmodifiedMod := mod.DeepCopy()

	mod.Status.ModuleLoader.NodesMatchingSelectorNumber = int32(len(targetedNodes))
	mod.Status.ModuleLoader.DesiredNumber = int32(len(nmcs))
	mod.Status.ModuleLoader.AvailableNumber = int32(numAvailable)

	return mnrh.client.Status().Patch(ctx, mod, client.MergeFrom(unmodifiedMod))
}

func prepareNodeSchedulingData(node v1.Node, mld *api.ModuleLoaderData, currentNMCs sets.Set[string]) schedulingData {
	versionLabel := ""
	present := false
	if mld != nil {
		versionLabel, present = utils.GetNodeWorkerPodVersionLabel(node.GetLabels(), mld.Namespace, mld.Name)
	}
	switch {
	case mld == nil:
		if currentNMCs.Has(node.Name) {
			// mld missing, Module does not have mapping for node's kernel, NMC for the node exists
			return schedulingData{action: actionDelete}
		}
	case mld.ModuleVersion == "":
		// mld exists, Version not define, should be running
		return schedulingData{action: actionAdd, mld: mld, node: &node}
	case present && versionLabel == mld.ModuleVersion:
		// mld exists, version label defined and equal to Module's version, should be running
		return schedulingData{action: actionAdd, mld: mld, node: &node}
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
