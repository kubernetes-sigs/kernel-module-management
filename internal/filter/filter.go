package filter

import (
	"context"
	"reflect"

	"github.com/go-logr/logr"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/kubectl/pkg/util/podutils"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	hubv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api-hub/v1beta1"
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	"github.com/kubernetes-sigs/kernel-module-management/internal/nmc"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
)

func HasLabel(label string) predicate.Predicate {
	return predicate.NewPredicateFuncs(func(o client.Object) bool {
		return o.GetLabels()[label] != ""
	})
}

var skipDeletions predicate.Predicate = predicate.Funcs{
	DeleteFunc: func(_ event.DeleteEvent) bool { return false },
}

var skipCreations predicate.Predicate = predicate.Funcs{
	CreateFunc: func(_ event.CreateEvent) bool { return false },
}

var nodeBecomesSchedulable predicate.Predicate = predicate.Funcs{
	UpdateFunc: func(e event.UpdateEvent) bool {
		oldNode, ok := e.ObjectOld.(*v1.Node)
		if !ok {
			return false
		}

		newNode, ok := e.ObjectNew.(*v1.Node)
		if !ok {
			return false
		}

		isOldSchedulable := utils.IsNodeSchedulable(oldNode)
		isNewSchedulable := utils.IsNodeSchedulable(newNode)
		if isOldSchedulable != isNewSchedulable && isNewSchedulable {
			return true
		}

		return false
	},
}

var kmmClusterClaimChanged predicate.Predicate = predicate.Funcs{
	UpdateFunc: func(e event.UpdateEvent) bool {
		oldManagedCluster, ok := e.ObjectOld.(*clusterv1.ManagedCluster)
		if !ok {
			return false
		}

		newManagedCluster, ok := e.ObjectNew.(*clusterv1.ManagedCluster)
		if !ok {
			return false
		}

		newClusterClaim := clusterClaim(constants.KernelVersionsClusterClaimName, newManagedCluster.Status.ClusterClaims)
		if newClusterClaim == nil {
			return false
		}
		oldClusterClaim := clusterClaim(constants.KernelVersionsClusterClaimName, oldManagedCluster.Status.ClusterClaims)

		return !reflect.DeepEqual(newClusterClaim, oldClusterClaim)
	},
}

var moduleJobSuccess predicate.Predicate = predicate.Funcs{
	UpdateFunc: func(e event.UpdateEvent) bool {
		job, ok := e.ObjectNew.(*batchv1.Job)
		if !ok {
			return true
		}
		if job.Status.Succeeded == 1 {
			return true
		}

		return false
	},
}

func clusterClaim(name string, clusterClaims []clusterv1.ManagedClusterClaim) *clusterv1.ManagedClusterClaim {
	for _, clusterClaim := range clusterClaims {
		if clusterClaim.Name == name {
			return &clusterClaim
		}
	}
	return nil
}

type Filter struct {
	client    client.Client
	nmcHelper nmc.Helper
}

func New(client client.Client, nmcHelper nmc.Helper) *Filter {
	return &Filter{
		client:    client,
		nmcHelper: nmcHelper,
	}
}

func (f *Filter) ModuleReconcilerNodePredicate(kernelLabel string) predicate.Predicate {
	return predicate.And(
		skipDeletions,
		HasLabel(kernelLabel),
		predicate.Or(nodeBecomesSchedulable, predicate.LabelChangedPredicate{}),
	)
}

func ModuleNMCReconcilerNodePredicate() predicate.Predicate {
	return predicate.And(
		skipDeletions,
		predicate.Or(nodeBecomesSchedulable, predicate.LabelChangedPredicate{}),
	)
}

func ModuleNMCReconcileJobPredicate() predicate.Predicate {
	return predicate.And(
		skipDeletions,
		skipCreations,
		moduleJobSuccess,
	)
}

func (f *Filter) NodeKernelReconcilerPredicate(labelName string) predicate.Predicate {
	labelMismatch := predicate.NewPredicateFuncs(func(o client.Object) bool {
		return o.GetLabels()[labelName] != o.(*v1.Node).Status.NodeInfo.KernelVersion
	})

	return predicate.And(skipDeletions, labelMismatch)
}

