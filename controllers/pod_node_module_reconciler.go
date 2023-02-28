package controllers

import (
	"context"
	"fmt"

	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	"github.com/kubernetes-sigs/kernel-module-management/internal/daemonset"
	"github.com/kubernetes-sigs/kernel-module-management/internal/filter"
	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/kubectl/pkg/util/podutils"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

//+kubebuilder:rbac:groups="core",resources=pods,verbs=get;patch;list;watch
//+kubebuilder:rbac:groups="core",resources=nodes,verbs=get;watch

const PodNodeModuleReconcilerName = "PodNodeModule"

type PodNodeModuleReconciler struct {
	client    client.Client
	daemonAPI daemonset.DaemonSetCreator
}

func NewPodNodeModuleReconciler(client client.Client, daemonAPI daemonset.DaemonSetCreator) *PodNodeModuleReconciler {
	return &PodNodeModuleReconciler{client: client, daemonAPI: daemonAPI}
}

func (pnmr *PodNodeModuleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := ctrl.LoggerFrom(ctx)

	pod := v1.Pod{}
	podNamespacedName := req.NamespacedName

	if err := pnmr.client.Get(ctx, podNamespacedName, &pod); err != nil {
		if k8serrors.IsNotFound(err) {
			logger.Info("Pod not found")
			return ctrl.Result{}, nil
		}

		return ctrl.Result{}, fmt.Errorf("could not get pod %s: %v", podNamespacedName, err)
	}

	nodeName := pod.Spec.NodeName

	moduleName, ok := pod.Labels[constants.ModuleNameLabel]
	if !ok {
		return ctrl.Result{}, fmt.Errorf("pod %s has no %q label", podNamespacedName, constants.ModuleNameLabel)
	}

	labelName := pnmr.daemonAPI.GetNodeLabelFromPod(&pod, moduleName, false)
	deprecatedLabelName := pnmr.daemonAPI.GetNodeLabelFromPod(&pod, moduleName, true)

	logger = logger.WithValues(
		"node name", nodeName,
		"module name", moduleName,
		"label name", labelName,
	)

	// when Daemonset/ReplicaSet controller deletes pod, the pods state stays Ready,
	// but its deletion timestamp is set. We use deletion timestamp to delete the label,
	// and not wait a probable TerminationGracePeriod, since Pre-Stop hooks is run
	// at the beginning. IsodReady condition should still be checked, in case pod state
	// has changed not due to Daemonset termination, but due to internal state of Daemonset on
	// cluster
	if !podutils.IsPodReady(&pod) || !pod.DeletionTimestamp.IsZero() {
		logger.Info("Unlabeling node")

		// Make sure we don't already have a new running pod before unlabeling the node
		labelSelector := client.MatchingLabels{constants.ModuleNameLabel: moduleName}
		fieldSelector := client.MatchingFields{"spec.nodeName": nodeName}
		var modulePodsList v1.PodList
		err := pnmr.client.List(ctx, &modulePodsList, labelSelector, fieldSelector)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to get list of all pods for module %s on node %s: %v", moduleName, nodeName, err)
		}
		var foundRunningPod bool
		for _, p := range modulePodsList.Items {
			if podutils.IsPodReady(&p) && p.DeletionTimestamp.IsZero() {
				foundRunningPod = true
				break
			}
		}
		if !foundRunningPod {
			if err := pnmr.deleteLabel(ctx, nodeName, labelName, deprecatedLabelName); err != nil {
				return ctrl.Result{}, fmt.Errorf("could not unlabel node %s with label {%q, %q}: %v",
					nodeName, labelName, deprecatedLabelName, err)
			}
		}

		if !pod.DeletionTimestamp.IsZero() {
			logger.Info("Pod deletion requested; removing finalizer")

			// the Pod finalizer update API call can return a NotFound error, which indicates that
			// the specified Pod has already been deleted. By ignoring NotFound errors we ensure
			// that no additional, unnecessary reconciliation request will be queued (since a
			// reconciliation result with a non-nil error will be requeued).
			if err := pnmr.deleteFinalizer(ctx, &pod); client.IgnoreNotFound(err) != nil {
				return ctrl.Result{}, fmt.Errorf("could not delete the pod finalizer: %v", err)
			}
		}

		return ctrl.Result{}, nil
	}

	logger.Info("Labeling node")

	if err := pnmr.addLabel(ctx, nodeName, labelName, deprecatedLabelName); err != nil {
		return ctrl.Result{}, fmt.Errorf("could not label node %s with {%q, %q}: %v", nodeName, labelName, deprecatedLabelName, err)
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (pnmr *PodNodeModuleReconciler) SetupWithManager(mgr ctrl.Manager) error {

	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &v1.Pod{}, "spec.nodeName", func(rawObj client.Object) []string {
		pod := rawObj.(*v1.Pod)
		return []string{pod.Spec.NodeName}
	}); err != nil {
		return err
	}

	p := predicate.And(
		predicate.Or(
			filter.PodReadinessChangedPredicate(
				mgr.GetLogger().WithName("pod-readiness-changed"),
			),
			filter.DeletingPredicate(),
		),
		filter.HasLabel(constants.ModuleNameLabel),
		filter.PodHasSpecNodeName(),
	)

	return ctrl.
		NewControllerManagedBy(mgr).
		Named(PodNodeModuleReconcilerName).
		For(&v1.Pod{}).
		WithEventFilter(p).
		Complete(pnmr)
}

func (pnmr *PodNodeModuleReconciler) addLabel(ctx context.Context, nodeName string, labelNames ...string) error {
	node := v1.Node{}

	if err := pnmr.client.Get(ctx, types.NamespacedName{Name: nodeName}, &node); err != nil {
		return fmt.Errorf("could not get node %s: %v", nodeName, err)
	}

	nodeCopy := node.DeepCopy()

	if node.Labels == nil {
		node.Labels = make(map[string]string, len(labelNames))
	}

	for _, ln := range labelNames {
		node.Labels[ln] = ""
	}

	return pnmr.client.Patch(ctx, &node, client.MergeFrom(nodeCopy))
}

func (pnmr *PodNodeModuleReconciler) deleteFinalizer(ctx context.Context, pod *v1.Pod) error {
	podCopy := pod.DeepCopy()

	controllerutil.RemoveFinalizer(pod, constants.NodeLabelerFinalizer)

	return pnmr.client.Patch(ctx, pod, client.MergeFrom(podCopy))
}

func (pnmr *PodNodeModuleReconciler) deleteLabel(ctx context.Context, nodeName string, labelNames ...string) error {
	node := v1.Node{}

	if err := pnmr.client.Get(ctx, types.NamespacedName{Name: nodeName}, &node); err != nil {
		return fmt.Errorf("could not get node %s: %v", nodeName, err)
	}

	nodeCopy := node.DeepCopy()

	for _, ln := range labelNames {
		delete(node.Labels, ln)
	}

	return pnmr.client.Patch(ctx, &node, client.MergeFrom(nodeCopy))
}
