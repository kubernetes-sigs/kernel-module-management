package controllers

import (
	"context"
	"errors"
	"fmt"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/client"
	"github.com/kubernetes-sigs/kernel-module-management/internal/mbsc"
	"github.com/kubernetes-sigs/kernel-module-management/internal/mic"
	"github.com/kubernetes-sigs/kernel-module-management/internal/pod"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var _ = Describe("MicReconciler_Reconcile", func() {
	var (
		ctrl               *gomock.Controller
		mockImagePuller    *pod.MockImagePuller
		mockMicReconHelper *MockmicReconcilerHelper
		mr                 *micReconciler
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockImagePuller = pod.NewMockImagePuller(ctrl)
		mockMicReconHelper = NewMockmicReconcilerHelper(ctrl)

		mr = &micReconciler{
			micReconHelper: mockMicReconHelper,
			imagePullerAPI: mockImagePuller,
		}
	})

	ctx := context.Background()
	testMic := kmmv1beta1.ModuleImagesConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "some name", Namespace: "some namespace"},
	}

	DescribeTable("check good and error flows", func(listPullPodsError,
		updateStatusByPodsError,
		updateStatusByMBSCError,
		processImagesSpecsError bool) {

		returnedError := errors.New("some error")
		expectedErr := returnedError
		pullPods := []v1.Pod{}

		mockMicReconHelper.EXPECT().handleImageRebuildTriggerGeneration(ctx, &testMic).Return(false, nil)

		if listPullPodsError {
			mockImagePuller.EXPECT().ListPullPods(ctx, "some name", "some namespace").Return(nil, returnedError)
			goto executeTestFunction
		}
		mockImagePuller.EXPECT().ListPullPods(ctx, "some name", "some namespace").Return(pullPods, nil)
		if updateStatusByPodsError {
			mockMicReconHelper.EXPECT().updateStatusByPullPods(ctx, &testMic, pullPods).Return(returnedError)
			goto executeTestFunction
		}
		mockMicReconHelper.EXPECT().updateStatusByPullPods(ctx, &testMic, pullPods).Return(nil)
		if updateStatusByMBSCError {
			mockMicReconHelper.EXPECT().updateStatusByMBSC(ctx, &testMic).Return(returnedError)
			goto executeTestFunction
		}
		mockMicReconHelper.EXPECT().updateStatusByMBSC(ctx, &testMic).Return(nil)
		if processImagesSpecsError {
			mockMicReconHelper.EXPECT().processImagesSpecs(ctx, &testMic, pullPods).Return(returnedError)
			goto executeTestFunction
		}
		mockMicReconHelper.EXPECT().processImagesSpecs(ctx, &testMic, pullPods).Return(nil)
		expectedErr = nil

	executeTestFunction:
		res, err := mr.Reconcile(ctx, &testMic)

		Expect(res).To(Equal(reconcile.Result{}))
		if expectedErr != nil {
			Expect(err).To(HaveOccurred())
		} else {
			Expect(err).To(BeNil())
		}
	},
		Entry("listPullPods failed", true, false, false, false),
		Entry("updateStatusByPullPods failed", false, true, false, false),
		Entry("updateStatusByMBSC failed", false, false, true, false),
		Entry("processImagesSpecs failed", false, false, false, true),
		Entry("everything worked", false, false, false, false),
	)

	It("should return error if handleImageRebuildTriggerGeneration fails", func() {
		mockMicReconHelper.EXPECT().handleImageRebuildTriggerGeneration(ctx, &testMic).Return(false, errors.New("trigger error"))

		res, err := mr.Reconcile(ctx, &testMic)

		Expect(res).To(Equal(reconcile.Result{}))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to handle ImageRebuildTriggerGeneration"))
	})

	It("should requeue immediately if trigger changed", func() {
		mockMicReconHelper.EXPECT().handleImageRebuildTriggerGeneration(ctx, &testMic).Return(true, nil)

		res, err := mr.Reconcile(ctx, &testMic)

		Expect(err).To(BeNil())
		Expect(res.Requeue).To(BeTrue())
	})
})

