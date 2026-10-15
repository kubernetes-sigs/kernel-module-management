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
	"errors"
	"fmt"
	"slices"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	"github.com/kubernetes-sigs/kernel-module-management/internal/filter"
	"github.com/kubernetes-sigs/kernel-module-management/internal/module"
	"github.com/kubernetes-sigs/kernel-module-management/internal/node"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	resourcev1 "k8s.io/api/resource/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	DRAReconcilerName = "DRAReconciler"

	kubeletPluginsVolumeName         = "kubelet-plugins"
	kubeletPluginsPath               = "/var/lib/kubelet/plugins/"
	kubeletPluginsRegistryVolumeName = "kubelet-plugins-registry"
	kubeletPluginsRegistryPath       = "/var/lib/kubelet/plugins_registry/"
	cdiVolumeName                    = "cdi"
	cdiPath                          = "/var/run/cdi"

	draHealthcheckPort = 51515
)

type DRAReconciler struct {
	client         client.Client
	filter         *filter.Filter
	reconHelperAPI draReconcilerHelperAPI
}

func NewDRAReconciler(
	client client.Client,
	apiReader client.Reader,
	filter *filter.Filter,
	nodeAPI node.Node,
	scheme *runtime.Scheme,
) *DRAReconciler {
	reconHelperAPI := newDRAReconcilerHelper(client, apiReader, nodeAPI, scheme)
	return &DRAReconciler{
		client:         client,
		filter:         filter,
		reconHelperAPI: reconHelperAPI,
	}
}

func (r *DRAReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kmmv1beta1.Module{}).
		Owns(&appsv1.DaemonSet{}).
		Watches(
			&resourcev1.DeviceClass{},
			handler.EnqueueRequestsFromMapFunc(filter.DeviceClassToModuleReconcileRequest),
			builder.WithPredicates(filter.HasLabel(constants.ModuleNameLabel)),
		).
		// The target label is decided from the node's labels and taints; no other watch sees those.
		Watches(
			&v1.Node{},
			handler.EnqueueRequestsFromMapFunc(r.filter.FindModulesForNode),
			builder.WithPredicates(filter.ModuleReconcilerNodePredicate()),
		).
		Named(DRAReconcilerName).
		Complete(
			reconcile.AsReconciler[*kmmv1beta1.Module](r.client, r),
		)
}

func (r *DRAReconciler) Reconcile(ctx context.Context, mod *kmmv1beta1.Module) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if mod.GetDeletionTimestamp() != nil {
		if err := r.reconHelperAPI.finalizeModule(ctx, mod); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to finalize Module %s/%s: %v", mod.Namespace, mod.Name, err)
		}
		return ctrl.Result{}, nil
	}

	// A Module with no DRA spec should hold none of our resources or finalizer; tear it down the
	// same way as on deletion, then reset the status since this Module lives on.
	if mod.Spec.DRA == nil {
		if err := r.reconHelperAPI.finalizeModule(ctx, mod); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to finalize Module %s/%s: %v", mod.Namespace, mod.Name, err)
		}
		return ctrl.Result{}, r.reconHelperAPI.clearDRAStatus(ctx, mod)
	}

	// Register the finalizer before writing anything it protects, so a delete starting mid-pass
	// still runs cleanup.
	if err := r.reconHelperAPI.setFinalizer(ctx, mod); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to set finalizer on Module %s/%s: %v", mod.Namespace, mod.Name, err)
	}

	existingDRADS, err := r.reconHelperAPI.getModuleDRADaemonSets(ctx, mod.Name, mod.Namespace)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("could not get DRA DaemonSets for module %s, namespace %s: %v", mod.Name, mod.Namespace, err)
	}

	existingDCs, err := r.reconHelperAPI.getModuleDeviceClasses(ctx, mod.Name, mod.Namespace)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("could not get DeviceClasses for module %s, namespace %s: %v", mod.Name, mod.Namespace, err)
	}

	if err = r.reconHelperAPI.handleDRATargetLabels(ctx, mod); err != nil {
		return ctrl.Result{}, fmt.Errorf("could not reconcile dra-target labels: %v", err)
	}

	if err = r.reconHelperAPI.handleDRA(ctx, mod, existingDRADS); err != nil {
		return ctrl.Result{}, fmt.Errorf("could not handle DRA: %v", err)
	}

	if err = r.reconHelperAPI.garbageCollectDRADaemonSets(ctx, mod, existingDRADS); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to run DRA garbage collection: %v", err)
	}

	if err = r.reconHelperAPI.handleDeviceClasses(ctx, mod, existingDCs); err != nil {
		return ctrl.Result{}, fmt.Errorf("could not handle DeviceClasses: %v", err)
	}

	if err = r.reconHelperAPI.moduleUpdateDRAStatus(ctx, mod, existingDRADS); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update DRA status of the module: %v", err)
	}

	logger.Info("DRA reconcile loop finished successfully")

	return ctrl.Result{}, nil
}

