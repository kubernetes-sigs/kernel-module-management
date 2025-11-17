package controllers

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/kubernetes-sigs/kernel-module-management/internal/node"
	"github.com/kubernetes-sigs/kernel-module-management/internal/pod"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/config"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	"github.com/kubernetes-sigs/kernel-module-management/internal/filter"
	"github.com/kubernetes-sigs/kernel-module-management/internal/nmc"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/yaml"
)

type WorkerAction string

const (
	NodeModulesConfigReconcilerName = "NodeModulesConfig"
)

type NMCReconciler struct {
	client     client.Client
	helper     nmcReconcilerHelper
	nodeAPI    node.Node
	podManager pod.WorkerPodManager
}

func NewNMCReconciler(
	client client.Client,
	scheme *runtime.Scheme,
	workerImage string,
	workerCfg *config.Worker,
	recorder record.EventRecorder,
	nodeAPI node.Node,
	podManager pod.WorkerPodManager,
) *NMCReconciler {
	helper := newNMCReconcilerHelper(client, podManager, recorder, nodeAPI)
	return &NMCReconciler{
		client:     client,
		helper:     helper,
		nodeAPI:    nodeAPI,
		podManager: podManager,
	}
}

func (r *NMCReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	logger := ctrl.LoggerFrom(ctx)

	nmcObj := kmmv1beta1.NodeModulesConfig{}

	if err := r.client.Get(ctx, req.NamespacedName, &nmcObj); err != nil {
		if k8serrors.IsNotFound(err) {
			// Pods are owned by the NMC, so the GC will have deleted them already.
			// Remove the finalizer if we did not have a chance to do it before NMC deletion.
			logger.Info("Clearing worker Pod finalizers")

			if err = r.helper.RemovePodFinalizers(ctx, req.Name); err != nil {
				return reconcile.Result{}, fmt.Errorf("could not clear all Pod finalizers for NMC %s: %v", req.Name, err)
			}

			return reconcile.Result{}, nil
		}

		return reconcile.Result{}, fmt.Errorf("could not get NodeModuleState %s: %v", req.NamespacedName, err)
	}

	node := v1.Node{}
	if err := r.client.Get(ctx, types.NamespacedName{Name: nmcObj.Name}, &node); err != nil {
		return ctrl.Result{}, fmt.Errorf("could not get node %s: %v", nmcObj.Name, err)
	}

	if err := r.helper.SyncStatus(ctx, &nmcObj, &node); err != nil {
		return reconcile.Result{}, fmt.Errorf("could not reconcile status for NodeModulesConfig %s: %v", nmcObj.Name, err)
	}

	// Statuses are now up-to-date.

	statusMap := make(map[string]*kmmv1beta1.NodeModuleStatus, len(nmcObj.Status.Modules))

	for i := 0; i < len(nmcObj.Status.Modules); i++ {
		status := nmcObj.Status.Modules[i]
		statusMap[status.Namespace+"/"+status.Name] = &nmcObj.Status.Modules[i]
	}

	errs := make([]error, 0, len(nmcObj.Spec.Modules)+len(nmcObj.Status.Modules))
	readyLabelsToRemove := make(map[string]string)
	for _, mod := range nmcObj.Spec.Modules {
		moduleNameKey := mod.Namespace + "/" + mod.Name

		logger := logger.WithValues("module", moduleNameKey)

		// skipping handling NMC spec module until node is ready
		if !r.nodeAPI.IsNodeSchedulable(&node, mod.Tolerations) {
			readyLabelsToRemove[utils.GetKernelModuleReadyNodeLabel(mod.Namespace, mod.Name)] = ""
			readyLabelsToRemove[utils.GetKernelModuleVersionReadyNodeLabel(mod.Namespace, mod.Name)] = ""
			delete(statusMap, moduleNameKey)
			continue
		}
		if err := r.helper.ProcessModuleSpec(ctrl.LoggerInto(ctx, logger), &nmcObj, &mod, statusMap[moduleNameKey], &node); err != nil {
			errs = append(
				errs,
				fmt.Errorf("error processing Module %s: %v", moduleNameKey, err),
			)
		}

		// deleting status always (even in case of an error), so that it won't be treated
		// as an orphaned status later in reconciliation
		delete(statusMap, moduleNameKey)
	}

	// We have processed all module specs.
	// Now, go through the remaining, "orphan" statuses that do not have a corresponding spec; those must be unloaded.

	for statusNameKey, status := range statusMap {
		logger := logger.WithValues("status", statusNameKey)

		if err := r.helper.ProcessUnconfiguredModuleStatus(ctrl.LoggerInto(ctx, logger), &nmcObj, status, &node); err != nil {
			errs = append(
				errs,
				fmt.Errorf("error processing orphan status for Module %s: %v", statusNameKey, err),
			)
		}
	}

	// removing label of loaded kmods
	if len(readyLabelsToRemove) != 0 {
		if err := r.nodeAPI.UpdateLabels(ctx, &node, nil, readyLabelsToRemove); err != nil {
			return ctrl.Result{}, fmt.Errorf("could remove node %s labels: %v", node.Name, err)
		}
		return ctrl.Result{}, nil
	}

	if err := r.helper.GarbageCollectInUseLabels(ctx, &nmcObj); err != nil {
		errs = append(errs, fmt.Errorf("failed to GC in-use labels for NMC %s: %v", req.NamespacedName, err))
	}

	if err := r.helper.GarbageCollectWorkerPods(ctx, &nmcObj); err != nil {
		errs = append(errs, fmt.Errorf("failed to GC orphan worker pods for NMC %s: %v", req.NamespacedName, err))
	}

	if loaded, unloaded, err := r.helper.UpdateNodeLabels(ctx, &nmcObj, &node); err != nil {
		errs = append(errs, fmt.Errorf("could not update node's labels for NMC %s: %v", req.NamespacedName, err))
	} else {
		r.helper.RecordEvents(&node, loaded, unloaded)
	}

	return ctrl.Result{}, errors.Join(errs...)
}

