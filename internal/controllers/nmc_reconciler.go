package controllers

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/kubernetes-sigs/kernel-module-management/internal/node"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/config"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	"github.com/kubernetes-sigs/kernel-module-management/internal/filter"
	"github.com/kubernetes-sigs/kernel-module-management/internal/meta"
	"github.com/kubernetes-sigs/kernel-module-management/internal/nmc"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
	"github.com/kubernetes-sigs/kernel-module-management/internal/worker"
	"github.com/mitchellh/hashstructure/v2"
	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/record"
	"k8s.io/kubectl/pkg/cmd/util/podcmd"
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
	WorkerActionLoad   = "Load"
	WorkerActionUnload = "Unload"

	NodeModulesConfigReconcilerName = "NodeModulesConfig"

	actionLabelKey             = "kmm.node.kubernetes.io/worker-action"
	configAnnotationKey        = "kmm.node.kubernetes.io/worker-config"
	hashAnnotationKey          = "kmm.node.kubernetes.io/worker-hash"
	modulesOrderKey            = "kmm.node.kubernetes.io/modules-order"
	nodeModulesConfigFinalizer = "kmm.node.kubernetes.io/nodemodulesconfig-reconciler"
	volumeNameConfig           = "config"
	workerContainerName        = "worker"
	initContainerName          = "image-extractor"
	sharedFilesDir             = "/tmp"
	volNameTmp                 = "tmp"
)

//+kubebuilder:rbac:groups=kmm.sigs.x-k8s.io,resources=nodemodulesconfigs,verbs=get;list;watch
//+kubebuilder:rbac:groups=kmm.sigs.x-k8s.io,resources=nodemodulesconfigs/status,verbs=patch
//+kubebuilder:rbac:groups="core",resources=pods,verbs=create;delete;get;list;watch
//+kubebuilder:rbac:groups="core",resources=nodes,verbs=get;list;watch
//+kubebuilder:rbac:groups="core",resources=secrets,verbs=get;list;watch
//+kubebuilder:rbac:groups="core",resources=serviceaccounts,verbs=get;list;watch

type NMCReconciler struct {
	client  client.Client
	helper  nmcReconcilerHelper
	nodeAPI node.Node
}

