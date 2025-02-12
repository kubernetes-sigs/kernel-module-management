package controllers

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/google/go-cmp/cmp"
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/client"
	"github.com/kubernetes-sigs/kernel-module-management/internal/mbsc"
	"github.com/kubernetes-sigs/kernel-module-management/internal/mic"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var _ = Describe("MicReconciler_Reconcile", func() {
	var (
		ctrl               *gomock.Controller
		mockPodHelper      *MockpullPodManager
		mockMicReconHelper *MockmicReconcilerHelper
		mr                 *micReconciler
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockPodHelper = NewMockpullPodManager(ctrl)
		mockMicReconHelper = NewMockmicReconcilerHelper(ctrl)

		mr = &micReconciler{
			micReconHelper: mockMicReconHelper,
			podHelper:      mockPodHelper,
		}
	})

	ctx := context.Background()
	testMic := kmmv1beta1.ModuleImagesConfig{}

	DescribeTable("check good and error flows", func(listPullPodsError,
		updateStatusByPodsError,
		updateStatusByMBSCError,
		processImagesSpecsError bool) {

		returnedError := errors.New("some error")
		expectedErr := returnedError
		pullPods := []v1.Pod{}
		if listPullPodsError {
			mockPodHelper.EXPECT().listImagesPullPods(ctx, &testMic).Return(nil, returnedError)
			goto executeTestFunction
		}
		mockPodHelper.EXPECT().listImagesPullPods(ctx, &testMic).Return(pullPods, nil)
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
})

var _ = Describe("updateStatusByPullPods", func() {
	var (
		ctrl          *gomock.Controller
		clnt          *client.MockClient
		statusWriter  *client.MockStatusWriter
		mockPodHelper *MockpullPodManager
		micHelper     *mic.MockMIC
		mrh           micReconcilerHelper
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		statusWriter = client.NewMockStatusWriter(ctrl)
		mockPodHelper = NewMockpullPodManager(ctrl)
		micHelper = mic.NewMockMIC(ctrl)
		mrh = newMICReconcilerHelper(clnt, mockPodHelper, micHelper, nil, nil)
	})

	ctx := context.Background()
	testMic := kmmv1beta1.ModuleImagesConfig{
		Spec: kmmv1beta1.ModuleImagesConfigSpec{
			Images: []kmmv1beta1.ModuleImageSpec{
				{
					Image: "image 1",
					Build: &kmmv1beta1.Build{},
				},
				{

					Image: "image 2",
					Sign:  &kmmv1beta1.Sign{},
				},
				{

					Image: "image 3",
				},
			},
		},
	}

	It("zero pull pods", func() {
		pullPods := []v1.Pod{}
		err := mrh.updateStatusByPullPods(ctx, &testMic, pullPods)
		Expect(err).To(BeNil())
	})

	It("pod's image is not in spec", func() {
		pullPod := v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{imageLabelKey: "image 3"},
			},
		}
		gomock.InOrder(
			micHelper.EXPECT().GetModuleImageSpec(&testMic, "image 3").Return(nil),
			clnt.EXPECT().Status().Return(statusWriter),
			statusWriter.EXPECT().Patch(ctx, &testMic, gomock.Any()),
			mockPodHelper.EXPECT().deletePod(ctx, &pullPod).Return(nil),
		)
		err := mrh.updateStatusByPullPods(ctx, &testMic, []v1.Pod{pullPod})
		Expect(err).To(BeNil())
	})

	It("pod failed, build config present", func() {
		pullPod := v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{imageLabelKey: "image 1"},
			},
			Status: v1.PodStatus{
				Phase: v1.PodFailed,
			},
		}
		gomock.InOrder(
			micHelper.EXPECT().GetModuleImageSpec(&testMic, "image 1").Return(&testMic.Spec.Images[0]),
			micHelper.EXPECT().SetImageStatus(&testMic, "image 1", kmmv1beta1.ImageNeedsBuilding),
			clnt.EXPECT().Status().Return(statusWriter),
			statusWriter.EXPECT().Patch(ctx, &testMic, gomock.Any()),
			mockPodHelper.EXPECT().deletePod(ctx, &pullPod).Return(nil),
		)
		err := mrh.updateStatusByPullPods(ctx, &testMic, []v1.Pod{pullPod})
		Expect(err).To(BeNil())
	})

	It("pod failed, build config not present, sign config present", func() {
		pullPod := v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{imageLabelKey: "image 2"},
			},
			Status: v1.PodStatus{
				Phase: v1.PodFailed,
			},
		}
		gomock.InOrder(
			micHelper.EXPECT().GetModuleImageSpec(&testMic, "image 2").Return(&testMic.Spec.Images[1]),
			micHelper.EXPECT().SetImageStatus(&testMic, "image 2", kmmv1beta1.ImageNeedsSigning),
			clnt.EXPECT().Status().Return(statusWriter),
			statusWriter.EXPECT().Patch(ctx, &testMic, gomock.Any()),
			mockPodHelper.EXPECT().deletePod(ctx, &pullPod).Return(nil),
		)
		err := mrh.updateStatusByPullPods(ctx, &testMic, []v1.Pod{pullPod})
		Expect(err).To(BeNil())
	})

	It("pod failed, build or sign config is not present", func() {
		pullPod := v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{imageLabelKey: "image 3"},
			},
			Status: v1.PodStatus{
				Phase: v1.PodFailed,
			},
		}
		gomock.InOrder(
			micHelper.EXPECT().GetModuleImageSpec(&testMic, "image 3").Return(&testMic.Spec.Images[2]),
			clnt.EXPECT().Status().Return(statusWriter),
			statusWriter.EXPECT().Patch(ctx, &testMic, gomock.Any()),
			mockPodHelper.EXPECT().deletePod(ctx, &pullPod).Return(nil),
		)
		err := mrh.updateStatusByPullPods(ctx, &testMic, []v1.Pod{pullPod})
		Expect(err).To(BeNil())
	})

	It("pod succeeded", func() {
		pullPod := v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{imageLabelKey: "image 2"},
			},
			Status: v1.PodStatus{
				Phase: v1.PodSucceeded,
			},
		}
		gomock.InOrder(
			micHelper.EXPECT().GetModuleImageSpec(&testMic, "image 2").Return(&testMic.Spec.Images[1]),
			micHelper.EXPECT().SetImageStatus(&testMic, "image 2", kmmv1beta1.ImageExists),
			clnt.EXPECT().Status().Return(statusWriter),
			statusWriter.EXPECT().Patch(ctx, &testMic, gomock.Any()),
			mockPodHelper.EXPECT().deletePod(ctx, &pullPod).Return(nil),
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
			Status: kmmv1beta1.ModuleImagesConfigStatus{
				ImagesStates: []kmmv1beta1.ModuleImageState{
					{
						Image: "some image",
					},
				},
			},
		}
		gomock.InOrder(
			mbscHelper.EXPECT().Get(ctx, testMic.Name, testMic.Namespace).Return(&testMBSC, nil),
			micHelper.EXPECT().GetModuleImageSpec(&testMic, testMBSC.Status.ImagesStates[0].Image).Return(nil),
			clnt.EXPECT().Status().Return(statusWriter),
			statusWriter.EXPECT().Patch(ctx, &testMic, gomock.Any()),
		)
		err := mrh.updateStatusByMBSC(ctx, &testMic)
		Expect(err).To(BeNil())
	})

	DescribeTable("images in MBSC status exist in MIC spec",
		func(signExists bool, mbscImageStatus kmmv1beta1.ImageState, expectedImageState kmmv1beta1.ImageState) {
			testMBSC := kmmv1beta1.ModuleBuildSignConfig{
				Status: kmmv1beta1.ModuleImagesConfigStatus{
					ImagesStates: []kmmv1beta1.ModuleImageState{
						{
							Image:  "some image",
							Status: mbscImageStatus,
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
				micHelper.EXPECT().SetImageStatus(&testMic, "some image", expectedImageState),
				clnt.EXPECT().Status().Return(statusWriter),
				statusWriter.EXPECT().Patch(ctx, &testMic, gomock.Any()),
			)

			err := mrh.updateStatusByMBSC(ctx, &testMic)
			Expect(err).To(BeNil())
		},
		Entry("sign config does not exists, status is ImageBuildFailed", false, kmmv1beta1.ImageBuildFailed, kmmv1beta1.ImageDoesNotExist),
		Entry("sign config does not exists, status is ImageBuildSucceeded", false, kmmv1beta1.ImageBuildSucceeded, kmmv1beta1.ImageExists),
		Entry("sign config exists, status is ImageBuildFailed", true, kmmv1beta1.ImageBuildFailed, kmmv1beta1.ImageDoesNotExist),
		Entry("sign config exists, status is ImageBuildSucceeded", true, kmmv1beta1.ImageBuildSucceeded, kmmv1beta1.ImageNeedsSigning),
		Entry("status is ImageSignFailed", false, kmmv1beta1.ImageSignFailed, kmmv1beta1.ImageDoesNotExist),
		Entry("status is ImageSignSucceeded", false, kmmv1beta1.ImageSignSucceeded, kmmv1beta1.ImageExists),
	)
})

var _ = Describe("processImagesSpecs", func() {
	var (
		ctrl          *gomock.Controller
		clnt          *client.MockClient
		mockPodHelper *MockpullPodManager
		mbscHelper    *mbsc.MockMBSC
		micHelper     *mic.MockMIC
		mrh           micReconcilerHelper
		testMic       kmmv1beta1.ModuleImagesConfig
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		mockPodHelper = NewMockpullPodManager(ctrl)
		micHelper = mic.NewMockMIC(ctrl)
		mbscHelper = mbsc.NewMockMBSC(ctrl)
		mrh = newMICReconcilerHelper(clnt, mockPodHelper, micHelper, mbscHelper, scheme)
		testMic = kmmv1beta1.ModuleImagesConfig{
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
				},
			},
		}

	})

	ctx := context.Background()
	pullPods := []v1.Pod{}
	testMic = kmmv1beta1.ModuleImagesConfig{
		Spec: kmmv1beta1.ModuleImagesConfigSpec{
			Images: []kmmv1beta1.ModuleImageSpec{
				{
					Image: "image 1",
				},
			},
		},
	}

	It("image status empty, pull pod does not exists, need to create a pull pod", func() {
		gomock.InOrder(
			micHelper.EXPECT().GetImageState(&testMic, "image 1").Return(kmmv1beta1.ImageState("")),
			mockPodHelper.EXPECT().getPullPodForImage(pullPods, "image 1").Return(nil),
			mockPodHelper.EXPECT().createPullPod(ctx, &testMic.Spec.Images[0], &testMic).Return(nil),
		)
		err := mrh.processImagesSpecs(ctx, &testMic, pullPods)
		Expect(err).To(BeNil())
	})

	It("image status empty, pull pod exists, nothing to do", func() {
		gomock.InOrder(
			micHelper.EXPECT().GetImageState(&testMic, "image 1").Return(kmmv1beta1.ImageState("")),
			mockPodHelper.EXPECT().getPullPodForImage(pullPods, "image 1").Return(&v1.Pod{}),
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

var _ = Describe("listImagesPullPods", func() {
	var (
		ctrl *gomock.Controller
		clnt *client.MockClient
		ppm  pullPodManager
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		ppm = newPullPodManager(clnt, scheme)
	})

	ctx := context.Background()
	testMic := kmmv1beta1.ModuleImagesConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "some name",
			Namespace: "some namespace",
		},
	}

	It("list succeeded", func() {
		hl := ctrlclient.HasLabels{imageLabelKey}
		ml := ctrlclient.MatchingLabels{moduleImageLabelKey: testMic.Name}

		clnt.EXPECT().List(context.Background(), gomock.Any(), ctrlclient.InNamespace(testMic.Namespace), hl, ml).DoAndReturn(
			func(_ interface{}, podList *v1.PodList, _ ...interface{}) error {
				podList.Items = []v1.Pod{v1.Pod{}, v1.Pod{}}
				return nil
			},
		)

		pullPods, err := ppm.listImagesPullPods(ctx, &testMic)
		Expect(err).To(BeNil())
		Expect(pullPods).ToNot(BeNil())
	})

	It("list failed", func() {
		hl := ctrlclient.HasLabels{imageLabelKey}
		ml := ctrlclient.MatchingLabels{moduleImageLabelKey: testMic.Name}

		clnt.EXPECT().List(context.Background(), gomock.Any(), ctrlclient.InNamespace(testMic.Namespace), hl, ml).Return(fmt.Errorf("some error"))

		pullPods, err := ppm.listImagesPullPods(ctx, &testMic)
		Expect(err).To(HaveOccurred())
		Expect(pullPods).To(BeNil())
	})
})