func (r *NMCReconciler) SetupWithManager(ctx context.Context, mgr manager.Manager) error {
	// Cache pods by the name of the node they run on.
	// Because NMC name == node name, we can efficiently reconcile the NMC status by listing all pods currently running
	// or completed for it.
	err := mgr.GetCache().IndexField(ctx, &v1.Pod{}, ".spec.nodeName", func(o client.Object) []string {
		return []string{o.(*v1.Pod).Spec.NodeName}
	})
	if err != nil {
		return fmt.Errorf("could not start the worker Pod indexer: %v", err)
	}

	nodeToNMCMapFunc := func(_ context.Context, o client.Object) []reconcile.Request {
		return []reconcile.Request{
			{NamespacedName: types.NamespacedName{Name: o.GetName()}},
		}
	}

	return ctrl.NewControllerManagedBy(mgr).
		Named(NodeModulesConfigReconcilerName).
		For(&kmmv1beta1.NodeModulesConfig{}).
		Owns(&v1.Pod{}).
		// TODO maybe replace this with Owns() if we make nodes the owners of NodeModulesConfigs.
		Watches(
			&v1.Node{},
			handler.EnqueueRequestsFromMapFunc(nodeToNMCMapFunc),
			builder.WithPredicates(filter.NMCReconcilerNodePredicate()),
		).
		Complete(r)
}

func GetContainerStatus(statuses []v1.ContainerStatus, name string) v1.ContainerStatus {
	for i := range statuses {
		if statuses[i].Name == name {
			return statuses[i]
		}
	}

	return v1.ContainerStatus{}
}

//go:generate mockgen -source=nmc_reconciler.go -package=controllers -destination=mock_nmc_reconciler.go workerHelper

