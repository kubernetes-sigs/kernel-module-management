package buildsign

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/api"
	"github.com/kubernetes-sigs/kernel-module-management/internal/client"
	"github.com/kubernetes-sigs/kernel-module-management/internal/kernel"
)

var _ = Describe("GetStatus", func() {
	var (
		ctrl                *gomock.Controller
		clnt                *client.MockClient
		mockResourceManager *MockResourceManager
		mgr                 Manager
	)
	const (
		mbscName      = "some-name"
		imageName     = "image-name"
		mbscNamespace = "some-namespace"
		kernelVersion = "some version"
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		mockResourceManager = NewMockResourceManager(ctrl)
		mgr = NewManager(clnt, mockResourceManager, scheme)
	})

	ctx := context.Background()
	testMBSC := kmmv1beta1.ModuleBuildSignConfig{}

	It("failed flow, GetResourceByKernel fails", func() {
		normalizedKernel := kernel.NormalizeVersion(kernelVersion)
		mockResourceManager.EXPECT().GetResourceByKernel(ctx, mbscName, mbscNamespace, normalizedKernel,
			kmmv1beta1.BuildImage, &testMBSC).
			Return(nil, fmt.Errorf("some error"))

		status, err := mgr.GetStatus(ctx, mbscName, mbscNamespace, kernelVersion, kmmv1beta1.BuildImage, &testMBSC)
		Expect(err).To(HaveOccurred())
		Expect(status).To(Equal(kmmv1beta1.BuildOrSignStatus("")))
	})

	It("GetResourceByKernel returns pod does not exists", func() {
		normalizedKernel := kernel.NormalizeVersion(kernelVersion)
		mockResourceManager.EXPECT().GetResourceByKernel(ctx, mbscName, mbscNamespace, normalizedKernel,
			kmmv1beta1.BuildImage, &testMBSC).
			Return(nil, ErrNoMatchingBuildSignResource)

		status, err := mgr.GetStatus(ctx, mbscName, mbscNamespace, kernelVersion, kmmv1beta1.BuildImage, &testMBSC)
		Expect(err).To(BeNil())
		Expect(status).To(Equal(kmmv1beta1.BuildOrSignStatus("")))
	})

	It("failed flow, GetResourceStatus fails", func() {
		foundPod := v1.Pod{}
		normalizedKernel := kernel.NormalizeVersion(kernelVersion)
		gomock.InOrder(
			mockResourceManager.EXPECT().GetResourceByKernel(ctx, mbscName, mbscNamespace, normalizedKernel,
				kmmv1beta1.BuildImage, &testMBSC).
				Return(&foundPod, nil),
			mockResourceManager.EXPECT().GetResourceStatus(&foundPod).Return(Status(""), fmt.Errorf("some error")),
		)

		status, err := mgr.GetStatus(ctx, mbscName, mbscNamespace, kernelVersion, kmmv1beta1.BuildImage, &testMBSC)
		Expect(err).To(HaveOccurred())
		Expect(status).To(Equal(kmmv1beta1.BuildOrSignStatus("")))
	})

	DescribeTable("check good flow and returned statuses", func(podStatus Status, expectedStatus kmmv1beta1.BuildOrSignStatus) {
		foundPod := v1.Pod{}
		normalizedKernel := kernel.NormalizeVersion(kernelVersion)
		gomock.InOrder(
			mockResourceManager.EXPECT().GetResourceByKernel(ctx, mbscName, mbscNamespace, normalizedKernel,
				kmmv1beta1.BuildImage, &testMBSC).
				Return(&foundPod, nil),
			mockResourceManager.EXPECT().GetResourceStatus(&foundPod).Return(podStatus, nil),
		)
		status, err := mgr.GetStatus(ctx, mbscName, mbscNamespace, kernelVersion, kmmv1beta1.BuildImage, &testMBSC)
		Expect(err).To(BeNil())
		Expect(status).To(Equal(expectedStatus))
	},
		Entry("pod's status is success", StatusCompleted, kmmv1beta1.ActionSuccess),
		Entry("pod's status is failure", StatusFailed, kmmv1beta1.ActionFailure),
		Entry("pod's status is in progress", StatusInProgress, kmmv1beta1.BuildOrSignStatus("")),
		Entry("pod's status is in unknown", Status(""), kmmv1beta1.BuildOrSignStatus("")),
	)
})

