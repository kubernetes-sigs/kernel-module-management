package pod

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
	buildsign "github.com/kubernetes-sigs/kernel-module-management/internal/buildsign"
	"github.com/kubernetes-sigs/kernel-module-management/internal/client"
	"github.com/kubernetes-sigs/kernel-module-management/internal/kernel"
)

var _ = Describe("GetStatus", func() {
	var (
		ctrl                    *gomock.Controller
		clnt                    *client.MockClient
		mockMaker               *MockMaker
		mockSigner              *MockSigner
		mockBuildSignPodManager *MockBuildSignPodManager
		mgr                     buildsign.Manager
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
		mockMaker = NewMockMaker(ctrl)
		mockSigner = NewMockSigner(ctrl)
		mockBuildSignPodManager = NewMockBuildSignPodManager(ctrl)
		mgr = NewManager(clnt, mockMaker, mockSigner, mockBuildSignPodManager)
	})

	ctx := context.Background()
	testMBSC := kmmv1beta1.ModuleBuildSignConfig{}

	It("failed flow, GetModulePodByKernel fails", func() {
		normalizedKernel := kernel.NormalizeVersion(kernelVersion)
		mockBuildSignPodManager.EXPECT().GetModulePodByKernel(ctx, mbscName, mbscNamespace, normalizedKernel, PodTypeBuild, &testMBSC).
			Return(nil, fmt.Errorf("some error"))

		status, err := mgr.GetStatus(ctx, mbscName, mbscNamespace, kernelVersion, kmmv1beta1.BuildImage, &testMBSC)
		Expect(err).To(HaveOccurred())
		Expect(status).To(Equal(kmmv1beta1.BuildOrSignStatus("")))
	})

	It("GetModulePodByKernel returns pod does not exists", func() {
		normalizedKernel := kernel.NormalizeVersion(kernelVersion)
		mockBuildSignPodManager.EXPECT().GetModulePodByKernel(ctx, mbscName, mbscNamespace, normalizedKernel, PodTypeBuild, &testMBSC).
			Return(nil, ErrNoMatchingPod)

		status, err := mgr.GetStatus(ctx, mbscName, mbscNamespace, kernelVersion, kmmv1beta1.BuildImage, &testMBSC)
		Expect(err).To(BeNil())
		Expect(status).To(Equal(kmmv1beta1.BuildOrSignStatus("")))
	})

	It("failed flow, GetPodStatus fails", func() {
		foundPod := v1.Pod{}
		normalizedKernel := kernel.NormalizeVersion(kernelVersion)
		gomock.InOrder(
			mockBuildSignPodManager.EXPECT().GetModulePodByKernel(ctx, mbscName, mbscNamespace, normalizedKernel, PodTypeBuild, &testMBSC).
				Return(&foundPod, nil),
			mockBuildSignPodManager.EXPECT().GetPodStatus(&foundPod).Return(Status(""), fmt.Errorf("some error")),
		)

		status, err := mgr.GetStatus(ctx, mbscName, mbscNamespace, kernelVersion, kmmv1beta1.BuildImage, &testMBSC)
		Expect(err).To(HaveOccurred())
		Expect(status).To(Equal(kmmv1beta1.BuildOrSignStatus("")))
	})

	DescribeTable("check good flow and returned statuses", func(podStatus Status, expectedStatus kmmv1beta1.BuildOrSignStatus) {
		foundPod := v1.Pod{}
		normalizedKernel := kernel.NormalizeVersion(kernelVersion)
		gomock.InOrder(
			mockBuildSignPodManager.EXPECT().GetModulePodByKernel(ctx, mbscName, mbscNamespace, normalizedKernel, PodTypeBuild, &testMBSC).
				Return(&foundPod, nil),
			mockBuildSignPodManager.EXPECT().GetPodStatus(&foundPod).Return(podStatus, nil),
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
		ctrl                    *gomock.Controller
		clnt                    *client.MockClient
		mockMaker               *MockMaker
		mockSigner              *MockSigner
		mockBuildSignPodManager *MockBuildSignPodManager
		mgr                     buildsign.Manager
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
		mockMaker = NewMockMaker(ctrl)
		mockSigner = NewMockSigner(ctrl)
		mockBuildSignPodManager = NewMockBuildSignPodManager(ctrl)
		mgr = NewManager(clnt, mockMaker, mockSigner, mockBuildSignPodManager)
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
		mockMaker.EXPECT().MakePodTemplate(ctx, testMLD, &testMBSC, true).Return(nil, fmt.Errorf("some error"))
		err := mgr.Sync(ctx, testMLD, true, &testMBSC, kmmv1beta1.BuildImage)
		Expect(err).To(HaveOccurred())

		By("test sign action")
		mockSigner.EXPECT().MakePodTemplate(ctx, testMLD, &testMBSC, true).Return(nil, fmt.Errorf("some error"))
		err = mgr.Sync(ctx, testMLD, true, &testMBSC, kmmv1beta1.SignImage)
		Expect(err).To(HaveOccurred())
	})

	It("GetModulePodByKernel failed", func() {
		gomock.InOrder(
			mockMaker.EXPECT().MakePodTemplate(ctx, testMLD, &testMBSC, true).Return(nil, nil),
			mockBuildSignPodManager.EXPECT().GetModulePodByKernel(ctx, mbscName, mbscNamespace, kernelVersion, PodTypeBuild, &testMBSC).
				Return(nil, fmt.Errorf("some error")),
		)
		err := mgr.Sync(ctx, testMLD, true, &testMBSC, kmmv1beta1.BuildImage)
		Expect(err).To(HaveOccurred())
	})

	It("CreatePod failed", func() {
		testTemplate := v1.Pod{}
		gomock.InOrder(
			mockMaker.EXPECT().MakePodTemplate(ctx, testMLD, &testMBSC, true).Return(&testTemplate, nil),
			mockBuildSignPodManager.EXPECT().GetModulePodByKernel(ctx, mbscName, mbscNamespace, kernelVersion, PodTypeBuild, &testMBSC).
				Return(nil, ErrNoMatchingPod),
			mockBuildSignPodManager.EXPECT().CreatePod(ctx, &testTemplate).Return(fmt.Errorf("some error")),
		)
		err := mgr.Sync(ctx, testMLD, true, &testMBSC, kmmv1beta1.BuildImage)
		Expect(err).To(HaveOccurred())
	})

	It("IsPodChanged failed", func() {
		testTemplate := v1.Pod{}
		testPod := v1.Pod{}
		gomock.InOrder(
			mockMaker.EXPECT().MakePodTemplate(ctx, testMLD, &testMBSC, true).Return(&testTemplate, nil),
			mockBuildSignPodManager.EXPECT().GetModulePodByKernel(ctx, mbscName, mbscNamespace, kernelVersion, PodTypeBuild, &testMBSC).
				Return(&testPod, nil),
			mockBuildSignPodManager.EXPECT().IsPodChanged(&testPod, &testTemplate).Return(false, fmt.Errorf("some error")),
		)
		err := mgr.Sync(ctx, testMLD, true, &testMBSC, kmmv1beta1.BuildImage)
		Expect(err).To(HaveOccurred())
	})

	It("DeletePod failed should not cause failure", func() {
		testTemplate := v1.Pod{}
		testPod := v1.Pod{}
		gomock.InOrder(
			mockMaker.EXPECT().MakePodTemplate(ctx, testMLD, &testMBSC, true).Return(&testTemplate, nil),
			mockBuildSignPodManager.EXPECT().GetModulePodByKernel(ctx, mbscName, mbscNamespace, kernelVersion, PodTypeBuild, &testMBSC).
				Return(&testPod, nil),
			mockBuildSignPodManager.EXPECT().IsPodChanged(&testPod, &testTemplate).Return(true, nil),
			mockBuildSignPodManager.EXPECT().DeletePod(ctx, &testPod).Return(fmt.Errorf("some error")),
		)
		err := mgr.Sync(ctx, testMLD, true, &testMBSC, kmmv1beta1.BuildImage)
		Expect(err).To(BeNil())
	})

	DescribeTable("check good flow", func(buildAction, podExists, podChanged, pushImage bool) {
		testAction := kmmv1beta1.BuildImage
		testPodTemplate := v1.Pod{}
		existingTestPod := v1.Pod{}
		podType := PodTypeBuild
		if !buildAction {
			podType = PodTypeSign
			testAction = kmmv1beta1.SignImage
		}

		if buildAction {
			mockMaker.EXPECT().MakePodTemplate(ctx, testMLD, &testMBSC, pushImage).Return(&testPodTemplate, nil)
		} else {
			mockSigner.EXPECT().MakePodTemplate(ctx, testMLD, &testMBSC, pushImage).Return(&testPodTemplate, nil)
		}
		var getPodError error
		if !podExists {
			getPodError = ErrNoMatchingPod
		}
		mockBuildSignPodManager.EXPECT().GetModulePodByKernel(ctx, mbscName, mbscNamespace, kernelVersion, podType, &testMBSC).Return(&existingTestPod, getPodError)
		if !podExists {
			mockBuildSignPodManager.EXPECT().CreatePod(ctx, &testPodTemplate).Return(nil)
			goto executeTestFunction
		}
		mockBuildSignPodManager.EXPECT().IsPodChanged(&existingTestPod, &testPodTemplate).Return(podChanged, nil)
		if podChanged {
			mockBuildSignPodManager.EXPECT().DeletePod(ctx, &existingTestPod).Return(nil)
		}

	executeTestFunction:
		err := mgr.Sync(ctx, testMLD, pushImage, &testMBSC, testAction)
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
		ctrl                    *gomock.Controller
		clnt                    *client.MockClient
		mockMaker               *MockMaker
		mockSigner              *MockSigner
		mockBuildSignPodManager *MockBuildSignPodManager
		mgr                     buildsign.Manager
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
		mockMaker = NewMockMaker(ctrl)
		mockSigner = NewMockSigner(ctrl)
		mockBuildSignPodManager = NewMockBuildSignPodManager(ctrl)
		mgr = NewManager(clnt, mockMaker, mockSigner, mockBuildSignPodManager)
	})

	ctx := context.Background()
	testMBSC := kmmv1beta1.ModuleBuildSignConfig{}

	It("failed to get module pods", func() {
		mockBuildSignPodManager.EXPECT().GetModulePods(ctx, mbscName, mbscNamespace, PodTypeBuild, &testMBSC).Return(nil, fmt.Errorf("some error"))

		_, err := mgr.GarbageCollect(ctx, mbscName, mbscNamespace, PodTypeBuild, &testMBSC)
		Expect(err).To(HaveOccurred())
	})

	It("delete pod failed", func() {
		testPod := v1.Pod{
			Status: v1.PodStatus{
				Phase: v1.PodSucceeded,
			},
		}
		gomock.InOrder(
			mockBuildSignPodManager.EXPECT().GetModulePods(ctx, mbscName, mbscNamespace, PodTypeBuild, &testMBSC).
				Return([]v1.Pod{testPod}, nil),
			mockBuildSignPodManager.EXPECT().DeletePod(ctx, &testPod).Return(fmt.Errorf("some error")),
		)

		_, err := mgr.GarbageCollect(ctx, mbscName, mbscNamespace, PodTypeBuild, &testMBSC)
		Expect(err).To(HaveOccurred())
	})

	It("good flow", func() {
		testPodSuccess := v1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "podSuccess"},
			Status:     v1.PodStatus{Phase: v1.PodSucceeded},
		}
		testPodFailure := v1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "podFailure"},
			Status:     v1.PodStatus{Phase: v1.PodFailed},
		}
		testPodRunning := v1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "podRunning"},
			Status:     v1.PodStatus{Phase: v1.PodRunning},
		}
		testPodUnknown := v1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "podUnknown"},
			Status:     v1.PodStatus{Phase: v1.PodUnknown},
		}
		testPodPending := v1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "podPending"},
			Status:     v1.PodStatus{Phase: v1.PodPending},
		}
		returnedPods := []v1.Pod{testPodSuccess, testPodFailure, testPodRunning, testPodUnknown, testPodPending}
		gomock.InOrder(
			mockBuildSignPodManager.EXPECT().GetModulePods(ctx, mbscName, mbscNamespace, PodTypeBuild, &testMBSC).
				Return(returnedPods, nil),
			mockBuildSignPodManager.EXPECT().DeletePod(ctx, &testPodSuccess).Return(nil),
		)

		res, err := mgr.GarbageCollect(ctx, mbscName, mbscNamespace, PodTypeBuild, &testMBSC)
		Expect(err).To(BeNil())
		Expect(res).To(Equal([]string{"podSuccess"}))
	})
})