type nmcReconcilerHelper interface {
	GarbageCollectInUseLabels(ctx context.Context, nmc *kmmv1beta1.NodeModulesConfig) error
	GarbageCollectWorkerPods(ctx context.Context, nmc *kmmv1beta1.NodeModulesConfig) error
	ProcessModuleSpec(ctx context.Context, nmc *kmmv1beta1.NodeModulesConfig, spec *kmmv1beta1.NodeModuleSpec, status *kmmv1beta1.NodeModuleStatus, node *v1.Node) error
	ProcessUnconfiguredModuleStatus(ctx context.Context, nmc *kmmv1beta1.NodeModulesConfig, status *kmmv1beta1.NodeModuleStatus, node *v1.Node) error
	RemovePodFinalizers(ctx context.Context, nodeName string) error
	SyncStatus(ctx context.Context, nmc *kmmv1beta1.NodeModulesConfig, node *v1.Node) error
	UpdateNodeLabels(ctx context.Context, nmc *kmmv1beta1.NodeModulesConfig, node *v1.Node) ([]types.NamespacedName, []types.NamespacedName, error)
	RecordEvents(node *v1.Node, loadedModules, unloadedModules []types.NamespacedName)
}

type nmcReconcilerHelperImpl struct {
	client     client.Client
	podManager pod.WorkerPodManager
	recorder   record.EventRecorder
	nodeAPI    node.Node
	lph        labelPreparationHelper
}

func newNMCReconcilerHelper(client client.Client, podManager pod.WorkerPodManager, recorder record.EventRecorder, nodeAPI node.Node) nmcReconcilerHelper {
	return &nmcReconcilerHelperImpl{
		client:     client,
		podManager: podManager,
		recorder:   recorder,
		nodeAPI:    nodeAPI,
		lph:        newLabelPreparationHelper(),
	}
}

func (h *nmcReconcilerHelperImpl) GarbageCollectWorkerPods(ctx context.Context, nmcObj *kmmv1beta1.NodeModulesConfig) error {
	podsList, err := h.podManager.ListWorkerPodsOnNode(ctx, nmcObj.Name)
	if err != nil {
		return fmt.Errorf("could not list worker Pods: %v", err)
	}

	modulePresentInNMC := sets.New[string]()
	for _, module := range nmcObj.Spec.Modules {
		modulePresentInNMC.Insert(module.Name)
	}
	for _, module := range nmcObj.Status.Modules {
		modulePresentInNMC.Insert(module.Name)
	}

	var errs []error
	for _, workerPod := range podsList {
		podModuleName := workerPod.Labels[constants.ModuleNameLabel]
		if !modulePresentInNMC.Has(podModuleName) {
			mergeFrom := client.MergeFrom(workerPod.DeepCopy())
			if controllerutil.RemoveFinalizer(&workerPod, pod.NodeModulesConfigFinalizer) {
				if err = h.client.Patch(ctx, &workerPod, mergeFrom); err != nil {
					errs = append(
						errs, fmt.Errorf("could not patch Pod %s/%s: %v", workerPod.Namespace, workerPod.Name, err),
					)
				}
			}

			if err = h.podManager.DeletePod(ctx, &workerPod); err != nil {
				errs = append(errs, fmt.Errorf("could not delete pod %s: %v", workerPod.Name, err))
			}
		}
	}
	return errors.Join(errs...)
}

// GarbageCollectInUseLabels removes all module-in-use labels for which there is no corresponding entry either in
// spec.modules or in status.modules.
func (h *nmcReconcilerHelperImpl) GarbageCollectInUseLabels(ctx context.Context, nmcObj *kmmv1beta1.NodeModulesConfig) error {
	labelSet := sets.New[string]()
	desiredSet := sets.New[string]()

	for k := range nmcObj.Labels {
		if ok, _, _ := nmc.IsModuleInUseLabel(k); ok {
			labelSet.Insert(k)
		}
	}

	for _, s := range nmcObj.Spec.Modules {
		desiredSet.Insert(
			nmc.ModuleInUseLabel(s.Namespace, s.Name),
		)
	}

	for _, s := range nmcObj.Status.Modules {
		desiredSet.Insert(
			nmc.ModuleInUseLabel(s.Namespace, s.Name),
		)
	}

	podList, err := h.podManager.ListWorkerPodsOnNode(ctx, nmcObj.Name)
	if err != nil {
		return fmt.Errorf("could not list worker Pods: %v", err)
	}

	for _, pod := range podList {
		desiredSet.Insert(
			nmc.ModuleInUseLabel(pod.Name, pod.Labels[constants.ModuleNameLabel]),
		)
	}

	diff := labelSet.Difference(desiredSet)

	if diff.Len() != 0 {
		patchFrom := client.MergeFrom(nmcObj.DeepCopy())

		for k := range diff {
			delete(nmcObj.Labels, k)
		}

		return h.client.Patch(ctx, nmcObj, patchFrom)
	}

	return nil
}

