package controllers

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/config"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	"github.com/kubernetes-sigs/kernel-module-management/internal/filter"
	"github.com/kubernetes-sigs/kernel-module-management/internal/meta"
	"github.com/kubernetes-sigs/kernel-module-management/internal/nmc"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
	"github.com/kubernetes-sigs/kernel-module-management/internal/worker"
	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/record"
	"k8s.io/kubectl/pkg/cmd/util/podcmd"
	"k8s.io/utils/pointer"
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
	modulesOrderKey            = "kmm.node.kubernetes.io/modules-order"
	nodeModulesConfigFinalizer = "kmm.node.kubernetes.io/nodemodulesconfig-reconciler"
	volumeNameConfig           = "config"
	workerContainerName        = "worker"
)

//+kubebuilder:rbac:groups=kmm.sigs.x-k8s.io,resources=nodemodulesconfigs,verbs=get;list;watch
//+kubebuilder:rbac:groups=kmm.sigs.x-k8s.io,resources=nodemodulesconfigs/status,verbs=patch
//+kubebuilder:rbac:groups="core",resources=pods,verbs=create;delete;get;list;watch
//+kubebuilder:rbac:groups="core",resources=nodes,verbs=get;list;watch
//+kubebuilder:rbac:groups="core",resources=secrets,verbs=get;list;watch
//+kubebuilder:rbac:groups="core",resources=serviceaccounts,verbs=get;list;watch

type NMCReconciler struct {
	client client.Client
	helper nmcReconcilerHelper
}

