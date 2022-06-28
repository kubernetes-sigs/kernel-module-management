package controllers

import (
	"context"
	"fmt"

	"github.com/qbarrand/oot-operator/internal/filter"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

//+kubebuilder:rbac:groups="core",resources=nodes,verbs=get;patch;list;watch

type NodeKernelReconciler struct {
	client    client.Client
	labelName string
	filter    *filter.Filter
}

func NewNodeKernelReconciler(client client.Client, labelName string, filter *filter.Filter) *NodeKernelReconciler {
	return &NodeKernelReconciler{
		client:    client,
		labelName: labelName,
		filter:    filter,
	}
}

func (r *NodeKernelReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	node := v1.Node{}

	logger := log.FromContext(ctx)

	if err := r.client.Get(ctx, types.NamespacedName{Name: req.Name}, &node); err != nil {
		return ctrl.Result{}, fmt.Errorf("could not get node: %v", err)
	}

	kernelVersion := node.Status.NodeInfo.KernelVersion

	logger.Info(
		"Patching node label",
		"old kernel", node.Labels[r.labelName],
		"new kernel", kernelVersion)

	p := client.MergeFrom(node.DeepCopy())

	if node.Labels == nil {
		node.Labels = make(map[string]string)
	}

	node.Labels[r.labelName] = kernelVersion

	if err := r.client.Patch(ctx, &node, p); err != nil {
		return ctrl.Result{}, fmt.Errorf("could not patch the node: %v", err)
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *NodeKernelReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.
		NewControllerManagedBy(mgr).
		Named("node-kernel").
		For(&v1.Node{}).
		WithEventFilter(
			r.filter.NodeKernelReconcilerPredicate(r.labelName),
		).
		Complete(r)
}
