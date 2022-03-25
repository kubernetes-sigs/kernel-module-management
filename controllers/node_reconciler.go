package controllers

import (
	"context"
	"fmt"

	ootov1beta1 "github.com/qbarrand/oot-operator/api/v1beta1"
	"github.com/qbarrand/oot-operator/controllers/module"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

//+kubebuilder:rbac:groups=ooto.sigs.k8s.io,resources=modules,verbs=list
//+kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=create;list;patch
//+kubebuilder:rbac:groups="core",resources=nodes,verbs=get;list;watch

// NodeReconciler looks for all Modules applicable to the Node and creates DaemonSets if necessary.
type NodeReconciler struct {
	client    client.Client
	dc        DaemonSetCreator
	km        module.KernelMapper
	namespace string
}

func NewNodeReconciler(client client.Client, namespace string, dc DaemonSetCreator, km module.KernelMapper) *NodeReconciler {
	return &NodeReconciler{
		client:    client,
		namespace: namespace,
		dc:        dc,
		km:        km,
	}
}

func (r *NodeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	node := v1.Node{}

	logger := log.FromContext(ctx)

	if err := r.client.Get(ctx, types.NamespacedName{Name: req.Name}, &node); err != nil {
		return ctrl.Result{}, fmt.Errorf("could not get the node: %v", err)
	}

	mods := ootov1beta1.ModuleList{}

	if err := r.client.List(ctx, &mods); err != nil {
		return ctrl.Result{}, fmt.Errorf("could not list nodules: %v", err)
	}

	logger.V(1).Info("Listed modules", "count", len(mods.Items))

	nodeLabelsSet := labels.Set(node.Labels)

	for _, mod := range mods.Items {
		logger := logger.WithValues("module name", mod.Name)

		logger.V(1).Info("Processing module")

		sel := labels.NewSelector()

		for k, v := range mod.Spec.Selector {
			logger.V(1).Info("Processing selector item", "key", k, "value", v)

			requirement, err := labels.NewRequirement(k, selection.Equals, []string{v})
			if err != nil {
				return ctrl.Result{Requeue: false}, fmt.Errorf("could not generate requirement: %v", err)
			}

			sel = sel.Add(*requirement)
		}

		if !sel.Matches(nodeLabelsSet) {
			logger.V(1).Info("Node labels do not match the module's selector; skipping")
			continue
		}

		dsByKernelVersion, err := r.dc.ModuleDaemonSetsByKernelVersion(ctx, mod)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("could get DaemonSets for module %s: %v", mod.Name, err)
		}

		kernelVersion := node.Status.NodeInfo.KernelVersion

		// This node needs mod.
		// Do we already have a DaemonSet for its kernel?
		if ds := dsByKernelVersion[kernelVersion]; ds != nil {
			logger.V(1).Info("DaemonSet already exists for node and module", "name", ds.Name)
			continue
		}

		containerImage, err := r.km.FindImageForKernel(mod.Spec.KernelMappings, kernelVersion)
		if err != nil {
			logger.Info("No suitable kernel mapping found; skipping node")
			continue
		}

		logger.Info("Kernel mapping found, reconciling DaemonSet", "container image", containerImage)

		ds := &appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: mod.Name + "-",
				Namespace:    r.namespace,
			},
		}

		res, err := controllerutil.CreateOrPatch(ctx, r.client, ds, func() error {
			return r.dc.SetAsDesired(ds, containerImage, mod, kernelVersion)
		})

		logger.Info("Reconciled DaemonSet", "name", ds.Name, "result", res)
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *NodeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1.Node{}).
		Owns(&appsv1.DaemonSet{}).
		WithEventFilter(SkipDeletions()).
		Complete(r)
}