// ProcessModuleSpec determines if a worker Pod should be created for a Module entry in a
// NodeModulesConfig .spec.modules.
// A loading worker pod is created when:
//   - there is no corresponding entry in the NodeModulesConfig's .status.modules list;
//   - the lastTransitionTime property in the .status.modules entry is older that the last transition time
//     of the Ready condition on the node. This makes sure that we always load modules after maintenance operations
//     that would make a node not Ready, such as a reboot.
//
// An unloading worker Pod is created when the entry in .spec.modules has a different config compared to the entry in
// .status.modules.
func (h *nmcReconcilerHelperImpl) ProcessModuleSpec(
	ctx context.Context,
	nmcObj *kmmv1beta1.NodeModulesConfig,
	spec *kmmv1beta1.NodeModuleSpec,
	status *kmmv1beta1.NodeModuleStatus,
	node *v1.Node,
) error {
	podName := pod.WorkerPodName(nmcObj.Name, spec.Name)

	logger := ctrl.LoggerFrom(ctx)

	p, err := h.podManager.GetWorkerPod(ctx, podName, spec.Namespace)
	if err != nil {
		return fmt.Errorf("could not get the worker Pod %s: %v", podName, err)
	}

	if p == nil {
		// new module is introduced, need to load it
		if status == nil {
			logger.Info("Missing status; creating loader Pod")
			return h.podManager.CreateLoaderPod(ctx, nmcObj, spec)
		}

		/* configuration changed for module: if spec status contain the same kernel,
		unload the kernel module, otherwise - load kernel modules, since the pod
		is not running, the module cannot be loaded using the old kernel configuration
		*/
		if !reflect.DeepEqual(spec.Config, status.Config) {
			if spec.Config.KernelVersion == status.Config.KernelVersion {
				logger.Info("Outdated config in status; creating unloader Pod")
				return h.podManager.CreateUnloaderPod(ctx, nmcObj, status)
			}
			logger.Info("Outdated config in status and kernels differ, probably due to upgrade; creating loader Pod")
			return h.podManager.CreateLoaderPod(ctx, nmcObj, spec)
		}

		if h.nodeAPI.IsNodeRebooted(node, status.BootId) {
			logger.Info("node has been rebooted and become ready after kernel module was loaded; creating loader Pod")
			return h.podManager.CreateLoaderPod(ctx, nmcObj, spec)
		}

		return nil
	}

	if !h.podManager.IsLoaderPod(p) {
		logger.Info("Worker Pod is not loading the kmod; doing nothing")
		return nil
	}

	if GetContainerStatus(p.Status.ContainerStatuses, pod.WorkerContainerName).RestartCount == 0 {
		logger.Info("Worker Loader Pod has not yet restarted; doing nothing")
		return nil
	}

	podTemplate, err := h.podManager.LoaderPodTemplate(ctx, nmcObj, spec)
	if err != nil {
		return fmt.Errorf("could not create the Pod template for %s: %v", podName, err)
	}

	if h.podManager.HashAnnotationDiffer(podTemplate, p) {
		logger.Info("Hash differs, deleting pod")
		return h.podManager.DeletePod(ctx, p)
	}

	return nil
}