//go:generate mockgen -source=dra_reconciler.go -package=controllers -destination=mock_dra_reconciler.go draReconcilerHelperAPI,draDaemonSetCreator

type draReconcilerHelperAPI interface {
	setFinalizer(ctx context.Context, mod *kmmv1beta1.Module) error
	finalizeModule(ctx context.Context, mod *kmmv1beta1.Module) error
	getModuleDRADaemonSets(ctx context.Context, name, namespace string) ([]appsv1.DaemonSet, error)
	handleDRA(ctx context.Context, mod *kmmv1beta1.Module, existingDRADS []appsv1.DaemonSet) error
	garbageCollectDRADaemonSets(ctx context.Context, mod *kmmv1beta1.Module, existingDS []appsv1.DaemonSet) error
	moduleUpdateDRAStatus(ctx context.Context, mod *kmmv1beta1.Module, existingDRADS []appsv1.DaemonSet) error
	clearDRAStatus(ctx context.Context, mod *kmmv1beta1.Module) error
	getModuleDeviceClasses(ctx context.Context, name, namespace string) ([]resourcev1.DeviceClass, error)
	handleDeviceClasses(ctx context.Context, mod *kmmv1beta1.Module, existingDCs []resourcev1.DeviceClass) error
	handleDRATargetLabels(ctx context.Context, mod *kmmv1beta1.Module) error
}

type draReconcilerHelper struct {
	client          client.Client
	apiReader       client.Reader
	daemonSetHelper draDaemonSetCreator
	nodeAPI         node.Node
}

func newDRAReconcilerHelper(client client.Client,
	apiReader client.Reader,
	nodeAPI node.Node,
	scheme *runtime.Scheme,
) draReconcilerHelperAPI {
	daemonSetHelper := newDRADaemonSetCreator(scheme)
	return &draReconcilerHelper{
		client:          client,
		apiReader:       apiReader,
		daemonSetHelper: daemonSetHelper,
		nodeAPI:         nodeAPI,
	}
}

// setFinalizer registers the DRA finalizer so the Module is held while its DRA resources are torn
// down. DRA shares the Module with other controllers' finalizers, so the patch takes an optimistic
// lock to avoid clobbering a concurrent write.
func (drh *draReconcilerHelper) setFinalizer(ctx context.Context, mod *kmmv1beta1.Module) error {
	if controllerutil.ContainsFinalizer(mod, constants.DRAFinalizer) {
		return nil
	}

	modCopy := mod.DeepCopy()
	controllerutil.AddFinalizer(mod, constants.DRAFinalizer)
	return drh.client.Patch(ctx, mod, client.MergeFromWithOptions(modCopy, client.MergeFromWithOptimisticLock{}))
}

// finalizeModule removes the dra-target labels, tears down the Module's DRA DaemonSets and
// DeviceClasses, and releases the finalizer once they are gone. It returns at once when the DRA
// finalizer is absent: the finalizer is set before anything it protects is written, so its absence
// means nothing was written and there is nothing to list or tear down. The DaemonSet controller
// handles the driver Pods, so it waits for the DaemonSets and DeviceClasses to go, re-triggered by
// their delete events, rather than polling.
func (drh *draReconcilerHelper) finalizeModule(ctx context.Context, mod *kmmv1beta1.Module) error {
	if !controllerutil.ContainsFinalizer(mod, constants.DRAFinalizer) {
		return nil
	}

	if err := drh.removeDRATargetLabels(ctx, mod); err != nil {
		return fmt.Errorf("could not remove dra-target labels: %v", err)
	}

	remaining, err := drh.deleteDRAResources(ctx, mod)
	if err != nil {
		return fmt.Errorf("could not delete DRA resources: %v", err)
	}

	if remaining > 0 {
		log.FromContext(ctx).Info("DRA resources still present; not removing the finalizer", "count", remaining)
		return nil
	}

	modCopy := mod.DeepCopy()
	controllerutil.RemoveFinalizer(mod, constants.DRAFinalizer)
	return drh.client.Patch(ctx, mod, client.MergeFromWithOptions(modCopy, client.MergeFromWithOptimisticLock{}))
}

