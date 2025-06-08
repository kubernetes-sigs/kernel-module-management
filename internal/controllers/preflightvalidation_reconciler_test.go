package controllers

import (
	"context"
	"errors"
	"fmt"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/api/v1beta2"
	"github.com/kubernetes-sigs/kernel-module-management/internal/api"
	"github.com/kubernetes-sigs/kernel-module-management/internal/client"
	"github.com/kubernetes-sigs/kernel-module-management/internal/metrics"
	"github.com/kubernetes-sigs/kernel-module-management/internal/mic"
	"github.com/kubernetes-sigs/kernel-module-management/internal/module"
	"github.com/kubernetes-sigs/kernel-module-management/internal/preflight"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("PreflightValidationReconciler", func() {
	var (
		mockCtrl      *gomock.Controller
		mockClient    *client.MockClient
		mockPreflight *preflight.MockPreflightAPI
		mockMetrics   *metrics.MockMetrics
		mockHelper    *MockpreflightReconcilerHelper
		p             *preflightValidationReconciler
	)

	BeforeEach(func() {
		mockCtrl = gomock.NewController(GinkgoT())
		mockClient = client.NewMockClient(mockCtrl)
		mockPreflight = preflight.NewMockPreflightAPI(mockCtrl)
		mockMetrics = metrics.NewMockMetrics(mockCtrl)
		mockHelper = NewMockpreflightReconcilerHelper(mockCtrl)
		p = &preflightValidationReconciler{
			client:       mockClient,
			metricsAPI:   mockMetrics,
			preflightAPI: mockPreflight,
			helper:       mockHelper,
		}
	})

	ctx := context.Background()
	pv := &v1beta2.PreflightValidation{}

	DescribeTable("check good and error flows", func(getModulesDataFailed, updateStatusFailed, runValidationFailed, notAllModulesVerified bool) {
		returnedError := errors.New("some error")
		modsWithMapping := []*api.ModuleLoaderData{}
		modsWithoutMapping := []types.NamespacedName{}

		mockMetrics.EXPECT().SetKMMPreflightsNum(1)
		if getModulesDataFailed {
			mockHelper.EXPECT().getModulesData(ctx, pv).Return(nil, nil, returnedError)
			goto executeTestFunction
		}
		mockHelper.EXPECT().getModulesData(ctx, pv).Return(modsWithMapping, modsWithoutMapping, nil)
		if updateStatusFailed {
			mockHelper.EXPECT().updateStatus(ctx, modsWithMapping, modsWithoutMapping, pv).Return(returnedError)
			goto executeTestFunction
		}
		mockHelper.EXPECT().updateStatus(ctx, modsWithMapping, modsWithoutMapping, pv).Return(nil)
		if runValidationFailed {
			mockHelper.EXPECT().processPreflightValidation(ctx, modsWithMapping, pv).Return(returnedError)
			goto executeTestFunction
		}
		mockHelper.EXPECT().processPreflightValidation(ctx, modsWithMapping, pv).Return(nil)
		mockPreflight.EXPECT().AllModulesVerified(pv).Return(!notAllModulesVerified)

	executeTestFunction:
		_, err := p.Reconcile(ctx, pv)
		if getModulesDataFailed || updateStatusFailed || runValidationFailed {
			Expect(err).To(HaveOccurred())
		} else {
			Expect(err).To(BeNil())
		}
	},
		Entry("getModulesData failed", true, false, false, false),
		Entry("updateStatus failed", false, true, false, false),
		Entry("processPreflightValidation failed", false, false, true, false),
		Entry("not all modules were verified", false, false, false, true),
		Entry("good flow", false, false, false, false),
	)
})

var _ = Describe("updateStatus", func() {
	var (
		mockCtrl         *gomock.Controller
		mockClient       *client.MockClient
		mockStatusWriter *client.MockStatusWriter
		mockPreflight    *preflight.MockPreflightAPI
		mockMic          *mic.MockMIC
		p                preflightReconcilerHelper
	)

	BeforeEach(func() {
		mockCtrl = gomock.NewController(GinkgoT())
		mockClient = client.NewMockClient(mockCtrl)
		mockStatusWriter = client.NewMockStatusWriter(mockCtrl)
		mockPreflight = preflight.NewMockPreflightAPI(mockCtrl)
		mockMic = mic.NewMockMIC(mockCtrl)
		p = newPreflightReconcilerHelper(mockClient, mockMic, nil, nil, mockPreflight)
	})

	ctx := context.Background()
	pv := &v1beta2.PreflightValidation{}

	It("should update status", func() {
		foundMic1 := &kmmv1beta1.ModuleImagesConfig{}
		foundMic2 := &kmmv1beta1.ModuleImagesConfig{}
		foundMic3 := &kmmv1beta1.ModuleImagesConfig{}
		modsWithMapping := []*api.ModuleLoaderData{
			{
				Name:           "mld name1",
				Namespace:      "mld namespace1",
				ContainerImage: "mld container image1",
			},
			{
				Name:           "mld name2",
				Namespace:      "mld namespace2",
				ContainerImage: "mld container image2",
			},
			{
				Name:           "mld name3",
				Namespace:      "mld namespace3",
				ContainerImage: "mld container image3",
			},
			{
				Name:           "mld name4",
				Namespace:      "mld namespace4",
				ContainerImage: "mld container image4",
			},
		}
		modsWithoutMapping := []types.NamespacedName{
			{
				Name:      "some name",
				Namespace: "some namespace",
			},
		}

		gomock.InOrder(
			mockPreflight.EXPECT().SetModuleStatus(pv, "some namespace", "some name", v1beta2.VerificationFailure, "mapping not found"),
			mockMic.EXPECT().Get(ctx, "mld name1-preflight", "mld namespace1").Return(foundMic1, nil),
			mockMic.EXPECT().GetImageState(foundMic1, "mld container image1").Return(kmmv1beta1.ImageExists),
			mockPreflight.EXPECT().SetModuleStatus(pv, "mld namespace1", "mld name1", v1beta2.VerificationSuccess, "verified image exists"),
			mockMic.EXPECT().Get(ctx, "mld name2-preflight", "mld namespace2").Return(foundMic2, nil),
			mockMic.EXPECT().GetImageState(foundMic2, "mld container image2").Return(kmmv1beta1.ImageDoesNotExist),
			mockPreflight.EXPECT().SetModuleStatus(pv, "mld namespace2", "mld name2", v1beta2.VerificationFailure, "verified image does not exist"),
			mockMic.EXPECT().Get(ctx, "mld name3-preflight", "mld namespace3").Return(foundMic3, nil),
			mockMic.EXPECT().GetImageState(foundMic3, "mld container image3").Return(kmmv1beta1.ImageState("")),
			mockPreflight.EXPECT().SetModuleStatus(pv, "mld namespace3", "mld name3", v1beta2.VerificationInProgress, "verification is not finished yet"),
			mockMic.EXPECT().Get(ctx, "mld name4-preflight", "mld namespace4").Return(nil, fmt.Errorf("some error")),
			mockPreflight.EXPECT().SetModuleStatus(pv, "mld namespace4", "mld name4", v1beta2.VerificationInProgress, "verification is not finished yet"),
			mockClient.EXPECT().Status().Return(mockStatusWriter),
			mockStatusWriter.EXPECT().Patch(ctx, pv, gomock.Any()).Return(nil),
		)

		err := p.updateStatus(ctx, modsWithMapping, modsWithoutMapping, pv)
		Expect(err).To(BeNil())
	})
})

var _ = Describe("getModulesData", func() {
	var (
		mockCtrl      *gomock.Controller
		mockClient    *client.MockClient
		mockMic       *mic.MockMIC
		mockPreflight *preflight.MockPreflightAPI
		mockKernel    *module.MockKernelMapper
		p             preflightReconcilerHelper
	)

	BeforeEach(func() {
		mockCtrl = gomock.NewController(GinkgoT())
		mockClient = client.NewMockClient(mockCtrl)
		mockMic = mic.NewMockMIC(mockCtrl)
		mockPreflight = preflight.NewMockPreflightAPI(mockCtrl)
		mockKernel = module.NewMockKernelMapper(mockCtrl)
		p = newPreflightReconcilerHelper(mockClient, mockMic, nil, mockKernel, mockPreflight)
	})

	ctx := context.Background()
	pv := &v1beta2.PreflightValidation{
		Spec: v1beta2.PreflightValidationSpec{
			KernelVersion: "some kernel version",
		},
	}

	It("should get modules data", func() {
		testMod1 := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{Name: "testMod1 name", Namespace: "testMod1 namespace"},
		}
		testMod2 := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{Name: "testMod2 name", Namespace: "testMod2 namespace"},
		}
		testMod3 := kmmv1beta1.Module{}
		now := metav1.Now()
		testMod3.SetDeletionTimestamp(&now)
		returnedMLD := api.ModuleLoaderData{Name: "testMod1 name", Namespace: "testMod1 namespace"}
		gomock.InOrder(
			mockClient.EXPECT().List(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ interface{}, list *kmmv1beta1.ModuleList, _ ...interface{}) error {
					list.Items = []kmmv1beta1.Module{testMod1, testMod2, testMod3}
					return nil
				},
			),
			mockKernel.EXPECT().GetModuleLoaderDataForKernel(&testMod1, "some kernel version").Return(&returnedMLD, nil),
			mockKernel.EXPECT().GetModuleLoaderDataForKernel(&testMod2, "some kernel version").Return(nil, module.ErrNoMatchingKernelMapping),
		)

		modsWithMapping, modsWithoutMapping, err := p.getModulesData(ctx, pv)
		Expect(err).To(BeNil())
		Expect(modsWithMapping).To(Equal([]*api.ModuleLoaderData{&returnedMLD}))
		Expect(modsWithoutMapping).To(Equal([]types.NamespacedName{{Name: testMod2.Name, Namespace: testMod2.Namespace}}))
	})
})