func NewNMCReconciler(
	client client.Client,
	scheme *runtime.Scheme,
	workerImage string,
	workerCfg *config.Worker,
	recorder record.EventRecorder,
	nodeAPI node.Node,
) *NMCReconciler {
	pm := newPodManager(client, workerImage, scheme, workerCfg)
	helper := newNMCReconcilerHelper(client, pm, recorder, nodeAPI)
	return &NMCReconciler{
		client:  client,
		helper:  helper,
		nodeAPI: nodeAPI,
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

	if err := r.helper.SyncStatus(ctx, &nmcObj); err != nil {
		return reconcile.Result{}, fmt.Errorf("could not reconcile status for NodeModulesConfig %s: %v", nmcObj.Name, err)
	}

	// Statuses are now up-to-date.

	statusMap := make(map[string]*kmmv1beta1.NodeModuleStatus, len(nmcObj.Status.Modules))

	for i := 0; i < len(nmcObj.Status.Modules); i++ {
		status := nmcObj.Status.Modules[i]
		statusMap[status.Namespace+"/"+status.Name] = &nmcObj.Status.Modules[i]
	}

	node := v1.Node{}
	if err := r.client.Get(ctx, types.NamespacedName{Name: nmcObj.Name}, &node); err != nil {
		return ctrl.Result{}, fmt.Errorf("could not get node %s: %v", nmcObj.Name, err)
	}

	errs := make([]error, 0, len(nmcObj.Spec.Modules)+len(nmcObj.Status.Modules))
	var readyLabelsToRemove []string
	for _, mod := range nmcObj.Spec.Modules {
		moduleNameKey := mod.Namespace + "/" + mod.Name

		logger := logger.WithValues("module", moduleNameKey)

		// skipping handling NMC spec module until node is ready
		if !r.nodeAPI.IsNodeSchedulable(&node, mod.Tolerations) {
			readyLabelsToRemove = append(readyLabelsToRemove, utils.GetKernelModuleReadyNodeLabel(mod.Namespace, mod.Name))
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
	if readyLabelsToRemove != nil {
		if err := r.nodeAPI.UpdateLabels(ctx, &node, nil, readyLabelsToRemove); err != nil {
			return ctrl.Result{}, fmt.Errorf("could remove node %s labels: %v", node.Name, err)
		}
		return ctrl.Result{}, nil
	}

	if err := r.helper.GarbageCollectInUseLabels(ctx, &nmcObj); err != nil {
		errs = append(errs, fmt.Errorf("failed to GC in-use labels for NMC %s: %v", req.NamespacedName, err))
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
			builder.WithPredicates(filter.SkipDeletions()),
		).
		Complete(r)
}

func workerPodName(nodeName, moduleName string) string {
	return fmt.Sprintf("kmm-worker-%s-%s", nodeName, moduleName)
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
	ProcessModuleSpec(ctx context.Context, nmc *kmmv1beta1.NodeModulesConfig, spec *kmmv1beta1.NodeModuleSpec, status *kmmv1beta1.NodeModuleStatus, node *v1.Node) error
	ProcessUnconfiguredModuleStatus(ctx context.Context, nmc *kmmv1beta1.NodeModulesConfig, status *kmmv1beta1.NodeModuleStatus, node *v1.Node) error
	RemovePodFinalizers(ctx context.Context, nodeName string) error
	SyncStatus(ctx context.Context, nmc *kmmv1beta1.NodeModulesConfig) error
	UpdateNodeLabels(ctx context.Context, nmc *kmmv1beta1.NodeModulesConfig, node *v1.Node) ([]types.NamespacedName, []types.NamespacedName, error)
	RecordEvents(node *v1.Node, loadedModules, unloadedModules []types.NamespacedName)
}

type nmcReconcilerHelperImpl struct {
	client   client.Client
	pm       podManager
	recorder record.EventRecorder
	nodeAPI  node.Node
	lph      labelPreparationHelper
}

func newNMCReconcilerHelper(client client.Client, pm podManager, recorder record.EventRecorder, nodeAPI node.Node) nmcReconcilerHelper {
	return &nmcReconcilerHelperImpl{
		client:   client,
		pm:       pm,
		recorder: recorder,
		nodeAPI:  nodeAPI,
		lph:      newLabelPreparationHelper(),
	}
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

	podList, err := h.pm.ListWorkerPodsOnNode(ctx, nmcObj.Name)
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
	podName := workerPodName(nmcObj.Name, spec.Name)

	logger := ctrl.LoggerFrom(ctx)

	pod, err := h.pm.GetWorkerPod(ctx, podName, spec.Namespace)
	if err != nil {
		return fmt.Errorf("could not get the worker Pod %s: %v", podName, err)
	}

	if pod == nil {
		// new module is introduced, need to load it
		if status == nil {
			logger.Info("Missing status; creating loader Pod")
			return h.pm.CreateLoaderPod(ctx, nmcObj, spec)
		}

		/* configuration changed for module: if spec status contain the same kernel,
		unload the kernel module, otherwise - load kernel modules, since the pod
		is not running, the module cannot be loaded using the old kernel configuration
		*/
		if !reflect.DeepEqual(spec.Config, status.Config) {
			if spec.Config.KernelVersion == status.Config.KernelVersion {
				logger.Info("Outdated config in status; creating unloader Pod")
				return h.pm.CreateUnloaderPod(ctx, nmcObj, status)
			}
			logger.Info("Outdated config in status and kernels differ, probably due to upgrade; creating loader Pod")
			return h.pm.CreateLoaderPod(ctx, nmcObj, spec)
		}

		if h.nodeAPI.NodeBecomeReadyAfter(node, status.LastTransitionTime) {
			logger.Info("node has been rebooted and become ready after kernel module was loaded; creating loader Pod")
			return h.pm.CreateLoaderPod(ctx, nmcObj, spec)
		}

		return nil
	}

	if pod.Labels[actionLabelKey] != WorkerActionLoad {
		logger.Info("Worker Pod is not loading the kmod; doing nothing")
		return nil
	}

	if GetContainerStatus(pod.Status.ContainerStatuses, workerContainerName).RestartCount == 0 {
		logger.Info("Worker Loader Pod has not yet restarted; doing nothing")
		return nil
	}

	podTemplate, err := h.pm.LoaderPodTemplate(ctx, nmcObj, spec)
	if err != nil {
		return fmt.Errorf("could not create the Pod template for %s: %v", podName, err)
	}

	if podTemplate.Annotations[hashAnnotationKey] != pod.Annotations[hashAnnotationKey] {
		logger.Info("Hash differs, deleting pod")
		return h.pm.DeletePod(ctx, pod)
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
	podName := workerPodName(nmcObj.Name, status.Name)

	logger := ctrl.LoggerFrom(ctx).WithValues("pod name", podName)

	/* node was rebooted, spec not set so no kernel module is loaded, no need to unload.
	   it also fixes the scenario when node's kernel was upgraded, so unload pod will fail anyway
	*/
	if h.nodeAPI.NodeBecomeReadyAfter(node, status.LastTransitionTime) {
		logger.Info("node was rebooted and spec is missing: delete the status to allow Module CR unload, if needed")
		patchFrom := client.MergeFrom(nmcObj.DeepCopy())
		nmc.RemoveModuleStatus(&nmcObj.Status.Modules, status.Namespace, status.Name)
		return h.client.Status().Patch(ctx, nmcObj, patchFrom)
	}

	pod, err := h.pm.GetWorkerPod(ctx, podName, status.Namespace)
	if err != nil {
		return fmt.Errorf("error while getting the worker Pod %s: %v", podName, err)
	}

	if pod == nil {
		logger.Info("Worker Pod does not exist; creating it")
		return h.pm.CreateUnloaderPod(ctx, nmcObj, status)
	}

	if pod.Labels[actionLabelKey] == WorkerActionLoad {
		logger.Info("Worker Pod is loading the kmod; deleting it")
		return h.pm.DeletePod(ctx, pod)
	}

	if GetContainerStatus(pod.Status.ContainerStatuses, workerContainerName).RestartCount == 0 {
		logger.Info("Worker Pod has not yet restarted; doing nothing")
		return nil
	}

	podTemplate, err := h.pm.UnloaderPodTemplate(ctx, nmcObj, status)
	if err != nil {
		return fmt.Errorf("could not create the Pod template for %s: %v", podName, err)
	}

	if podTemplate.Annotations[hashAnnotationKey] != pod.Annotations[hashAnnotationKey] {
		logger.Info("Hash differs, deleting pod")
		return h.pm.DeletePod(ctx, pod)
	}

	return nil
}

func (h *nmcReconcilerHelperImpl) RemovePodFinalizers(ctx context.Context, nodeName string) error {
	pods, err := h.pm.ListWorkerPodsOnNode(ctx, nodeName)
	if err != nil {
		return fmt.Errorf("could not delete orphan worker Pods on node %s: %v", nodeName, err)
	}

	errs := make([]error, 0, len(pods))

	for i := 0; i < len(pods); i++ {
		pod := &pods[i]

		mergeFrom := client.MergeFrom(pod.DeepCopy())

		if controllerutil.RemoveFinalizer(pod, nodeModulesConfigFinalizer) {
			if err = h.client.Patch(ctx, pod, mergeFrom); err != nil {
				errs = append(
					errs,
					fmt.Errorf("could not patch Pod %s/%s: %v", pod.Namespace, pod.Name, err),
				)

				continue
			}
		}
	}

	return errors.Join(errs...)
}

func (h *nmcReconcilerHelperImpl) SyncStatus(ctx context.Context, nmcObj *kmmv1beta1.NodeModulesConfig) error {
	logger := ctrl.LoggerFrom(ctx)

	logger.Info("Syncing status")

	pods, err := h.pm.ListWorkerPodsOnNode(ctx, nmcObj.Name)
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
			if p.Labels[actionLabelKey] == WorkerActionUnload {
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

			if err = yaml.UnmarshalStrict([]byte(p.Annotations[configAnnotationKey]), &status.Config); err != nil {
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
			status.Tolerations = p.Spec.Tolerations

			podLTT := GetContainerStatus(p.Status.ContainerStatuses, workerContainerName).
				State.
				Terminated.
				FinishedAt

			status.LastTransitionTime = podLTT

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
		err = h.pm.DeletePod(ctx, &pod)
		errs = append(errs, err)
	}

	return errors.Join(errs...)
}

type labelPreparationHelper interface {
	getDeprecatedKernelModuleReadyLabels(node v1.Node) sets.Set[string]
	getNodeKernelModuleReadyLabels(node v1.Node) sets.Set[types.NamespacedName]
	getSpecLabelsAndTheirConfigs(nmc *kmmv1beta1.NodeModulesConfig) map[types.NamespacedName]kmmv1beta1.ModuleConfig
	getStatusLabelsAndTheirConfigs(nmc *kmmv1beta1.NodeModulesConfig) map[types.NamespacedName]kmmv1beta1.ModuleConfig
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

func (lph *labelPreparationHelperImpl) getDeprecatedKernelModuleReadyLabels(node v1.Node) sets.Set[string] {
	deprecatedNodeModuleReadyLabels := sets.New[string]()

	for label := range node.GetLabels() {
		if utils.IsDeprecatedKernelModuleReadyNodeLabel(label) {
			deprecatedNodeModuleReadyLabels.Insert(label)
		}
	}
	return deprecatedNodeModuleReadyLabels
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

func (h *nmcReconcilerHelperImpl) UpdateNodeLabels(ctx context.Context, nmc *kmmv1beta1.NodeModulesConfig, node *v1.Node) ([]types.NamespacedName, []types.NamespacedName, error) {

	// get all the kernel module ready labels of the node
	nodeModuleReadyLabels := h.lph.getNodeKernelModuleReadyLabels(*node)
	deprecatedNodeModuleReadyLabels := h.lph.getDeprecatedKernelModuleReadyLabels(*node)

	// get spec labels and their config
	specLabels := h.lph.getSpecLabelsAndTheirConfigs(nmc)

	// get status labels and their config
	statusLabels := h.lph.getStatusLabelsAndTheirConfigs(nmc)

	// label in node but not in spec or status - should be removed
	nsnLabelsToBeRemoved := h.lph.removeOrphanedLabels(nodeModuleReadyLabels, specLabels, statusLabels)

	// label in spec and status and config equal - should be added
	nsnLabelsToBeLoaded := h.lph.addEqualLabels(nodeModuleReadyLabels, specLabels, statusLabels)

	var loadedLabels []string
	unloadedLabels := deprecatedNodeModuleReadyLabels.UnsortedList()

	for _, label := range nsnLabelsToBeRemoved {
		unloadedLabels = append(unloadedLabels, utils.GetKernelModuleReadyNodeLabel(label.Namespace, label.Name))
	}
	for _, label := range nsnLabelsToBeLoaded {
		loadedLabels = append(loadedLabels, utils.GetKernelModuleReadyNodeLabel(label.Namespace, label.Name))
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

const (
	configFileName = "config.yaml"
	configFullPath = volMountPointConfig + "/" + configFileName

	volNameConfig       = "config"
	volMountPointConfig = "/etc/kmm-worker"
)

//go:generate mockgen -source=nmc_reconciler.go -package=controllers -destination=mock_nmc_reconciler.go podManager

type podManager interface {
	CreateLoaderPod(ctx context.Context, nmc client.Object, nms *kmmv1beta1.NodeModuleSpec) error
	CreateUnloaderPod(ctx context.Context, nmc client.Object, nms *kmmv1beta1.NodeModuleStatus) error
	DeletePod(ctx context.Context, pod *v1.Pod) error
	ListWorkerPodsOnNode(ctx context.Context, nodeName string) ([]v1.Pod, error)
	LoaderPodTemplate(ctx context.Context, nmc client.Object, nms *kmmv1beta1.NodeModuleSpec) (*v1.Pod, error)
	GetWorkerPod(ctx context.Context, podName, namespace string) (*v1.Pod, error)
	UnloaderPodTemplate(ctx context.Context, nmc client.Object, nms *kmmv1beta1.NodeModuleStatus) (*v1.Pod, error)
}

type podManagerImpl struct {
	client      client.Client
	scheme      *runtime.Scheme
	workerCfg   *config.Worker
	workerImage string
}

func newPodManager(client client.Client, workerImage string, scheme *runtime.Scheme, workerCfg *config.Worker) podManager {
	return &podManagerImpl{
		client:      client,
		scheme:      scheme,
		workerCfg:   workerCfg,
		workerImage: workerImage,
	}
}

func (p *podManagerImpl) CreateLoaderPod(ctx context.Context, nmcObj client.Object, nms *kmmv1beta1.NodeModuleSpec) error {
	pod, err := p.LoaderPodTemplate(ctx, nmcObj, nms)
	if err != nil {
		return fmt.Errorf("could not get loader Pod template: %v", err)
	}

	return p.client.Create(ctx, pod)
}

func (p *podManagerImpl) CreateUnloaderPod(ctx context.Context, nmc client.Object, nms *kmmv1beta1.NodeModuleStatus) error {
	pod, err := p.UnloaderPodTemplate(ctx, nmc, nms)
	if err != nil {
		return fmt.Errorf("could not create the Pod template: %v", err)
	}

	return p.client.Create(ctx, pod)
}

func (p *podManagerImpl) DeletePod(ctx context.Context, pod *v1.Pod) error {
	logger := ctrl.LoggerFrom(ctx)

	logger.Info("Removing Pod finalizer")

	podPatch := client.MergeFrom(pod.DeepCopy())

	controllerutil.RemoveFinalizer(pod, nodeModulesConfigFinalizer)

	if err := p.client.Patch(ctx, pod, podPatch); err != nil {
		return fmt.Errorf("could not patch Pod %s/%s: %v", pod.Namespace, pod.Name, err)
	}

	if pod.DeletionTimestamp == nil {
		logger.Info("DeletionTimestamp not set; deleting Pod")

		if err := p.client.Delete(ctx, pod); client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("could not delete Pod %s/%s: %v", pod.Namespace, pod.Name, err)
		}
	} else {
		logger.Info("DeletionTimestamp set; not deleting Pod")
	}

	return nil
}

func (p *podManagerImpl) GetWorkerPod(ctx context.Context, podName, namespace string) (*v1.Pod, error) {
	pod := v1.Pod{}
	nsn := types.NamespacedName{Namespace: namespace, Name: podName}

	if err := p.client.Get(ctx, nsn, &pod); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, nil
		} else {
			return nil, fmt.Errorf("could not get pod %s: %v", nsn, err)
		}
	}

	return &pod, nil
}

func (p *podManagerImpl) ListWorkerPodsOnNode(ctx context.Context, nodeName string) ([]v1.Pod, error) {
	logger := ctrl.LoggerFrom(ctx).WithValues("node name", nodeName)

	pl := v1.PodList{}

	hl := client.HasLabels{actionLabelKey}
	mf := client.MatchingFields{".spec.nodeName": nodeName}

	logger.V(1).Info("Listing worker Pods")

	if err := p.client.List(ctx, &pl, hl, mf); err != nil {
		return nil, fmt.Errorf("could not list worker pods for node %s: %v", nodeName, err)
	}

	return pl.Items, nil
}

func (p *podManagerImpl) LoaderPodTemplate(ctx context.Context, nmc client.Object, nms *kmmv1beta1.NodeModuleSpec) (*v1.Pod, error) {
	pod, err := p.baseWorkerPod(ctx, nmc, &nms.ModuleItem, &nms.Config)
	if err != nil {
		return nil, fmt.Errorf("could not create the base Pod: %v", err)
	}

	if nms.Config.Modprobe.ModulesLoadingOrder != nil {
		if err = setWorkerSofdepConfig(pod, nms.Config.Modprobe.ModulesLoadingOrder); err != nil {
			return nil, fmt.Errorf("could not set software dependency for mulitple modules: %v", err)
		}
	}

	args := []string{"kmod", "load", configFullPath}

	privileged := false
	if nms.Config.Modprobe.FirmwarePath != "" {

		firmwareHostPath := p.workerCfg.FirmwareHostPath
		if firmwareHostPath == nil {
			return nil, fmt.Errorf("firmwareHostPath wasn't set, while the Module requires firmware loading")
		}

		args = append(args, "--"+worker.FlagFirmwarePath, *firmwareHostPath)

		firmwarePathContainerImg := filepath.Join(nms.Config.Modprobe.FirmwarePath, "*")
		firmwarePathWorkerImg := filepath.Join(sharedFilesDir, nms.Config.Modprobe.FirmwarePath)
		if err = addCopyCommand(pod, firmwarePathContainerImg, firmwarePathWorkerImg); err != nil {
			return nil, fmt.Errorf("could not add the copy command to the init container: %v", err)
		}

		if err = setFirmwareVolume(pod, firmwareHostPath); err != nil {
			return nil, fmt.Errorf("could not map host volume needed for firmware loading: %v", err)
		}

		privileged = true
	}

	if err = setWorkerConfigAnnotation(pod, nms.Config); err != nil {
		return nil, fmt.Errorf("could not set worker config: %v", err)
	}

	if err = setWorkerSecurityContext(pod, p.workerCfg, privileged); err != nil {
		return nil, fmt.Errorf("could not set the worker Pod as privileged: %v", err)
	}

	if err = setWorkerContainerArgs(pod, args); err != nil {
		return nil, fmt.Errorf("could not set worker container args: %v", err)
	}

	meta.SetLabel(pod, actionLabelKey, WorkerActionLoad)

	return pod, setHashAnnotation(pod)
}

func (p *podManagerImpl) UnloaderPodTemplate(ctx context.Context, nmc client.Object, nms *kmmv1beta1.NodeModuleStatus) (*v1.Pod, error) {
	pod, err := p.baseWorkerPod(ctx, nmc, &nms.ModuleItem, &nms.Config)
	if err != nil {
		return nil, fmt.Errorf("could not create the base Pod: %v", err)
	}

	args := []string{"kmod", "unload", configFullPath}

	if err = setWorkerConfigAnnotation(pod, nms.Config); err != nil {
		return nil, fmt.Errorf("could not set worker config: %v", err)
	}

	if err = setWorkerSecurityContext(pod, p.workerCfg, false); err != nil {
		return nil, fmt.Errorf("could not set the worker Pod's security context: %v", err)
	}

	if nms.Config.Modprobe.ModulesLoadingOrder != nil {
		if err = setWorkerSofdepConfig(pod, nms.Config.Modprobe.ModulesLoadingOrder); err != nil {
			return nil, fmt.Errorf("could not set software dependency for mulitple modules: %v", err)
		}
	}

	if nms.Config.Modprobe.FirmwarePath != "" {
		firmwareHostPath := p.workerCfg.FirmwareHostPath
		if firmwareHostPath == nil {
			return nil, fmt.Errorf("firmwareHostPath was not set while the Module requires firmware unloading")
		}
		args = append(args, "--"+worker.FlagFirmwarePath, *firmwareHostPath)

		firmwarePathContainerImg := filepath.Join(nms.Config.Modprobe.FirmwarePath, "*")
		firmwarePathWorkerImg := filepath.Join(sharedFilesDir, nms.Config.Modprobe.FirmwarePath)
		if err = addCopyCommand(pod, firmwarePathContainerImg, firmwarePathWorkerImg); err != nil {
			return nil, fmt.Errorf("could not add the copy command to the init container: %v", err)
		}

		if err = setFirmwareVolume(pod, firmwareHostPath); err != nil {
			return nil, fmt.Errorf("could not map host volume needed for firmware unloading: %v", err)
		}
	}

	if err = setWorkerContainerArgs(pod, args); err != nil {
		return nil, fmt.Errorf("could not set worker container args: %v", err)
	}

	meta.SetLabel(pod, actionLabelKey, WorkerActionUnload)

	return pod, setHashAnnotation(pod)
}

var (
	requests = v1.ResourceList{
		v1.ResourceCPU:    resource.MustParse("0.5"),
		v1.ResourceMemory: resource.MustParse("64Mi"),
	}
	limits = v1.ResourceList{
		v1.ResourceCPU:    resource.MustParse("1"),
		v1.ResourceMemory: resource.MustParse("128Mi"),
	}
)

func addCopyCommand(pod *v1.Pod, src, dst string) error {

	container, _ := podcmd.FindContainerByName(pod, initContainerName)
	if container == nil {
		return errors.New("could not find the init container")
	}

	const template = `
mkdir -p %s;
cp -R %s %s;
`
	copyCommand := fmt.Sprintf(template, dst, src, dst)
	container.Args[0] = strings.Join([]string{container.Args[0], copyCommand}, "")

	return nil
}

func (p *podManagerImpl) baseWorkerPod(ctx context.Context, nmc client.Object, item *kmmv1beta1.ModuleItem,
	moduleConfig *kmmv1beta1.ModuleConfig) (*v1.Pod, error) {

	const (
		volNameLibModules    = "lib-modules"
		volNameUsrLibModules = "usr-lib-modules"
	)

	hostPathDirectory := v1.HostPathDirectory

	volumes := []v1.Volume{
		{
			Name: volumeNameConfig,
			VolumeSource: v1.VolumeSource{
				DownwardAPI: &v1.DownwardAPIVolumeSource{
					Items: []v1.DownwardAPIVolumeFile{
						{
							Path: configFileName,
							FieldRef: &v1.ObjectFieldSelector{
								FieldPath: fmt.Sprintf("metadata.annotations['%s']", configAnnotationKey),
							},
						},
					},
				},
			},
		},
		{
			Name: volNameLibModules,
			VolumeSource: v1.VolumeSource{
				HostPath: &v1.HostPathVolumeSource{
					Path: "/lib/modules",
					Type: &hostPathDirectory,
				},
			},
		},
		{
			Name: volNameUsrLibModules,
			VolumeSource: v1.VolumeSource{
				HostPath: &v1.HostPathVolumeSource{
					Path: "/usr/lib/modules",
					Type: &hostPathDirectory,
				},
			},
		},
		{
			Name: volNameTmp,
			VolumeSource: v1.VolumeSource{
				EmptyDir: &v1.EmptyDirVolumeSource{},
			},
		},
	}

	volumeMounts := []v1.VolumeMount{
		{
			Name:      volNameConfig,
			MountPath: volMountPointConfig,
			ReadOnly:  true,
		},
		{
			Name:      volNameLibModules,
			MountPath: "/lib/modules",
			ReadOnly:  true,
		},
		{
			Name:      volNameUsrLibModules,
			MountPath: "/usr/lib/modules",
			ReadOnly:  true,
		},
		{
			Name:      volNameTmp,
			MountPath: sharedFilesDir,
			ReadOnly:  true,
		},
	}

	var imagePullSecrets []v1.LocalObjectReference
	if item.ImageRepoSecret != nil {
		imagePullSecrets = append(imagePullSecrets, *item.ImageRepoSecret)
	}

	nodeName := nmc.GetName()
	pod := v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: item.Namespace,
			Name:      workerPodName(nodeName, item.Name),
			Labels: map[string]string{
				"app.kubernetes.io/name":      "kmm",
				"app.kubernetes.io/component": "worker",
				"app.kubernetes.io/part-of":   "kmm",
				constants.ModuleNameLabel:     item.Name,
			},
		},
		Spec: v1.PodSpec{
			InitContainers: []v1.Container{
				{
					Name:            initContainerName,
					Image:           moduleConfig.ContainerImage,
					ImagePullPolicy: moduleConfig.ImagePullPolicy,
					Command:         []string{"/bin/sh", "-c"},
					Args:            []string{""},
					VolumeMounts: []v1.VolumeMount{
						{
							Name:      volNameTmp,
							MountPath: sharedFilesDir,
						},
					},
					Resources: v1.ResourceRequirements{
						Requests: requests,
						Limits:   limits,
					},
				},
			},
			Containers: []v1.Container{
				{
					Name:         workerContainerName,
					Image:        p.workerImage,
					VolumeMounts: volumeMounts,
					Resources: v1.ResourceRequirements{
						Requests: requests,
						Limits:   limits,
					},
				},
			},
			NodeName:           nodeName,
			RestartPolicy:      v1.RestartPolicyOnFailure,
			ServiceAccountName: item.ServiceAccountName,
			ImagePullSecrets:   imagePullSecrets,
			Volumes:            volumes,
			Tolerations:        item.Tolerations,
		},
	}

	if err := ctrl.SetControllerReference(nmc, &pod, p.scheme); err != nil {
		return nil, fmt.Errorf("could not set the owner as controller: %v", err)
	}

	kmodsPathContainerImg := filepath.Join(moduleConfig.Modprobe.DirName, "lib", "modules", moduleConfig.KernelVersion)
	kmodsPathWorkerImg := filepath.Join(sharedFilesDir, moduleConfig.Modprobe.DirName, "lib", "modules")
	if err := addCopyCommand(&pod, kmodsPathContainerImg, kmodsPathWorkerImg); err != nil {
		return nil, fmt.Errorf("could not add the copy command to the init container: %v", err)
	}

	controllerutil.AddFinalizer(&pod, nodeModulesConfigFinalizer)

	return &pod, nil
}

func setWorkerConfigAnnotation(pod *v1.Pod, cfg kmmv1beta1.ModuleConfig) error {
	b, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("could not marshal the ModuleConfig to YAML: %v", err)
	}
	meta.SetAnnotation(pod, configAnnotationKey, string(b))

	return nil
}

func setWorkerContainerArgs(pod *v1.Pod, args []string) error {
	container, _ := podcmd.FindContainerByName(pod, workerContainerName)
	if container == nil {
		return errors.New("could not find the worker container")
	}

	container.Args = args

	return nil
}

func setWorkerSecurityContext(pod *v1.Pod, workerCfg *config.Worker, privileged bool) error {
	container, _ := podcmd.FindContainerByName(pod, workerContainerName)
	if container == nil {
		return errors.New("could not find the worker container")
	}

	sc := &v1.SecurityContext{}

	if privileged {
		sc.Privileged = &privileged
	} else {
		sc.Capabilities = &v1.Capabilities{
			Add: []v1.Capability{"SYS_MODULE"},
		}
		sc.RunAsUser = workerCfg.RunAsUser
		sc.SELinuxOptions = &v1.SELinuxOptions{Type: workerCfg.SELinuxType}
	}

	container.SecurityContext = sc

	return nil
}

func setWorkerSofdepConfig(pod *v1.Pod, modulesLoadingOrder []string) error {
	softdepAnnotationValue := getModulesOrderAnnotationValue(modulesLoadingOrder)
	meta.SetAnnotation(pod, modulesOrderKey, softdepAnnotationValue)

	softdepVolume := v1.Volume{
		Name: "modules-order",
		VolumeSource: v1.VolumeSource{
			DownwardAPI: &v1.DownwardAPIVolumeSource{
				Items: []v1.DownwardAPIVolumeFile{
					{
						Path:     "softdep.conf",
						FieldRef: &v1.ObjectFieldSelector{FieldPath: fmt.Sprintf("metadata.annotations['%s']", modulesOrderKey)},
					},
				},
			},
		},
	}
	softDepVolumeMount := v1.VolumeMount{
		Name:      "modules-order",
		ReadOnly:  true,
		MountPath: "/etc/modprobe.d",
	}
	pod.Spec.Volumes = append(pod.Spec.Volumes, softdepVolume)
	container, _ := podcmd.FindContainerByName(pod, workerContainerName)
	if container == nil {
		return errors.New("could not find the worker container")
	}
	container.VolumeMounts = append(container.VolumeMounts, softDepVolumeMount)
	return nil
}

func setFirmwareVolume(pod *v1.Pod, firmwareHostPath *string) error {

	const volNameVarLibFirmware = "lib-firmware"
	container, _ := podcmd.FindContainerByName(pod, workerContainerName)
	if container == nil {
		return errors.New("could not find the worker container")
	}

	if firmwareHostPath == nil {
		return errors.New("hostFirmwarePath must be set")
	}

	firmwareVolumeMount := v1.VolumeMount{
		Name:      volNameVarLibFirmware,
		MountPath: *firmwareHostPath,
	}

	hostPathDirectoryOrCreate := v1.HostPathDirectoryOrCreate
	firmwareVolume := v1.Volume{
		Name: volNameVarLibFirmware,
		VolumeSource: v1.VolumeSource{
			HostPath: &v1.HostPathVolumeSource{
				Path: *firmwareHostPath,
				Type: &hostPathDirectoryOrCreate,
			},
		},
	}

	pod.Spec.Volumes = append(pod.Spec.Volumes, firmwareVolume)
	container.VolumeMounts = append(container.VolumeMounts, firmwareVolumeMount)

	return nil
}

func setHashAnnotation(pod *v1.Pod) error {
	hash, err := hashstructure.Hash(pod, hashstructure.FormatV2, nil)
	if err != nil {
		return fmt.Errorf("could not hash the pod template: %v", err)
	}

	pod.Annotations[hashAnnotationKey] = fmt.Sprintf("%d", hash)

	return nil
}

func getModulesOrderAnnotationValue(modulesNames []string) string {
	var softDepData strings.Builder
	for i := 0; i < len(modulesNames)-1; i++ {
		fmt.Fprintf(&softDepData, "softdep %s pre: %s\n", modulesNames[i], modulesNames[i+1])
	}
	return softDepData.String()
}
