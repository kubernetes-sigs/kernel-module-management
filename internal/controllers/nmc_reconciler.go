package controllers

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	"github.com/kubernetes-sigs/kernel-module-management/internal/filter"
	"github.com/kubernetes-sigs/kernel-module-management/internal/nmc"
	"github.com/kubernetes-sigs/kernel-module-management/internal/worker"
	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
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
	nodeModulesConfigFinalizer = "kmm.node.kubernetes.io/nodemodulesconfig-reconciler"
	volumeNameConfig           = "config"
	workerContainerName        = "worker"
)

//+kubebuilder:rbac:groups=kmm.sigs.x-k8s.io,resources=nodemodulesconfigs,verbs=get;list;watch
//+kubebuilder:rbac:groups=kmm.sigs.x-k8s.io,resources=nodemodulesconfigs/status,verbs=patch
//+kubebuilder:rbac:groups="core",resources=pods,verbs=create;delete;get;list;watch
//+kubebuilder:rbac:groups="core",resources=nodes,verbs=get;list;watch
//+kubebuilder:rbac:groups="core",resources=secrets,verbs=get;list;watch

type NMCReconciler struct {
	client client.Client
	helper workerHelper
}

func NewNMCReconciler(client client.Client, scheme *runtime.Scheme, workerImage string) *NMCReconciler {
	return &NMCReconciler{
		client: client,
		helper: &workerHelperImpl{
			client: client,
			pm: &podManagerImpl{
				client:      client,
				scheme:      scheme,
				workerImage: workerImage,
			},
		},
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
		}

		delete(statusMap, moduleNameKey)
	}

	// We have processed all module specs.
	// Now, go through the remaining, "orphan" statuses that do not have a corresponding spec; those must be unloaded.

	for statusNameKey, status := range statusMap {
		logger := logger.WithValues("status", statusNameKey)

		if err := r.helper.ProcessOrphanModuleStatus(ctrl.LoggerInto(ctx, logger), &nmcObj, status); err != nil {
			errs = append(
				errs,
				fmt.Errorf("erorr processing orphan status for Module %s: %v", statusNameKey, err),
			)
		}
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

type workerHelper interface {
	ProcessModuleSpec(ctx context.Context, nmc *kmmv1beta1.NodeModulesConfig, spec *kmmv1beta1.NodeModuleSpec, status *kmmv1beta1.NodeModuleStatus) error
	ProcessOrphanModuleStatus(ctx context.Context, nmc *kmmv1beta1.NodeModulesConfig, status *kmmv1beta1.NodeModuleStatus) error
	SyncStatus(ctx context.Context, nmc *kmmv1beta1.NodeModulesConfig) error
	RemoveOrphanFinalizers(ctx context.Context, nodeName string) error
}

type workerHelperImpl struct {
	client client.Client
	pm     podManager
}

func NewWorkerHelper(client client.Client, pm podManager) workerHelper {
	return &workerHelperImpl{
		client: client,
		pm:     pm,
	}
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
func (w *workerHelperImpl) ProcessModuleSpec(
	ctx context.Context,
	nmc *kmmv1beta1.NodeModulesConfig,
	spec *kmmv1beta1.NodeModuleSpec,
	status *kmmv1beta1.NodeModuleStatus,
) error {
	logger := ctrl.LoggerFrom(ctx)

	if status == nil {
		logger.Info("Missing status; creating loader Pod")
		return w.pm.CreateLoaderPod(ctx, nmc, spec)
	}

	if status.InProgress {
		logger.Info("Worker pod is running; skipping")
		return nil
	}

	if status.Config == nil {
		logger.Info("Missing status config and pod is not running: previously failed pod, creating loader Pod")
		return w.pm.CreateLoaderPod(ctx, nmc, spec)
	}

	if !reflect.DeepEqual(spec.Config, *status.Config) {
		logger.Info("Outdated config in status; creating unloader Pod")

		return w.pm.CreateUnloaderPod(ctx, nmc, status)
	}

	node := v1.Node{}

	if err := w.client.Get(ctx, types.NamespacedName{Name: nmc.Name}, &node); err != nil {
		return fmt.Errorf("could not get node %s: %v", nmc.Name, err)
	}

	readyCondition := FindNodeCondition(node.Status.Conditions, v1.NodeReady)
	if readyCondition == nil {
		return fmt.Errorf("node %s has no Ready condition", nmc.Name)
	}

	if readyCondition.Status == v1.ConditionTrue && status.LastTransitionTime.Before(&readyCondition.LastTransitionTime) {
		logger.Info("Outdated last transition time status; creating unloader Pod")

		return w.pm.CreateLoaderPod(ctx, nmc, spec)
	}

	logger.Info("Spec and status in sync; nothing to do")

	return nil
}

func (w *workerHelperImpl) ProcessOrphanModuleStatus(
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
		return w.client.Status().Patch(ctx, nmcObj, patchFrom)
	}

	logger.Info("Creating unloader Pod")

	return w.pm.CreateUnloaderPod(ctx, nmcObj, status)
}

func (w *workerHelperImpl) RemoveOrphanFinalizers(ctx context.Context, nodeName string) error {
	pods, err := w.pm.ListWorkerPodsOnNode(ctx, nodeName)
	if err != nil {
		return fmt.Errorf("could not delete orphan worker Pods on node %s: %v", nodeName, err)
	}

	errs := make([]error, 0, len(pods))

	for i := 0; i < len(pods); i++ {
		pod := &pods[i]

		mergeFrom := client.MergeFrom(pod.DeepCopy())

		if controllerutil.RemoveFinalizer(pod, nodeModulesConfigFinalizer) {
			if err = w.client.Patch(ctx, pod, mergeFrom); err != nil {
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

func (w *workerHelperImpl) SyncStatus(ctx context.Context, nmcObj *kmmv1beta1.NodeModulesConfig) error {
	logger := ctrl.LoggerFrom(ctx)

	logger.Info("Syncing status")

	pods, err := w.pm.ListWorkerPodsOnNode(ctx, nmcObj.Name)
	if err != nil {
		return fmt.Errorf("could not list worker pods for NodeModulesConfig %s: %v", nmcObj.Name, err)
	}

	logger.V(1).Info("List worker Pods", "count", len(pods))

	if len(pods) == 0 {
		return nil
	}

	patchFrom := client.MergeFrom(nmcObj.DeepCopy())
	errs := make([]error, 0, len(pods))

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
					ImageRepoSecret:    nil,
					Name:               modName,
					Namespace:          modNamespace,
					ServiceAccountName: "",
				},
			}
		}

		deletePod := false
		updateModuleStatus := false

		switch phase {
		case v1.PodFailed:
			status.InProgress = false
			deletePod = true
			updateModuleStatus = true
		case v1.PodSucceeded:
			deletePod = true

			if p.Labels[actionLabelKey] == WorkerActionUnload {
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

		if deletePod {
			if err = w.pm.DeletePod(ctx, &p); err != nil {
				errs = append(
					errs,
					fmt.Errorf("could not delete worker Pod %s: %v", podNSN, errs),
				)

				continue
			}
		}

		if updateModuleStatus {
			nmc.SetModuleStatus(&nmcObj.Status.Modules, *status)
		}
	}

	if err = errors.Join(errs...); err != nil {
		return fmt.Errorf("encountered errors while reconciling NMC %s status: %v", nmcObj.Name, err)
	}

	if err = w.client.Status().Patch(ctx, nmcObj, patchFrom); err != nil {
		return fmt.Errorf("could not patch NodeModulesConfig %s status: %v", nmcObj.Name, err)
	}

	return nil
}

const (
	configFileName = "config.yaml"
	configFullPath = volMountPoingConfig + "/" + configFileName

	volNameConfig       = "config"
	volMountPoingConfig = "/etc/kmm-worker"
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
	workerImage string
}

func NewPodManager(client client.Client, workerImage string, scheme *runtime.Scheme) podManager {
	return &podManagerImpl{
		client:      client,
		psh:         &pullSecretHelperImpl{client: client},
		scheme:      scheme,
		workerImage: workerImage,
	}
}

func (p *podManagerImpl) CreateLoaderPod(ctx context.Context, nmc client.Object, nms *kmmv1beta1.NodeModuleSpec) error {
	pod, err := p.baseWorkerPod(ctx, nmc.GetName(), &nms.ModuleItem, nmc)
	if err != nil {
		return fmt.Errorf("could not create the base Pod: %v", err)
	}

	if err = setWorkerContainerArgs(pod, []string{"kmod", "load", configFullPath}); err != nil {
		return fmt.Errorf("could not set worker container args: %v", err)
	}

	if err = setWorkerConfigAnnotation(pod, nms.Config); err != nil {
		return fmt.Errorf("could not set worker config: %v", err)
	}

	setWorkerActionLabel(pod, WorkerActionLoad)
	pod.Spec.RestartPolicy = v1.RestartPolicyNever

	return p.client.Create(ctx, pod)
}

func (p *podManagerImpl) CreateUnloaderPod(ctx context.Context, nmc client.Object, nms *kmmv1beta1.NodeModuleStatus) error {
	pod, err := p.baseWorkerPod(ctx, nmc.GetName(), &nms.ModuleItem, nmc)
	if err != nil {
		return fmt.Errorf("could not create the base Pod: %v", err)
	}

	if err = setWorkerContainerArgs(pod, []string{"kmod", "unload", configFullPath}); err != nil {
		return fmt.Errorf("could not set worker container args: %v", err)
	}

	if err = setWorkerConfigAnnotation(pod, *nms.Config); err != nil {
		return fmt.Errorf("could not set worker config: %v", err)
	}

	setWorkerActionLabel(pod, WorkerActionUnload)

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

func (p *podManagerImpl) baseWorkerPod(
	ctx context.Context,
	nodeName string,
	item *kmmv1beta1.ModuleItem,
	owner client.Object,
) (*v1.Pod, error) {
	const (
		volNameLibModules     = "lib-modules"
		volNameUsrLibModules  = "usr-lib-modules"
		volNameVarLibFirmware = "var-lib-firmware"
	)

	hostPathDirectory := v1.HostPathDirectory
	hostPathDirectoryOrCreate := v1.HostPathDirectoryOrCreate

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
		{
			Name: volNameVarLibFirmware,
			VolumeSource: v1.VolumeSource{
				HostPath: &v1.HostPathVolumeSource{
					Path: "/var/lib/firmware",
					Type: &hostPathDirectoryOrCreate,
				},
			},
		},
	}

	volumeMounts := []v1.VolumeMount{
		{
			Name:      volNameConfig,
			MountPath: volMountPoingConfig,
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
			Name:      volNameVarLibFirmware,
			MountPath: "/var/lib/firmware",
		},
	}

	pod := v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: item.Namespace,
			Name:      workerPodName(nodeName, item.Name),
			Labels:    map[string]string{constants.ModuleNameLabel: item.Name},
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name:         workerContainerName,
					Image:        p.workerImage,
					VolumeMounts: append(volumeMounts, psvm...),
					SecurityContext: &v1.SecurityContext{
						Capabilities: &v1.Capabilities{
							Add: []v1.Capability{"SYS_MODULE"},
						},
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

func setWorkerActionLabel(pod *v1.Pod, action WorkerAction) {
	labels := pod.GetLabels()

	if labels == nil {
		labels = make(map[string]string)
	}

	labels[actionLabelKey] = string(action)

	pod.SetLabels(labels)
}

func setWorkerConfigAnnotation(pod *v1.Pod, cfg kmmv1beta1.ModuleConfig) error {
	b, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("could not marshal the ModuleConfig to YAML: %v", err)
	}

	annotations := pod.GetAnnotations()

	if annotations == nil {
		annotations = make(map[string]string)
	}

	annotations[configAnnotationKey] = string(b)

	pod.SetAnnotations(annotations)

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

//go:generate mockgen -source=nmc_reconciler.go -package=controllers -destination=mock_nmc_reconciler.go pullSecretHelper

type pullSecretHelper interface {
	VolumesAndVolumeMounts(ctx context.Context, nms *kmmv1beta1.ModuleItem) ([]v1.Volume, []v1.VolumeMount, error)
}

type pullSecretHelperImpl struct {
	client client.Client
}

func (p *pullSecretHelperImpl) VolumesAndVolumeMounts(ctx context.Context, item *kmmv1beta1.ModuleItem) ([]v1.Volume, []v1.VolumeMount, error) {
	logger := ctrl.LoggerFrom(ctx)

	var pullSecretNames []string

	if irs := item.ImageRepoSecret; irs != nil {
		pullSecretNames = append(pullSecretNames, irs.Name)
	}

	if san := item.ServiceAccountName; san != "" {
		sa := v1.ServiceAccount{}
		nsn := types.NamespacedName{Namespace: item.Namespace, Name: san}

		logger.V(1).Info("Getting service account", "name", nsn)

		if err := p.client.Get(ctx, nsn, &sa); err != nil {
			return nil, nil, fmt.Errorf("could not get ServiceAccount %s: %v", nsn, err)
		}

		for _, s := range sa.ImagePullSecrets {
			pullSecretNames = append(pullSecretNames, s.Name)
		}
	}

	volumes := make([]v1.Volume, 0, len(pullSecretNames))
	volumeMounts := make([]v1.VolumeMount, 0, len(pullSecretNames))

	for _, s := range pullSecretNames {
		volumeName := "pull-secret-" + s

		v := v1.Volume{
			Name: volumeName,
			VolumeSource: v1.VolumeSource{
				Secret: &v1.SecretVolumeSource{
					SecretName: s,
					Optional:   pointer.Bool(true),
				},
			},
		}

		volumes = append(volumes, v)

		vm := v1.VolumeMount{
			Name:      volumeName,
			ReadOnly:  true,
			MountPath: filepath.Join(worker.PullSecretsDir, s),
		}

		volumeMounts = append(volumeMounts, vm)
	}

	return volumes, volumeMounts, nil
}
