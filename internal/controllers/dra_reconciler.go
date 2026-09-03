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
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

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
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/record"
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
	recorder record.EventRecorder,
	scheme *runtime.Scheme,
) *DRAReconciler {
	reconHelperAPI := newDRAReconcilerHelper(client, apiReader, nodeAPI, recorder, scheme)
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
		Watches(
			&v1.Node{},
			handler.EnqueueRequestsFromMapFunc(r.filter.FindDRAModulesForNode),
			builder.WithPredicates(filter.DRAReconcilerNodePredicate()),
		).
		Watches(
			&resourcev1.ResourceClaim{},
			handler.EnqueueRequestsFromMapFunc(r.filter.FindDRAModulesForResourceClaim),
			builder.WithPredicates(filter.ResourceClaimUsageChanged()),
		).
		Named(DRAReconcilerName).
		Complete(
			reconcile.AsReconciler[*kmmv1beta1.Module](r.client, r),
		)
}

func (r *DRAReconciler) Reconcile(ctx context.Context, mod *kmmv1beta1.Module) (ctrl.Result, error) {
	res := ctrl.Result{}

	logger := log.FromContext(ctx)

	existingDRADS, err := r.reconHelperAPI.getModuleDRADaemonSets(ctx, mod.Name, mod.Namespace)
	if err != nil {
		return res, fmt.Errorf("could not get DRA DaemonSets for module %s, namespace %s: %v", mod.Name, mod.Namespace, err)
	}

	existingDCs, err := r.reconHelperAPI.getModuleDeviceClasses(ctx, mod.Name, mod.Namespace)
	if err != nil {
		return res, fmt.Errorf("could not get DeviceClasses for module %s, namespace %s: %v", mod.Name, mod.Namespace, err)
	}

	// Everything past here writes, and a pass working from a spec the Module has moved on from can
	// undo the current one.
	current, err := r.reconHelperAPI.confirmCurrentModule(ctx, mod)
	if err != nil {
		return res, err
	}

	if !current {
		logger.Info("Module changed while this pass was reading it, leaving the rest to the next one")

		return ctrl.Result{RequeueAfter: draTargetRequeue}, nil
	}

	// Labels go after the resources selecting on them, so a failed patch strands no DaemonSet.
	if mod.GetDeletionTimestamp() != nil {
		if err = r.reconHelperAPI.deleteDRAResources(ctx, mod.Name, mod.Namespace); err != nil {
			return ctrl.Result{}, err
		}
		if err = r.reconHelperAPI.removeDRATargetLabels(ctx, mod); err != nil {
			return ctrl.Result{}, fmt.Errorf("could not remove dra-target labels on deletion: %v", err)
		}
		return ctrl.Result{}, nil
	}

	if mod.Spec.DRA == nil {
		if err = r.reconHelperAPI.deleteDRAResources(ctx, mod.Name, mod.Namespace); err != nil {
			return ctrl.Result{}, err
		}
		if err = r.reconHelperAPI.removeDRATargetLabels(ctx, mod); err != nil {
			return ctrl.Result{}, fmt.Errorf("could not remove dra-target labels: %v", err)
		}
		return ctrl.Result{}, r.reconHelperAPI.clearDRAStatus(ctx, mod)
	}

	// The label has to exist before the DaemonSet requires it, and there is only something to
	// sequence against while a kernel module is loaded, so without a loader it is dropped instead.
	targetResult := draTargetResult{}
	if mod.Spec.ModuleLoader != nil {
		if targetResult, err = r.reconHelperAPI.handleDRATargetLabels(ctx, mod, existingDRADS); err != nil {
			return res, fmt.Errorf("could not reconcile dra-target labels: %v", err)
		}
	}
	// The rest of this pass would write from a spec that is no longer the Module's, and deleting a
	// DaemonSet the current spec wants back is not something a later pass can undo.
	if targetResult.stale {
		logger.Info("Module changed while this pass was reading it, leaving the rest to the next one")

		return ctrl.Result{RequeueAfter: draTargetRequeue}, nil
	}

	// setDRAAsDesired applies the same selector, and garbage collection reads a replica count the
	// held migration has not settled yet, so both wait with it.
	if !targetResult.deferDaemonSets {
		if err = r.reconHelperAPI.handleDRA(ctx, mod, existingDRADS); err != nil {
			return res, fmt.Errorf("could not handle DRA: %v", err)
		}

		if err = r.reconHelperAPI.garbageCollectDRADaemonSets(ctx, mod, existingDRADS); err != nil {
			return res, fmt.Errorf("failed to run DRA garbage collection: %v", err)
		}
	}

	if mod.Spec.ModuleLoader == nil {
		if err = r.reconHelperAPI.removeDRATargetLabels(ctx, mod); err != nil {
			return res, fmt.Errorf("could not remove stale dra-target labels: %v", err)
		}
	}

	err = r.reconHelperAPI.handleDeviceClasses(ctx, mod, existingDCs)
	if err != nil {
		return res, fmt.Errorf("could not handle DeviceClasses: %v", err)
	}

	err = r.reconHelperAPI.moduleUpdateDRAStatus(ctx, mod, existingDRADS, targetResult.targeted)
	if err != nil {
		return res, fmt.Errorf("failed to update DRA status of the module: %v", err)
	}

	logger.Info("DRA reconcile loop finished successfully")

	// Only here: controller-runtime ignores a result handed to it next to an error.
	res.RequeueAfter = targetResult.requeueAfter

	return res, nil
}