var _ = Describe("handleImageRebuildTriggerGeneration", func() {
	var (
		ctrl         *gomock.Controller
		clnt         *client.MockClient
		statusWriter *client.MockStatusWriter
		mbscHelper   *mbsc.MockMBSC
		mrh          micReconcilerHelper
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		statusWriter = client.NewMockStatusWriter(ctrl)
		mbscHelper = mbsc.NewMockMBSC(ctrl)
		mrh = newMICReconcilerHelper(clnt, nil, nil, mbscHelper, nil)
	})

	ctx := context.Background()

	It("should return false when spec and status triggers are the same", func() {
		triggerValue := 1
		testMic := kmmv1beta1.ModuleImagesConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "test-mic", Namespace: "test-ns"},
			Spec: kmmv1beta1.ModuleImagesConfigSpec{
				ImageRebuildTriggerGeneration: &triggerValue,
			},
			Status: kmmv1beta1.ModuleImagesConfigStatus{
				ImageRebuildTriggerGeneration: &triggerValue,
				ImagesStates:                  []kmmv1beta1.ModuleImageState{},
			},
		}

		changed, err := mrh.handleImageRebuildTriggerGeneration(ctx, &testMic)

		Expect(err).To(BeNil())
		Expect(changed).To(BeFalse())
	})

	It("should return false when both triggers are nil", func() {
		testMic := kmmv1beta1.ModuleImagesConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "test-mic", Namespace: "test-ns"},
			Spec: kmmv1beta1.ModuleImagesConfigSpec{
				ImageRebuildTriggerGeneration: nil,
			},
			Status: kmmv1beta1.ModuleImagesConfigStatus{
				ImageRebuildTriggerGeneration: nil,
				ImagesStates:                  []kmmv1beta1.ModuleImageState{},
			},
		}

		changed, err := mrh.handleImageRebuildTriggerGeneration(ctx, &testMic)

		Expect(err).To(BeNil())
		Expect(changed).To(BeFalse())
	})

	It("should delete MBSC, clear status, and return true when trigger changes from nil to value", func() {
		triggerValue := 1
		testMic := kmmv1beta1.ModuleImagesConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "test-mic", Namespace: "test-ns"},
			Spec: kmmv1beta1.ModuleImagesConfigSpec{
				ImageRebuildTriggerGeneration: &triggerValue,
			},
			Status: kmmv1beta1.ModuleImagesConfigStatus{
				ImageRebuildTriggerGeneration: nil,
				ImagesStates: []kmmv1beta1.ModuleImageState{
					{Image: "image1", Status: kmmv1beta1.ImageExists},
					{Image: "image2", Status: kmmv1beta1.ImageExists},
				},
			},
		}

		gomock.InOrder(
			mbscHelper.EXPECT().Delete(ctx, "test-mic", "test-ns").Return(nil),
			clnt.EXPECT().Status().Return(statusWriter),
			statusWriter.EXPECT().Patch(ctx, &testMic, gomock.Any()).Return(nil),
		)

		changed, err := mrh.handleImageRebuildTriggerGeneration(ctx, &testMic)

		Expect(err).To(BeNil())
		Expect(changed).To(BeTrue())
		// Verify status was cleared
		Expect(testMic.Status.ImagesStates).To(BeEmpty())
		Expect(*testMic.Status.ImageRebuildTriggerGeneration).To(Equal(1))
	})

	It("should delete MBSC, clear status, and return true when trigger changes to a new value", func() {
		oldTriggerValue := 1
		newTriggerValue := 2
		testMic := kmmv1beta1.ModuleImagesConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "test-mic", Namespace: "test-ns"},
			Spec: kmmv1beta1.ModuleImagesConfigSpec{
				ImageRebuildTriggerGeneration: &newTriggerValue,
			},
			Status: kmmv1beta1.ModuleImagesConfigStatus{
				ImageRebuildTriggerGeneration: &oldTriggerValue,
				ImagesStates: []kmmv1beta1.ModuleImageState{
					{Image: "image1", Status: kmmv1beta1.ImageExists},
				},
			},
		}

		gomock.InOrder(
			mbscHelper.EXPECT().Delete(ctx, "test-mic", "test-ns").Return(nil),
			clnt.EXPECT().Status().Return(statusWriter),
			statusWriter.EXPECT().Patch(ctx, &testMic, gomock.Any()).Return(nil),
		)

		changed, err := mrh.handleImageRebuildTriggerGeneration(ctx, &testMic)

		Expect(err).To(BeNil())
		Expect(changed).To(BeTrue())
		Expect(testMic.Status.ImagesStates).To(BeEmpty())
		Expect(*testMic.Status.ImageRebuildTriggerGeneration).To(Equal(2))
	})

	It("should return error when MBSC delete fails", func() {
		triggerValue := 1
		testMic := kmmv1beta1.ModuleImagesConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "test-mic", Namespace: "test-ns"},
			Spec: kmmv1beta1.ModuleImagesConfigSpec{
				ImageRebuildTriggerGeneration: &triggerValue,
			},
			Status: kmmv1beta1.ModuleImagesConfigStatus{
				ImageRebuildTriggerGeneration: nil,
				ImagesStates:                  []kmmv1beta1.ModuleImageState{},
			},
		}

		mbscHelper.EXPECT().Delete(ctx, "test-mic", "test-ns").Return(errors.New("delete failed"))

		changed, err := mrh.handleImageRebuildTriggerGeneration(ctx, &testMic)

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to delete MBSC"))
		Expect(changed).To(BeFalse())
	})

	It("should return error when status patch fails", func() {
		triggerValue := 1
		testMic := kmmv1beta1.ModuleImagesConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "test-mic", Namespace: "test-ns"},
			Spec: kmmv1beta1.ModuleImagesConfigSpec{
				ImageRebuildTriggerGeneration: &triggerValue,
			},
			Status: kmmv1beta1.ModuleImagesConfigStatus{
				ImageRebuildTriggerGeneration: nil,
				ImagesStates:                  []kmmv1beta1.ModuleImageState{},
			},
		}

		gomock.InOrder(
			mbscHelper.EXPECT().Delete(ctx, "test-mic", "test-ns").Return(nil),
			clnt.EXPECT().Status().Return(statusWriter),
			statusWriter.EXPECT().Patch(ctx, &testMic, gomock.Any()).Return(errors.New("patch failed")),
		)

		changed, err := mrh.handleImageRebuildTriggerGeneration(ctx, &testMic)

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to patch MIC status"))
		Expect(changed).To(BeFalse())
	})
})