var _ = Describe("Sync", func() {
	var (
		ctrl                *gomock.Controller
		clnt                *client.MockClient
		mockResourceManager *MockResourceManager
		mgr                 Manager
	)
	const (
		mbscName      = "some-name"
		imageName     = "image-name"
		mbscNamespace = "some-namespace"
		kernelVersion = "some version"
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		mockResourceManager = NewMockResourceManager(ctrl)
		mgr = NewManager(clnt, mockResourceManager, scheme)
	})

	ctx := context.Background()
	testMBSC := kmmv1beta1.ModuleBuildSignConfig{}
	testMLD := &api.ModuleLoaderData{
		Name:                    mbscName,
		Namespace:               mbscNamespace,
		KernelNormalizedVersion: kernelVersion,
	}

	It("MakePodTemplate failed", func() {
		By("test build action")
		mockResourceManager.EXPECT().MakeResourceTemplate(ctx, testMLD, &testMBSC, true, kmmv1beta1.BuildImage).
			Return(nil, fmt.Errorf("some error"))
		err := mgr.Sync(ctx, testMLD, true, kmmv1beta1.BuildImage, &testMBSC)
		Expect(err).To(HaveOccurred())

		By("test sign action")
		mockResourceManager.EXPECT().MakeResourceTemplate(ctx, testMLD, &testMBSC, true, kmmv1beta1.SignImage).
			Return(nil, fmt.Errorf("some error"))
		err = mgr.Sync(ctx, testMLD, true, kmmv1beta1.SignImage, &testMBSC)
		Expect(err).To(HaveOccurred())
	})

	It("GetResourceByKernel failed", func() {
		gomock.InOrder(
			mockResourceManager.EXPECT().MakeResourceTemplate(ctx, testMLD, &testMBSC, true, kmmv1beta1.BuildImage).
				Return(nil, nil),
			mockResourceManager.EXPECT().GetResourceByKernel(ctx, mbscName, mbscNamespace, kernelVersion,
				kmmv1beta1.BuildImage, &testMBSC).
				Return(nil, fmt.Errorf("some error")),
		)
		err := mgr.Sync(ctx, testMLD, true, kmmv1beta1.BuildImage, &testMBSC)
		Expect(err).To(HaveOccurred())
	})

	It("CreateResource failed", func() {
		testTemplate := v1.Pod{}
		gomock.InOrder(
			mockResourceManager.EXPECT().MakeResourceTemplate(ctx, testMLD, &testMBSC, true, kmmv1beta1.BuildImage).
				Return(&testTemplate, nil),
			mockResourceManager.EXPECT().GetResourceByKernel(ctx, mbscName, mbscNamespace, kernelVersion,
				kmmv1beta1.BuildImage, &testMBSC).
				Return(nil, ErrNoMatchingBuildSignResource),
			mockResourceManager.EXPECT().CreateResource(ctx, &testTemplate).Return(fmt.Errorf("some error")),
		)
		err := mgr.Sync(ctx, testMLD, true, kmmv1beta1.BuildImage, &testMBSC)
		Expect(err).To(HaveOccurred())
	})

	It("IsResourceChanged failed", func() {
		testTemplate := v1.Pod{}
		testPod := v1.Pod{}
		gomock.InOrder(
			mockResourceManager.EXPECT().MakeResourceTemplate(ctx, testMLD, &testMBSC, true, kmmv1beta1.BuildImage).
				Return(&testTemplate, nil),
			mockResourceManager.EXPECT().GetResourceByKernel(ctx, mbscName, mbscNamespace, kernelVersion,
				kmmv1beta1.BuildImage, &testMBSC).
				Return(&testPod, nil),
			mockResourceManager.EXPECT().IsResourceChanged(&testPod, &testTemplate).Return(false, fmt.Errorf("some error")),
		)
		err := mgr.Sync(ctx, testMLD, true, kmmv1beta1.BuildImage, &testMBSC)
		Expect(err).To(HaveOccurred())
	})

	It("DeleteResource failed should not cause failure", func() {
		testTemplate := v1.Pod{}
		testPod := v1.Pod{}
		gomock.InOrder(
			mockResourceManager.EXPECT().MakeResourceTemplate(ctx, testMLD, &testMBSC, true, kmmv1beta1.BuildImage).
				Return(&testTemplate, nil),
			mockResourceManager.EXPECT().GetResourceByKernel(ctx, mbscName, mbscNamespace, kernelVersion,
				kmmv1beta1.BuildImage, &testMBSC).
				Return(&testPod, nil),
			mockResourceManager.EXPECT().IsResourceChanged(&testPod, &testTemplate).Return(true, nil),
			mockResourceManager.EXPECT().DeleteResource(ctx, &testPod).Return(fmt.Errorf("some error")),
		)
		err := mgr.Sync(ctx, testMLD, true, kmmv1beta1.BuildImage, &testMBSC)
		Expect(err).To(BeNil())
	})

	DescribeTable("check good flow", func(buildAction, podExists, podChanged, pushImage bool) {
		testPodTemplate := v1.Pod{}
		existingTestPod := v1.Pod{}
		testAction := kmmv1beta1.BuildImage
		if !buildAction {
			testAction = kmmv1beta1.SignImage
		}

		if buildAction {
			mockResourceManager.EXPECT().MakeResourceTemplate(ctx, testMLD, &testMBSC, pushImage, kmmv1beta1.BuildImage).
				Return(&testPodTemplate, nil)
		} else {
			mockResourceManager.EXPECT().MakeResourceTemplate(ctx, testMLD, &testMBSC, pushImage, kmmv1beta1.SignImage).
				Return(&testPodTemplate, nil)
		}
		var getPodError error
		if !podExists {
			getPodError = ErrNoMatchingBuildSignResource
		}
		mockResourceManager.EXPECT().GetResourceByKernel(ctx, mbscName, mbscNamespace, kernelVersion,
			testAction, &testMBSC).Return(&existingTestPod, getPodError)
		if !podExists {
			mockResourceManager.EXPECT().CreateResource(ctx, &testPodTemplate).Return(nil)
			goto executeTestFunction
		}
		mockResourceManager.EXPECT().IsResourceChanged(&existingTestPod, &testPodTemplate).Return(podChanged, nil)
		if podChanged {
			mockResourceManager.EXPECT().DeleteResource(ctx, &existingTestPod).Return(nil)
		}

	executeTestFunction:
		err := mgr.Sync(ctx, testMLD, pushImage, testAction, &testMBSC)
		Expect(err).To(BeNil())
	},
		Entry("action build, build pod does not exists", true, false, false, true),
		Entry("action sign, sign pod does not exists", false, false, false, false),
		Entry("action build, build pod exists, pod has not changed", true, true, false, true),
		Entry("action sign, sign pod exists, pod has not changed", false, true, false, false),
		Entry("action build, build pod exists, pod has changed", true, true, true, false),
		Entry("action sign, sign pod exists, pod has changed", false, true, true, false),
	)
})

