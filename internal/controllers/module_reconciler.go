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
	"github.com/kubernetes-sigs/kernel-module-management/internal/mic"
	"github.com/kubernetes-sigs/kernel-module-management/internal/module"
	"github.com/kubernetes-sigs/kernel-module-management/internal/nmc"
	"github.com/kubernetes-sigs/kernel-module-management/internal/node"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
	v1 "k8s.io/api/core/v1"
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
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

//+kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=create;delete;get;list;patch;watch
//+kubebuilder:rbac:groups=cluster.open-cluster-management.io,resources=clusterclaims,resourceNames=kernel-versions.kmm.node.kubernetes.io,verbs=delete;patch;update
//+kubebuilder:rbac:groups=cluster.open-cluster-management.io,resources=clusterclaims,verbs=create;get;list;watch
//+kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch
//+kubebuilder:rbac:groups=core,resources=namespaces,verbs=get;list;patch;watch
//+kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch;patch
//+kubebuilder:rbac:groups=core,resources=pods,verbs=create;delete;get;list;patch;watch
//+kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch
//+kubebuilder:rbac:groups=core,resources=serviceaccounts,verbs=get;list;watch
//+kubebuilder:rbac:groups=kmm.sigs.x-k8s.io,resources=modulebuildsignconfigs,verbs=get;list;watch;update;patch;create
//+kubebuilder:rbac:groups=kmm.sigs.x-k8s.io,resources=modulebuildsignconfigs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=kmm.sigs.x-k8s.io,resources=moduleimagesconfigs,verbs=get;list;watch;patch;create;delete
//+kubebuilder:rbac:groups=kmm.sigs.x-k8s.io,resources=moduleimagesconfigs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=kmm.sigs.x-k8s.io,resources=modules,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=kmm.sigs.x-k8s.io,resources=modules/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=kmm.sigs.x-k8s.io,resources=nodemodulesconfigs,verbs=get;list;watch;patch;create;delete
//+kubebuilder:rbac:groups=kmm.sigs.x-k8s.io,resources=nodemodulesconfigs/status,verbs=patch
//+kubebuilder:rbac:groups=kmm.sigs.x-k8s.io,resources=preflightvalidations,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=kmm.sigs.x-k8s.io,resources=preflightvalidations/status,verbs=get;update;patch

const (
	ModuleReconcilerName = "ModuleReconciler"
	actionDelete         = "delete"
	actionAdd            = "add"
)

type schedulingData struct {
	action string
	mld    *api.ModuleLoaderData
	node   *v1.Node
}

type ModuleReconciler struct {
	filter      *filter.Filter
	nsLabeler   namespaceLabeler
	reconHelper moduleReconcilerHelperAPI
	nodeAPI     node.Node
	micAPI      mic.MIC
}

func NewModuleReconciler(client client.Client,
	kernelAPI module.KernelMapper,
	nmcHelper nmc.Helper,
	filter *filter.Filter,
	nodeAPI node.Node,
	micAPI mic.MIC,
	scheme *runtime.Scheme) *ModuleReconciler {
	reconHelper := newModuleReconcilerHelper(client, kernelAPI, micAPI, nmcHelper, scheme)
	return &ModuleReconciler{
		filter:      filter,
		nsLabeler:   newNamespaceLabeler(client),
		reconHelper: reconHelper,
		nodeAPI:     nodeAPI,
	}
}

