package controllers

import (
	"context"
	"errors"
	"fmt"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/buildsign"
	"github.com/kubernetes-sigs/kernel-module-management/internal/client"
	"github.com/kubernetes-sigs/kernel-module-management/internal/mbsc"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var _ = Describe("MBSCReconciler_Reconcile", func() {
	var (
		ctrl                *gomock.Controller
		mockMBSCReconHelper *MockmbscReconcilerHelperAPI
		mr                  *mbscReconciler
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockMBSCReconHelper = NewMockmbscReconcilerHelperAPI(ctrl)

		mr = &mbscReconciler{
			reconHelperAPI: mockMBSCReconHelper,
		}
	})

	ctx := context.Background()
	testMBSC := kmmv1beta1.ModuleBuildSignConfig{}

	DescribeTable("check good and error flows", func(updateStatusError, processImagesSpecsError, garbageCollectError bool) {

		returnedError := errors.New("some error")
		expectedErr := returnedError
		if updateStatusError {
			mockMBSCReconHelper.EXPECT().updateStatus(ctx, &testMBSC).Return(returnedError)
			goto executeTestFunction
		}
		mockMBSCReconHelper.EXPECT().updateStatus(ctx, &testMBSC).Return(nil)
		if processImagesSpecsError {
			mockMBSCReconHelper.EXPECT().processImagesSpecs(ctx, &testMBSC).Return(returnedError)
			goto executeTestFunction
		}
		mockMBSCReconHelper.EXPECT().processImagesSpecs(ctx, &testMBSC).Return(nil)
		if garbageCollectError {
			mockMBSCReconHelper.EXPECT().garbageCollect(ctx, &testMBSC).Return(returnedError)
			goto executeTestFunction
		}
		mockMBSCReconHelper.EXPECT().garbageCollect(ctx, &testMBSC).Return(nil)
		expectedErr = nil

	executeTestFunction:
		res, err := mr.Reconcile(ctx, &testMBSC)

		Expect(res).To(Equal(reconcile.Result{}))
		if expectedErr != nil {
			Expect(err).To(HaveOccurred())
		} else {
			Expect(err).To(BeNil())
		}
	},
		Entry("updateStatus failed", true, false, false),
		Entry("processImageSpecs failed", false, true, false),
		Entry("garbageCollect failed", false, false, true),
		Entry("everything worked", false, false, false),
	)
})