func NodeUpdateKernelChangedPredicate() predicate.Predicate {
	return predicate.Funcs{
		UpdateFunc: func(updateEvent event.UpdateEvent) bool {
			oldNode, ok := updateEvent.ObjectOld.(*v1.Node)
			if !ok {
				return false
			}

			newNode, ok := updateEvent.ObjectNew.(*v1.Node)
			if !ok {
				return false
			}

			return oldNode.Status.NodeInfo.KernelVersion != newNode.Status.NodeInfo.KernelVersion
		},
	}
}

func (f *Filter) FindModulesForNode(ctx context.Context, node client.Object) []reconcile.Request {
	logger := ctrl.LoggerFrom(ctx).WithValues("node", node.GetName())

	reqs := make([]reconcile.Request, 0)

	logger.Info("Listing all modules")

	mods := kmmv1beta1.ModuleList{}

	if err := f.client.List(context.Background(), &mods); err != nil {
		logger.Error(err, "could not list modules")
		return reqs
	}

	logger.Info("Listed modules", "count", len(mods.Items))

	for _, mod := range mods.Items {
		logger := logger.WithValues("module name", mod.Name)

		logger.V(1).Info("Processing module")

		moduleSelectorMatchNode, err := utils.IsObjectSelectedByLabels(node.GetLabels(), mod.Spec.Selector)
		if err != nil {
			logger.Error(err, "could not determine if node is selected by module", "node", node.GetName(), "module", mod.Name)
			return reqs
		}

		if !moduleSelectorMatchNode {
			logger.V(1).Info("Node labels do not match the module's selector; skipping")
			continue
		}

		nsn := types.NamespacedName{Name: mod.Name, Namespace: mod.Namespace}

		reqs = append(reqs, reconcile.Request{NamespacedName: nsn})
	}

	logger.Info("Adding reconciliation requests", "count", len(reqs))
	logger.V(1).Info("New requests", "requests", reqs)

	return reqs
}

// FindModulesForNMCNodeChange finds the modules that are affected by node changes that result
// in ModuleNMCReconcilerNodePredicate predicate. First it find all the Module that can run on the node, based
// on the Modules' Selector field and on node's labels. Then, in case NMC for the node exists, it adds all the
// Modules already set in NMC ( in case they were not added in a previous step).
func (f *Filter) FindModulesForNMCNodeChange(ctx context.Context, node client.Object) []reconcile.Request {
	logger := ctrl.LoggerFrom(ctx).WithValues("node", node.GetName())

	logger.Info("Listing all modules")

	mods := kmmv1beta1.ModuleList{}

	err := f.client.List(ctx, &mods)
	if err != nil {
		logger.Error(err, "could not list modules")
		return nil
	}

	logger.Info("Listed modules", "count", len(mods.Items))

	reqSet := sets.New[reconcile.Request]()

	for _, mod := range mods.Items {
		logger := logger.WithValues("module name", mod.Name)

		logger.V(1).Info("Processing module")

		moduleSelectorMatchNode, err := utils.IsObjectSelectedByLabels(node.GetLabels(), mod.Spec.Selector)
		if err != nil {
			logger.Error(err, "could not determine if node is selected by module", "node", node.GetName(), "module", mod.Name)
			continue
		}

		if !moduleSelectorMatchNode {
			logger.V(1).Info("Node labels do not match the module's selector; skipping")
			continue
		}

		nsn := types.NamespacedName{Name: mod.Name, Namespace: mod.Namespace}

		reqSet.Insert(reconcile.Request{NamespacedName: nsn})
	}

	nms, err := f.nmcHelper.Get(ctx, node.GetName())
	if err != nil || nms == nil {
		return reqSet.UnsortedList()
	}

	// go over modules of NodeModulesConfig and add them to request if they are not there already
	for _, mod := range nms.Spec.Modules {
		nsn := types.NamespacedName{Name: mod.Name, Namespace: mod.Namespace}
		reqSet.Insert(reconcile.Request{NamespacedName: nsn})
	}

	reqs := reqSet.UnsortedList()

	logger.Info("Adding reconciliation requests", "count", len(reqs))
	logger.V(1).Info("New requests", "requests", reqs)

	return reqs
}

