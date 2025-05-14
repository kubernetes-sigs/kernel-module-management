package controllers

import (
	"context"
	"fmt"

	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	"github.com/kubernetes-sigs/kernel-module-management/internal/filter"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/kubectl/pkg/util/podutils"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

const DevicePluginPodReconcilerName = "DevicePluginPod"

type DevicePluginPodReconciler struct {
	client client.Client
}

func NewDevicePluginPodReconciler(client client.Client) *DevicePluginPodReconciler {
	return &DevicePluginPodReconciler{client: client}
}

func (dppr *DevicePluginPodReconciler) Reconcile(ctx context.Context, pod *v1.Pod) (ctrl.Result, error) {
	logger := ctrl.LoggerFrom(ctx)

	nodeName := pod.Spec.NodeName

	moduleName, ok := pod.Labels[constants.ModuleNameLabel]
	if !ok {
		return ctrl.Result{}, fmt.Errorf("pod %s/%s has no %q label", pod.Namespace, pod.Name, constants.ModuleNameLabel)
	}

	labelName := utils.GetDevicePluginNodeLabel(pod.Namespace, moduleName)

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
	if !podutils.IsPodReady(pod) || !pod.DeletionTimestamp.IsZero() {
		// If the pod was created very recently but immediately deleted, its .spec.nodeName may still be empty.
		// In that case, no need to try and unlabel the node; because .spec.nodeName is empty, it was never labeled in
		// the first place.
		if nodeName != "" {
			logger.Info("Unlabeling node")

			// Make sure we don't already have a new running pod before unlabeling the node
			labelSelector := client.MatchingLabels{constants.ModuleNameLabel: moduleName}
			fieldSelector := client.MatchingFields{"spec.nodeName": nodeName}
			var modulePodsList v1.PodList
			err := dppr.client.List(ctx, &modulePodsList, labelSelector, fieldSelector)
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
				if err := dppr.deleteLabel(ctx, nodeName, labelName); err != nil {
					return ctrl.Result{}, fmt.Errorf("could not unlabel node %s with label %s: %v",
						nodeName, labelName, err)
				}
			}
		}

		if !pod.DeletionTimestamp.IsZero() {
			logger.Info("Pod deletion requested; removing finalizer")

			// the Pod finalizer update API call can return a NotFound error, which indicates that
			// the specified Pod has already been deleted. By ignoring NotFound errors we ensure
			// that no additional, unnecessary reconciliation request will be queued (since a
			// reconciliation result with a non-nil error will be requeued).
			if err := dppr.deleteFinalizer(ctx, pod); client.IgnoreNotFound(err) != nil {
				return ctrl.Result{}, fmt.Errorf("could not delete the pod finalizer: %v", err)
			}
		}

		return ctrl.Result{}, nil
	}

	logger.Info("Labeling node")

	if err := dppr.addLabel(ctx, nodeName, labelName); err != nil {
		return ctrl.Result{}, fmt.Errorf("could not label node %s with %s: %v", nodeName, labelName, err)
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (dppr *DevicePluginPodReconciler) SetupWithManager(mgr ctrl.Manager) error {

	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &v1.Pod{}, "spec.nodeName", func(rawObj client.Object) []string {
		pod := rawObj.(*v1.Pod)
		return []string{pod.Spec.NodeName}
	}); err != nil {
		return err
	}

	isDaemonSetPod := predicate.NewPredicateFuncs(func(o client.Object) bool {
		ownerReferences := o.GetOwnerReferences()
		for _, ownerReference := range ownerReferences {
			if ownerReference.Kind == "DaemonSet" {
				return true
			}
		}
		return false
	})

	p := predicate.And(
		predicate.Or(
			filter.PodReadinessChangedPredicate(
				mgr.GetLogger().WithName("pod-readiness-changed"),
			),
			filter.DeletingPredicate(),
		),
		filter.HasLabel(constants.ModuleNameLabel),
		isDaemonSetPod,
	)

	return ctrl.
		NewControllerManagedBy(mgr).
		Named(DevicePluginPodReconcilerName).
		For(&v1.Pod{}).
		WithEventFilter(p).
		Complete(
			reconcile.AsReconciler[*v1.Pod](dppr.client, dppr),
		)
}

func (dppr *DevicePluginPodReconciler) addLabel(ctx context.Context, nodeName string, labelName string) error {
	node := v1.Node{}

	if err := dppr.client.Get(ctx, types.NamespacedName{Name: nodeName}, &node); err != nil {
		return fmt.Errorf("could not get node %s: %v", nodeName, err)
	}

	nodeCopy := node.DeepCopy()

	if node.Labels == nil {
		node.Labels = make(map[string]string)
	}

	node.Labels[labelName] = ""

	return dppr.client.Patch(ctx, &node, client.MergeFrom(nodeCopy))
}

func (dppr *DevicePluginPodReconciler) deleteFinalizer(ctx context.Context, pod *v1.Pod) error {
	podCopy := pod.DeepCopy()

	controllerutil.RemoveFinalizer(pod, constants.NodeLabelerFinalizer)

	return dppr.client.Patch(ctx, pod, client.MergeFrom(podCopy))
}

func (dppr *DevicePluginPodReconciler) deleteLabel(ctx context.Context, nodeName string, labelName string) error {
	node := v1.Node{}

	if err := dppr.client.Get(ctx, types.NamespacedName{Name: nodeName}, &node); err != nil {
		return fmt.Errorf("could not get node %s: %v", nodeName, err)
	}

	nodeCopy := node.DeepCopy()

	delete(node.Labels, labelName)

	return dppr.client.Patch(ctx, &node, client.MergeFrom(nodeCopy))
}
