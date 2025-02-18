package controllers

import (
	"context"
	"fmt"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/api"
	"github.com/kubernetes-sigs/kernel-module-management/internal/build"
	"github.com/kubernetes-sigs/kernel-module-management/internal/module"
	"github.com/kubernetes-sigs/kernel-module-management/internal/node"
	"github.com/kubernetes-sigs/kernel-module-management/internal/pod"
	"github.com/kubernetes-sigs/kernel-module-management/internal/sign"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var _ = Describe("BuildSignReconciler_Reconcile", func() {
	const moduleName = "test-module"

	var (
		ctrl            *gomock.Controller
		mockReconHelper *MockbuildSignReconcilerHelperAPI
		mod             *kmmv1beta1.Module
		bsr             *BuildSignReconciler
		mn              *node.MockNode
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockReconHelper = NewMockbuildSignReconcilerHelperAPI(ctrl)
		mn = node.NewMockNode(ctrl)

		mod = &kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: moduleName},
		}

		bsr = &BuildSignReconciler{
			reconHelperAPI: mockReconHelper,
			nodeAPI:        mn,
		}
	})

	ctx := context.Background()

	DescribeTable("check error flows", func(getNodesError, getMappingsError, handleBuildError, handleSignError bool) {
		mod := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: moduleName},
		}
		selectNodesList := []v1.Node{v1.Node{}}
		mappings := map[string]*api.ModuleLoaderData{"kernelVersion": &api.ModuleLoaderData{}}
		returnedError := fmt.Errorf("some error")
		if getNodesError {
			mn.EXPECT().GetNodesListBySelector(ctx, mod.Spec.Selector, nil).Return(nil, returnedError)
			goto executeTestFunction
		}
		mn.EXPECT().GetNodesListBySelector(ctx, mod.Spec.Selector, nil).Return(selectNodesList, nil)
		if getMappingsError {
			mockReconHelper.EXPECT().getRelevantKernelMappings(ctx, &mod, selectNodesList).Return(nil, returnedError)
			goto executeTestFunction
		}
		mockReconHelper.EXPECT().getRelevantKernelMappings(ctx, &mod, selectNodesList).Return(mappings, nil)
		if handleBuildError {
			mockReconHelper.EXPECT().handleBuild(ctx, mappings["kernelVersion"]).Return(false, returnedError)
			goto executeTestFunction
		}
		mockReconHelper.EXPECT().handleBuild(ctx, mappings["kernelVersion"]).Return(true, nil)
		if handleSignError {
			mockReconHelper.EXPECT().handleSigning(ctx, mappings["kernelVersion"]).Return(false, returnedError)
			goto executeTestFunction
		}
		mockReconHelper.EXPECT().handleSigning(ctx, mappings["kernelVersion"]).Return(true, nil)
		mockReconHelper.EXPECT().garbageCollect(ctx, &mod, mappings).Return(returnedError)

	executeTestFunction:
		res, err := bsr.Reconcile(ctx, &mod)

		Expect(res).To(Equal(reconcile.Result{}))
		Expect(err).To(HaveOccurred())
	},
		Entry("getNodesListBySelector failed", true, false, false, false),
		Entry("getRelevantKernelMappingsAndNodes failed", false, true, false, false),
		Entry("handleBuild failed ", false, false, true, false),
		Entry("handleSign failed", false, false, false, true),
		Entry("garbageCollect failed", false, false, false, false),
	)

	It("Build has not completed successfully", func() {
		selectNodesList := []v1.Node{v1.Node{}}
		mappings := map[string]*api.ModuleLoaderData{"kernelVersion": &api.ModuleLoaderData{}}
		gomock.InOrder(
			mn.EXPECT().GetNodesListBySelector(ctx, mod.Spec.Selector, nil).Return(selectNodesList, nil),
			mockReconHelper.EXPECT().getRelevantKernelMappings(ctx, mod, selectNodesList).Return(mappings, nil),
			mockReconHelper.EXPECT().handleBuild(ctx, mappings["kernelVersion"]).Return(false, nil),
			mockReconHelper.EXPECT().garbageCollect(ctx, mod, mappings).Return(nil),
		)

		res, err := bsr.Reconcile(ctx, mod)

		Expect(res).To(Equal(reconcile.Result{}))
		Expect(err).NotTo(HaveOccurred())
	})

	It("Signing has not completed successfully", func() {
		selectNodesList := []v1.Node{v1.Node{}}
		mappings := map[string]*api.ModuleLoaderData{"kernelVersion": &api.ModuleLoaderData{}}
		gomock.InOrder(
			mn.EXPECT().GetNodesListBySelector(ctx, mod.Spec.Selector, nil).Return(selectNodesList, nil),
			mockReconHelper.EXPECT().getRelevantKernelMappings(ctx, mod, selectNodesList).Return(mappings, nil),
			mockReconHelper.EXPECT().handleBuild(ctx, mappings["kernelVersion"]).Return(true, nil),
			mockReconHelper.EXPECT().handleSigning(ctx, mappings["kernelVersion"]).Return(false, nil),
			mockReconHelper.EXPECT().garbageCollect(ctx, mod, mappings).Return(nil),
		)

		res, err := bsr.Reconcile(ctx, mod)

		Expect(res).To(Equal(reconcile.Result{}))
		Expect(err).NotTo(HaveOccurred())
	})

	It("Good flow", func() {

		selectNodesList := []v1.Node{v1.Node{}}
		mappings := map[string]*api.ModuleLoaderData{"kernelVersion": &api.ModuleLoaderData{}}
		gomock.InOrder(
			mn.EXPECT().GetNodesListBySelector(ctx, mod.Spec.Selector, nil).Return(selectNodesList, nil),
			mockReconHelper.EXPECT().getRelevantKernelMappings(ctx, mod, selectNodesList).Return(mappings, nil),
			mockReconHelper.EXPECT().handleBuild(ctx, mappings["kernelVersion"]).Return(true, nil),
			mockReconHelper.EXPECT().handleSigning(ctx, mappings["kernelVersion"]).Return(true, nil),
			mockReconHelper.EXPECT().garbageCollect(ctx, mod, mappings).Return(nil),
		)

		res, err := bsr.Reconcile(ctx, mod)

		Expect(res).To(Equal(reconcile.Result{}))
		Expect(err).NotTo(HaveOccurred())
	})
})