func (f *Filter) FindManagedClusterModulesForCluster(ctx context.Context, cluster client.Object) []reconcile.Request {
	logger := ctrl.LoggerFrom(ctx).WithValues("managedcluster", cluster.GetName())

	reqs := make([]reconcile.Request, 0)

	logger.Info("Listing all ManagedClusterModules")

	mods := hubv1beta1.ManagedClusterModuleList{}

	if err := f.client.List(context.Background(), &mods); err != nil {
		logger.Error(err, "could not list ManagedClusterModules")
		return reqs
	}

	logger.Info("Listed ManagedClusterModules", "count", len(mods.Items))

	for _, mod := range mods.Items {
		logger := logger.WithValues("ManagedClusterModule name", mod.Name)

		logger.V(1).Info("Processing ManagedClusterModule")

		mcmSelectorMatchCluster, err := utils.IsObjectSelectedByLabels(cluster.GetLabels(), mod.Spec.Selector)
		if err != nil {
			logger.Error(err, "could not determine if cluster is selected by ManagedClusterModule", "node", cluster.GetName(), "module", mod.Name)
			return reqs
		}

		if !mcmSelectorMatchCluster {
			logger.V(1).Info("Cluster labels do not match the ManagedClusterModule's selector; skipping")
			continue
		}

		nsn := types.NamespacedName{Name: mod.Name}

		reqs = append(reqs, reconcile.Request{NamespacedName: nsn})
	}

	logger.Info("Adding reconciliation requests", "count", len(reqs))
	logger.V(1).Info("New requests", "requests", reqs)

	return reqs
}

func (f *Filter) ManagedClusterModuleReconcilerManagedClusterPredicate() predicate.Predicate {
	return predicate.Or(
		predicate.LabelChangedPredicate{},
		kmmClusterClaimChanged,
	)
}

func (f *Filter) EnqueueAllPreflightValidations(ctx context.Context, mod client.Object) []reconcile.Request {
	reqs := make([]reconcile.Request, 0)

	logger := ctrl.LoggerFrom(ctx).WithValues("module", mod.GetName())
	logger.Info("Listing all preflights")
	preflights := kmmv1beta1.PreflightValidationList{}
	if err := f.client.List(context.Background(), &preflights); err != nil {
		logger.Error(err, "could not list preflights")
		return reqs
	}

	for _, preflight := range preflights.Items {
		// skip the preflight being deleted
		if preflight.GetDeletionTimestamp() != nil {
			continue
		}
		nsn := types.NamespacedName{Name: preflight.Name, Namespace: preflight.Namespace}
		reqs = append(reqs, reconcile.Request{NamespacedName: nsn})
	}
	return reqs
}

// DeletingPredicate returns a predicate that returns true if the object is being deleted.
func DeletingPredicate() predicate.Predicate {
	return predicate.NewPredicateFuncs(func(object client.Object) bool {
		return !object.GetDeletionTimestamp().IsZero()
	})
}

// PodReadinessChangedPredicate returns a predicate for Update events that only returns true if the Ready condition
// changed.
func PodReadinessChangedPredicate(logger logr.Logger) predicate.Predicate {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldPod, ok := e.ObjectOld.(*v1.Pod)
			if !ok {
				logger.Info("Old object is not a pod", "object", e.ObjectOld)
				return true
			}

			newPod, ok := e.ObjectNew.(*v1.Pod)
			if !ok {
				logger.Info("New object is not a pod", "object", e.ObjectNew)
				return true
			}

			return podutils.IsPodReady(oldPod) != podutils.IsPodReady(newPod)
		},
	}
}

func PreflightReconcilerUpdatePredicate() predicate.Predicate {
	return predicate.GenerationChangedPredicate{}
}

func NodeLabelModuleVersionUpdatePredicate(logger logr.Logger) predicate.Predicate {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldNode, ok := e.ObjectOld.(*v1.Node)
			if !ok {
				logger.Info("Old object is not a node", "object", e.ObjectOld)
				return true
			}

			newNode, ok := e.ObjectNew.(*v1.Node)
			if !ok {
				logger.Info("New object is not a node", "object", e.ObjectNew)
				return true
			}

			oldNodeVersionLabels := utils.GetNodesVersionLabels(oldNode.Labels)
			newNodeVersionLabels := utils.GetNodesVersionLabels(newNode.Labels)
			return !reflect.DeepEqual(oldNodeVersionLabels, newNodeVersionLabels)
		},
	}
}