func NewNMCReconciler(
	client client.Client,
	scheme *runtime.Scheme,
	workerImage string,
	workerCfg *config.Worker,
	recorder record.EventRecorder,
) *NMCReconciler {
	pm := newPodManager(client, workerImage, scheme, workerCfg)
	helper := newNMCReconcilerHelper(client, pm, recorder)
	return &NMCReconciler{
		client: client,
		helper: helper,
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

			if err = r.helper.RemoveOrphanFinalizers(ctx, req.Name); err != nil {
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

	errs := make([]error, 0, len(nmcObj.Spec.Modules)+len(nmcObj.Status.Modules))

	for _, mod := range nmcObj.Spec.Modules {
		moduleNameKey := mod.Namespace + "/" + mod.Name

		logger := logger.WithValues("module", moduleNameKey)

		if err := r.helper.ProcessModuleSpec(ctrl.LoggerInto(ctx, logger), &nmcObj, &mod, statusMap[moduleNameKey]); err != nil {
			errs = append(
				errs,
				fmt.Errorf("error processing Module %s: %v", moduleNameKey, err),
			)

			continue
		}

		delete(statusMap, moduleNameKey)
	}

	// We have processed all module specs.
	// Now, go through the remaining, "orphan" statuses that do not have a corresponding spec; those must be unloaded.

	for statusNameKey, status := range statusMap {
		logger := logger.WithValues("status", statusNameKey)

		if err := r.helper.ProcessUnconfiguredModuleStatus(ctrl.LoggerInto(ctx, logger), &nmcObj, status); err != nil {
			errs = append(
				errs,
				fmt.Errorf("erorr processing orphan status for Module %s: %v", statusNameKey, err),
			)
		}
	}

	if err := errors.Join(errs...); err != nil {
		return reconcile.Result{}, fmt.Errorf("could not process orphan statuses: %v", err)
	}

	if err := r.helper.GarbageCollectInUseLabels(ctx, &nmcObj); err != nil {
		return reconcile.Result{}, fmt.Errorf("could not garbage collect in-use labels for NMC %s: %v", req.NamespacedName, err)
	}

	if err := r.helper.UpdateNodeLabelsAndRecordEvents(ctx, &nmcObj); err != nil {
		return reconcile.Result{}, fmt.Errorf("could not update node's labels for NMC %s: %v", req.NamespacedName, err)
	}

	return ctrl.Result{}, nil
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

func FindNodeCondition(cond []v1.NodeCondition, conditionType v1.NodeConditionType) *v1.NodeCondition {
	for i := 0; i < len(cond); i++ {
		c := cond[i]

		if c.Type == conditionType {
			return &c
		}
	}

	return nil
}

//go:generate mockgen -source=nmc_reconciler.go -package=controllers -destination=mock_nmc_reconciler.go workerHelper

type nmcReconcilerHelper interface {
	GarbageCollectInUseLabels(ctx context.Context, nmc *kmmv1beta1.NodeModulesConfig) error
	ProcessModuleSpec(ctx context.Context, nmc *kmmv1beta1.NodeModulesConfig, spec *kmmv1beta1.NodeModuleSpec, status *kmmv1beta1.NodeModuleStatus) error
	ProcessUnconfiguredModuleStatus(ctx context.Context, nmc *kmmv1beta1.NodeModulesConfig, status *kmmv1beta1.NodeModuleStatus) error
	RemoveOrphanFinalizers(ctx context.Context, nodeName string) error
	SyncStatus(ctx context.Context, nmc *kmmv1beta1.NodeModulesConfig) error
	UpdateNodeLabelsAndRecordEvents(ctx context.Context, nmc *kmmv1beta1.NodeModulesConfig) error
}

type nmcReconcilerHelperImpl struct {
	client   client.Client
	pm       podManager
	recorder record.EventRecorder
}

func newNMCReconcilerHelper(client client.Client, pm podManager, recorder record.EventRecorder) nmcReconcilerHelper {
	return &nmcReconcilerHelperImpl{
		client:   client,
		pm:       pm,
		recorder: recorder,
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
) error {
	logger := ctrl.LoggerFrom(ctx)

	if status == nil {
		logger.Info("Missing status; creating loader Pod")
		return h.pm.CreateLoaderPod(ctx, nmcObj, spec)
	}

	if status.InProgress {
		logger.Info("Worker pod is running; skipping")
		return nil
	}

	if status.Config == nil {
		logger.Info("Missing status config and pod is not running: previously failed pod, creating loader Pod")
		return h.pm.CreateLoaderPod(ctx, nmcObj, spec)
	}

	if !reflect.DeepEqual(spec.Config, *status.Config) {
		logger.Info("Outdated config in status; creating unloader Pod")

		return h.pm.CreateUnloaderPod(ctx, nmcObj, status)
	}

	node := v1.Node{}

	if err := h.client.Get(ctx, types.NamespacedName{Name: nmcObj.Name}, &node); err != nil {
		return fmt.Errorf("could not get node %s: %v", nmcObj.Name, err)
	}

	readyCondition := FindNodeCondition(node.Status.Conditions, v1.NodeReady)
	if readyCondition == nil {
		return fmt.Errorf("node %s has no Ready condition", nmcObj.Name)
	}

	if readyCondition.Status == v1.ConditionTrue && status.LastTransitionTime.Before(&readyCondition.LastTransitionTime) {
		logger.Info("Outdated last transition time status; creating loader Pod")

		return h.pm.CreateLoaderPod(ctx, nmcObj, spec)
	}

	logger.Info("Spec and status in sync; nothing to do")

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
) error {
	logger := ctrl.LoggerFrom(ctx)

	if status.InProgress {
		logger.Info("Sync status is in progress; skipping")
		return nil
	}

	if status.Config == nil {
		logger.Info("Missing status config and pod is not running: previously failed pod, no need to unload")
		patchFrom := client.MergeFrom(nmcObj.DeepCopy())
		nmc.RemoveModuleStatus(&nmcObj.Status.Modules, status.Namespace, status.Name)
		return h.client.Status().Patch(ctx, nmcObj, patchFrom)
	}

	logger.Info("Creating unloader Pod")

	return h.pm.CreateUnloaderPod(ctx, nmcObj, status)
}

func (h *nmcReconcilerHelperImpl) RemoveOrphanFinalizers(ctx context.Context, nodeName string) error {
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
		if status == nil {
			status = &kmmv1beta1.NodeModuleStatus{
				ModuleItem: kmmv1beta1.ModuleItem{
					Name:      modName,
					Namespace: modNamespace,
				},
			}
		}

		if irsName, err := getImageRepoSecretName(&p); err != nil {
			logger.Info(
				utils.WarnString("Error while looking for the imageRepoSecret volume"),
				"error",
				err,
			)
		} else if irsName != "" {
			status.ImageRepoSecret = &v1.LocalObjectReference{Name: irsName}
		}

		status.ServiceAccountName = p.Spec.ServiceAccountName

		updateModuleStatus := false

		switch phase {
		case v1.PodFailed:
			status.InProgress = false
			updateModuleStatus = true
			podsToDelete = append(podsToDelete, p)
		case v1.PodSucceeded:
			if p.Labels[actionLabelKey] == WorkerActionUnload {
				podsToDelete = append(podsToDelete, p)
				nmc.RemoveModuleStatus(&nmcObj.Status.Modules, modNamespace, modName)
				break
			}

			updateModuleStatus = true

			config := kmmv1beta1.ModuleConfig{}

			if err = yaml.UnmarshalStrict([]byte(p.Annotations[configAnnotationKey]), &config); err != nil {
				errs = append(
					errs,
					fmt.Errorf("%s: could not unmarshal the ModuleConfig from YAML: %v", podNSN, err),
				)

				continue
			}

			status.Config = &config
			status.InProgress = false

			podLTT := GetContainerStatus(p.Status.ContainerStatuses, workerContainerName).
				State.
				Terminated.
				FinishedAt

			status.LastTransitionTime = &podLTT
			podsToDelete = append(podsToDelete, p)
		case v1.PodPending, v1.PodRunning:
			status.InProgress = true
			updateModuleStatus = true
			// TODO: if the NMC's spec changed compared to the Pod's config, recreate the Pod
		default:
			errs = append(
				errs,
				fmt.Errorf("%s: unhandled Pod phase %q", podNSN, phase),
			)
		}

		if updateModuleStatus {
			nmc.SetModuleStatus(&nmcObj.Status.Modules, *status)
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

func (h *nmcReconcilerHelperImpl) UpdateNodeLabelsAndRecordEvents(ctx context.Context, nmc *kmmv1beta1.NodeModulesConfig) error {
	node := v1.Node{}
	if err := h.client.Get(ctx, types.NamespacedName{Name: nmc.Name}, &node); err != nil {
		return fmt.Errorf("could not get node %s: %v", nmc.Name, err)
	}

	// get all the kernel module ready labels of the node
	nodeModuleReadyLabels := sets.New[types.NamespacedName]()

	for label := range node.GetLabels() {
		if ok, namespace, name := utils.IsKernelModuleReadyNodeLabel(label); ok {
			nodeModuleReadyLabels.Insert(types.NamespacedName{Namespace: namespace, Name: name})
		}
	}

	// get spec labels and their config
	specLabels := make(map[types.NamespacedName]kmmv1beta1.ModuleConfig)
	for _, module := range nmc.Spec.Modules {
		specLabels[types.NamespacedName{Namespace: module.Namespace, Name: module.Name}] = module.Config
	}

	// get status labels and their config
	statusLabels := make(map[types.NamespacedName]kmmv1beta1.ModuleConfig)
	for _, module := range nmc.Status.Modules {
		label := types.NamespacedName{Namespace: module.Namespace, Name: module.Name}
		statusLabels[label] = kmmv1beta1.ModuleConfig{}
		if module.Config != nil {
			statusLabels[label] = *module.Config
		}
	}

	unloaded := make([]types.NamespacedName, 0, len(nodeModuleReadyLabels))
	loaded := make([]types.NamespacedName, 0, len(specLabels))

	patchFrom := client.MergeFrom(node.DeepCopy())

	// label in node but not in spec or status - should be removed
	for nsn := range nodeModuleReadyLabels {
		_, inSpec := specLabels[nsn]
		_, inStatus := statusLabels[nsn]
		if !inSpec && !inStatus {
			meta.RemoveLabel(
				&node,
				utils.GetKernelModuleReadyNodeLabel(nsn.Namespace, nsn.Name),
			)

			unloaded = append(unloaded, nsn)
		}
	}

	// label in spec and status and config equal - should be added
	for nsn, specConfig := range specLabels {
		statusConfig, ok := statusLabels[nsn]
		if ok && reflect.DeepEqual(specConfig, statusConfig) && !nodeModuleReadyLabels.Has(nsn) {
			meta.SetLabel(
				&node,
				utils.GetKernelModuleReadyNodeLabel(nsn.Namespace, nsn.Name),
				"",
			)

			loaded = append(loaded, nsn)
		}
	}

	if err := h.client.Patch(ctx, &node, patchFrom); err != nil {
		return fmt.Errorf("could not patch node: %v", err)
	}

	for _, nsn := range unloaded {
		h.recorder.AnnotatedEventf(
			&node,
			map[string]string{"module": nsn.String()},
			v1.EventTypeNormal,
			"ModuleUnloaded",
			"Module %s unloaded from the kernel",
			nsn.String(),
		)
	}

	for _, nsn := range loaded {
		h.recorder.AnnotatedEventf(
			&node,
			map[string]string{"module": nsn.String()},
			v1.EventTypeNormal,
			"ModuleLoaded",
			"Module %s loaded into the kernel",
			nsn.String(),
		)
	}

	return nil
}

const (
	configFileName = "config.yaml"
	configFullPath = volMountPointConfig + "/" + configFileName

	volNameConfig          = "config"
	volNameImageRepoSecret = "image-repo-secret"
	volMountPointConfig    = "/etc/kmm-worker"
)

//go:generate mockgen -source=nmc_reconciler.go -package=controllers -destination=mock_nmc_reconciler.go podManager

type podManager interface {
	CreateLoaderPod(ctx context.Context, nmc client.Object, nms *kmmv1beta1.NodeModuleSpec) error
	CreateUnloaderPod(ctx context.Context, nmc client.Object, nms *kmmv1beta1.NodeModuleStatus) error
	DeletePod(ctx context.Context, pod *v1.Pod) error
	ListWorkerPodsOnNode(ctx context.Context, nodeName string) ([]v1.Pod, error)
}

type podManagerImpl struct {
	client      client.Client
	psh         pullSecretHelper
	scheme      *runtime.Scheme
	workerCfg   *config.Worker
	workerImage string
}

func newPodManager(client client.Client, workerImage string, scheme *runtime.Scheme, workerCfg *config.Worker) podManager {
	return &podManagerImpl{
		client:      client,
		psh:         &pullSecretHelperImpl{client: client},
		scheme:      scheme,
		workerCfg:   workerCfg,
		workerImage: workerImage,
	}
}

func (p *podManagerImpl) CreateLoaderPod(ctx context.Context, nmc client.Object, nms *kmmv1beta1.NodeModuleSpec) error {
	pod, err := p.baseWorkerPod(ctx, nmc.GetName(), &nms.ModuleItem, nmc)
	if err != nil {
		return fmt.Errorf("could not create the base Pod: %v", err)
	}

	args := []string{"kmod", "load", configFullPath}

	privileged := false

	if p.workerCfg.SetFirmwareClassPath != nil {
		args = append(args, "--"+worker.FlagFirmwareClassPath, *p.workerCfg.SetFirmwareClassPath)
		privileged = true
	}

	if err = setWorkerConfigAnnotation(pod, nms.Config); err != nil {
		return fmt.Errorf("could not set worker config: %v", err)
	}

	if err = setWorkerSecurityContext(pod, p.workerCfg, privileged); err != nil {
		return fmt.Errorf("could not set the worker Pod as privileged: %v", err)
	}

	if nms.Config.Modprobe.ModulesLoadingOrder != nil {
		if err = setWorkerSofdepConfig(pod, nms.Config.Modprobe.ModulesLoadingOrder); err != nil {
			return fmt.Errorf("could not set software dependency for mulitple modules: %v", err)
		}
	}

	if nms.Config.Modprobe.FirmwarePath != "" {
		args = append(args, "--"+worker.FlagFirmwareMountPath, worker.FirmwareMountPath)
		if err = setFirmwareVolume(pod, p.workerCfg.SetFirmwareClassPath); err != nil {
			return fmt.Errorf("could not map host volume needed for firmware loading: %v", err)
		}
	}

	if err = setWorkerContainerArgs(pod, args); err != nil {
		return fmt.Errorf("could not set worker container args: %v", err)
	}

	meta.SetLabel(pod, actionLabelKey, WorkerActionLoad)

	return p.client.Create(ctx, pod)
}

func (p *podManagerImpl) CreateUnloaderPod(ctx context.Context, nmc client.Object, nms *kmmv1beta1.NodeModuleStatus) error {
	pod, err := p.baseWorkerPod(ctx, nmc.GetName(), &nms.ModuleItem, nmc)
	if err != nil {
		return fmt.Errorf("could not create the base Pod: %v", err)
	}

	args := []string{"kmod", "unload", configFullPath}

	if err = setWorkerConfigAnnotation(pod, *nms.Config); err != nil {
		return fmt.Errorf("could not set worker config: %v", err)
	}

	if err = setWorkerSecurityContext(pod, p.workerCfg, false); err != nil {
		return fmt.Errorf("could not set the worker Pod's security context: %v", err)
	}

	if nms.Config.Modprobe.ModulesLoadingOrder != nil {
		if err = setWorkerSofdepConfig(pod, nms.Config.Modprobe.ModulesLoadingOrder); err != nil {
			return fmt.Errorf("could not set software dependency for mulitple modules: %v", err)
		}
	}

	if nms.Config.Modprobe.FirmwarePath != "" {
		args = append(args, "--"+worker.FlagFirmwareMountPath, worker.FirmwareMountPath)
		if err = setFirmwareVolume(pod, p.workerCfg.SetFirmwareClassPath); err != nil {
			return fmt.Errorf("could not map host volume needed for firmware loading: %v", err)
		}
	}

	if err = setWorkerContainerArgs(pod, args); err != nil {
		return fmt.Errorf("could not set worker container args: %v", err)
	}

	meta.SetLabel(pod, actionLabelKey, WorkerActionUnload)

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

func (p *podManagerImpl) PodExists(ctx context.Context, nodeName, modName, modNamespace string) (bool, error) {
	pod := v1.Pod{}

	nsn := types.NamespacedName{
		Namespace: modNamespace,
		Name:      workerPodName(nodeName, modName),
	}

	if err := p.client.Get(ctx, nsn, &pod); err != nil {
		if k8serrors.IsNotFound(err) {
			return false, nil
		}

		return false, fmt.Errorf("error getting Pod %s: %v", nsn, err)
	}

	return true, nil
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

func (p *podManagerImpl) baseWorkerPod(
	ctx context.Context,
	nodeName string,
	item *kmmv1beta1.ModuleItem,
	owner client.Object,
) (*v1.Pod, error) {
	const (
		volNameLibModules    = "lib-modules"
		volNameUsrLibModules = "usr-lib-modules"
	)

	hostPathDirectory := v1.HostPathDirectory

	psv, psvm, err := p.psh.VolumesAndVolumeMounts(ctx, item)
	if err != nil {
		return nil, fmt.Errorf("could not list pull secrets for worker Pod: %v", err)
	}

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
	}

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
			Containers: []v1.Container{
				{
					Name:         workerContainerName,
					Image:        p.workerImage,
					VolumeMounts: append(volumeMounts, psvm...),
					Resources: v1.ResourceRequirements{
						Requests: requests,
						Limits:   limits,
					},
				},
			},
			NodeName:           nodeName,
			RestartPolicy:      v1.RestartPolicyNever,
			ServiceAccountName: item.ServiceAccountName,
			Volumes:            append(volumes, psv...),
		},
	}

	if err = ctrl.SetControllerReference(owner, &pod, p.scheme); err != nil {
		return nil, fmt.Errorf("could not set the owner as controller: %v", err)
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

func setFirmwareVolume(pod *v1.Pod, hostFirmwarePath *string) error {
	const volNameVarLibFirmware = "var-lib-firmware"
	container, _ := podcmd.FindContainerByName(pod, workerContainerName)
	if container == nil {
		return errors.New("could not find the worker container")
	}

	firmwareVolumeMount := v1.VolumeMount{
		Name:      volNameVarLibFirmware,
		MountPath: worker.FirmwareMountPath,
	}

	hostMountPath := "/var/lib/firmware"
	if hostFirmwarePath != nil {
		hostMountPath = *hostFirmwarePath
	}

	hostPathDirectoryOrCreate := v1.HostPathDirectoryOrCreate
	firmwareVolume := v1.Volume{
		Name: volNameVarLibFirmware,
		VolumeSource: v1.VolumeSource{
			HostPath: &v1.HostPathVolumeSource{
				Path: hostMountPath,
				Type: &hostPathDirectoryOrCreate,
			},
		},
	}

	pod.Spec.Volumes = append(pod.Spec.Volumes, firmwareVolume)
	container.VolumeMounts = append(container.VolumeMounts, firmwareVolumeMount)
	return nil
}

func getImageRepoSecretName(pod *v1.Pod) (string, error) {
	for _, v := range pod.Spec.Volumes {
		if v.Name == volNameImageRepoSecret {
			svs := v.VolumeSource.Secret

			if svs == nil {
				return "", fmt.Errorf("volume %s is not of type secret", volNameImageRepoSecret)
			}

			return svs.SecretName, nil
		}
	}

	return "", nil
}

func getModulesOrderAnnotationValue(modulesNames []string) string {
	var softDepData strings.Builder
	for i := 0; i < len(modulesNames)-1; i++ {
		fmt.Fprintf(&softDepData, "softdep %s pre: %s\n", modulesNames[i], modulesNames[i+1])
	}
	return softDepData.String()
}

//go:generate mockgen -source=nmc_reconciler.go -package=controllers -destination=mock_nmc_reconciler.go pullSecretHelper

type pullSecretHelper interface {
	VolumesAndVolumeMounts(ctx context.Context, nms *kmmv1beta1.ModuleItem) ([]v1.Volume, []v1.VolumeMount, error)
}

type pullSecretHelperImpl struct {
	client client.Client
}

func (p *pullSecretHelperImpl) VolumesAndVolumeMounts(ctx context.Context, item *kmmv1beta1.ModuleItem) ([]v1.Volume, []v1.VolumeMount, error) {
	logger := ctrl.LoggerFrom(ctx)

	secretNames := sets.New[string]()

	type pullSecret struct {
		secretName string
		volumeName string
		optional   bool
	}

	pullSecrets := make([]pullSecret, 0)

	if irs := item.ImageRepoSecret; irs != nil {
		secretNames.Insert(irs.Name)

		ps := pullSecret{
			secretName: irs.Name,
			volumeName: volNameImageRepoSecret,
		}

		pullSecrets = append(pullSecrets, ps)
	}

	if san := item.ServiceAccountName; san != "" {
		sa := v1.ServiceAccount{}
		nsn := types.NamespacedName{Namespace: item.Namespace, Name: san}

		logger.V(1).Info("Getting service account", "name", nsn)

		if err := p.client.Get(ctx, nsn, &sa); err != nil {
			return nil, nil, fmt.Errorf("could not get ServiceAccount %s: %v", nsn, err)
		}

		for _, s := range sa.ImagePullSecrets {
			if secretNames.Has(s.Name) {
				continue
			}

			secretNames.Insert(s.Name)

			ps := pullSecret{
				secretName: s.Name,
				volumeName: "pull-secret-" + s.Name,
				optional:   true, // to match the node's container runtime behaviour
			}

			pullSecrets = append(pullSecrets, ps)
		}
	}

	volumes := make([]v1.Volume, 0, len(pullSecrets))
	volumeMounts := make([]v1.VolumeMount, 0, len(pullSecrets))

	for _, s := range pullSecrets {
		v := v1.Volume{
			Name: s.volumeName,
			VolumeSource: v1.VolumeSource{
				Secret: &v1.SecretVolumeSource{
					SecretName: s.secretName,
					Optional:   pointer.Bool(s.optional),
				},
			},
		}

		volumes = append(volumes, v)

		vm := v1.VolumeMount{
			Name:      s.volumeName,
			ReadOnly:  true,
			MountPath: filepath.Join(worker.PullSecretsDir, s.secretName),
		}

		volumeMounts = append(volumeMounts, vm)
	}

	return volumes, volumeMounts, nil
}