//go:generate mockgen -source=dra_reconciler.go -package=controllers -destination=mock_dra_reconciler.go draReconcilerHelperAPI,draDaemonSetCreator

type draReconcilerHelperAPI interface {
	getModuleDRADaemonSets(ctx context.Context, name, namespace string) ([]appsv1.DaemonSet, error)
	handleDRA(ctx context.Context, mod *kmmv1beta1.Module, existingDRADS []appsv1.DaemonSet) error
	confirmCurrentModule(ctx context.Context, mod *kmmv1beta1.Module) (bool, error)
	handleDRATargetLabels(ctx context.Context, mod *kmmv1beta1.Module, existingDRADS []appsv1.DaemonSet) (draTargetResult, error)
	removeDRATargetLabels(ctx context.Context, mod *kmmv1beta1.Module) error
	garbageCollectDRADaemonSets(ctx context.Context, mod *kmmv1beta1.Module, existingDS []appsv1.DaemonSet) error
	deleteDRAResources(ctx context.Context, moduleName, moduleNamespace string) error
	moduleUpdateDRAStatus(ctx context.Context, mod *kmmv1beta1.Module, existingDRADS []appsv1.DaemonSet, targetedNodes int) error
	clearDRAStatus(ctx context.Context, mod *kmmv1beta1.Module) error
	getModuleDeviceClasses(ctx context.Context, name, namespace string) ([]resourcev1.DeviceClass, error)
	handleDeviceClasses(ctx context.Context, mod *kmmv1beta1.Module, existingDCs []resourcev1.DeviceClass) error
}

type draReconcilerHelper struct {
	client client.Client
	// apiReader bypasses the cache: dropping a label deletes the driver Pod, and nothing calls the
	// termination back, while adding one from a stale read is harmless.
	apiReader       client.Reader
	daemonSetHelper draDaemonSetCreator
	nodeAPI         node.Node
	recorder        record.EventRecorder
}

// event is a no-op when the helper was built without a recorder, which is how the unit tests build
// it. A drain that does not converge is otherwise only visible in the controller's own logs.
func (drh *draReconcilerHelper) event(mod *kmmv1beta1.Module, reason, format string, args ...any) {
	if drh.recorder == nil {
		return
	}

	drh.recorder.Eventf(mod, v1.EventTypeWarning, reason, format, args...)
}

func newDRAReconcilerHelper(client client.Client,
	apiReader client.Reader,
	nodeAPI node.Node,
	recorder record.EventRecorder,
	scheme *runtime.Scheme,
) draReconcilerHelperAPI {
	daemonSetHelper := newDRADaemonSetCreator(scheme)
	return &draReconcilerHelper{
		client:          client,
		apiReader:       apiReader,
		daemonSetHelper: daemonSetHelper,
		nodeAPI:         nodeAPI,
		recorder:        recorder,
	}
}