func (mr *ModuleReconciler) Reconcile(ctx context.Context, mod *kmmv1beta1.Module) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	logger.Info("Starting Module reconciliation")

	if mod.GetDeletionTimestamp() != nil {
		//Module is being deleted
		if err := mr.nsLabeler.tryRemovingLabel(ctx, mod.Namespace, mod.Name); err != nil {
			return ctrl.Result{}, fmt.Errorf("error while trying to remove the label on namespace %s: %v", mod.Namespace, err)
		}

		err := mr.reconHelper.finalizeModule(ctx, mod)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to finalize Module %s/%s: %v", mod.Namespace, mod.Name, err)
		}
		return ctrl.Result{}, nil
	}

	if err := mr.nsLabeler.setLabel(ctx, mod.Namespace); err != nil {
		return ctrl.Result{}, fmt.Errorf("could not set label %q on namespace %s: %v", constants.NamespaceLabelKey, mod.Namespace, err)
	}

	err := mr.reconHelper.setFinalizerAndStatus(ctx, mod)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to set finalizer on Module %s/%s: %v", mod.Namespace, mod.Name, err)
	}

	if mod.Spec.ModuleLoader == nil {
		logger.Info("ModuleLoader is not defined, skipping loading kernel module")
		return ctrl.Result{}, nil
	}

	// get nodes targeted by selector
	targetedNodes, err := mr.nodeAPI.GetNodesListBySelector(ctx, mod.Spec.Selector, mod.Spec.Tolerations)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get list of nodes by selector: %v", err)
	}

	if err := mr.reconHelper.handleMIC(ctx, mod, targetedNodes); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to handle MIC: %v", err)
	}

	currentNMCs, err := mr.reconHelper.getNMCsByModuleSet(ctx, mod)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get NMCs for Module %s/%s: %v", mod.Namespace, mod.Name, err)
	}

	sdMap, prepareErrs := mr.reconHelper.prepareSchedulingData(ctx, mod, targetedNodes, currentNMCs)
	errs := make([]error, 0, len(sdMap)+1)
	errs = append(errs, prepareErrs...)

	for nodeName, sd := range sdMap {
		if sd.action == actionAdd {
			err = mr.reconHelper.enableModuleOnNode(ctx, sd.mld, sd.node)
		}
		if sd.action == actionDelete {
			err = mr.reconHelper.disableModuleOnNode(ctx, mod.Namespace, mod.Name, nodeName)
		}
		errs = append(errs, err)
	}

	err = mr.reconHelper.moduleUpdateWorkerPodsStatus(ctx, mod, targetedNodes)
	errs = append(errs, err)

	err = errors.Join(errs...)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to reconcile module %s/%s config: %v", mod.Namespace, mod.Name, err)
	}
	return ctrl.Result{}, nil
}

func (mr *ModuleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.
		NewControllerManagedBy(mgr).
		For(&kmmv1beta1.Module{}).
		Owns(&v1.Pod{}, builder.WithPredicates(filter.ModuleReconcilePodPredicate())).
		Owns(&kmmv1beta1.ModuleImagesConfig{}, builder.WithPredicates(filter.ModuleReconcileMICPredicate())).
		Watches(
			&v1.Node{},
			handler.EnqueueRequestsFromMapFunc(mr.filter.FindModulesForNMCNodeChange),
			builder.WithPredicates(
				filter.ModuleReconcilerNodePredicate(),
			),
		).
		Watches(
			&kmmv1beta1.NodeModulesConfig{},
			handler.EnqueueRequestsFromMapFunc(filter.ListModulesForNMC),
		).
		Named(ModuleReconcilerName).
		Complete(
			reconcile.AsReconciler[*kmmv1beta1.Module](mgr.GetClient(), mr),
		)
}

//go:generate mockgen -source=module_reconciler.go -package=controllers -destination=mock_module_reconciler.go moduleReconcilerHelperAPI,namespaceLabeler

type moduleReconcilerHelperAPI interface {
	handleMIC(ctx context.Context, mod *kmmv1beta1.Module, nodes []v1.Node) error
	setFinalizerAndStatus(ctx context.Context, mod *kmmv1beta1.Module) error
	finalizeModule(ctx context.Context, mod *kmmv1beta1.Module) error
	getNMCsByModuleSet(ctx context.Context, mod *kmmv1beta1.Module) (sets.Set[string], error)
	prepareSchedulingData(ctx context.Context, mod *kmmv1beta1.Module, targetedNodes []v1.Node, currentNMCs sets.Set[string]) (map[string]schedulingData, []error)
	enableModuleOnNode(ctx context.Context, mld *api.ModuleLoaderData, node *v1.Node) error
	disableModuleOnNode(ctx context.Context, modNamespace, modName, nodeName string) error
	moduleUpdateWorkerPodsStatus(ctx context.Context, mod *kmmv1beta1.Module, targetedNodes []v1.Node) error
}

type moduleReconcilerHelper struct {
	client    client.Client
	kernelAPI module.KernelMapper
	micAPI    mic.MIC
	nmcHelper nmc.Helper
	scheme    *runtime.Scheme
}

func newModuleReconcilerHelper(
	client client.Client,
	kernelAPI module.KernelMapper,
	micAPI mic.MIC,
	nmcHelper nmc.Helper,
	scheme *runtime.Scheme) moduleReconcilerHelperAPI {
	return &moduleReconcilerHelper{
		client:    client,
		kernelAPI: kernelAPI,
		micAPI:    micAPI,
		nmcHelper: nmcHelper,
		scheme:    scheme,
	}
}