// ProcessUnconfiguredModuleStatus cleans up a NodeModuleStatus.
// It should be called for each status entry for which the NodeModulesConfigs does not have a spec entry; this means
// that KMM wants the module unloaded from the node.
// If status.Config field is nil, then it represents a module that could not be loaded by a worker Pod.
// ProcessUnconfiguredModuleStatus will then remove status from nmcObj's Status.Modules.
// If status.Config is not nil, it means that the module was successfully loaded.
// ProcessUnconfiguredModuleStatus will then create a worker pod to unload the module.
func (h *nmcReconcilerHelperImpl) ProcessUnconfiguredModuleStatus(
	ctx context.Context,
	nmcObj *kmmv1beta1.NodeModulesConfig,
	status *kmmv1beta1.NodeModuleStatus,
	node *v1.Node,
) error {
	podName := pod.WorkerPodName(nmcObj.Name, status.Name)

	logger := ctrl.LoggerFrom(ctx).WithValues("pod name", podName)

	/* node was rebooted, spec not set so no kernel module is loaded, no need to unload.
	   it also fixes the scenario when node's kernel was upgraded, so unload pod will fail anyway
	*/
	if h.nodeAPI.IsNodeRebooted(node, status.BootId) {
		logger.Info("node was rebooted and spec is missing: delete the status to allow Module CR unload, if needed")
		patchFrom := client.MergeFrom(nmcObj.DeepCopy())
		nmc.RemoveModuleStatus(&nmcObj.Status.Modules, status.Namespace, status.Name)
		return h.client.Status().Patch(ctx, nmcObj, patchFrom)
	}

	p, err := h.podManager.GetWorkerPod(ctx, podName, status.Namespace)
	if err != nil {
		return fmt.Errorf("error while getting the worker Pod %s: %v", podName, err)
	}

	if p == nil {
		logger.Info("Worker Pod does not exist; creating it")
		return h.podManager.CreateUnloaderPod(ctx, nmcObj, status)
	}

	if h.podManager.IsLoaderPod(p) {
		logger.Info("Worker Pod is loading the kmod; deleting it")
		return h.podManager.DeletePod(ctx, p)
	}

	if GetContainerStatus(p.Status.ContainerStatuses, pod.WorkerContainerName).RestartCount == 0 {
		logger.Info("Worker Pod has not yet restarted; doing nothing")
		return nil
	}

	podTemplate, err := h.podManager.UnloaderPodTemplate(ctx, nmcObj, status)
	if err != nil {
		return fmt.Errorf("could not create the Pod template for %s: %v", podName, err)
	}

	if h.podManager.HashAnnotationDiffer(podTemplate, p) {
		logger.Info("Hash differs, deleting pod")
		return h.podManager.DeletePod(ctx, p)
	}

	return nil
}

func (h *nmcReconcilerHelperImpl) RemovePodFinalizers(ctx context.Context, nodeName string) error {
	pods, err := h.podManager.ListWorkerPodsOnNode(ctx, nodeName)
	if err != nil {
		return fmt.Errorf("could not delete orphan worker Pods on node %s: %v", nodeName, err)
	}

	errs := make([]error, 0, len(pods))

	for i := 0; i < len(pods); i++ {
		p := &pods[i]

		mergeFrom := client.MergeFrom(p.DeepCopy())

		if controllerutil.RemoveFinalizer(p, pod.NodeModulesConfigFinalizer) {
			if err = h.client.Patch(ctx, p, mergeFrom); err != nil {
				errs = append(
					errs,
					fmt.Errorf("could not patch Pod %s/%s: %v", p.Namespace, p.Name, err),
				)

				continue
			}
		}
	}

	return errors.Join(errs...)
}