var _ = Describe("updateStatusByPullPods", func() {
	var (
		ctrl            *gomock.Controller
		clnt            *client.MockClient
		statusWriter    *client.MockStatusWriter
		mockImagePuller *pod.MockImagePuller
		micHelper       *mic.MockMIC
		mrh             micReconcilerHelper
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		statusWriter = client.NewMockStatusWriter(ctrl)
		mockImagePuller = pod.NewMockImagePuller(ctrl)
		micHelper = mic.NewMockMIC(ctrl)
		mrh = newMICReconcilerHelper(clnt, mockImagePuller, micHelper, nil, nil)
	})

	ctx := context.Background()
	testMic := kmmv1beta1.ModuleImagesConfig{}

	It("zero pull pods", func() {
		pullPods := []v1.Pod{}
		err := mrh.updateStatusByPullPods(ctx, &testMic, pullPods)
		Expect(err).To(BeNil())
	})

	It("pod's image is not in spec", func() {
		pullPod := v1.Pod{}
		gomock.InOrder(
			mockImagePuller.EXPECT().GetPullPodImage(pullPod).Return("missing image"),
			micHelper.EXPECT().GetModuleImageSpec(&testMic, "missing image").Return(nil),
			clnt.EXPECT().Status().Return(statusWriter),
			statusWriter.EXPECT().Patch(ctx, &testMic, gomock.Any()),
			mockImagePuller.EXPECT().DeletePod(ctx, &pullPod).Return(nil),
		)
		err := mrh.updateStatusByPullPods(ctx, &testMic, []v1.Pod{pullPod})
		Expect(err).To(BeNil())
	})

	DescribeTable("pod failed scenarios",
		func(buildExists, signExists, skipWaitMissingImage bool, stateToSet kmmv1beta1.ImageState) {
			pullPod := v1.Pod{}
			micSpec := kmmv1beta1.ModuleImageSpec{
				Image: "some test image",
			}
			if buildExists {
				micSpec.Build = &kmmv1beta1.Build{}
			}
			if signExists {
				micSpec.Sign = &kmmv1beta1.Sign{}
			}
			micSpec.SkipWaitMissingImage = skipWaitMissingImage
			gomock.InOrder(
				mockImagePuller.EXPECT().GetPullPodImage(pullPod).Return("some test image"),
				micHelper.EXPECT().GetModuleImageSpec(&testMic, "some test image").Return(&micSpec),
				mockImagePuller.EXPECT().GetPullPodStatus(&pullPod).Return(pod.PullImageFailed),
				micHelper.EXPECT().SetImageStatus(&testMic, "some test image", stateToSet),
				clnt.EXPECT().Status().Return(statusWriter),
				statusWriter.EXPECT().Patch(ctx, &testMic, gomock.Any()),
				mockImagePuller.EXPECT().DeletePod(ctx, &pullPod).Return(nil),
			)
			err := mrh.updateStatusByPullPods(ctx, &testMic, []v1.Pod{pullPod})
			Expect(err).To(BeNil())
		},
		Entry("build exists, sign missing, skipWait false, state ImageNeedsBuilding", true, false, false, kmmv1beta1.ImageNeedsBuilding),
		Entry("build missing, sign exists, skipWait false, state ImageNeedsSigning", false, true, false, kmmv1beta1.ImageNeedsSigning),
		Entry("build missing, sign missing, skipWait true, state ImageDoesNotExist", false, false, true, kmmv1beta1.ImageDoesNotExist),
	)

	It("pod failed, build or sign configs are not present, skipWait is false", func() {
		pullPod := v1.Pod{}
		micSpec := kmmv1beta1.ModuleImageSpec{
			Image: "some test image",
		}

		gomock.InOrder(
			mockImagePuller.EXPECT().GetPullPodImage(pullPod).Return("some test image"),
			micHelper.EXPECT().GetModuleImageSpec(&testMic, "some test image").Return(&micSpec),
			mockImagePuller.EXPECT().GetPullPodStatus(&pullPod).Return(pod.PullImageFailed),
			clnt.EXPECT().Status().Return(statusWriter),
			statusWriter.EXPECT().Patch(ctx, &testMic, gomock.Any()),
			mockImagePuller.EXPECT().DeletePod(ctx, &pullPod).Return(nil),
		)
		err := mrh.updateStatusByPullPods(ctx, &testMic, []v1.Pod{pullPod})
		Expect(err).To(BeNil())
	})

	It("pod succeeded", func() {
		pullPod := v1.Pod{}
		micSpec := kmmv1beta1.ModuleImageSpec{
			Image: "some test image",
		}

		gomock.InOrder(
			mockImagePuller.EXPECT().GetPullPodImage(pullPod).Return("some test image"),
			micHelper.EXPECT().GetModuleImageSpec(&testMic, "some test image").Return(&micSpec),
			mockImagePuller.EXPECT().GetPullPodStatus(&pullPod).Return(pod.PullImageSuccess),
			micHelper.EXPECT().SetImageStatus(&testMic, "some test image", kmmv1beta1.ImageExists),
			clnt.EXPECT().Status().Return(statusWriter),
			statusWriter.EXPECT().Patch(ctx, &testMic, gomock.Any()),
			mockImagePuller.EXPECT().DeletePod(ctx, &pullPod).Return(nil),
		)
		err := mrh.updateStatusByPullPods(ctx, &testMic, []v1.Pod{pullPod})
		Expect(err).To(BeNil())
	})
})