var _ = Describe("BuildSignReconciler_getRelevantKernelMappings", func() {
	var (
		ctrl   *gomock.Controller
		mockKM *module.MockKernelMapper
		bsrh   buildSignReconcilerHelperAPI
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockKM = module.NewMockKernelMapper(ctrl)
		bsrh = newBuildSignReconcilerHelper(nil, nil, nil, mockKM)
	})

	node1 := v1.Node{
		Status: v1.NodeStatus{
			NodeInfo: v1.NodeSystemInfo{
				KernelVersion: "kernelVersion1",
			},
		},
	}
	node2 := v1.Node{
		Status: v1.NodeStatus{
			NodeInfo: v1.NodeSystemInfo{
				KernelVersion: "kernelVersion2",
			},
		},
	}
	node3 := v1.Node{
		Status: v1.NodeStatus{
			NodeInfo: v1.NodeSystemInfo{
				KernelVersion: "kernelVersion1",
			},
		},
	}

	mld1 := api.ModuleLoaderData{Name: "name1"}
	mld2 := api.ModuleLoaderData{Name: "name2"}

	It("good flow, all mappings exist", func() {
		nodes := []v1.Node{node1, node2, node3}
		expectedMappings := map[string]*api.ModuleLoaderData{"kernelVersion1": &mld1, "kernelVersion2": &mld2}
		gomock.InOrder(
			mockKM.EXPECT().GetModuleLoaderDataForKernel(&kmmv1beta1.Module{}, node1.Status.NodeInfo.KernelVersion).Return(&mld1, nil),
			mockKM.EXPECT().GetModuleLoaderDataForKernel(&kmmv1beta1.Module{}, node2.Status.NodeInfo.KernelVersion).Return(&mld2, nil),
		)

		mappings, err := bsrh.getRelevantKernelMappings(context.Background(), &kmmv1beta1.Module{}, nodes)

		Expect(err).NotTo(HaveOccurred())
		Expect(mappings).To(Equal(expectedMappings))

	})

	It("good flow, one mapping does not exist", func() {
		nodes := []v1.Node{node1, node2, node3}
		expectedMappings := map[string]*api.ModuleLoaderData{"kernelVersion1": &mld1}
		gomock.InOrder(
			mockKM.EXPECT().GetModuleLoaderDataForKernel(&kmmv1beta1.Module{}, node1.Status.NodeInfo.KernelVersion).Return(&mld1, nil),
			mockKM.EXPECT().GetModuleLoaderDataForKernel(&kmmv1beta1.Module{}, node2.Status.NodeInfo.KernelVersion).Return(nil, fmt.Errorf("some error")),
		)

		mappings, err := bsrh.getRelevantKernelMappings(context.Background(), &kmmv1beta1.Module{}, nodes)

		Expect(err).NotTo(HaveOccurred())
		Expect(mappings).To(Equal(expectedMappings))

	})

})