func (h *nmcReconcilerHelperImpl) SyncStatus(ctx context.Context, nmcObj *kmmv1beta1.NodeModulesConfig, node *v1.Node) error {
	logger := ctrl.LoggerFrom(ctx)

	logger.Info("Syncing status")

	pods, err := h.podManager.ListWorkerPodsOnNode(ctx, nmcObj.Name)
	if err != nil {
		return fmt.Errorf("could not list worker pods for NodeModulesConfig %s: %v", nmcObj.Name, err)
	}

	logger.V(1).Info("List worker Pods", "count", len(pods))

	if len(pods) == 0 {
		return nil
	}

	specEntries := sets.New[types.NamespacedName]()

	for _, e := range nmcObj.Spec.Modules {
		specEntries.Insert(types.NamespacedName{Namespace: e.Namespace, Name: e.Name})
	}

	patchFrom := client.MergeFrom(nmcObj.DeepCopy())
	errs := make([]error, 0, len(pods))
	podsToDelete := make([]v1.Pod, 0, len(pods))

	for _, p := range pods {
		podNSN := types.NamespacedName{Namespace: p.Namespace, Name: p.Name}

		modNamespace := p.Namespace
		modName := p.Labels[constants.ModuleNameLabel]
		phase := p.Status.Phase

		logger := logger.WithValues("pod name", p.Name, "pod phase", p.Status.Phase)

		logger.Info("Processing worker Pod")

		status := nmc.FindModuleStatus(nmcObj.Status.Modules, modNamespace, modName)

		switch phase {
		case v1.PodRunning:
			// Delete Pod if orphan
			if !specEntries.Has(types.NamespacedName{Namespace: modNamespace, Name: modName}) && status == nil {
				logger.Info("Orphan pod; deleting")
				podsToDelete = append(podsToDelete, p)
			}
		case v1.PodFailed:
			podsToDelete = append(podsToDelete, p)
		case v1.PodSucceeded:
			if h.podManager.IsUnloaderPod(&p) {
				podsToDelete = append(podsToDelete, p)
				nmc.RemoveModuleStatus(&nmcObj.Status.Modules, modNamespace, modName)
				break
			}

			if status == nil {
				status = &kmmv1beta1.NodeModuleStatus{
					ModuleItem: kmmv1beta1.ModuleItem{
						Name:      modName,
						Namespace: modNamespace,
					},
				}
			}

			configAnnotation := h.podManager.GetConfigAnnotation(&p)
			if err = yaml.UnmarshalStrict([]byte(configAnnotation), &status.Config); err != nil {
				errs = append(
					errs,
					fmt.Errorf("%s: could not unmarshal the ModuleConfig from YAML: %v", podNSN, err),
				)
				continue
			}
			tolerationsAnnotation := h.podManager.GetTolerationsAnnotation(&p)
			if err = yaml.UnmarshalStrict([]byte(tolerationsAnnotation), &status.Tolerations); err != nil {
				errs = append(
					errs,
					fmt.Errorf("%s: could not unmarshal the ModuleConfig from YAML: %v", podNSN, err),
				)
				continue
			}

			if p.Spec.ImagePullSecrets != nil {
				status.ImageRepoSecret = &p.Spec.ImagePullSecrets[0]
			}
			status.ServiceAccountName = p.Spec.ServiceAccountName

			status.BootId = node.Status.NodeInfo.BootID

			status.Version = h.podManager.GetModuleVersionAnnotation(&p)

			nmc.SetModuleStatus(&nmcObj.Status.Modules, *status)

			podsToDelete = append(podsToDelete, p)
		}
	}

	err = h.client.Status().Patch(ctx, nmcObj, patchFrom)
	errs = append(errs, err)
	if err = errors.Join(errs...); err != nil {
		return fmt.Errorf("encountered errors while reconciling NMC %s status: %v", nmcObj.Name, err)
	}

	// Delete the pod after the NMC status was updated successfully. Otherwise, in case NMC status update has failed, but the
	// pod was already deleted, we have no way to know how to update NMC status, and it will always be stuck in the previous
	// status without any real way to see/affect. In case we fail to delete pod after NMC status is updated, we will be stuck
	// in the reconcile loop, and in that case we can always try to delete the pod manually, and after that the flow will be able to continue
	errs = errs[:0]
	for _, pod := range podsToDelete {
		err = h.podManager.DeletePod(ctx, &pod)
		errs = append(errs, err)
	}

	return errors.Join(errs...)
}

