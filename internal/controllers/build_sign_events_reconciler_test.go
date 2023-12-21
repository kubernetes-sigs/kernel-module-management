package controllers

import (
	"context"
	"errors"

	"github.com/budougumi0617/cmpmock"
	"github.com/kubernetes-sigs/kernel-module-management/api-hub/v1beta1"
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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func getOwnerReferenceFromObject(obj ctrlclient.Object) metav1.OwnerReference {
	GinkgoHelper()

	uo := &unstructured.Unstructured{}
	uo.SetNamespace(obj.GetNamespace())

	Expect(
		controllerutil.SetOwnerReference(obj, uo, scheme),
	).NotTo(
		HaveOccurred(),
	)

	return uo.GetOwnerReferences()[0]
}

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

var (
	ownerModule = &kmmv1beta1.Module{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "module-name",
			Namespace: namespace,
		},
	}
	ownerPreflight = &kmmv1beta1.PreflightValidation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "preflight-name",
			Namespace: namespace,
		},
	}
	ownerMCM = &v1beta1.ManagedClusterModule{
		ObjectMeta: metav1.ObjectMeta{Name: "mcm"},
	}
)

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
		mockHelper   *MockJobEventReconcilerHelper
		podNSN       = types.NamespacedName{Namespace: namespace, Name: "name"}
		r            *JobEventReconciler
	)

	BeforeEach(func() {
		ctrl := gomock.NewController(GinkgoT())
		fakeRecorder = record.NewFakeRecorder(2)
		mockClient = testclient.NewMockClient(ctrl)
		mockHelper = NewMockJobEventReconcilerHelper(ctrl)
		r = NewBuildSignEventsReconciler(mockClient, mockHelper, fakeRecorder)
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
		or := getOwnerReferenceFromObject(ownerModule)

		pod := &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{createdAnnotationKey: ""},
				Labels: map[string]string{
					constants.PodType:            utils.PodTypeBuild,
					constants.TargetKernelTarget: kernelVersion,
				},
				OwnerReferences: []metav1.OwnerReference{or},
				Namespace:       namespace,
			},
		}

		gomock.InOrder(
			mockClient.
				EXPECT().
				Get(ctx, podNSN, &v1.Pod{}).
				Do(func(_ context.Context, _ types.NamespacedName, p *v1.Pod, _ ...ctrlclient.GetOption) {
					meta.SetLabel(p, constants.PodType, utils.PodTypeBuild)
					meta.SetLabel(p, constants.TargetKernelTarget, kernelVersion)
					p.Namespace = namespace
					p.OwnerReferences = []metav1.OwnerReference{or}
				}),
			mockHelper.EXPECT().GetOwner(ctx, or, namespace),
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
		or := getOwnerReferenceFromObject(ownerModule)

		gomock.InOrder(
			mockClient.
				EXPECT().
				Get(ctx, podNSN, &v1.Pod{}).
				Do(func(_ context.Context, _ types.NamespacedName, p *v1.Pod, _ ...ctrlclient.GetOption) {
					meta.SetLabel(p, constants.PodType, utils.PodTypeBuild)
					meta.SetAnnotation(p, createdAnnotationKey, "")
					p.Namespace = namespace
					p.OwnerReferences = []metav1.OwnerReference{or}
				}),
			mockHelper.EXPECT().GetOwner(ctx, or, namespace),
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
		or := getOwnerReferenceFromObject(ownerModule)

		pod := &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Labels:          map[string]string{constants.PodType: utils.PodTypeBuild},
				Finalizers:      make([]string, 0),
				Namespace:       namespace,
				OwnerReferences: []metav1.OwnerReference{or},
			},
		}

		gomock.InOrder(
			mockClient.
				EXPECT().
				Get(ctx, podNSN, &v1.Pod{}).
				Do(func(_ context.Context, _ types.NamespacedName, p *v1.Pod, _ ...ctrlclient.GetOption) {
					meta.SetLabel(p, constants.PodType, utils.PodTypeBuild)
					controllerutil.AddFinalizer(p, constants.JobEventFinalizer)
					p.Namespace = namespace
					p.OwnerReferences = []metav1.OwnerReference{or}
				}),
			mockHelper.
				EXPECT().
				GetOwner(ctx, or, namespace).
				Return(
					nil,
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
		func(jobType string, phase v1.PodPhase, sendEventAndRemoveFinalizer bool, substring string, owner ctrlclient.Object) {
			or := getOwnerReferenceFromObject(owner)

			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{createdAnnotationKey: ""},
					Labels: map[string]string{
						constants.PodType:            jobType,
						constants.TargetKernelTarget: kernelVersion,
					},
					Finalizers:      make([]string, 0),
					Namespace:       namespace,
					OwnerReferences: []metav1.OwnerReference{or},
				},
				Status: v1.PodStatus{Phase: phase},
			}

			calls := []any{
				mockClient.
					EXPECT().
					Get(ctx, podNSN, &v1.Pod{}).
					Do(func(_ context.Context, _ types.NamespacedName, p *v1.Pod, _ ...ctrlclient.GetOption) {
						p.Namespace = namespace

						meta.SetAnnotation(p, createdAnnotationKey, "")
						meta.SetLabel(p, constants.PodType, jobType)
						meta.SetLabel(p, constants.TargetKernelTarget, kernelVersion)
						controllerutil.AddFinalizer(p, constants.JobEventFinalizer)
						p.OwnerReferences = []metav1.OwnerReference{or}
						p.Status.Phase = phase
					}),
				mockHelper.EXPECT().GetOwner(ctx, or, namespace),
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
		Entry(nil, "test", v1.PodRunning, false, "", ownerModule),
		Entry(nil, "test", v1.PodPending, false, "", ownerModule),
		Entry(nil, "build", v1.PodFailed, true, "Warning BuildFailed Build job failed for kernel "+kernelVersion, ownerModule),
		Entry(nil, "sign", v1.PodSucceeded, true, "Normal SignSucceeded Sign job succeeded for kernel "+kernelVersion, ownerModule),
		Entry(nil, "build", v1.PodFailed, true, "Warning BuildFailed Build job failed for kernel "+kernelVersion, ownerPreflight),
		Entry(nil, "sign", v1.PodSucceeded, true, "Normal SignSucceeded Sign job succeeded for kernel "+kernelVersion, ownerPreflight),
		Entry(nil, "build", v1.PodFailed, true, "Warning BuildFailed Build job failed for kernel "+kernelVersion, ownerMCM),
		Entry(nil, "sign", v1.PodSucceeded, true, "Normal SignSucceeded Sign job succeeded for kernel "+kernelVersion, ownerMCM),
		Entry(nil, "random", v1.PodFailed, true, "Warning RandomFailed Random job failed for kernel "+kernelVersion, ownerModule),
		Entry(nil, "random", v1.PodSucceeded, true, "Normal RandomSucceeded Random job succeeded for kernel "+kernelVersion, ownerModule),
	)
})