func (drh *draReconcilerHelper) getModuleDRADaemonSets(ctx context.Context, name, namespace string) ([]appsv1.DaemonSet, error) {
	dsList := appsv1.DaemonSetList{}
	opts := []client.ListOption{
		client.MatchingLabels(map[string]string{
			constants.ModuleNameLabel: name,
			constants.DaemonSetRole:   constants.DRARoleLabelValue,
		}),
		client.InNamespace(namespace),
	}
	if err := drh.client.List(ctx, &dsList, opts...); err != nil {
		return nil, fmt.Errorf("could not list DaemonSets: %v", err)
	}

	return dsList.Items, nil
}

func (drh *draReconcilerHelper) handleDRA(ctx context.Context, mod *kmmv1beta1.Module, existingDRADS []appsv1.DaemonSet) error {
	if mod.Spec.DRA == nil {
		return nil
	}

	logger := log.FromContext(ctx)

	ds, version := getExistingDRADSFromVersion(existingDRADS, mod.Namespace, mod.Name, mod.Spec.ModuleLoader)
	if ds == nil {
		logger.Info("creating new DRA DaemonSet", "version", version)
		ds = &appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{Namespace: mod.Namespace, GenerateName: mod.Name + "-dra-"},
		}
	}

	opRes, err := controllerutil.CreateOrPatch(ctx, drh.client, ds, func() error {
		return drh.daemonSetHelper.setDRAAsDesired(ctx, ds, mod)
	})

	if err == nil {
		logger.Info("Reconciled DRA", "name", ds.Name, "result", opRes)
	}

	return err
}

// handleDRATargetLabels brings the nodes carrying the dra-target label in line, then the DaemonSets
// selecting on it, in that order, since a DaemonSet must never require a label before its nodes
// have it. It returns how many driver Pods the reconciled label leaves the Module wanting.
func (drh *draReconcilerHelper) handleDRATargetLabels(
	ctx context.Context,
	mod *kmmv1beta1.Module,
	existingDRADS []appsv1.DaemonSet,
) (draTargetResult, error) {
	if mod.Spec.DRA == nil {
		return draTargetResult{}, nil
	}

	usage, err := drh.nodesUsingDRADriver(ctx, drh.client, mod)
	if err != nil {
		return draTargetResult{}, fmt.Errorf("could not determine which nodes still use the DRA driver: %v", err)
	}

	targetLabel := utils.GetDRATargetNodeLabel(mod.Namespace, mod.Name)

	result, err := drh.reconcileDRATargetLabel(ctx, mod, targetLabel, usage, drh.newDriverRecheck(ctx, mod))

	drift := targetSelectorDriftIn(existingDRADS, targetLabel)
	if err != nil || result.stale || (!drift.missing && !drift.wrong) {
		return result, err
	}

	if !drift.missing {
		retry, err := drh.ensureDRATargetNodeSelector(ctx, existingDRADS, targetLabel, true)
		if retry {
			result.requeueAfter = draTargetRequeue
		}

		return result, err
	}

	// Adding the selector to a DaemonSet that predates it rolls its Pods, and a retrying unloader
	// can take the kernel module in that gap. Read again rather than reusing what the label pass
	// saw: a reservation can appear between dropping a label and rolling a DaemonSet.
	fresh, err := drh.recheckDriverUsage(ctx, mod)
	if err != nil {
		return result, fmt.Errorf("could not confirm the DRA driver is unused before migrating: %v", err)
	}

	// Nothing else, not even the recovery: this pass no longer knows what the Module wants.
	if fresh.stale {
		result.stale = true
		result.requeueAfter = draTargetRequeue

		return result, nil
	}

	if fresh.unresolved || fresh.nodes.Len() > 0 {
		result.deferDaemonSets = true
		result.requeueAfter = draTargetRequeue

		log.FromContext(ctx).Info("Holding the DRA DaemonSet migration",
			"label", targetLabel, "nodes", fresh.nodes.Len(), "unresolved", fresh.unresolved)
		drh.event(mod, "DRAMigrationDeferred",
			"Holding the DRA DaemonSet selector migration: %d node(s) still hold a claim for %s, unresolved reservations: %t",
			fresh.nodes.Len(), mod.Spec.DRA.DriverName, fresh.unresolved)

		// A wrong value is still worth correcting: it is what took the driver away.
		_, err = drh.ensureDRATargetNodeSelector(ctx, existingDRADS, targetLabel, true)

		return result, err
	}

	retry, err := drh.ensureDRATargetNodeSelector(ctx, existingDRADS, targetLabel, false)
	if retry {
		result.requeueAfter = draTargetRequeue
	}

	return result, err
}