var _ = Describe("GarbageCollect", func() {
	var (
		ctrl                *gomock.Controller
		clnt                *client.MockClient
		mockResourceManager *MockResourceManager
		mgr                 Manager
	)
	const (
		mbscName      = "some-name"
		imageName     = "image-name"
		mbscNamespace = "some-namespace"
		kernelVersion = "some version"
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		mockResourceManager = NewMockResourceManager(ctrl)
		mgr = NewManager(clnt, mockResourceManager, scheme)
	})

	ctx := context.Background()
	testMBSC := kmmv1beta1.ModuleBuildSignConfig{}

	It("failed to get module pods", func() {
		mockResourceManager.EXPECT().GetModuleResources(ctx, mbscName, mbscNamespace,
			kmmv1beta1.BuildImage, &testMBSC).Return(nil, fmt.Errorf("some error"))

		_, err := mgr.GarbageCollect(ctx, mbscName, mbscNamespace, kmmv1beta1.BuildImage, &testMBSC)
		Expect(err).To(HaveOccurred())
	})

	It("delete pod failed", func() {
		testPod := &v1.Pod{
			Status: v1.PodStatus{
				Phase: v1.PodSucceeded,
			},
		}
		gomock.InOrder(
			mockResourceManager.EXPECT().GetModuleResources(ctx, mbscName, mbscNamespace, kmmv1beta1.SignImage, &testMBSC).
				Return([]metav1.Object{testPod}, nil),
			mockResourceManager.EXPECT().HasResourcesCompletedSuccessfully(ctx, testPod).Return(true, nil),
			mockResourceManager.EXPECT().DeleteResource(ctx, testPod).Return(fmt.Errorf("some error")),
		)

		_, err := mgr.GarbageCollect(ctx, mbscName, mbscNamespace, kmmv1beta1.SignImage, &testMBSC)
		Expect(err).To(HaveOccurred())
	})

	It("good flow", func() {
		testPodSuccess := &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "podSuccess"},
			Status:     v1.PodStatus{Phase: v1.PodSucceeded},
		}
		testPodFailure := &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "podFailure"},
			Status:     v1.PodStatus{Phase: v1.PodFailed},
		}
		testPodRunning := &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "podRunning"},
			Status:     v1.PodStatus{Phase: v1.PodRunning},
		}
		testPodUnknown := &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "podUnknown"},
			Status:     v1.PodStatus{Phase: v1.PodUnknown},
		}
		testPodPending := &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "podPending"},
			Status:     v1.PodStatus{Phase: v1.PodPending},
		}
		returnedPods := []metav1.Object{testPodSuccess, testPodFailure, testPodRunning, testPodUnknown, testPodPending}
		gomock.InOrder(
			mockResourceManager.EXPECT().GetModuleResources(ctx, mbscName, mbscNamespace, kmmv1beta1.BuildImage, &testMBSC).
				Return(returnedPods, nil),
			mockResourceManager.EXPECT().HasResourcesCompletedSuccessfully(ctx, testPodSuccess).Return(true, nil),
			mockResourceManager.EXPECT().DeleteResource(ctx, testPodSuccess).Return(nil),
			mockResourceManager.EXPECT().HasResourcesCompletedSuccessfully(ctx, testPodFailure).Return(false, nil),
			mockResourceManager.EXPECT().HasResourcesCompletedSuccessfully(ctx, testPodRunning).Return(false, nil),
			mockResourceManager.EXPECT().HasResourcesCompletedSuccessfully(ctx, testPodUnknown).Return(false, nil),
			mockResourceManager.EXPECT().HasResourcesCompletedSuccessfully(ctx, testPodPending).Return(false, nil),
		)

		res, err := mgr.GarbageCollect(ctx, mbscName, mbscNamespace, kmmv1beta1.BuildImage, &testMBSC)
		Expect(err).To(BeNil())
		Expect(res).To(Equal([]string{"podSuccess"}))
	})
})