func (h *nmcReconcilerHelperImpl) UpdateNodeLabels(ctx context.Context, nmc *kmmv1beta1.NodeModulesConfig, node *v1.Node) ([]types.NamespacedName, []types.NamespacedName, error) {

	// get all the kernel module ready labels of the node
	nodeModuleReadyLabels := h.lph.getNodeKernelModuleReadyLabels(*node)
	deprecatedNodeModuleReadyLabels := h.lph.getDeprecatedKernelModuleReadyLabels(*node)

	// get spec labels and their config
	specLabels := h.lph.getSpecLabelsAndTheirConfigs(nmc)

	// get status labels and their config
	statusLabels := h.lph.getStatusLabelsAndTheirConfigs(nmc)

	// get the versions per the name/namespace of the module
	statusVersions := h.lph.getStatusVersions(nmc)

	// label in node but not in spec or status - should be removed
	nsnLabelsToBeRemoved := h.lph.removeOrphanedLabels(nodeModuleReadyLabels, specLabels, statusLabels)

	// label in spec and status and config equal - should be added
	nsnLabelsToBeLoaded := h.lph.addEqualLabels(nodeModuleReadyLabels, specLabels, statusLabels)

	loadedLabels := make(map[string]string)
	unloadedLabels := deprecatedNodeModuleReadyLabels

	for _, label := range nsnLabelsToBeRemoved {
		unloadedLabels[utils.GetKernelModuleReadyNodeLabel(label.Namespace, label.Name)] = ""
		// unload the kernel ready version label also. if it does not exists, that's ok, the code won't fail.It also means
		// that the version label will not be in labelsToAdd, since status and spec are missing
		unloadedLabels[utils.GetKernelModuleVersionReadyNodeLabel(label.Namespace, label.Name)] = ""
	}

	for _, label := range nsnLabelsToBeLoaded {
		loadedLabels[utils.GetKernelModuleReadyNodeLabel(label.Namespace, label.Name)] = ""
		if version, ok := statusVersions[label]; ok {
			loadedLabels[utils.GetKernelModuleVersionReadyNodeLabel(label.Namespace, label.Name)] = version
		}
	}

	if err := h.nodeAPI.UpdateLabels(ctx, node, loadedLabels, unloadedLabels); err != nil {
		return nil, nil, fmt.Errorf("could not update labels on the node: %v", err)
	}

	return nsnLabelsToBeLoaded, nsnLabelsToBeRemoved, nil
}

func (h *nmcReconcilerHelperImpl) RecordEvents(node *v1.Node, loadedModules, unloadedModules []types.NamespacedName) {
	for _, nsn := range loadedModules {
		h.recorder.AnnotatedEventf(
			node,
			map[string]string{"module": nsn.String()},
			v1.EventTypeNormal,
			"ModuleLoaded",
			"Module %s loaded into the kernel",
			nsn.String(),
		)
	}
	for _, nsn := range unloadedModules {
		h.recorder.AnnotatedEventf(
			node,
			map[string]string{"module": nsn.String()},
			v1.EventTypeNormal,
			"ModuleUnloaded",
			"Module %s unloaded from the kernel",
			nsn.String(),
		)
	}
}

type labelPreparationHelper interface {
	getDeprecatedKernelModuleReadyLabels(node v1.Node) map[string]string
	getNodeKernelModuleReadyLabels(node v1.Node) sets.Set[types.NamespacedName]
	getSpecLabelsAndTheirConfigs(nmc *kmmv1beta1.NodeModulesConfig) map[types.NamespacedName]kmmv1beta1.ModuleConfig
	getStatusLabelsAndTheirConfigs(nmc *kmmv1beta1.NodeModulesConfig) map[types.NamespacedName]kmmv1beta1.ModuleConfig
	getStatusVersions(nmc *kmmv1beta1.NodeModulesConfig) map[types.NamespacedName]string
	addEqualLabels(nodeModuleReadyLabels sets.Set[types.NamespacedName],
		specLabels, statusLabels map[types.NamespacedName]kmmv1beta1.ModuleConfig) []types.NamespacedName
	removeOrphanedLabels(nodeModuleReadyLabels sets.Set[types.NamespacedName],
		specLabels, statusLabels map[types.NamespacedName]kmmv1beta1.ModuleConfig) []types.NamespacedName
}
type labelPreparationHelperImpl struct{}