var _ = Describe("deletePod", func() {
	var (
		ctrl *gomock.Controller
		clnt *client.MockClient
		ppm  pullPodManager
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		ppm = newPullPodManager(clnt, scheme)
	})

	ctx := context.Background()

	It("good flow", func() {
		pod := v1.Pod{}
		clnt.EXPECT().Delete(ctx, &pod).Return(nil)
		err := ppm.deletePod(ctx, &pod)
		Expect(err).To(BeNil())
	})

	It("error flow", func() {
		pod := v1.Pod{}
		clnt.EXPECT().Delete(ctx, &pod).Return(fmt.Errorf("some error"))
		err := ppm.deletePod(ctx, &pod)
		Expect(err).To(HaveOccurred())
	})

	It("deletion timestamp set", func() {
		pod := v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				DeletionTimestamp: &metav1.Time{},
			},
		}
		err := ppm.deletePod(ctx, &pod)
		Expect(err).To(BeNil())
	})
})

var _ = Describe("getPullPodForImage", func() {
	var (
		ppm pullPodManager
	)

	BeforeEach(func() {
		ppm = newPullPodManager(nil, nil)
	})

	pullPods := []v1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{imageLabelKey: "image 1"},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{imageLabelKey: "image 2"},
			},
		},
	}

	It("there is a pull pod for that image", func() {
		res := ppm.getPullPodForImage(pullPods, "image 2")
		Expect(res).ToNot(BeNil())
		Expect(res.Labels[imageLabelKey]).To(Equal("image 2"))
	})

	It("there is no pull pod for that image", func() {
		res := ppm.getPullPodForImage(pullPods, "image 23")
		Expect(res).To(BeNil())
	})
})