var _ = Describe("updateStatusByMBSC", func() {
	var (
		ctrl         *gomock.Controller
		clnt         *client.MockClient
		statusWriter *client.MockStatusWriter
		mbscHelper   *mbsc.MockMBSC
		micHelper    *mic.MockMIC
		mrh          micReconcilerHelper
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		statusWriter = client.NewMockStatusWriter(ctrl)
		micHelper = mic.NewMockMIC(ctrl)
		mbscHelper = mbsc.NewMockMBSC(ctrl)
		mrh = newMICReconcilerHelper(clnt, nil, micHelper, mbscHelper, nil)
	})

	ctx := context.Background()
	testMic := kmmv1beta1.ModuleImagesConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "some name",
			Namespace: "some namespace",
		},
		Spec: kmmv1beta1.ModuleImagesConfigSpec{
			Images: []kmmv1beta1.ModuleImageSpec{
				{
					Image: "image 1",
					Build: &kmmv1beta1.Build{},
				},
				{

					Image: "image 2",
				},
			},
		},
	}

	It("failed to get MBSC", func() {
		mbscHelper.EXPECT().Get(ctx, testMic.Name, testMic.Namespace).Return(nil, fmt.Errorf("some error"))
		err := mrh.updateStatusByMBSC(ctx, &testMic)
		Expect(err).To(HaveOccurred())
	})

	It("MBSC does not exists", func() {
		mbscHelper.EXPECT().Get(ctx, testMic.Name, testMic.Namespace).Return(nil, nil)
		err := mrh.updateStatusByMBSC(ctx, &testMic)
		Expect(err).To(BeNil())
	})

	It("Image in MBSC status does not exists in MIC spec", func() {
		testMBSC := kmmv1beta1.ModuleBuildSignConfig{
			Status: kmmv1beta1.ModuleBuildSignConfigStatus{
				Images: []kmmv1beta1.BuildSignImageState{
					{
						Image: "some image",
					},
				},
			},
		}
		gomock.InOrder(
			mbscHelper.EXPECT().Get(ctx, testMic.Name, testMic.Namespace).Return(&testMBSC, nil),
			micHelper.EXPECT().GetModuleImageSpec(&testMic, "some image").Return(nil),
			clnt.EXPECT().Status().Return(statusWriter),
			statusWriter.EXPECT().Patch(ctx, &testMic, gomock.Any()),
		)
		err := mrh.updateStatusByMBSC(ctx, &testMic)
		Expect(err).To(BeNil())
	})

	DescribeTable("image has status in MBSC and spec in MIC",
		func(signExists bool, mbscImageAction kmmv1beta1.BuildOrSignAction, mbscImageStatus kmmv1beta1.BuildOrSignStatus,
			expectedMICImageState kmmv1beta1.ImageState) {
			testMBSC := kmmv1beta1.ModuleBuildSignConfig{
				Status: kmmv1beta1.ModuleBuildSignConfigStatus{
					Images: []kmmv1beta1.BuildSignImageState{
						{
							Image:  "some image",
							Status: mbscImageStatus,
							Action: mbscImageAction,
						},
					},
				},
			}
			imageSpec := kmmv1beta1.ModuleImageSpec{}
			if signExists {
				imageSpec.Sign = &kmmv1beta1.Sign{}
			}
			gomock.InOrder(
				mbscHelper.EXPECT().Get(ctx, testMic.Name, testMic.Namespace).Return(&testMBSC, nil),
				micHelper.EXPECT().GetModuleImageSpec(&testMic, "some image").Return(&imageSpec),
				micHelper.EXPECT().SetImageStatus(&testMic, "some image", expectedMICImageState),
				clnt.EXPECT().Status().Return(statusWriter),
				statusWriter.EXPECT().Patch(ctx, &testMic, gomock.Any()),
			)

			err := mrh.updateStatusByMBSC(ctx, &testMic)
			Expect(err).To(BeNil())
		},
		Entry("sign config does not exists, action Build, status Failed", false, kmmv1beta1.BuildImage, kmmv1beta1.ActionFailure, kmmv1beta1.ImageDoesNotExist),
		Entry("sign config does not exists, action Sign, status Failed", false, kmmv1beta1.SignImage, kmmv1beta1.ActionFailure, kmmv1beta1.ImageDoesNotExist),
		Entry("sign config does not exists, action Build, status Succeeded", false, kmmv1beta1.BuildImage, kmmv1beta1.ActionSuccess, kmmv1beta1.ImageExists),
		Entry("sign config does not exists, action Sign, status Succeeded", false, kmmv1beta1.SignImage, kmmv1beta1.ActionSuccess, kmmv1beta1.ImageExists),
		Entry("sign config exists, action Build, status Failed", true, kmmv1beta1.BuildImage, kmmv1beta1.ActionFailure, kmmv1beta1.ImageDoesNotExist),
		Entry("sign config exists, action Sign, status Failed", true, kmmv1beta1.SignImage, kmmv1beta1.ActionFailure, kmmv1beta1.ImageDoesNotExist),
		Entry("sign config exists, action Build, status Succeeded", true, kmmv1beta1.BuildImage, kmmv1beta1.ActionSuccess, kmmv1beta1.ImageNeedsSigning),
		Entry("sign config exists, action Sign, status Succeeded", true, kmmv1beta1.SignImage, kmmv1beta1.ActionSuccess, kmmv1beta1.ImageExists),
	)
})

