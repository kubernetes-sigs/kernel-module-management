package controllers

import (
	"context"
	"fmt"

	"github.com/kubernetes-sigs/kernel-module-management/internal/filter"
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

	if err := r.addLabel(ctx, &node, r.labelName, kernelVersion); err != nil {
		return ctrl.Result{}, fmt.Errorf("could not patch the node: %v", err)
	}

	return ctrl.Result{}, nil
}

func (r *NodeKernelReconciler) ClearNodes(ctx context.Context) {
	nodes := v1.NodeList{}
	err := r.client.List(ctx, &nodes)
	if err != nil {
		return
	}
	for _, node := range nodes.Items {
		_ = r.deleteLabel(ctx, &node, r.labelName)
	}
}

func (r *NodeKernelReconciler) addLabel(ctx context.Context, node *v1.Node, labelName, labelValue string) error {
	p := client.MergeFrom(node.DeepCopy())
	if node.Labels == nil {
		node.Labels = make(map[string]string)
	}
	node.Labels[labelName] = labelValue

	return r.client.Patch(ctx, node, p)
}

func (r *NodeKernelReconciler) deleteLabel(ctx context.Context, node *v1.Node, labelName string) error {
	p := client.MergeFrom(node.DeepCopy())

	delete(node.Labels, labelName)

	return r.client.Patch(ctx, node, p)
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