var _ = Describe("BuildSignReconciler_handleBuild", func() {
	var (
		ctrl   *gomock.Controller
		mockBM *build.MockManager
		bsrh   buildSignReconcilerHelperAPI
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockBM = build.NewMockManager(ctrl)
		bsrh = newBuildSignReconcilerHelper(nil, mockBM, nil, nil)
	})

	const (
		moduleName    = "test-module"
		kernelVersion = "1.2.3"
		imageName     = "test-image"
	)

	It("should do nothing when build is skipped", func() {
		mld := &api.ModuleLoaderData{KernelVersion: kernelVersion}

		gomock.InOrder(
			mockBM.EXPECT().ShouldSync(gomock.Any(), mld).Return(false, nil),
		)

		completed, err := bsrh.handleBuild(context.Background(), mld)
		Expect(err).NotTo(HaveOccurred())
		Expect(completed).To(BeTrue())
	})

	It("should record that a pod was created when the build sync returns StatusCreated", func() {
		mld := api.ModuleLoaderData{
			Name:           moduleName,
			Namespace:      namespace,
			ContainerImage: imageName,
			Build:          &kmmv1beta1.Build{},
			KernelVersion:  kernelVersion,
		}

		gomock.InOrder(
			mockBM.EXPECT().ShouldSync(gomock.Any(), &mld).Return(true, nil),
			mockBM.EXPECT().Sync(gomock.Any(), &mld, true, mld.Owner).Return(pod.Status(pod.StatusCreated), nil),
		)

		completed, err := bsrh.handleBuild(context.Background(), &mld)

		Expect(err).NotTo(HaveOccurred())
		Expect(completed).To(BeFalse())
	})

	It("should record that a pod was completed, when the build sync returns StatusCompleted", func() {
		mld := &api.ModuleLoaderData{
			Name:           moduleName,
			Namespace:      namespace,
			ContainerImage: imageName,
			Build:          &kmmv1beta1.Build{},
			Owner:          &kmmv1beta1.Module{},
			KernelVersion:  kernelVersion,
		}
		gomock.InOrder(
			mockBM.EXPECT().ShouldSync(gomock.Any(), mld).Return(true, nil),
			mockBM.EXPECT().Sync(gomock.Any(), mld, true, mld.Owner).Return(pod.Status(pod.StatusCompleted), nil),
		)

		completed, err := bsrh.handleBuild(context.Background(), mld)

		Expect(err).NotTo(HaveOccurred())
		Expect(completed).To(BeTrue())
	})
})

