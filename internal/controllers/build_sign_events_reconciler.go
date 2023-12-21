package controllers

import (
	"context"
	"errors"
	"fmt"

	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	"github.com/kubernetes-sigs/kernel-module-management/internal/meta"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
	"golang.org/x/exp/maps"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	BuildSignEventsReconcilerName = "BuildSignEvents"

	createdAnnotationKey = "kmm.node.kubernetes.io/created-event-sent"
)

type jobEvent struct {
	jobType string
}

func (b *jobEvent) ReasonCreated() string {
	return b.jobType + "Created"
}

func (b *jobEvent) ReasonFailed() string {
	return b.jobType + "Failed"
}

func (b *jobEvent) ReasonSucceeded() string {
	return b.jobType + "Succeeded"
}

func (b *jobEvent) String() string {
	return b.jobType
}

var titler = cases.Title(language.English)

func newJobEvent(jobType string) (*jobEvent, error) {
	if jobType == "" {
		return nil, errors.New("jobType cannot be empty")
	}

	je := &jobEvent{
		jobType: titler.String(jobType),
	}

	return je, nil
}

type JobEventReconciler struct {
	client   client.Client
	helper   JobEventReconcilerHelper
	recorder record.EventRecorder
}

func NewBuildSignEventsReconciler(client client.Client, helper JobEventReconcilerHelper, eventRecorder record.EventRecorder) *JobEventReconciler {
	return &JobEventReconciler{
		client:   client,
		helper:   helper,
		recorder: eventRecorder,
	}
}

func (r *JobEventReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	logger := ctrl.LoggerFrom(ctx)

	pod := v1.Pod{}

	if err := r.client.Get(ctx, req.NamespacedName, &pod); err != nil {
		if k8serrors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}

		return reconcile.Result{}, fmt.Errorf("could not get pod %s: %v", req.NamespacedName, err)
	}

	je, err := newJobEvent(pod.Labels[constants.PodType])
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("could not parse job type: %v", err)
	}

	kernelVersion := pod.Labels[constants.TargetKernelTarget]

	if nor := len(pod.OwnerReferences); nor != 1 {
		return ctrl.Result{}, fmt.Errorf("unexpected number of owner references: expected 1, got %d", nor)
	}

	owner, err := r.helper.GetOwner(ctx, pod.OwnerReferences[0], req.Namespace)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			logger.Info("Job owner not found; removing finalizer")
			return ctrl.Result{}, r.removeFinalizer(ctx, &pod)
		}

		return ctrl.Result{}, err
	}

	eventAnnotations := map[string]string{
		"kernel-version": kernelVersion,
		"pod-name":       pod.Name,
	}

	if _, ok := pod.GetAnnotations()[createdAnnotationKey]; !ok {
		patchFrom := client.MergeFrom(pod.DeepCopy())

		meta.SetAnnotation(&pod, createdAnnotationKey, "")

		if err = r.client.Patch(ctx, &pod, patchFrom); err != nil {
			return ctrl.Result{}, fmt.Errorf("could not patch Pod %s: %v", req.NamespacedName, err)
		}

		ann := maps.Clone(eventAnnotations)
		ann["pod-creation-timestamp"] = pod.CreationTimestamp.String()

		r.recorder.AnnotatedEventf(
			owner,
			ann,
			v1.EventTypeNormal,
			je.ReasonCreated(),
			"%s created for kernel %s",
			je,
			kernelVersion,
		)
	}

	var eventType, fmtString, reason string

	switch pod.Status.Phase {
	case v1.PodFailed:
		eventType = v1.EventTypeWarning
		fmtString = "%s job failed for kernel %s"
		reason = je.ReasonFailed()
	case v1.PodSucceeded:
		eventType = v1.EventTypeNormal
		fmtString = "%s job succeeded for kernel %s"
		reason = je.ReasonSucceeded()
	default:
		// still running, nothing to do
		return ctrl.Result{}, nil
	}

	if err = r.removeFinalizer(ctx, &pod); err != nil {
		return reconcile.Result{}, fmt.Errorf("could not patch pod %s: %v", req.NamespacedName, err)
	}

	r.recorder.AnnotatedEventf(
		owner,
		eventAnnotations,
		eventType,
		reason,
		fmtString,
		je.String(),
		kernelVersion,
	)

	return ctrl.Result{}, nil
}

var jobEventPredicate = predicate.NewPredicateFuncs(func(obj client.Object) bool {
	label := obj.GetLabels()[constants.PodType]

	return (label == utils.PodTypeBuild || label == utils.PodTypeSign) &&
		controllerutil.ContainsFinalizer(obj, constants.JobEventFinalizer)
})

func (r *JobEventReconciler) SetupWithManager(mgr manager.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(
			&v1.Pod{},
			builder.WithPredicates(jobEventPredicate),
		).
		Named(BuildSignEventsReconcilerName).
		Complete(r)
}

func (r *JobEventReconciler) removeFinalizer(ctx context.Context, pod *v1.Pod) error {
	if controllerutil.ContainsFinalizer(pod, constants.JobEventFinalizer) {
		patchFrom := client.MergeFrom(pod.DeepCopy())

		controllerutil.RemoveFinalizer(pod, constants.JobEventFinalizer)

		if err := r.client.Patch(ctx, pod, patchFrom); err != nil {
			return fmt.Errorf("patch failed: %v", err)
		}
	}

	return nil
}

//go:generate mockgen -source=build_sign_events_reconciler.go -package=controllers -destination=mock_build_sign_events_reconciler.go JobEventReconcilerHelper

type JobEventReconcilerHelper interface {
	GetOwner(context.Context, metav1.OwnerReference, string) (client.Object, error)
}

type jobEventReconcilerHelper struct {
	client client.Client
}

func NewJobEventReconcilerHelper(client client.Client) JobEventReconcilerHelper {
	return &jobEventReconcilerHelper{client: client}
}

func (h *jobEventReconcilerHelper) GetOwner(ctx context.Context, ref metav1.OwnerReference, namespace string) (client.Object, error) {
	owner := &unstructured.Unstructured{}
	owner.SetKind(ref.Kind)
	owner.SetAPIVersion(ref.APIVersion)
	owner.SetUID(ref.UID)

	ownerNSN := types.NamespacedName{Name: ref.Name}

	namespaced, err := h.client.IsObjectNamespaced(owner)
	if err != nil {
		return nil, fmt.Errorf("could not determine if object %s is namespaced: %v", owner, err)
	}

	if namespaced {
		ownerNSN.Namespace = namespace
	}

	if err = h.client.Get(ctx, ownerNSN, owner); err != nil {
		return nil, fmt.Errorf("could not get owner with kind %s and name %s: %w", owner.GetKind(), ownerNSN, err)
	}

	return owner, nil
}