var _ = Describe("jobEventReconcilerHelper_GetOwner", func() {
	var (
		ctx = context.TODO()

		mockClient *testclient.MockClient
		r          *jobEventReconcilerHelper
	)

	BeforeEach(func() {
		ctrl := gomock.NewController(GinkgoT())
		mockClient = testclient.NewMockClient(ctrl)
		r = &jobEventReconcilerHelper{client: mockClient}
	})

	DescribeTable(
		"should look for the right object",
		func(owner ctrlclient.Object, namespaced bool) {
			or := getOwnerReferenceFromObject(owner)

			nsn := types.NamespacedName{
				Name: owner.GetName(),
			}

			gvk, err := apiutil.GVKForObject(owner, scheme)
			Expect(err).NotTo(HaveOccurred())

			uo := &unstructured.Unstructured{}
			uo.SetGroupVersionKind(gvk)

			if namespaced {
				nsn.Namespace = namespace
			}

			gomock.InOrder(
				mockClient.EXPECT().IsObjectNamespaced(uo).Return(namespaced, nil),
				mockClient.EXPECT().Get(ctx, nsn, uo),
			)

			Expect(
				r.GetOwner(ctx, or, namespace),
			).To(
				Equal(uo),
			)
		},
		Entry(nil, ownerModule, true),
		Entry(nil, ownerPreflight, false),
		Entry(nil, ownerMCM, false),
	)

	It("should return an error wrapping the original one", func() {
		or := getOwnerReferenceFromObject(ownerModule)

		ownerName := ownerModule.GetName()

		nsn := types.NamespacedName{
			Name:      ownerName,
			Namespace: namespace,
		}

		gvk, err := apiutil.GVKForObject(ownerModule, scheme)
		Expect(err).NotTo(HaveOccurred())

		uo := &unstructured.Unstructured{}
		uo.SetGroupVersionKind(gvk)

		gomock.InOrder(
			mockClient.EXPECT().IsObjectNamespaced(uo).Return(true, nil),
			mockClient.EXPECT().Get(ctx, nsn, uo).Return(
				k8serrors.NewNotFound(schema.GroupResource{Group: gvk.Group, Resource: gvk.Kind}, ownerName),
			),
		)

		_, err = r.GetOwner(ctx, or, namespace)
		Expect(
			k8serrors.IsNotFound(err),
		).To(
			BeTrue(),
		)
	})
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