var _ = Describe("BuildSignReconciler_handleSigning", func() {
	var (
		ctrl   *gomock.Controller
		mockSM *sign.MockSignManager
		bsrh   buildSignReconcilerHelperAPI
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockSM = sign.NewMockSignManager(ctrl)
		bsrh = newBuildSignReconcilerHelper(nil, nil, mockSM, nil)
	})

	const (
		moduleName    = "test-module"
		kernelVersion = "1.2.3"
		imageName     = "test-image"
	)

	It("should do nothing when build is skipped", func() {
		mld := &api.ModuleLoaderData{
			ContainerImage: imageName,
			KernelVersion:  kernelVersion,
		}

		gomock.InOrder(
			mockSM.EXPECT().ShouldSync(gomock.Any(), mld).Return(false, nil),
		)

		completed, err := bsrh.handleSigning(context.Background(), mld)

		Expect(err).NotTo(HaveOccurred())
		Expect(completed).To(BeTrue())
	})

	It("should record that a pod was created when the sign sync returns StatusCreated", func() {
		mld := api.ModuleLoaderData{
			Name:           moduleName,
			Namespace:      namespace,
			ContainerImage: imageName,
			Sign:           &kmmv1beta1.Sign{},
			KernelVersion:  kernelVersion,
		}

		gomock.InOrder(
			mockSM.EXPECT().ShouldSync(gomock.Any(), &mld).Return(true, nil),
			mockSM.EXPECT().Sync(gomock.Any(), &mld, "", true, mld.Owner).Return(pod.Status(pod.StatusCreated), nil),
		)

		completed, err := bsrh.handleSigning(context.Background(), &mld)

		Expect(err).NotTo(HaveOccurred())
		Expect(completed).To(BeFalse())
	})

	It("should record that a pod was completed when the sign sync returns StatusCompleted", func() {
		mld := api.ModuleLoaderData{
			Name:           moduleName,
			Namespace:      namespace,
			ContainerImage: imageName,
			Sign:           &kmmv1beta1.Sign{},
			KernelVersion:  kernelVersion,
		}

		gomock.InOrder(
			mockSM.EXPECT().ShouldSync(gomock.Any(), &mld).Return(true, nil),
			mockSM.EXPECT().Sync(gomock.Any(), &mld, "", true, mld.Owner).Return(pod.Status(pod.StatusCompleted), nil),
		)

		completed, err := bsrh.handleSigning(context.Background(), &mld)

		Expect(err).NotTo(HaveOccurred())
		Expect(completed).To(BeTrue())
	})

	It("should run sign sync with the previous image as well when module build and sign are specified", func() {
		mld := &api.ModuleLoaderData{
			Name:           moduleName,
			Namespace:      namespace,
			ContainerImage: imageName,
			Sign:           &kmmv1beta1.Sign{},
			Build:          &kmmv1beta1.Build{},
			Owner:          &kmmv1beta1.Module{},
			KernelVersion:  kernelVersion,
		}

		gomock.InOrder(
			mockSM.EXPECT().ShouldSync(gomock.Any(), mld).Return(true, nil),
			mockSM.EXPECT().Sync(gomock.Any(), mld, imageName+":"+namespace+"_"+moduleName+"_kmm_unsigned", true, mld.Owner).
				Return(pod.Status(pod.StatusCompleted), nil),
		)

		completed, err := bsrh.handleSigning(context.Background(), mld)

		Expect(err).NotTo(HaveOccurred())
		Expect(completed).To(BeTrue())
	})
})

var _ = Describe("ModuleReconciler_garbageCollect", func() {
	var (
		ctrl   *gomock.Controller
		mockBM *build.MockManager
		mockSM *sign.MockSignManager
		bsrh   buildSignReconcilerHelperAPI
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockBM = build.NewMockManager(ctrl)
		mockSM = sign.NewMockSignManager(ctrl)
		bsrh = newBuildSignReconcilerHelper(nil, mockBM, mockSM, nil)
	})

	mod := &kmmv1beta1.Module{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "moduleName",
			Namespace: "namespace",
		},
	}

	It("good flow", func() {
		mldMappings := map[string]*api.ModuleLoaderData{
			"kernelVersion1": &api.ModuleLoaderData{}, "kernelVersion2": &api.ModuleLoaderData{},
		}
		gomock.InOrder(
			mockBM.EXPECT().GarbageCollect(context.Background(), mod.Name, mod.Namespace, mod).Return(nil, nil),
			mockSM.EXPECT().GarbageCollect(context.Background(), mod.Name, mod.Namespace, mod).Return(nil, nil),
		)

		err := bsrh.garbageCollect(context.Background(), mod, mldMappings)

		Expect(err).NotTo(HaveOccurred())
	})

	DescribeTable("check error flows", func(buildError bool) {
		returnedError := fmt.Errorf("some error")
		mldMappings := map[string]*api.ModuleLoaderData{
			"kernelVersion1": &api.ModuleLoaderData{}, "kernelVersion2": &api.ModuleLoaderData{},
		}
		if buildError {
			mockBM.EXPECT().GarbageCollect(context.Background(), mod.Name, mod.Namespace, mod).Return(nil, returnedError)
			goto executeTestFunction
		}
		mockBM.EXPECT().GarbageCollect(context.Background(), mod.Name, mod.Namespace, mod).Return(nil, nil)
		mockSM.EXPECT().GarbageCollect(context.Background(), mod.Name, mod.Namespace, mod).Return(nil, returnedError)
	executeTestFunction:
		err := bsrh.garbageCollect(context.Background(), mod, mldMappings)

		Expect(err).To(HaveOccurred())
	},
		Entry("build GC failed", true),
		Entry("sign GC failed", false),
	)

})