// handleDRATargetLabels ensures the dra-target label is present on schedulable nodes targeted by
// the Module, and removed from unschedulable nodes. This enables the DRA driver DaemonSet to be
// evicted from draining nodes. Nodes outside the Module's selector are not reconciled here.
// IsNodeSchedulable already includes InternalTolerations, so disk/memory/pid pressure
// does not drop the label — matching the DRA pod's own tolerations.
func (drh *draReconcilerHelper) handleDRATargetLabels(ctx context.Context, mod *kmmv1beta1.Module) error {
	if mod.Spec.DRA == nil {
		return nil
	}

	targetLabel := utils.GetDRATargetNodeLabel(mod.Namespace, mod.Name)

	nodes, err := drh.nodeAPI.GetAllNodesBySelector(ctx, mod.Spec.Selector)
	if err != nil {
		return fmt.Errorf("could not list nodes targeted by module: %v", err)
	}

	var errs []error
	for i := range nodes {
		node := &nodes[i]

		var err error
		if drh.nodeAPI.IsNodeSchedulable(node, mod.Spec.Tolerations) {
			err = drh.nodeAPI.UpdateLabels(ctx, node, map[string]string{targetLabel: ""}, nil)
		} else {
			err = drh.nodeAPI.UpdateLabels(ctx, node, nil, map[string]string{targetLabel: ""})
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("could not reconcile dra-target label on node %s: %v", node.Name, err))
		}
	}

	return errors.Join(errs...)
}

// removeDRATargetLabels removes the dra-target label from every node that carries it, used on
// cleanup. It lists by the label key, not an empty value, and through the API reader, not the
// cache: a value match would skip a node whose label drifted off the empty value, and a stale cache
// reads as no node carrying the label. Either leaves the key set and lets finalizeModule drop the
// finalizer while a node is still labelled.
func (drh *draReconcilerHelper) removeDRATargetLabels(ctx context.Context, mod *kmmv1beta1.Module) error {
	targetLabel := utils.GetDRATargetNodeLabel(mod.Namespace, mod.Name)

	nodeList := v1.NodeList{}
	if err := drh.apiReader.List(ctx, &nodeList, client.HasLabels{targetLabel}); err != nil {
		return fmt.Errorf("could not list nodes with dra-target label: %v", err)
	}

	var errs []error
	for i := range nodeList.Items {
		node := &nodeList.Items[i]
		if err := drh.nodeAPI.UpdateLabels(ctx, node, nil, map[string]string{targetLabel: ""}); err != nil {
			errs = append(errs, fmt.Errorf("could not remove dra-target label from node %s: %v", node.Name, err))
		}
	}

	return errors.Join(errs...)
}

// listModuleDRADaemonSets takes the reader, so that the same selector serves the cached pass and
// the uncached check handleDRA makes before it generates a name.
func listModuleDRADaemonSets(ctx context.Context, reader client.Reader, name, namespace string) ([]appsv1.DaemonSet, error) {
	dsList := appsv1.DaemonSetList{}
	opts := []client.ListOption{
		client.MatchingLabels(map[string]string{
			constants.ModuleNameLabel: name,
			constants.DaemonSetRole:   constants.DRARoleLabelValue,
		}),
		client.InNamespace(namespace),
	}
	if err := reader.List(ctx, &dsList, opts...); err != nil {
		return nil, fmt.Errorf("could not list DaemonSets: %v", err)
	}

	return dsList.Items, nil
}

func (drh *draReconcilerHelper) getModuleDRADaemonSets(ctx context.Context, name, namespace string) ([]appsv1.DaemonSet, error) {
	return listModuleDRADaemonSets(ctx, drh.client, name, namespace)
}