// jsonPointerEscape encodes a map key for a JSON Patch path. Label keys carry a slash.
func jsonPointerEscape(key string) string {
	return strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1")
}

// targetSelectorDrift separates the two ways a DaemonSet can fail to select on targetLabel, because
// they are not equally safe to correct. Adding the key narrows what the DaemonSet selects and rolls
// its Pods; a wrong value already selects no node, so correcting it is what brings the driver back.
type targetSelectorDrift struct {
	missing bool
	wrong   bool
}

func targetSelectorDriftIn(existingDRADS []appsv1.DaemonSet, targetLabel string) targetSelectorDrift {
	drift := targetSelectorDrift{}

	for i := range existingDRADS {
		ds := &existingDRADS[i]

		if ds.GetDeletionTimestamp() != nil {
			continue
		}

		switch value, ok := ds.Spec.Template.Spec.NodeSelector[targetLabel]; {
		case !ok:
			drift.missing = true
		case value != "":
			drift.wrong = true
		}
	}

	return drift
}

// reconcileDRATargetLabel makes targetLabel reflect the desired state over the nodes the Module
// selects and those already carrying it. A node keeps it while the Module selects it and can
// schedule on it; usage overrides both, since a Pod there still holds one of the driver's devices.
func (drh *draReconcilerHelper) reconcileDRATargetLabel(
	ctx context.Context,
	mod *kmmv1beta1.Module,
	targetLabel string,
	usage driverUsage,
	recheck driverRecheck,
) (draTargetResult, error) {
	result := draTargetResult{}

	selectedNodes, err := drh.nodeAPI.GetAllNodesBySelector(ctx, mod.Spec.Selector)
	if err != nil {
		return result, fmt.Errorf("could not list nodes targeted by module: %v", err)
	}

	// By key, not value: a node whose label value was corrupted still needs correcting.
	labeledNodes, err := drh.nodeAPI.GetAllNodesByLabelKey(ctx, targetLabel)
	if err != nil {
		return result, fmt.Errorf("could not list nodes with %s label: %v", targetLabel, err)
	}

	selectedNames := sets.New[string]()
	nodesByName := make(map[string]*v1.Node, len(selectedNodes)+len(labeledNodes))

	for i := range selectedNodes {
		n := &selectedNodes[i]
		selectedNames.Insert(n.Name)
		nodesByName[n.Name] = n
	}

	for i := range labeledNodes {
		n := &labeledNodes[i]
		if _, ok := nodesByName[n.Name]; !ok {
			nodesByName[n.Name] = n
		}
	}

	// A node that no longer matches the selector and has already lost the label appears in neither
	// list, so fetch the ones still in use by name to put their label back.
	for _, name := range sets.List(usage.nodes) {
		if _, ok := nodesByName[name]; ok {
			continue
		}

		// Uncached, and asking again later: this is the only path that can put the label back on a
		// node the selector has dropped, and a node event for it maps to no Module.
		n := &v1.Node{}
		if err := drh.apiReader.Get(ctx, types.NamespacedName{Name: name}, n); apierrors.IsNotFound(err) {
			result.requeueAfter = draTargetRequeue
			continue
		} else if err != nil {
			return result, fmt.Errorf("could not get node %s still using the DRA driver: %v", name, err)
		}

		nodesByName[name] = n
	}

	tolerations := module.EffectiveTolerations(mod.Spec.Tolerations)

	// Sorted so that the errors returned for a given cluster state are always the same.
	names := slices.Sorted(maps.Keys(nodesByName))

	var (
		errs        []error
		errReported bool
	)

	// Dropping a label deletes the driver Pod, so it is judged against an uncached read rather than
	// the informer, which can lag the reservation being decided against. Every removal in this pass
	// shares the one read; the migration takes its own.
	mustKeep := func(name string) bool {
		fresh, err := recheck()
		if err != nil {
			if !errReported {
				errReported = true
				errs = append(errs, fmt.Errorf("could not confirm the DRA driver is unused: %v", err))
			}

			return true
		}

		result.stale = result.stale || fresh.stale

		return fresh.stale || fresh.unresolved || fresh.nodes.Has(name)
	}

	if usage.unresolved {
		result.requeueAfter = draTargetRequeue
	}

	for _, name := range names {
		n := nodesByName[name]

		value, hasLabel := n.Labels[targetLabel]

		// Cordoning, an unrelated NoSchedule taint and a narrowed selector all leave running Pods
		// in place, so a node still in use overrides both conditions.
		byClaim := usage.nodes.Has(name)
		bySelector := selectedNames.Has(name) && drh.nodeAPI.IsNodeSchedulable(n, tolerations)
		wantLabel := byClaim || bySelector

		if !wantLabel && hasLabel && mustKeep(name) {
			wantLabel = true
			byClaim = true
		}

		// Nothing here watches what releases a claim on a node the selector would otherwise have
		// given up, so that node has to be looked at again unprompted.
		if byClaim && !bySelector {
			result.requeueAfter = draTargetRequeue
		}

		counted := wantLabel

		// The DaemonSet selects on targetLabel="", so any other value still has to be patched.
		converged := (wantLabel && hasLabel && value == "") || (!wantLabel && !hasLabel)

		if !converged {
			if wantLabel {
				// A node deleted between the list and the patch needs no label, and leaves the count.
				err := drh.nodeAPI.UpdateLabels(ctx, n, map[string]string{targetLabel: ""}, nil)
				switch {
				case apierrors.IsNotFound(err):
					counted = false
				case err != nil:
					errs = append(errs, fmt.Errorf("could not add %s label to node %s: %v", targetLabel, name, err))
				}
			} else {
				// The node changed after the read this decision came from, and the change that
				// caused it may be one this controller never sees.
				err := drh.nodeAPI.UpdateLabelsWithOptimisticLock(ctx, n, nil, map[string]string{targetLabel: ""})
				switch {
				case apierrors.IsNotFound(err):
				case apierrors.IsConflict(err):
					result.requeueAfter = draTargetRequeue
					counted = true
				case err != nil:
					errs = append(errs, fmt.Errorf("could not remove %s label from node %s: %v", targetLabel, name, err))
				}
			}
		}

		if counted {
			result.targeted++
		}
	}

	return result, errors.Join(errs...)
}

