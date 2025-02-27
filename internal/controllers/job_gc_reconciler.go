package controllers

import (
	"context"
	"time"

	"github.com/kubernetes-sigs/kernel-module-management/internal/buildsign/pod"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const JobGCReconcilerName = "JobGCReconciler"

// JobGCReconciler removes the GC finalizer from deleted build & signing pods, after the optional GC delay has passed
// or if the pod has failed.
type JobGCReconciler struct {
	client client.Client
	delay  time.Duration
}

func NewJobGCReconciler(client client.Client, delay time.Duration) *JobGCReconciler {
	return &JobGCReconciler{
		client: client,
		delay:  delay,
	}
}

func (r *JobGCReconciler) Reconcile(ctx context.Context, pod *v1.Pod) (reconcile.Result, error) {
	logger := ctrl.LoggerFrom(ctx)

	releaseAt := pod.DeletionTimestamp.Add(r.delay)
	now := time.Now()

	// Only delay the deletion of successful pods.
	if pod.Status.Phase != v1.PodSucceeded || now.After(releaseAt) {
		logger.Info("Releasing finalizer")

		podCopy := pod.DeepCopy()

		controllerutil.RemoveFinalizer(pod, constants.GCDelayFinalizer)

		return reconcile.Result{}, r.client.Patch(ctx, pod, client.MergeFrom(podCopy))
	}

	requeueAfter := releaseAt.Sub(now)

	logger.Info("Not yet removing finalizer", "requeue after", requeueAfter)

	return reconcile.Result{RequeueAfter: requeueAfter}, nil
}

func (r *JobGCReconciler) SetupWithManager(mgr manager.Manager) error {
	podTypes := sets.New(pod.PodTypeBuild, pod.PodTypeSign)

	p := predicate.NewPredicateFuncs(func(object client.Object) bool {
		return podTypes.Has(
			object.GetLabels()[constants.PodType],
		) &&
			controllerutil.ContainsFinalizer(object, constants.GCDelayFinalizer) &&
			object.GetDeletionTimestamp() != nil
	})

	return ctrl.NewControllerManagedBy(mgr).
		For(
			&v1.Pod{},
			builder.WithPredicates(p),
		).
		Named(JobGCReconcilerName).
		Complete(
			reconcile.AsReconciler[*v1.Pod](r.client, r),
		)
}