var _ = Describe("updateStatus", func() {
	var (
		ctrl         *gomock.Controller
		clnt         *client.MockClient
		statusWriter *client.MockStatusWriter
		mockManager  *buildsign.MockManager
		mockMBSC     *mbsc.MockMBSC
		testMBSC     kmmv1beta1.ModuleBuildSignConfig
		mrh          mbscReconcilerHelperAPI
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		statusWriter = client.NewMockStatusWriter(ctrl)
		mockManager = buildsign.NewMockManager(ctrl)
		mockMBSC = mbsc.NewMockMBSC(ctrl)
		mrh = newMBSCReconcilerHelper(clnt, mockManager, mockMBSC)
		testMBSC = kmmv1beta1.ModuleBuildSignConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "some name",
				Namespace: "some namespace",
			},
		}
	})

	ctx := context.Background()

	It("no images in spec", func() {
		gomock.InOrder(
			clnt.EXPECT().Status().Return(statusWriter),
			statusWriter.EXPECT().Patch(ctx, &testMBSC, gomock.Any()).Return(nil),
		)

		err := mrh.updateStatus(ctx, &testMBSC)
		Expect(err).To(BeNil())
	})

	It("multiple images, some statuses are error or empty", func() {
		testMBSC.Spec.Images = []kmmv1beta1.ModuleBuildSignSpec{
			{
				ModuleImageSpec: kmmv1beta1.ModuleImageSpec{
					Image:         "image 1",
					KernelVersion: "kernel version 1",
				},
				Action: kmmv1beta1.BuildImage,
			},
			{
				ModuleImageSpec: kmmv1beta1.ModuleImageSpec{
					Image:         "image 2",
					KernelVersion: "kernel version 2",
				},
				Action: kmmv1beta1.SignImage,
			},
			{
				ModuleImageSpec: kmmv1beta1.ModuleImageSpec{
					Image:         "image 3",
					KernelVersion: "kernel version 3",
				},
				Action: kmmv1beta1.BuildImage,
			},
		}
		gomock.InOrder(
			mockManager.EXPECT().GetStatus(ctx, "some name", "some namespace", "kernel version 1", kmmv1beta1.BuildImage, &testMBSC.ObjectMeta).
				Return(kmmv1beta1.ActionSuccess, nil),
			mockMBSC.EXPECT().SetImageStatus(&testMBSC, "image 1", kmmv1beta1.BuildImage, kmmv1beta1.ActionSuccess),
			mockManager.EXPECT().GetStatus(ctx, "some name", "some namespace", "kernel version 2", kmmv1beta1.SignImage, &testMBSC.ObjectMeta).
				Return(kmmv1beta1.BuildOrSignStatus(""), nil),
			mockManager.EXPECT().GetStatus(ctx, "some name", "some namespace", "kernel version 3", kmmv1beta1.BuildImage, &testMBSC.ObjectMeta).
				Return(kmmv1beta1.BuildOrSignStatus(""), fmt.Errorf("some error")),
			clnt.EXPECT().Status().Return(statusWriter),
			statusWriter.EXPECT().Patch(ctx, &testMBSC, gomock.Any()).Return(nil),
		)

		err := mrh.updateStatus(ctx, &testMBSC)
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("processImagesSpecs", func() {
	var (
		ctrl        *gomock.Controller
		clnt        *client.MockClient
		mockManager *buildsign.MockManager
		mockMBSC    *mbsc.MockMBSC
		mrh         mbscReconcilerHelperAPI
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		mockManager = buildsign.NewMockManager(ctrl)
		mockMBSC = mbsc.NewMockMBSC(ctrl)
		mrh = newMBSCReconcilerHelper(clnt, mockManager, mockMBSC)
	})

	ctx := context.Background()
	testMBSC := kmmv1beta1.ModuleBuildSignConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "some name",
			Namespace: "some namespace",
		},
		Spec: kmmv1beta1.ModuleBuildSignConfigSpec{
			Images: []kmmv1beta1.ModuleBuildSignSpec{
				{
					ModuleImageSpec: kmmv1beta1.ModuleImageSpec{
						Image: "image 1",
					},
					Action: kmmv1beta1.BuildImage,
				},
				{
					ModuleImageSpec: kmmv1beta1.ModuleImageSpec{
						Image: "image 2",
					},
					Action: kmmv1beta1.SignImage,
				},
				{
					ModuleImageSpec: kmmv1beta1.ModuleImageSpec{
						Image: "image 3",
					},
					Action: kmmv1beta1.BuildImage,
				},
			},
		},
	}

	It("multiple image, some statuses are success, some failures", func() {
		gomock.InOrder(
			mockMBSC.EXPECT().GetImageStatus(&testMBSC, "image 1", kmmv1beta1.BuildImage).Return(kmmv1beta1.ActionSuccess),
			mockMBSC.EXPECT().GetImageStatus(&testMBSC, "image 2", kmmv1beta1.SignImage).Return(kmmv1beta1.ActionFailure),
			mockManager.EXPECT().Sync(ctx, gomock.Any(), true, kmmv1beta1.SignImage, &testMBSC.ObjectMeta).Return(nil),
			mockMBSC.EXPECT().GetImageStatus(&testMBSC, "image 3", kmmv1beta1.BuildImage).Return(kmmv1beta1.BuildOrSignStatus("")),
			mockManager.EXPECT().Sync(ctx, gomock.Any(), true, kmmv1beta1.BuildImage, &testMBSC.ObjectMeta).Return(fmt.Errorf("some error")),
		)

		err := mrh.processImagesSpecs(ctx, &testMBSC)
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("garbageCollect", func() {
	var (
		ctrl        *gomock.Controller
		clnt        *client.MockClient
		mockManager *buildsign.MockManager
		mockMBSC    *mbsc.MockMBSC
		mrh         mbscReconcilerHelperAPI
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		mockManager = buildsign.NewMockManager(ctrl)
		mockMBSC = mbsc.NewMockMBSC(ctrl)
		mrh = newMBSCReconcilerHelper(clnt, mockManager, mockMBSC)
	})

	ctx := context.Background()
	testMBSC := kmmv1beta1.ModuleBuildSignConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "some name",
			Namespace: "some namespace",
		},
	}

	It("check all flows", func() {
		By("garbage collect for build failed")
		mockManager.EXPECT().GarbageCollect(ctx, "some name", "some namespace", kmmv1beta1.BuildImage, &testMBSC.ObjectMeta).Return(nil, fmt.Errorf("some error"))
		err := mrh.garbageCollect(ctx, &testMBSC)
		Expect(err).To(HaveOccurred())

		By("garbage collect for sign failed")
		gomock.InOrder(
			mockManager.EXPECT().GarbageCollect(ctx, "some name", "some namespace", kmmv1beta1.BuildImage, &testMBSC.ObjectMeta).
				Return([]string{}, nil),
			mockManager.EXPECT().GarbageCollect(ctx, "some name", "some namespace", kmmv1beta1.SignImage, &testMBSC.ObjectMeta).
				Return(nil, fmt.Errorf("some error")),
		)
		err = mrh.garbageCollect(ctx, &testMBSC)
		Expect(err).To(HaveOccurred())

		By("success")
		gomock.InOrder(
			mockManager.EXPECT().GarbageCollect(ctx, "some name", "some namespace", kmmv1beta1.BuildImage, &testMBSC.ObjectMeta).Return([]string{}, nil),
			mockManager.EXPECT().GarbageCollect(ctx, "some name", "some namespace", kmmv1beta1.SignImage, &testMBSC.ObjectMeta).Return([]string{}, nil),
		)
		err = mrh.garbageCollect(ctx, &testMBSC)
		Expect(err).To(BeNil())
	})
})