// ensureDRATargetNodeSelector adds targetLabel to the node selector of every DaemonSet passed in,
// and nothing else. Only the current version goes through setDRAAsDesired, so the ones an ordered
// upgrade left behind would otherwise survive a cordon on the kernel-module-ready label alone.
func (drh *draReconcilerHelper) ensureDRATargetNodeSelector(
	ctx context.Context,
	existingDRADS []appsv1.DaemonSet,
	targetLabel string,
	recoverOnly bool,
) (bool, error) {
	logger := log.FromContext(ctx)

	var (
		errs  []error
		retry bool
	)
	for i := range existingDRADS {
		ds := &existingDRADS[i]

		// Patching a DaemonSet on its way out would only race with its deletion.
		if ds.GetDeletionTimestamp() != nil {
			continue
		}

		// Nodes carry it with an empty value, so any other value selects nothing and invites the GC.
		value, ok := ds.Spec.Template.Spec.NodeSelector[targetLabel]
		if ok && value == "" {
			continue
		}

		if recoverOnly && !ok {
			continue
		}

		var patch client.Patch

		if ok {
			// Guarded on the value this pass classified. A bare replace would not do: the API
			// server accepts one for a key that has since gone, which would turn the correction
			// into the migration the claims are holding back.
			pointer := jsonPointerEscape(targetLabel)
			patch = client.RawPatch(types.JSONPatchType, []byte(fmt.Sprintf(
				`[{"op":"test","path":"/spec/template/spec/nodeSelector/%s","value":%q},`+
					`{"op":"replace","path":"/spec/template/spec/nodeSelector/%s","value":""}]`,
				pointer, value, pointer,
			)))
		} else {
			patchFrom := client.MergeFrom(ds.DeepCopy())

			if ds.Spec.Template.Spec.NodeSelector == nil {
				ds.Spec.Template.Spec.NodeSelector = make(map[string]string, 1)
			}
			ds.Spec.Template.Spec.NodeSelector[targetLabel] = ""

			patch = patchFrom
		}

		err := drh.client.Patch(ctx, ds, patch)
		switch {
		case apierrors.IsNotFound(err):
			continue
		// The guard did not hold, so the DaemonSet is not the one this pass classified.
		case apierrors.IsInvalid(err):
			logger.Info("Target node selector changed under the correction", "name", ds.Name)

			retry = true

			continue
		case err != nil:
			errs = append(errs, fmt.Errorf("could not add %s to DaemonSet %s: %v", targetLabel, ds.Name, err))
			continue
		}

		logger.Info("Added the target node selector to an existing DaemonSet", "name", ds.Name, "label", targetLabel)
	}

	return retry, errors.Join(errs...)
}