var _ = Describe("createPullPod", func() {
	var (
		ctrl *gomock.Controller
		clnt *client.MockClient
		ppm  pullPodManager
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		ppm = newPullPodManager(clnt, scheme)
	})

	ctx := context.Background()
	testMic := kmmv1beta1.ModuleImagesConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "some name",
			Namespace: "some namespace",
		},
	}
	testImageSpec := kmmv1beta1.ModuleImageSpec{
		Image: "some image",
	}

	It("check the pod fields", func() {
		expectedPod := v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: testMic.Name + "-pull-pod-",
				Namespace:    testMic.Namespace,
				Labels: map[string]string{
					moduleImageLabelKey: "some name",
					imageLabelKey:       "some image",
				},
			},
			Spec: v1.PodSpec{
				Containers: []v1.Container{
					{
						Name:    pullerContainerName,
						Image:   "some image",
						Command: []string{"/bin/sh", "-c", "exit 0"},
					},
				},
				RestartPolicy: v1.RestartPolicyOnFailure,
			},
		}

		clnt.EXPECT().Create(ctx, gomock.Any()).DoAndReturn(
			func(ctx context.Context, obj ctrlclient.Object, opts ...ctrlclient.CreateOption) error {
				if pullPod, ok := obj.(*v1.Pod); ok {
					pullPod.OwnerReferences = nil
					if !reflect.DeepEqual(&expectedPod, pullPod) {
						diff := cmp.Diff(expectedPod, *pullPod)
						fmt.Println("Differences:\n", diff)
						return fmt.Errorf("pods not equal")
					}
				}
				return nil
			})
		err := ppm.createPullPod(ctx, &testImageSpec, &testMic)
		Expect(err).To(BeNil())
	})
})
