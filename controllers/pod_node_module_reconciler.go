package controllers

import (
	"context"
	"fmt"

	"github.com/qbarrand/oot-operator/internal/constants"
	"github.com/qbarrand/oot-operator/internal/daemonset"
	"github.com/qbarrand/oot-operator/internal/filter"
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

type PodNodeModuleReconciler struct {
	client client.Client
}

func NewPodNodeModuleReconciler(client client.Client) *PodNodeModuleReconciler {
	return &PodNodeModuleReconciler{client: client}
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

	labelName := daemonset.GetDriverContainerNodeLabel(moduleName)

	logger = logger.WithValues(
		"node name", nodeName,
		"module name", moduleName,
		"label name", labelName,
	)

	if !podutils.IsPodReady(&pod) {
		logger.Info("Unlabeling node")

		if err := pnmr.deleteLabel(ctx, nodeName, labelName); err != nil {
			return ctrl.Result{}, fmt.Errorf("could not unlabel node %s: %v", nodeName, err)
		}

		if !pod.DeletionTimestamp.IsZero() {
			logger.Info("Pod deletion requested; removing finalizer")

			if err := pnmr.deleteFinalizer(ctx, &pod); err != nil {
				return ctrl.Result{}, fmt.Errorf("could not delete the pod finalizer: %v", err)
			}
		}

		return ctrl.Result{}, nil
	}

	logger.Info("Labeling node")

	if err := pnmr.addLabel(ctx, nodeName, labelName); err != nil {
		return ctrl.Result{}, fmt.Errorf("could not label node %s with %q: %v", nodeName, labelName, err)
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (pnmr *PodNodeModuleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	p := predicate.And(
		filter.PodReadinessChangedPredicate(
			mgr.GetLogger().WithName("pod-readiness-changed"),
		),
		filter.HasLabel(constants.ModuleNameLabel),
		filter.PodHasSpecNodeName(),
	)

	return ctrl.
		NewControllerManagedBy(mgr).
		Named("pod-node-module").
		For(&v1.Pod{}).
		WithEventFilter(p).
		Complete(pnmr)
}

func (pnmr *PodNodeModuleReconciler) addLabel(ctx context.Context, nodeName, labelName string) error {
	node := v1.Node{}

	if err := pnmr.client.Get(ctx, types.NamespacedName{Name: nodeName}, &node); err != nil {
		return fmt.Errorf("could not get node %s: %v", nodeName, err)
	}

	nodeCopy := node.DeepCopy()

	if node.Labels == nil {
		node.Labels = make(map[string]string, 1)
	}

	node.Labels[labelName] = ""

	return pnmr.client.Patch(ctx, &node, client.MergeFrom(nodeCopy))
}

func (pnmr *PodNodeModuleReconciler) deleteFinalizer(ctx context.Context, pod *v1.Pod) error {
	podCopy := pod.DeepCopy()

	controllerutil.RemoveFinalizer(pod, constants.NodeLabelerFinalizer)

	return pnmr.client.Patch(ctx, pod, client.MergeFrom(podCopy))
}

func (pnmr *PodNodeModuleReconciler) deleteLabel(ctx context.Context, nodeName, labelName string) error {
	node := v1.Node{}

	if err := pnmr.client.Get(ctx, types.NamespacedName{Name: nodeName}, &node); err != nil {
		return fmt.Errorf("could not get node %s: %v", nodeName, err)
	}

	nodeCopy := node.DeepCopy()

	delete(node.Labels, labelName)

	return pnmr.client.Patch(ctx, &node, client.MergeFrom(nodeCopy))
}