// draTargetResult is what the label pass leaves for its caller: how many nodes end up wanting the
// driver, whether the DaemonSets have to wait for the claims, and when to look again unprompted.
type draTargetResult struct {
	targeted        int
	deferDaemonSets bool
	stale           bool
	requeueAfter    time.Duration
}

// draTargetRequeue paces the passes that cannot settle on an event. Nothing wakes this controller
// when a consumer Pod is finally scheduled, and a node that lost a conflict only changed its status,
// which the node predicate drops.
const draTargetRequeue = 15 * time.Second

// driverUsage names the nodes that still need this Module's DRA driver. unresolved records a
// reservation that could not be placed on a node, and stale that the Module moved on while the pass
// was reading it; either one makes dropping a label unsafe.
type driverUsage struct {
	nodes      sets.Set[string]
	unresolved bool
	stale      bool
}

// driverRecheck answers the uncached question a destructive decision has to ask, once for the pass
// that shares it: is anything still using the driver, and is the answer still about this spec.
type driverRecheck func() (driverUsage, error)

func (drh *draReconcilerHelper) newDriverRecheck(ctx context.Context, mod *kmmv1beta1.Module) driverRecheck {
	return sync.OnceValues(func() (driverUsage, error) {
		return drh.recheckDriverUsage(ctx, mod)
	})
}

// confirmCurrentModule reports whether mod is still the Module the API server holds. Every branch
// below it writes something the next pass cannot always take back, and a converged pass would
// otherwise never look: it drops out of the label pass before any uncached read happens.
func (drh *draReconcilerHelper) confirmCurrentModule(ctx context.Context, mod *kmmv1beta1.Module) (bool, error) {
	current := kmmv1beta1.Module{}
	key := types.NamespacedName{Namespace: mod.Namespace, Name: mod.Name}

	if err := drh.apiReader.Get(ctx, key, &current); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}

		return false, fmt.Errorf("could not re-read module %s: %v", key, err)
	}

	return current.UID == mod.UID &&
		current.Generation == mod.Generation &&
		current.DeletionTimestamp.IsZero() == mod.DeletionTimestamp.IsZero(), nil
}

// recheckDriverUsage re-reads, uncached, the state a target label removal rests on. A Module that
// changed is reported as such: a newer selector or toleration can make the node eligible again.
func (drh *draReconcilerHelper) recheckDriverUsage(
	ctx context.Context,
	mod *kmmv1beta1.Module,
) (driverUsage, error) {
	current := kmmv1beta1.Module{}
	key := types.NamespacedName{Namespace: mod.Namespace, Name: mod.Name}

	if err := drh.apiReader.Get(ctx, key, &current); err != nil {
		return driverUsage{}, fmt.Errorf("could not re-read module %s: %v", key, err)
	}

	// Deletion does not bump the generation, so a pass that started before it has to be caught
	// here too rather than carrying on writing.
	if current.UID != mod.UID || current.Generation != mod.Generation ||
		current.DeletionTimestamp.IsZero() != mod.DeletionTimestamp.IsZero() {
		return driverUsage{stale: true}, nil
	}

	return drh.nodesUsingDRADriver(ctx, drh.apiReader, mod)
}

