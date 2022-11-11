package controllers

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/kubernetes-sigs/kernel-module-management/internal/filter"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"open-cluster-management.io/api/cluster/v1alpha1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

//+kubebuilder:rbac:groups="core",resources=nodes,verbs=get;patch;list;watch
//+kubebuilder:rbac:groups=cluster.open-cluster-management.io,resources=clusterclaims,resourceNames=kernel-versions.kmm.node.kubernetes.io,verbs=create;delete;get;list;patch;update;watch

const clusterClaimName = "kernel-versions.kmm.node.kubernetes.io"

type NodeKernelClusterClaimReconciler struct {
	client client.Client
}

func NewNodeKernelClusterClaimReconciler(client client.Client) *NodeKernelClusterClaimReconciler {
	return &NodeKernelClusterClaimReconciler{client: client}
}

func (r *NodeKernelClusterClaimReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	nodeList := v1.NodeList{}

	logger.Info("Listing all nodes")

	if err := r.client.List(ctx, &nodeList); err != nil {
		return ctrl.Result{}, fmt.Errorf("could not list nodes: %v", err)
	}

	kernelsMap := make(map[string]struct{})

	for _, n := range nodeList.Items {
		kernelsMap[n.Status.NodeInfo.KernelVersion] = struct{}{}
	}

	// The map's order is unpredictable; we do not want to update the ClusterClaim if the value only
	// changed because of a different order in the kernel versions.
	// Copy the kernels into a slice and sort it alphabetically.
	kernelsSlice := make([]string, 0, len(kernelsMap))

	for k := range kernelsMap {
		kernelsSlice = append(kernelsSlice, k)
	}

	sort.Strings(kernelsSlice)

	cc := v1alpha1.ClusterClaim{
		ObjectMeta: metav1.ObjectMeta{Name: clusterClaimName},
	}

	logger.Info("Creating or patching ClusterClaim")

	opRes, err := controllerutil.CreateOrPatch(ctx, r.client, &cc, func() error {
		cc.Spec = v1alpha1.ClusterClaimSpec{
			Value: strings.Join(kernelsSlice, "\n"),
		}

		return nil
	})

	logger.Info("Reconciled ClusterClaim", "res", opRes)

	return ctrl.Result{}, err
}

// SetupWithManager sets up the controller with the Manager.
func (r *NodeKernelClusterClaimReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.
		NewControllerManagedBy(mgr).
		Named("node-kernel-clusterclaim").
		// Each time a Node's .status.nodeInfo.kernelVersion is updated, enqueue a reconciliation request
		For(
			&v1.Node{},
			builder.WithPredicates(
				filter.NodeUpdateKernelChangedPredicate(),
			),
		).
		// Each time our ClusterClaim is updated, enqueue an empty reconciliation request.
		// We list all nodes during reconciliation, so sending an empty request is OK.
		Watches(
			&source.Kind{
				Type: &v1alpha1.ClusterClaim{},
			},
			handler.EnqueueRequestsFromMapFunc(func(_ client.Object) []reconcile.Request {
				return []reconcile.Request{{}}
			}),
			builder.WithPredicates(
				predicate.NewPredicateFuncs(func(object client.Object) bool {
					return object.GetName() == clusterClaimName
				}),
			),
		).
		Complete(r)
}