var _ = Describe("processImagesSpecs", func() {
	var (
		ctrl            *gomock.Controller
		clnt            *client.MockClient
		mockImagePuller *pod.MockImagePuller
		mbscHelper      *mbsc.MockMBSC
		micHelper       *mic.MockMIC
		mrh             micReconcilerHelper
		testMic         kmmv1beta1.ModuleImagesConfig
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		mockImagePuller = pod.NewMockImagePuller(ctrl)
		micHelper = mic.NewMockMIC(ctrl)
		mbscHelper = mbsc.NewMockMBSC(ctrl)
		mrh = newMICReconcilerHelper(clnt, mockImagePuller, micHelper, mbscHelper, scheme)
		testMic = kmmv1beta1.ModuleImagesConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "some name",
				Namespace: "some namespace",
			},
			Spec: kmmv1beta1.ModuleImagesConfigSpec{
				Images: []kmmv1beta1.ModuleImageSpec{
					{
						Image: "image 1",
					},
				},
			},
		}

	})

	ctx := context.Background()
	pullPods := []v1.Pod{}

	DescribeTable("image status empty, pull pod does not exists, create a pull pod",
		func(buildExists, signExists, skipWaitMissingImage, expectedOneTimePodFlag bool) {
			if buildExists {
				testMic.Spec.Images[0].Build = &kmmv1beta1.Build{}
			}
			if signExists {
				testMic.Spec.Images[0].Sign = &kmmv1beta1.Sign{}
			}
			testMic.Spec.Images[0].SkipWaitMissingImage = skipWaitMissingImage
			gomock.InOrder(
				micHelper.EXPECT().GetImageState(&testMic, "image 1").Return(kmmv1beta1.ImageState("")),
				mockImagePuller.EXPECT().GetPullPodForImage(pullPods, "image 1").Return(nil),
				mockImagePuller.EXPECT().CreatePullPod(ctx, "some name", "some namespace", "image 1", expectedOneTimePodFlag,
					nil, v1.PullPolicy(""), &testMic).Return(nil),
			)
			err := mrh.processImagesSpecs(ctx, &testMic, pullPods)
			Expect(err).To(BeNil())
		},
		Entry("build exists, sign missing, skipWait false, expectedFlag true", true, false, false, true),
		Entry("build missing, sign exists, skipWait false, expectedFlag true", false, true, false, true),
		Entry("build missing, sign missing, skipWait true, expectedFlag true", false, false, true, true),
		Entry("build exists, sign exists, skipWait false, expectedFlag true", true, true, false, true),
		Entry("build exists, sign exists, skipWait true, expectedFlag true", true, true, true, true),
		Entry("build missing, sign missing, skipWait false, expectedFlag false", false, false, false, false),
	)

	It("image status empty, pull pod exists, nothing to do", func() {
		gomock.InOrder(
			micHelper.EXPECT().GetImageState(&testMic, "image 1").Return(kmmv1beta1.ImageState("")),
			mockImagePuller.EXPECT().GetPullPodForImage(pullPods, "image 1").Return(&v1.Pod{}),
		)
		err := mrh.processImagesSpecs(ctx, &testMic, pullPods)
		Expect(err).To(BeNil())
	})

	DescribeTable("images in MBSC status exist in MIC spec",
		func(imageState kmmv1beta1.ImageState, buildExists, signExists, updateMSBC bool, msbcAction kmmv1beta1.BuildOrSignAction) {
			testMic := kmmv1beta1.ModuleImagesConfig{
				Spec: kmmv1beta1.ModuleImagesConfigSpec{
					Images: []kmmv1beta1.ModuleImageSpec{
						{
							Image: "image 1",
						},
					},
				},
			}
			if buildExists {
				testMic.Spec.Images[0].Build = &kmmv1beta1.Build{}
			}
			if signExists {
				testMic.Spec.Images[0].Sign = &kmmv1beta1.Sign{}
			}
			micHelper.EXPECT().GetImageState(&testMic, "image 1").Return(imageState)

			if updateMSBC {
				mbscHelper.EXPECT().CreateOrPatch(ctx, &testMic, &testMic.Spec.Images[0], msbcAction).Return(nil)
			}

			err := mrh.processImagesSpecs(ctx, &testMic, pullPods)
			Expect(err).To(BeNil())
		},
		Entry("image state ImageDoesNotExist, no build or sign configs, do nothing",
			kmmv1beta1.ImageDoesNotExist, false, false, false, kmmv1beta1.SignImage),
		Entry("image state ImageDoesNotExist, build config exists, sign does not exists, update MBSC to build action",
			kmmv1beta1.ImageDoesNotExist, true, false, true, kmmv1beta1.BuildImage),
		Entry("image state ImageDoesNotExist, build config does not exists, sign config exists, update MBSC to build action",
			kmmv1beta1.ImageDoesNotExist, false, true, true, kmmv1beta1.BuildImage),
		Entry("image state ImageNeedsBuilding, build/sign config not important, update MBSC to build action",
			kmmv1beta1.ImageNeedsBuilding, false, false, true, kmmv1beta1.BuildImage),
		Entry("image state ImageNeedsSigning, build/sign config not important, update MBSC to sign action",
			kmmv1beta1.ImageNeedsSigning, false, false, true, kmmv1beta1.SignImage),
	)
})