// nodesUsingDRADriver returns the nodes running a Pod that still holds a ResourceClaim allocated to
// this Module's DRA driver. Kubelet calls the driver to unprepare those devices, so it has to stay
// until they are done, and a terminating Pod counts: unpreparing is what it is waiting for.
func (drh *draReconcilerHelper) nodesUsingDRADriver(
	ctx context.Context,
	reader client.Reader,
	mod *kmmv1beta1.Module,
) (driverUsage, error) {
	logger := log.FromContext(ctx)
	usage := driverUsage{nodes: sets.New[string]()}

	claims := resourcev1.ResourceClaimList{}
	if err := reader.List(ctx, &claims); err != nil {
		return usage, fmt.Errorf("could not list ResourceClaims: %v", err)
	}

	for i := range claims.Items {
		claim := &claims.Items[i]

		if !claimUsesDriver(claim, mod.Spec.DRA.DriverName) {
			continue
		}

		for _, consumer := range claim.Status.ReservedFor {
			nodeName := ""

			if consumer.APIGroup == "" && consumer.Resource == "pods" {
				pod := v1.Pod{}
				key := types.NamespacedName{Namespace: claim.Namespace, Name: consumer.Name}
				err := reader.Get(ctx, key, &pod)
				if err != nil && !apierrors.IsNotFound(err) {
					return usage, fmt.Errorf("could not get Pod %s reserving ResourceClaim %s: %v", key, claim.Name, err)
				}

				// The UID is what stops a Pod recreated under the same name inheriting it.
				if err == nil && pod.UID == consumer.UID {
					nodeName = pod.Spec.NodeName
				}
			}

			// A reservation the Pod cannot answer for still pins the driver: unbound, or gone.
			if nodeName == "" {
				nodeName = allocatedNodeName(claim)
			}

			// An allocation does not always name a node, and a reservation this pass cannot place
			// is not proof the driver is free, so hold every label it would otherwise drop.
			if nodeName == "" {
				// Named once: the first is enough to point at what is holding the labels.
				if !usage.unresolved {
					logger.Info("Cannot place a ResourceClaim reservation, keeping every DRA target label",
						"claim", claim.Namespace+"/"+claim.Name, "consumer", consumer.Resource+"/"+consumer.Name)
					drh.event(mod, "DRAReservationUnresolved",
						"ResourceClaim %s/%s is reserved by %s/%s, which cannot be placed on a node; keeping every %s label",
						claim.Namespace, claim.Name, consumer.Resource, consumer.Name,
						utils.GetDRATargetNodeLabel(mod.Namespace, mod.Name))
				}
				usage.unresolved = true
				continue
			}

			usage.nodes.Insert(nodeName)
		}
	}

	return usage, nil
}

// allocatedNodeName returns the node a claim's devices sit on, or "" when the selector could match
// more than one: terms are ORed, so every term has to pin the same metadata.name.
func allocatedNodeName(claim *resourcev1.ResourceClaim) string {
	if claim.Status.Allocation == nil || claim.Status.Allocation.NodeSelector == nil {
		return ""
	}

	nodeName := ""

	for _, term := range claim.Status.Allocation.NodeSelector.NodeSelectorTerms {
		termName := ""

		for _, req := range term.MatchFields {
			if req.Key != "metadata.name" || req.Operator != v1.NodeSelectorOpIn || len(req.Values) != 1 {
				continue
			}

			if termName != "" && termName != req.Values[0] {
				return ""
			}

			termName = req.Values[0]
		}

		// A term that does not pin the name can match another node, so one is enough to give up.
		if termName == "" || (nodeName != "" && nodeName != termName) {
			return ""
		}

		nodeName = termName
	}

	return nodeName
}

func claimUsesDriver(claim *resourcev1.ResourceClaim, driverName string) bool {
	if claim.Status.Allocation == nil {
		return false
	}

	for _, result := range claim.Status.Allocation.Devices.Results {
		if result.Driver == driverName {
			return true
		}
	}

	return false
}

