package controllers

import (
	"context"
	"errors"

	"github.com/budougumi0617/cmpmock"
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	testclient "github.com/kubernetes-sigs/kernel-module-management/internal/client"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	"github.com/kubernetes-sigs/kernel-module-management/internal/meta"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func mustJobEvent(jobType string) *jobEvent {
	GinkgoHelper()

	je, err := newJobEvent(jobType)
	Expect(err).NotTo(HaveOccurred())

	return je
}

var _ = Describe("jobEvent_ReasonCreated", func() {
	It("should work as expected", func() {
		Expect(
			mustJobEvent("build").ReasonCreated(),
		).To(
			Equal("BuildCreated"),
		)
	})
})

var _ = Describe("jobEvent_ReasonFailed", func() {
	It("should work as expected", func() {
		Expect(
			mustJobEvent("build").ReasonFailed(),
		).To(
			Equal("BuildFailed"),
		)
	})
})

var _ = Describe("jobEvent_ReasonSucceeded", func() {
	It("should work as expected", func() {
		Expect(
			mustJobEvent("build").ReasonSucceeded(),
		).To(
			Equal("BuildSucceeded"),
		)
	})
})

var _ = Describe("jobEvent_String", func() {
	DescribeTable(
		"should capitalize correctly",
		func(jobType string, expectedError bool, expectedString string) {
			je, err := newJobEvent(jobType)

			if expectedError {
				Expect(err).To(HaveOccurred())
				return
			}

			Expect(je.String()).To(Equal(expectedString))
		},
		Entry(nil, "", true, ""),
		Entry(nil, "sign", false, "Sign"),
		Entry(nil, "build", false, "Build"),
		Entry(nil, "anything", false, "Anything"),
		Entry(nil, "with-hyphen", false, "With-Hyphen"),
	)
})

var _ = Describe("JobEventReconciler_Reconcile", func() {
	const (
		kernelVersion = "1.2.3"
		moduleName    = "module-name"
		namespace     = "namespace"
	)

	var (
		ctx = context.TODO()

		fakeRecorder *record.FakeRecorder
		mockClient   *testclient.MockClient
		modNSN       = types.NamespacedName{Namespace: namespace, Name: moduleName}
		podNSN       = types.NamespacedName{Namespace: namespace, Name: "name"}
		r            *JobEventReconciler
	)

	BeforeEach(func() {
		ctrl := gomock.NewController(GinkgoT())
		fakeRecorder = record.NewFakeRecorder(2)
		mockClient = testclient.NewMockClient(ctrl)
		r = NewBuildSignEventsReconciler(mockClient, fakeRecorder)
	})

	closeAndGetAllEvents := func(events chan string) []string {
		GinkgoHelper()

		close(events)

		elems := make([]string, 0)

		for s := range events {
			elems = append(elems, s)
		}

		return elems
	}

	It("should return no error if the Pod could not be found", func() {
		mockClient.
			EXPECT().
			Get(ctx, podNSN, &v1.Pod{}).
			Return(
				k8serrors.NewNotFound(schema.GroupResource{Resource: "pods"}, podNSN.Name),
			)

		Expect(
			r.Reconcile(ctx, reconcile.Request{NamespacedName: podNSN}),
		).To(
			Equal(reconcile.Result{}),
		)
	})

	It("should return no error if we cannot get the Pod for a random reason", func() {
		mockClient.
			EXPECT().
			Get(ctx, podNSN, &v1.Pod{}).
			Return(
				errors.New("random error"),
			)

		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: podNSN})
		Expect(err).To(HaveOccurred())
	})

	It("should add the annotation and send the created event", func() {
		pod := &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{createdAnnotationKey: ""},
				Labels: map[string]string{
					constants.PodType:            utils.PodTypeBuild,
					constants.ModuleNameLabel:    moduleName,
					constants.TargetKernelTarget: kernelVersion,
				},
			},
		}

		gomock.InOrder(
			mockClient.
				EXPECT().
				Get(ctx, podNSN, &v1.Pod{}).
				Do(func(_ context.Context, _ types.NamespacedName, p *v1.Pod, _ ...ctrlclient.GetOption) {
					meta.SetLabel(p, constants.PodType, utils.PodTypeBuild)
					meta.SetLabel(p, constants.ModuleNameLabel, moduleName)
					meta.SetLabel(p, constants.TargetKernelTarget, kernelVersion)
				}),
			mockClient.
				EXPECT().
				Get(ctx, modNSN, &kmmv1beta1.Module{}),
			mockClient.EXPECT().Patch(ctx, cmpmock.DiffEq(pod), gomock.Any()),
		)

		Expect(
			r.Reconcile(ctx, reconcile.Request{NamespacedName: podNSN}),
		).To(
			Equal(ctrl.Result{}),
		)

		events := closeAndGetAllEvents(fakeRecorder.Events)
		Expect(events).To(HaveLen(1))
		Expect(events[0]).To(ContainSubstring("Normal BuildCreated Build created for kernel " + kernelVersion))
	})

	It("should do nothing if the annotation is already there and the pod is still running", func() {
		gomock.InOrder(
			mockClient.
				EXPECT().
				Get(ctx, podNSN, &v1.Pod{}).
				Do(func(_ context.Context, _ types.NamespacedName, p *v1.Pod, _ ...ctrlclient.GetOption) {
					meta.SetLabel(p, constants.PodType, utils.PodTypeBuild)
					meta.SetLabel(p, constants.ModuleNameLabel, moduleName)
					meta.SetAnnotation(p, createdAnnotationKey, "")
				}),
			mockClient.
				EXPECT().
				Get(ctx, modNSN, &kmmv1beta1.Module{}),
		)

		Expect(
			r.Reconcile(ctx, reconcile.Request{NamespacedName: podNSN}),
		).To(
			Equal(ctrl.Result{}),
		)

		events := closeAndGetAllEvents(fakeRecorder.Events)
		Expect(events).To(BeEmpty())
	})

	It("should just remove the finalizer if the module could not be found", func() {
		pod := &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					constants.PodType:         utils.PodTypeBuild,
					constants.ModuleNameLabel: moduleName,
				},
				Finalizers: make([]string, 0),
			},
		}

		gomock.InOrder(
			mockClient.
				EXPECT().
				Get(ctx, podNSN, &v1.Pod{}).
				Do(func(_ context.Context, _ types.NamespacedName, p *v1.Pod, _ ...ctrlclient.GetOption) {
					meta.SetLabel(p, constants.PodType, utils.PodTypeBuild)
					meta.SetLabel(p, constants.ModuleNameLabel, moduleName)
					controllerutil.AddFinalizer(p, constants.JobEventFinalizer)
				}),
			mockClient.
				EXPECT().
				Get(ctx, modNSN, &kmmv1beta1.Module{}).
				Return(
					k8serrors.NewNotFound(schema.GroupResource{}, moduleName),
				),
			mockClient.EXPECT().Patch(ctx, cmpmock.DiffEq(pod), gomock.Any()),
		)

		Expect(
			r.Reconcile(ctx, reconcile.Request{NamespacedName: podNSN}),
		).To(
			Equal(ctrl.Result{}),
		)

		events := closeAndGetAllEvents(fakeRecorder.Events)
		Expect(events).To(BeEmpty())
	})

	DescribeTable(
		"should send the event for terminated pods",
		func(jobType string, phase v1.PodPhase, sendEventAndRemoveFinalizer bool, substring string) {
			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{createdAnnotationKey: ""},
					Labels: map[string]string{
						constants.PodType:            jobType,
						constants.ModuleNameLabel:    moduleName,
						constants.TargetKernelTarget: kernelVersion,
					},
					Finalizers: make([]string, 0),
				},
				Status: v1.PodStatus{Phase: phase},
			}

			calls := []any{
				mockClient.
					EXPECT().
					Get(ctx, podNSN, &v1.Pod{}).
					Do(func(_ context.Context, _ types.NamespacedName, p *v1.Pod, _ ...ctrlclient.GetOption) {
						meta.SetAnnotation(p, createdAnnotationKey, "")
						meta.SetLabel(p, constants.PodType, jobType)
						meta.SetLabel(p, constants.ModuleNameLabel, moduleName)
						meta.SetLabel(p, constants.TargetKernelTarget, kernelVersion)
						controllerutil.AddFinalizer(p, constants.JobEventFinalizer)
						p.Status.Phase = phase
					}),
				mockClient.
					EXPECT().
					Get(ctx, modNSN, &kmmv1beta1.Module{}),
			}

			if sendEventAndRemoveFinalizer {
				calls = append(
					calls,
					mockClient.EXPECT().Patch(ctx, cmpmock.DiffEq(pod), gomock.Any()),
				)
			}

			gomock.InOrder(calls...)

			Expect(
				r.Reconcile(ctx, reconcile.Request{NamespacedName: podNSN}),
			).To(
				Equal(ctrl.Result{}),
			)

			events := closeAndGetAllEvents(fakeRecorder.Events)

			if !sendEventAndRemoveFinalizer {
				Expect(events).To(HaveLen(0))
				return
			}

			Expect(events).To(HaveLen(1))
			Expect(events[0]).To(ContainSubstring(substring))
		},
		Entry(nil, "test", v1.PodRunning, false, ""),
		Entry(nil, "test", v1.PodPending, false, ""),
		Entry(nil, "build", v1.PodFailed, true, "Warning BuildFailed Build job failed for kernel "+kernelVersion),
		Entry(nil, "sign", v1.PodSucceeded, true, "Normal SignSucceeded Sign job succeeded for kernel "+kernelVersion),
		Entry(nil, "random", v1.PodFailed, true, "Warning RandomFailed Random job failed for kernel "+kernelVersion),
		Entry(nil, "random", v1.PodSucceeded, true, "Normal RandomSucceeded Random job succeeded for kernel "+kernelVersion),
	)
})

var _ = Describe("jobEventPredicate", func() {
	DescribeTable(
		"should work as expected",
		func(podType string, hasFinalizer, expectedResult bool) {
			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{constants.PodType: podType},
				},
			}

			if hasFinalizer {
				controllerutil.AddFinalizer(pod, constants.JobEventFinalizer)
			}

			Expect(
				jobEventPredicate.Create(event.CreateEvent{Object: pod}),
			).To(
				Equal(expectedResult),
			)

			Expect(
				jobEventPredicate.Delete(event.DeleteEvent{Object: pod}),
			).To(
				Equal(expectedResult),
			)

			Expect(
				jobEventPredicate.Generic(event.GenericEvent{Object: pod}),
			).To(
				Equal(expectedResult),
			)

			Expect(
				jobEventPredicate.Update(event.UpdateEvent{ObjectNew: pod}),
			).To(
				Equal(expectedResult),
			)
		},
		Entry(nil, utils.PodTypeBuild, true, true),
		Entry(nil, utils.PodTypeSign, true, true),
		Entry(nil, "random", true, false),
		Entry(nil, "", true, false),
		Entry("finalizer", utils.PodTypeBuild, true, true),
		Entry("no finalizer", utils.PodTypeBuild, false, false),
	)
})