func newLabelPreparationHelper() labelPreparationHelper {
	return &labelPreparationHelperImpl{}
}

func (lph *labelPreparationHelperImpl) getNodeKernelModuleReadyLabels(node v1.Node) sets.Set[types.NamespacedName] {
	nodeModuleReadyLabels := sets.New[types.NamespacedName]()

	for label := range node.GetLabels() {
		if ok, namespace, name := utils.IsKernelModuleReadyNodeLabel(label); ok {
			nodeModuleReadyLabels.Insert(types.NamespacedName{Namespace: namespace, Name: name})
		}
	}
	return nodeModuleReadyLabels
}

func (lph *labelPreparationHelperImpl) getDeprecatedKernelModuleReadyLabels(node v1.Node) map[string]string {
	deprecatedLabels := make(map[string]string)

	for key, val := range node.GetLabels() {
		if utils.IsDeprecatedKernelModuleReadyNodeLabel(key) {
			deprecatedLabels[key] = val
		}
	}
	return deprecatedLabels
}

func (lph *labelPreparationHelperImpl) getSpecLabelsAndTheirConfigs(nmc *kmmv1beta1.NodeModulesConfig) map[types.NamespacedName]kmmv1beta1.ModuleConfig {
	specLabels := make(map[types.NamespacedName]kmmv1beta1.ModuleConfig)

	for _, module := range nmc.Spec.Modules {
		specLabels[types.NamespacedName{Namespace: module.Namespace, Name: module.Name}] = module.Config
	}
	return specLabels
}

func (lph *labelPreparationHelperImpl) getStatusLabelsAndTheirConfigs(nmc *kmmv1beta1.NodeModulesConfig) map[types.NamespacedName]kmmv1beta1.ModuleConfig {
	statusLabels := make(map[types.NamespacedName]kmmv1beta1.ModuleConfig)

	for _, module := range nmc.Status.Modules {
		label := types.NamespacedName{Namespace: module.Namespace, Name: module.Name}
		statusLabels[label] = module.Config
	}
	return statusLabels
}

func (lph *labelPreparationHelperImpl) getStatusVersions(nmc *kmmv1beta1.NodeModulesConfig) map[types.NamespacedName]string {
	versions := make(map[types.NamespacedName]string)

	for _, module := range nmc.Status.Modules {
		if module.Version != "" {
			versions[types.NamespacedName{Namespace: module.Namespace, Name: module.Name}] = module.Version
		}
	}
	return versions
}

func (lph *labelPreparationHelperImpl) removeOrphanedLabels(nodeModuleReadyLabels sets.Set[types.NamespacedName],
	specLabels, statusLabels map[types.NamespacedName]kmmv1beta1.ModuleConfig) []types.NamespacedName {

	unloaded := make([]types.NamespacedName, 0, len(nodeModuleReadyLabels))

	for nsn := range nodeModuleReadyLabels {
		_, inSpec := specLabels[nsn]
		_, inStatus := statusLabels[nsn]
		if !inSpec && !inStatus {
			unloaded = append(unloaded, nsn)
		}
	}
	return unloaded
}
func (lph *labelPreparationHelperImpl) addEqualLabels(nodeModuleReadyLabels sets.Set[types.NamespacedName],
	specLabels, statusLabels map[types.NamespacedName]kmmv1beta1.ModuleConfig) []types.NamespacedName {

	loaded := make([]types.NamespacedName, 0, len(nodeModuleReadyLabels))

	for nsn, specConfig := range specLabels {
		statusConfig, ok := statusLabels[nsn]
		if ok && reflect.DeepEqual(specConfig, statusConfig) && !nodeModuleReadyLabels.Has(nsn) {
			loaded = append(loaded, nsn)
		}
	}
	return loaded
}