// removeDRATargetLabels removes the dra-target label from every node carrying it, so it also
// reaches nodes the Module's selector no longer matches. It does not check for claims: its callers
// have stopped managing a DRA driver at all, and gating them needs the finalizer from #1331.
func (drh *draReconcilerHelper) removeDRATargetLabels(ctx context.Context, mod *kmmv1beta1.Module) error {
	targetLabel := utils.GetDRATargetNodeLabel(mod.Namespace, mod.Name)

	nodes, err := drh.nodeAPI.GetAllNodesByLabelKey(ctx, targetLabel)
	if err != nil {
		return fmt.Errorf("could not list nodes with %s label: %v", targetLabel, err)
	}

	var errs []error
	for i := range nodes {
		n := &nodes[i]
		if err := drh.nodeAPI.UpdateLabels(ctx, n, nil, map[string]string{targetLabel: ""}); apierrors.IsNotFound(err) {
			continue
		} else if err != nil {
			errs = append(errs, fmt.Errorf("could not remove %s label from node %s: %v", targetLabel, n.Name, err))
		}
	}

	return errors.Join(errs...)
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
	// A DaemonSet whose node selector was just migrated still reports the replica count from
	// before the patch, so wait for its status to catch up before reading zero as unused. Wanting
	// no Pods is not the same as having none, and deleting the DaemonSet takes the rest with it.
	return ds.Labels[versionLabel] != moduleVersion &&
		ds.Status.ObservedGeneration >= ds.Generation &&
		ds.Status.DesiredNumberScheduled == 0 &&
		ds.Status.CurrentNumberScheduled == 0 &&
		ds.Status.NumberReady == 0
}

// deleteDRAResources deletes all DRA-owned DaemonSets and DeviceClasses using label-based bulk deletion.
func (drh *draReconcilerHelper) deleteDRAResources(ctx context.Context, moduleName, moduleNamespace string) error {
	var errs []error

	dsDeleteOpts := []client.DeleteAllOfOption{
		client.MatchingLabels{
			constants.ModuleNameLabel: moduleName,
			constants.DaemonSetRole:   constants.DRARoleLabelValue,
		},
		client.InNamespace(moduleNamespace),
	}
	if err := drh.client.DeleteAllOf(ctx, &appsv1.DaemonSet{}, dsDeleteOpts...); err != nil {
		errs = append(errs, fmt.Errorf("failed to delete DRA DaemonSets for module %s/%s: %v", moduleNamespace, moduleName, err))
	}

	dcDeleteOpts := []client.DeleteAllOfOption{
		client.MatchingLabels{
			constants.ModuleNameLabel:      moduleName,
			constants.ModuleNamespaceLabel: moduleNamespace,
		},
	}
	if err := drh.client.DeleteAllOf(ctx, &resourcev1.DeviceClass{}, dcDeleteOpts...); err != nil {
		errs = append(errs, fmt.Errorf("failed to delete DeviceClasses for module %s/%s: %v", moduleNamespace, moduleName, err))
	}

	return errors.Join(errs...)
}

func (drh *draReconcilerHelper) moduleUpdateDRAStatus(ctx context.Context,
	mod *kmmv1beta1.Module,
	existingDRADS []appsv1.DaemonSet,
	targetedNodes int) error {

	if mod.Spec.DRA == nil {
		return nil
	}

	// The same tolerations the target pass uses, so the two numbers cannot disagree.
	numTargetedNodes, err := drh.nodeAPI.GetNumTargetedNodes(ctx, mod.Spec.Selector, module.EffectiveTolerations(mod.Spec.Tolerations))
	if err != nil {
		return fmt.Errorf("failed to determine the number of nodes targeted by Module %s/%s selector: %v", mod.Namespace, mod.Name, err)
	}

	// Every version the Module still owns counts, so during an ordered upgrade this can exceed the
	// desired number while two DaemonSets briefly run a driver for the same node.
	numAvailable := int32(0)
	for _, ds := range existingDRADS {
		numAvailable += ds.Status.NumberAvailable
	}

	unmodifiedMod := mod.DeepCopy()

	// The target label, not the selector, is what the DaemonSet ends up selecting on, and a claim
	// can keep a node in that set after the selector has stopped matching it.
	desired := targetedNodes
	if mod.Spec.ModuleLoader == nil {
		desired = numTargetedNodes
	}

	mod.Status.DRA.NodesMatchingSelectorNumber = int32(numTargetedNodes)
	mod.Status.DRA.DesiredNumber = int32(desired)
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
		// Nothing to unload, so drain gating is unnecessary.
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
				Tolerations:                  mod.Spec.Tolerations,
				AutomountServiceAccountToken: mod.Spec.DRA.AutomountServiceAccountToken,
			},
		},
	}

	return controllerutil.SetControllerReference(mod, ds, dsci.scheme)
}
