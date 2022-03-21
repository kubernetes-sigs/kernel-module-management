package controllers

import (
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

//+kubebuilder:rbac:groups="core",resources=nodes,verbs=get;patch;watch

type NodeKernelReconciler struct {
	client    client.Client
	labelName string
}

func NewNodeKernelReconciler(client client.Client, labelName string) *NodeKernelReconciler {
	return &NodeKernelReconciler{
		client:    client,
		labelName: labelName,
	}
}

func (r *NodeKernelReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	node := v1.Node{}

	logger := log.FromContext(ctx)

	if err := r.client.Get(ctx, types.NamespacedName{Name: req.Name}, &node); err != nil {
		if k8serrors.IsNotFound(err) {
			logger.Info("Node not found; probably deleted")
			return ctrl.Result{}, nil
		}

		return ctrl.Result{Requeue: true}, fmt.Errorf("could not get node: %v", err)
	}

	labelValue := node.Labels[r.labelName]
	kernelVersion := node.Status.NodeInfo.KernelVersion

	if labelValue != kernelVersion {
		logger.Info("Patching node label", "old kernel", labelValue, "new kernel", kernelVersion)

		p := client.MergeFrom(node.DeepCopy())

		if node.Labels == nil {
			node.Labels = make(map[string]string)
		}

		node.Labels[r.labelName] = node.Status.NodeInfo.KernelVersion

		if err := r.client.Patch(ctx, &node, p); err != nil {
			return ctrl.Result{Requeue: true}, fmt.Errorf("could not patch the node: %v", err)
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *NodeKernelReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).For(&v1.Node{}).Complete(r)
}