func (drh *draReconcilerHelper) handleDRA(ctx context.Context, mod *kmmv1beta1.Module, existingDRADS []appsv1.DaemonSet) error {
	if mod.Spec.DRA == nil {
		return nil
	}

	logger := log.FromContext(ctx)

	ds, version := getExistingDRADSFromVersion(existingDRADS, mod.Namespace, mod.Name, mod.Spec.ModuleLoader)

	// The finalizer registered above is a Module update, so the next pass can start before the
	// DaemonSet informer has seen what this one created. Ask before generating another name.
	if ds == nil {
		fresh, err := listModuleDRADaemonSets(ctx, drh.apiReader, mod.Name, mod.Namespace)
		if err != nil {
			return err
		}

		ds, version = getExistingDRADSFromVersion(fresh, mod.Namespace, mod.Name, mod.Spec.ModuleLoader)
	}

	if ds == nil {
		logger.Info("creating new DRA DaemonSet", "version", version)
		ds = &appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{Namespace: mod.Namespace, GenerateName: mod.Name + "-dra-"},
		}
	}

	targetLabel := utils.GetDRATargetNodeLabel(mod.Namespace, mod.Name)
	creating := ds.Name == ""

	opRes, err := controllerutil.CreateOrPatch(ctx, drh.client, ds, func() error {
		// Requiring the label on a live DaemonSet drops Pods from unlabelled nodes; keep its entry.
		held, wasSet := ds.Spec.Template.Spec.NodeSelector[targetLabel]

		if err := drh.daemonSetHelper.setDRAAsDesired(ctx, ds, mod); err != nil {
			return err
		}

		// Without a loader the node selector is the Module's own map, which must not be edited.
		if creating || mod.Spec.ModuleLoader == nil {
			return nil
		}

		if wasSet {
			ds.Spec.Template.Spec.NodeSelector[targetLabel] = held
		} else {
			delete(ds.Spec.Template.Spec.NodeSelector, targetLabel)
		}

		return nil
	})

	if err == nil {
		logger.Info("Reconciled DRA", "name", ds.Name, "result", opRes)
	}

	return err
}

func (drh *draReconcilerHelper) garbageCollectDRADaemonSets(ctx context.Context, mod *kmmv1beta1.Module, existingDS []appsv1.DaemonSet) error {
	if mod.Spec.ModuleLoader == nil {
		return nil
	}

	logger := log.FromContext(ctx)
	deleted := make([]string, 0)
	for _, ds := range existingDS {
		if isOlderVersionUnusedDRADaemonSet(&ds, mod.Namespace, mod.Spec.ModuleLoader.Container.Version) {
			deleted = append(deleted, ds.Name)
			if err := drh.client.Delete(ctx, &ds); err != nil {
				return fmt.Errorf("could not delete DRA DaemonSet %s: %v", ds.Name, err)
			}
		}
	}

	logger.Info("garbage-collected DRA DaemonSets", "names", deleted)
	return nil
}

func getExistingDRADSFromVersion(existingDS []appsv1.DaemonSet,
	moduleNamespace string,
	moduleName string,
	moduleLoader *kmmv1beta1.ModuleLoaderSpec) (*appsv1.DaemonSet, string) {
	version := ""
	if moduleLoader != nil {
		version = moduleLoader.Container.Version
	}

	versionLabel := utils.GetSchedulePodVersionLabelName(moduleNamespace, moduleName)
	for _, ds := range existingDS {
		dsModuleVersion := ds.GetLabels()[versionLabel]
		if dsModuleVersion == version {
			return &ds, version
		}
	}
	return nil, version
}

func isOlderVersionUnusedDRADaemonSet(ds *appsv1.DaemonSet, moduleNamespace, moduleVersion string) bool {
	moduleName := ds.Labels[constants.ModuleNameLabel]
	versionLabel := utils.GetSchedulePodVersionLabelName(moduleNamespace, moduleName)
	return ds.Labels[versionLabel] != moduleVersion && ds.Status.DesiredNumberScheduled == 0
}