var _ = Describe("processPreflightValidation", func() {
	var (
		mockCtrl      *gomock.Controller
		mockClient    *client.MockClient
		mockMic       *mic.MockMIC
		mockPreflight *preflight.MockPreflightAPI
		p             preflightReconcilerHelper
	)

	BeforeEach(func() {
		mockCtrl = gomock.NewController(GinkgoT())
		mockClient = client.NewMockClient(mockCtrl)
		mockMic = mic.NewMockMIC(mockCtrl)
		mockPreflight = preflight.NewMockPreflightAPI(mockCtrl)
		p = newPreflightReconcilerHelper(mockClient, mockMic, nil, nil, mockPreflight)
	})

	ctx := context.Background()
	pv := &v1beta2.PreflightValidation{}

	It("should run preflight validation", func() {
		modsWithMapping := []*api.ModuleLoaderData{
			{
				Name:           "mld name1",
				Namespace:      "mld namespace1",
				ContainerImage: "mld container image1",
			},
			{
				Name:           "mld name2",
				Namespace:      "mld namespace2",
				ContainerImage: "mld container image2",
			},
			{
				Name:           "mld name3",
				Namespace:      "mld namespace3",
				ContainerImage: "mld container image3",
			},
			{
				Name:           "mld name4",
				Namespace:      "mld namespace4",
				ContainerImage: "mld container image4",
			},
		}

		expectedMic3 := kmmv1beta1.ModuleImageSpec{
			Image: "mld container image3",
		}
		expectedMic4 := kmmv1beta1.ModuleImageSpec{
			Image: "mld container image4",
		}

		gomock.InOrder(
			mockPreflight.EXPECT().GetModuleStatus(pv, "mld namespace1", "mld name1").Return(v1beta2.VerificationSuccess),
			mockPreflight.EXPECT().GetModuleStatus(pv, "mld namespace2", "mld name2").Return(v1beta2.VerificationFailure),
			mockPreflight.EXPECT().GetModuleStatus(pv, "mld namespace3", "mld name3").Return(v1beta2.VerificationInProgress),
			mockMic.EXPECT().CreateOrPatch(ctx, "mld name3-preflight", "mld namespace3", []kmmv1beta1.ModuleImageSpec{expectedMic3},
				nil, v1.PullPolicy(""), pv).Return(nil),
			mockPreflight.EXPECT().GetModuleStatus(pv, "mld namespace4", "mld name4").Return(""),
			mockMic.EXPECT().CreateOrPatch(ctx, "mld name4-preflight", "mld namespace4", []kmmv1beta1.ModuleImageSpec{expectedMic4},
				nil, v1.PullPolicy(""), pv).Return(nil),
		)

		err := p.processPreflightValidation(ctx, modsWithMapping, pv)
		Expect(err).To(BeNil())
	})
})