func (mrh *moduleReconcilerHelper) setFinalizerAndStatus(ctx context.Context, mod *kmmv1beta1.Module) error {
	if controllerutil.ContainsFinalizer(mod, constants.ModuleFinalizer) {
		return nil
	}

	logger := log.FromContext(ctx)
	logger.Info("Adding finalizer", "module name", mod.Name, "module namespace", mod.Namespace)

	modCopy := mod.DeepCopy()
	controllerutil.AddFinalizer(mod, constants.ModuleFinalizer)
	err := mrh.client.Patch(ctx, mod, client.MergeFrom(modCopy))
	if err != nil {
		return fmt.Errorf("failed to set finalizer for module %s/%s: %v", mod.Namespace, mod.Name, err)
	}

	//mod.Status.ModuleLoader.NodesMatchingSelectorNumber = 0
	//mod.Status.ModuleLoader.DesiredNumber = 0
	//mod.Status.ModuleLoader.AvailableNumber = 0

	return mrh.client.Status().Update(ctx, mod)
}

func (mrh *moduleReconcilerHelper) finalizeModule(ctx context.Context, mod *kmmv1beta1.Module) error {
	nmcList := kmmv1beta1.NodeModulesConfigList{}

	modNSN := types.NamespacedName{Namespace: mod.Namespace, Name: mod.Name}

	matchesConfigured := client.MatchingLabels{
		nmc.ModuleConfiguredLabel(mod.Namespace, mod.Name): "",
	}

	if err := mrh.client.List(ctx, &nmcList, matchesConfigured); err != nil {
		return fmt.Errorf("failed to list NMCs with %s configured in the cluster: %v", modNSN, err)
	}

	errs := make([]error, 0, len(nmcList.Items))
	for _, nmc := range nmcList.Items {
		err := mrh.removeModuleFromNMC(ctx, &nmc, mod.Namespace, mod.Name)
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

	if err := mrh.client.List(ctx, &nmcList, matchesInUse); err != nil {
		return fmt.Errorf("failed to list NMCs with %s loaded in the cluster: %v", modNSN, err)
	}

	if l := len(nmcList.Items); l > 0 {
		ctrl.LoggerFrom(ctx).Info("Some NMCs still list the Module as in-use; not removing the finalizer", "count", l)
		return nil
	}

	// remove finalizer
	modCopy := mod.DeepCopy()
	controllerutil.RemoveFinalizer(mod, constants.ModuleFinalizer)

	return mrh.client.Patch(ctx, mod, client.MergeFrom(modCopy))
}

func (mrh *moduleReconcilerHelper) getNMCsByModuleSet(ctx context.Context, mod *kmmv1beta1.Module) (sets.Set[string], error) {
	nmcList, err := mrh.getNMCsForModule(ctx, mod)
	if err != nil {
		return nil, fmt.Errorf("failed to get list of %s/%s module's NMC for map: %v", mod.Namespace, mod.Name, err)
	}

	result := sets.New[string]()
	for _, nmc := range nmcList {
		result.Insert(nmc.Name)
	}
	return result, nil
}

func (mrh *moduleReconcilerHelper) getNMCsForModule(ctx context.Context, mod *kmmv1beta1.Module) ([]kmmv1beta1.NodeModulesConfig, error) {
	logger := log.FromContext(ctx)
	moduleLabel := nmc.ModuleConfiguredLabel(mod.Namespace, mod.Name)
	logger.V(1).Info("Listing nmcs", "selector", moduleLabel)
	selectedNMCs := kmmv1beta1.NodeModulesConfigList{}
	opt := client.MatchingLabels(map[string]string{moduleLabel: ""})
	if err := mrh.client.List(ctx, &selectedNMCs, opt); err != nil {
		return nil, fmt.Errorf("could not list NMCs: %v", err)
	}

	return selectedNMCs.Items, nil
}

// prepareSchedulingData prepare data needed to scheduling enable/disable module per node
// in case there is an error during handling one of the nodes, function continues to the next node
// It returns the map of scheduling data per successfully processed node, and slice of errors
// per unsuccessfuly processed nodes
func (mrh *moduleReconcilerHelper) prepareSchedulingData(ctx context.Context,
	mod *kmmv1beta1.Module,
	targetedNodes []v1.Node,
	currentNMCs sets.Set[string]) (map[string]schedulingData, []error) {

	logger := log.FromContext(ctx)
	result := make(map[string]schedulingData)
	errs := make([]error, 0, len(targetedNodes))
	for _, node := range targetedNodes {
		kernelVersion := strings.TrimSuffix(node.Status.NodeInfo.KernelVersion, "+")
		mld, err := mrh.kernelAPI.GetModuleLoaderDataForKernel(mod, kernelVersion)
		if err != nil && !errors.Is(err, module.ErrNoMatchingKernelMapping) {
			// deleting earlier, so as not to change NMC in case we failed to determine mld
			currentNMCs.Delete(node.Name)
			logger.Info(utils.WarnString(
				fmt.Sprintf("internal errors while fetching kernel mapping for version %s: %v", kernelVersion, err),
			))
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

func (mrh *moduleReconcilerHelper) handleMIC(ctx context.Context, mod *kmmv1beta1.Module, targetedNodes []v1.Node) error {

	var (
		logger = log.FromContext(ctx)
		images []kmmv1beta1.ModuleImageSpec
		errs   []error
	)

	for _, node := range targetedNodes {
		kernelVersion := strings.TrimSuffix(node.Status.NodeInfo.KernelVersion, "+")
		mld, err := mrh.kernelAPI.GetModuleLoaderDataForKernel(mod, kernelVersion)
		if err != nil {
			if !errors.Is(err, module.ErrNoMatchingKernelMapping) {
				logger.Info(utils.WarnString(
					fmt.Sprintf("internal errors while fetching kernel mapping for kernel %s: %v", kernelVersion, err),
				))
				errs = append(errs, fmt.Errorf("failed to get moduleLoaderData for kernel %s: %v", kernelVersion, err))
			}
			// node is not targeted by module
			continue
		}
		mis := kmmv1beta1.ModuleImageSpec{
			Image:         mld.ContainerImage,
			KernelVersion: mld.KernelVersion,
			Build:         mld.Build,
			Sign:          mld.Sign,
			RegistryTLS:   mld.RegistryTLS,
		}
		images = append(images, mis)
	}

	if err := mrh.micAPI.CreateOrPatch(ctx, mod.Name, mod.Namespace, images, mod.Spec.ImageRepoSecret,
		mod.Spec.ModuleLoader.Container.ImagePullPolicy, mod); err != nil {
		errs = append(errs, fmt.Errorf("failed to apply %s/%s MIC: %v", mod.Namespace, mod.Name, err))
	}

	return errors.Join(errs...)
}

func (mrh *moduleReconcilerHelper) enableModuleOnNode(ctx context.Context, mld *api.ModuleLoaderData, node *v1.Node) error {

	logger := log.FromContext(ctx)

	micObj, err := mrh.micAPI.Get(ctx, mld.Name, mld.Namespace)
	if err != nil {
		return fmt.Errorf("failed to get moduleImagesConfig %s: %v", mld.Name, err)
	}

	imageStatus := mrh.micAPI.GetImageState(micObj, mld.ContainerImage)
	if imageStatus != kmmv1beta1.ImageExists {
		// skip updating NMC, reconciliation will kick in once the build pod is completed
		logger.V(1).Info("Image does not exist, not adding to NMC", "nmc name", node.Name, "container image", mld.ContainerImage)
		return nil
	}

	moduleConfig := kmmv1beta1.ModuleConfig{
		KernelVersion:         mld.KernelVersion,
		ContainerImage:        mld.ContainerImage,
		ImagePullPolicy:       mld.ImagePullPolicy,
		InTreeModulesToRemove: mld.InTreeModulesToRemove,
		Modprobe:              mld.Modprobe,
	}

	if tls := mld.RegistryTLS; tls != nil {
		moduleConfig.InsecurePull = tls.Insecure || tls.InsecureSkipTLSVerify
	}

	nmcObj := &kmmv1beta1.NodeModulesConfig{
		ObjectMeta: metav1.ObjectMeta{Name: node.Name},
	}

	opRes, err := controllerutil.CreateOrPatch(ctx, mrh.client, nmcObj, func() error {
		if err := mrh.nmcHelper.SetModuleConfig(nmcObj, mld, &moduleConfig); err != nil {
			return err
		}

		meta.SetLabel(nmcObj, nmc.ModuleConfiguredLabel(mld.Namespace, mld.Name), "")
		meta.SetLabel(nmcObj, nmc.ModuleInUseLabel(mld.Namespace, mld.Name), "")

		return controllerutil.SetOwnerReference(node, nmcObj, mrh.scheme)
	})

	if err != nil {
		return fmt.Errorf("failed to enable module %s/%s in NMC %s: %v", mld.Namespace, mld.Name, node.Name, err)
	}
	logger.Info("Enable module in NMC", "name", mld.Name, "namespace", mld.Namespace, "node", node.Name, "result", opRes)
	return nil
}

func (mrh *moduleReconcilerHelper) disableModuleOnNode(ctx context.Context, modNamespace, modName, nodeName string) error {
	nmc := &kmmv1beta1.NodeModulesConfig{
		ObjectMeta: metav1.ObjectMeta{Name: nodeName},
	}

	return mrh.removeModuleFromNMC(ctx, nmc, modNamespace, modName)
}

func (mrh *moduleReconcilerHelper) removeModuleFromNMC(ctx context.Context, nmcObj *kmmv1beta1.NodeModulesConfig, modNamespace, modName string) error {
	logger := log.FromContext(ctx)
	opRes, err := controllerutil.CreateOrPatch(ctx, mrh.client, nmcObj, func() error {
		if err := mrh.nmcHelper.RemoveModuleConfig(nmcObj, modNamespace, modName); err != nil {
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

func (mrh *moduleReconcilerHelper) moduleUpdateWorkerPodsStatus(ctx context.Context, mod *kmmv1beta1.Module, targetedNodes []v1.Node) error {
	logger := log.FromContext(ctx)
	// get nmcs with configured
	nmcs, err := mrh.getNMCsForModule(ctx, mod)
	if err != nil {
		return fmt.Errorf("failed to get configured NMCs for module %s/%s: %v", mod.Namespace, mod.Name, err)
	}

	numAvailable := 0
	for _, nmc := range nmcs {
		modSpec, _ := mrh.nmcHelper.GetModuleSpecEntry(&nmc, mod.Namespace, mod.Name)
		if modSpec == nil {
			logger.Info(utils.WarnString(
				fmt.Sprintf("module %s/%s spec is missing in NMC %s although config label is present", mod.Namespace,
					mod.Name, nmc.Name),
			))
			continue
		}
		modStatus := mrh.nmcHelper.GetModuleStatusEntry(&nmc, mod.Namespace, mod.Name)
		if modStatus != nil && reflect.DeepEqual(modSpec.Config, modStatus.Config) {
			numAvailable += 1
		}
	}

	unmodifiedMod := mod.DeepCopy()

	mod.Status.ModuleLoader.NodesMatchingSelectorNumber = int32(len(targetedNodes))
	mod.Status.ModuleLoader.DesiredNumber = int32(len(nmcs))
	mod.Status.ModuleLoader.AvailableNumber = int32(numAvailable)

	return mrh.client.Status().Patch(ctx, mod, client.MergeFrom(unmodifiedMod))
}

type namespaceLabeler interface {
	setLabel(ctx context.Context, name string) error
	tryRemovingLabel(ctx context.Context, name, moduleName string) error
}

type namespaceLabelerImpl struct {
	client client.Client
}

func newNamespaceLabeler(client client.Client) namespaceLabeler {
	return &namespaceLabelerImpl{client: client}
}

func (h *namespaceLabelerImpl) setLabel(ctx context.Context, name string) error {
	logger := log.FromContext(ctx)

	ns := v1.Namespace{}

	if err := h.client.Get(ctx, types.NamespacedName{Name: name}, &ns); err != nil {
		return fmt.Errorf("could not get namespace %s: %v", name, err)
	}

	if !meta.HasLabel(&ns, constants.NamespaceLabelKey) {
		nsCopy := ns.DeepCopy()

		logger.Info("Setting namespace label")
		meta.SetLabel(&ns, constants.NamespaceLabelKey, "")

		return h.client.Patch(ctx, &ns, client.MergeFrom(nsCopy))
	}

	return nil
}

func (h *namespaceLabelerImpl) tryRemovingLabel(ctx context.Context, name, moduleName string) error {
	logger := log.FromContext(ctx)

	modList := kmmv1beta1.ModuleList{}

	opt := client.InNamespace(name)

	if err := h.client.List(ctx, &modList, opt); err != nil {
		return fmt.Errorf("could not list modules in namespace %s: %v", name, err)
	}

	if count := len(modList.Items); count > 1 {
		logger.Info("Namespace still contains modules; not removing the label", "count", count)

		if verboseLogger := logger.V(1); verboseLogger.Enabled() {
			modNames := make([]string, 0, count)

			for _, m := range modList.Items {
				modNames = append(modNames, m.Name)
			}

			verboseLogger.Info("Remaining modules", "names", modNames)
		}

		return nil
	}

	ns := &v1.Namespace{}

	if err := h.client.Get(ctx, types.NamespacedName{Name: name}, ns); err != nil {
		return fmt.Errorf("could not get namespace %s: %v", name, err)
	}

	nsCopy := ns.DeepCopy()

	meta.RemoveLabel(ns, constants.NamespaceLabelKey)

	return h.client.Patch(ctx, ns, client.MergeFrom(nsCopy))
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