// deleteDRAResources deletes the Module's DRA DaemonSets and DeviceClasses and returns how many are
// still present, reading through the API reader since a stale cache reads as an empty cluster. A
// resource that is still finalizing counts as present.
func (drh *draReconcilerHelper) deleteDRAResources(ctx context.Context, mod *kmmv1beta1.Module) (int, error) {
	var errs []error
	remaining := 0

	dsList := appsv1.DaemonSetList{}
	dsOpts := []client.ListOption{
		client.MatchingLabels{
			constants.ModuleNameLabel: mod.Name,
			constants.DaemonSetRole:   constants.DRARoleLabelValue,
		},
		client.InNamespace(mod.Namespace),
	}
	if err := drh.apiReader.List(ctx, &dsList, dsOpts...); err != nil {
		return 0, fmt.Errorf("could not list DRA DaemonSets for module %s/%s: %v", mod.Namespace, mod.Name, err)
	}

	daemonSets := make([]client.Object, 0, len(dsList.Items))
	for i := range dsList.Items {
		daemonSets = append(daemonSets, &dsList.Items[i])
	}

	n, err := deleteDRAObjects(ctx, drh.client, daemonSets)
	remaining += n
	errs = append(errs, err)

	// A DeviceClass is cluster scoped, so a namespaced Module cannot own it and the namespace is
	// carried as a label instead. InNamespace would match nothing here.
	dcList := resourcev1.DeviceClassList{}
	dcOpts := []client.ListOption{
		client.MatchingLabels{
			constants.ModuleNameLabel:      mod.Name,
			constants.ModuleNamespaceLabel: mod.Namespace,
		},
	}
	if err := drh.apiReader.List(ctx, &dcList, dcOpts...); err != nil {
		errs = append(errs, fmt.Errorf("could not list DeviceClasses for module %s/%s: %v", mod.Namespace, mod.Name, err))
		return 0, errors.Join(errs...)
	}

	deviceClasses := make([]client.Object, 0, len(dcList.Items))
	for i := range dcList.Items {
		deviceClasses = append(deviceClasses, &dcList.Items[i])
	}

	n, err = deleteDRAObjects(ctx, drh.client, deviceClasses)
	remaining += n
	errs = append(errs, err)

	return remaining, errors.Join(errs...)
}

// deleteDRAObjects deletes the objects that are not already going and returns how many it saw, since
// one that is still finalizing has not gone. Each delete pins the UID and resourceVersion it read.
func deleteDRAObjects(ctx context.Context, c client.Client, objs []client.Object) (int, error) {
	var errs []error

	for _, obj := range objs {
		// Asking again puts the GC finalizer back, and every one of those writes wakes us again.
		if obj.GetDeletionTimestamp() != nil {
			continue
		}

		opts := &client.DeleteOptions{
			Preconditions: &metav1.Preconditions{
				UID:             ptr.To(obj.GetUID()),
				ResourceVersion: ptr.To(obj.GetResourceVersion()),
			},
			PropagationPolicy: ptr.To(metav1.DeletePropagationForeground),
		}

		if err := c.Delete(ctx, obj, opts); err != nil && !apierrors.IsNotFound(err) {
			// Items of a typed list carry no Kind, and a DeviceClass has no namespace.
			errs = append(errs, fmt.Errorf("could not delete %s: %v", klog.KObj(obj), err))
		}
	}

	return len(objs), errors.Join(errs...)
}

func (drh *draReconcilerHelper) moduleUpdateDRAStatus(ctx context.Context,
	mod *kmmv1beta1.Module,
	existingDRADS []appsv1.DaemonSet) error {

	if mod.Spec.DRA == nil {
		return nil
	}

	numTargetedNodes, err := drh.nodeAPI.GetNumTargetedNodes(ctx, mod.Spec.Selector, mod.Spec.Tolerations)
	if err != nil {
		return fmt.Errorf("failed to determine the number of nodes targeted by Module %s/%s selector: %v", mod.Namespace, mod.Name, err)
	}

	numAvailable := int32(0)
	for _, ds := range existingDRADS {
		numAvailable += ds.Status.NumberAvailable
	}

	unmodifiedMod := mod.DeepCopy()

	mod.Status.DRA.NodesMatchingSelectorNumber = int32(numTargetedNodes)
	mod.Status.DRA.DesiredNumber = int32(numTargetedNodes)
	mod.Status.DRA.AvailableNumber = numAvailable

	return drh.client.Status().Patch(ctx, mod, client.MergeFrom(unmodifiedMod))
}

func (drh *draReconcilerHelper) clearDRAStatus(ctx context.Context, mod *kmmv1beta1.Module) error {
	emptyStatus := kmmv1beta1.DaemonSetStatus{}
	if mod.Status.DRA == emptyStatus {
		return nil
	}

	unmodifiedMod := mod.DeepCopy()

	mod.Status.DRA = kmmv1beta1.DaemonSetStatus{}

	return drh.client.Status().Patch(ctx, mod, client.MergeFrom(unmodifiedMod))
}

func (drh *draReconcilerHelper) getModuleDeviceClasses(ctx context.Context, name, namespace string) ([]resourcev1.DeviceClass, error) {
	dcList := resourcev1.DeviceClassList{}
	opts := []client.ListOption{
		client.MatchingLabels(map[string]string{
			constants.ModuleNameLabel:      name,
			constants.ModuleNamespaceLabel: namespace,
		}),
	}
	if err := drh.client.List(ctx, &dcList, opts...); err != nil {
		return nil, fmt.Errorf("could not list DeviceClasses: %v", err)
	}

	return dcList.Items, nil
}

// handleDeviceClasses reconciles cluster-scoped DeviceClass resources to match the desired state
// declared in mod.Spec.DRA.DeviceClasses. It performs declarative convergence:
//   - DeviceClasses present in the spec but missing from the cluster are created.
//   - DeviceClasses present in both are patched to reflect the current spec (drift correction).
//   - DeviceClasses present in the cluster but absent from the spec are deleted (stale cleanup).
func (drh *draReconcilerHelper) handleDeviceClasses(ctx context.Context, mod *kmmv1beta1.Module, existingDCs []resourcev1.DeviceClass) error {
	if mod.Spec.DRA == nil {
		return nil
	}

	logger := log.FromContext(ctx)

	existingByName := make(map[string]resourcev1.DeviceClass, len(existingDCs))
	for _, dc := range existingDCs {
		existingByName[dc.Name] = dc
	}

	var errs []error

	// Create missing or patch existing DeviceClasses to match the desired spec.
	for _, desired := range mod.Spec.DRA.DeviceClasses {
		dc := &resourcev1.DeviceClass{
			ObjectMeta: metav1.ObjectMeta{Name: desired.Name},
		}
		opRes, err := controllerutil.CreateOrPatch(ctx, drh.client, dc, func() error {
			if dc.Labels == nil {
				dc.Labels = make(map[string]string)
			}
			dc.Labels[constants.ModuleNameLabel] = mod.Name
			dc.Labels[constants.ModuleNamespaceLabel] = mod.Namespace
			dc.Spec.Selectors = desired.Selectors
			dc.Spec.Config = desired.Config
			return nil
		})
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to create or patch DeviceClass %s: %v", desired.Name, err))
		} else {
			logger.Info("Reconciled DeviceClass", "name", desired.Name, "result", opRes)
		}
		delete(existingByName, desired.Name)
	}

	// Delete DeviceClasses that exist in the cluster but are no longer in the desired spec.
	for name, dc := range existingByName {
		if deleteErr := drh.client.Delete(ctx, &dc); deleteErr != nil && !apierrors.IsNotFound(deleteErr) {
			errs = append(errs, fmt.Errorf("failed to delete DeviceClass %s: %v", name, deleteErr))
		} else {
			logger.Info("Deleted extra DeviceClass", "name", name)
		}
	}

	return errors.Join(errs...)
}

type draDaemonSetCreator interface {
	setDRAAsDesired(ctx context.Context, ds *appsv1.DaemonSet, mod *kmmv1beta1.Module) error
}

type draDaemonSetCreatorImpl struct {
	scheme *runtime.Scheme
}

func newDRADaemonSetCreator(scheme *runtime.Scheme) draDaemonSetCreator {
	return &draDaemonSetCreatorImpl{
		scheme: scheme,
	}
}

func (dsci *draDaemonSetCreatorImpl) setDRAAsDesired(
	ctx context.Context,
	ds *appsv1.DaemonSet,
	mod *kmmv1beta1.Module,
) error {
	if ds == nil {
		return errors.New("ds cannot be nil")
	}

	if mod.Spec.DRA == nil {
		return errors.New("DRA spec in module should not be nil")
	}

	hostPathDirOrCreate := v1.HostPathDirectoryOrCreate
	hostPathDir := v1.HostPathDirectory

	pluginsVolume := v1.Volume{
		Name: kubeletPluginsVolumeName,
		VolumeSource: v1.VolumeSource{
			HostPath: &v1.HostPathVolumeSource{
				Path: kubeletPluginsPath,
				Type: &hostPathDirOrCreate,
			},
		},
	}

	registryVolume := v1.Volume{
		Name: kubeletPluginsRegistryVolumeName,
		VolumeSource: v1.VolumeSource{
			HostPath: &v1.HostPathVolumeSource{
				Path: kubeletPluginsRegistryPath,
				Type: &hostPathDir,
			},
		},
	}

	cdiVolume := v1.Volume{
		Name: cdiVolumeName,
		VolumeSource: v1.VolumeSource{
			HostPath: &v1.HostPathVolumeSource{
				Path: cdiPath,
				Type: &hostPathDirOrCreate,
			},
		},
	}

	containerVolumeMounts := []v1.VolumeMount{
		{Name: kubeletPluginsVolumeName, MountPath: kubeletPluginsPath},
		{Name: kubeletPluginsRegistryVolumeName, MountPath: kubeletPluginsRegistryPath},
		{Name: cdiVolumeName, MountPath: cdiPath},
	}

	presetEnv := []v1.EnvVar{
		{
			Name: "NODE_NAME",
			ValueFrom: &v1.EnvVarSource{
				FieldRef: &v1.ObjectFieldSelector{FieldPath: "spec.nodeName"},
			},
		},
		{
			Name: "POD_UID",
			ValueFrom: &v1.EnvVarSource{
				FieldRef: &v1.ObjectFieldSelector{FieldPath: "metadata.uid"},
			},
		},
		{
			Name:  "CDI_ROOT",
			Value: cdiPath,
		},
		{
			Name:  "KUBELET_REGISTRAR_DIRECTORY_PATH",
			Value: kubeletPluginsRegistryPath,
		},
		{
			Name:  "KUBELET_PLUGINS_DIRECTORY_PATH",
			Value: kubeletPluginsPath,
		},
		{
			Name:  "HEALTHCHECK_PORT",
			Value: fmt.Sprintf("%d", draHealthcheckPort),
		},
	}

	draLivenessProbe := &v1.Probe{
		ProbeHandler: v1.ProbeHandler{
			GRPC: &v1.GRPCAction{
				Port:    draHealthcheckPort,
				Service: ptr.To("liveness"),
			},
		},
		InitialDelaySeconds: 30,
		PeriodSeconds:       10,
		TimeoutSeconds:      5,
		FailureThreshold:    3,
	}

	standardLabels := map[string]string{
		constants.ModuleNameLabel: mod.Name,
		constants.DaemonSetRole:   constants.DRARoleLabelValue,
	}

	nodeSelector := map[string]string{
		utils.GetKernelModuleReadyNodeLabel(mod.Namespace, mod.Name): "",
		utils.GetDRATargetNodeLabel(mod.Namespace, mod.Name):         "",
	}

	if mod.Spec.ModuleLoader != nil {
		if mod.Spec.ModuleLoader.Container.Version != "" {
			versionLabel := utils.GetSchedulePodVersionLabelName(mod.Namespace, mod.Name)
			standardLabels[versionLabel] = mod.Spec.ModuleLoader.Container.Version
			nodeSelector[versionLabel] = mod.Spec.ModuleLoader.Container.Version
		}
	} else {
		nodeSelector = mod.Spec.Selector
	}

	ds.SetLabels(
		overrideLabels(ds.GetLabels(), standardLabels),
	)

	effectiveLivenessProbe := draLivenessProbe
	if mod.Spec.DRA.Container.LivenessProbe != nil {
		effectiveLivenessProbe = mod.Spec.DRA.Container.LivenessProbe
	}

	ds.Spec = appsv1.DaemonSetSpec{
		Selector: &metav1.LabelSelector{MatchLabels: standardLabels},
		Template: v1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels:     standardLabels,
				Finalizers: []string{constants.NodeLabelerFinalizer},
			},
			Spec: v1.PodSpec{
				InitContainers:               generatePodContainerSpec(mod.Spec.DRA.InitContainer, "dra-init", nil, nil, nil, nil),
				Containers:                   generatePodContainerSpec(&mod.Spec.DRA.Container, "dra", containerVolumeMounts, presetEnv, effectiveLivenessProbe, mod.Spec.DRA.Container.StartupProbe),
				PriorityClassName:            "system-node-critical",
				HostNetwork:                  true,
				ImagePullSecrets:             getPodPullSecrets(mod.Spec.ImageRepoSecret),
				NodeSelector:                 nodeSelector,
				ServiceAccountName:           mod.Spec.DRA.ServiceAccountName,
				Volumes:                      append([]v1.Volume{pluginsVolume, registryVolume, cdiVolume}, mod.Spec.DRA.Volumes...),
				Tolerations:                  slices.Concat(mod.Spec.Tolerations, module.InternalTolerations),
				AutomountServiceAccountToken: mod.Spec.DRA.AutomountServiceAccountToken,
			},
		},
	}

	return controllerutil.SetControllerReference(mod, ds, dsci.scheme)
}
