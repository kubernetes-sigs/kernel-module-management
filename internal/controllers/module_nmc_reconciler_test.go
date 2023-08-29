package controllers

import (
	"context"
	"fmt"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/api"
	"github.com/kubernetes-sigs/kernel-module-management/internal/client"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	"github.com/kubernetes-sigs/kernel-module-management/internal/module"
	"github.com/kubernetes-sigs/kernel-module-management/internal/nmc"
	"github.com/kubernetes-sigs/kernel-module-management/internal/registry"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var _ = Describe("ModuleNMCReconciler_Reconcile", func() {
	var (
		ctrl            *gomock.Controller
		mockKernel      *module.MockKernelMapper
		mockReconHelper *MockmoduleNMCReconcilerHelperAPI
		mnr             *ModuleNMCReconciler
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockKernel = module.NewMockKernelMapper(ctrl)
		mockReconHelper = NewMockmoduleNMCReconcilerHelperAPI(ctrl)

		mnr = &ModuleNMCReconciler{
			kernelAPI:   mockKernel,
			reconHelper: mockReconHelper,
		}
	})

	const moduleName = "test-module"
	const kernelVersion = "kernel version"

	nsn := types.NamespacedName{
		Name:      moduleName,
		Namespace: namespace,
	}

	req := reconcile.Request{NamespacedName: nsn}

	ctx := context.Background()

	It("should return ok if module has been deleted", func() {
		mockReconHelper.EXPECT().getRequestedModule(ctx, nsn).Return(nil, apierrors.NewNotFound(schema.GroupResource{}, "whatever"))

		res, err := mnr.Reconcile(ctx, req)

		Expect(res).To(Equal(reconcile.Result{}))
		Expect(err).NotTo(HaveOccurred())
	})

	It("should run finalization in case module is being deleted", func() {
		mod := kmmv1beta1.Module{}
		mod.SetDeletionTimestamp(&metav1.Time{})
		gomock.InOrder(
			mockReconHelper.EXPECT().getRequestedModule(ctx, nsn).Return(&mod, nil),
			mockReconHelper.EXPECT().finalizeModule(ctx, &mod).Return(nil),
		)

		res, err := mnr.Reconcile(ctx, req)

		Expect(res).To(Equal(reconcile.Result{}))
		Expect(err).NotTo(HaveOccurred())
	})

	DescribeTable("check error flows", func(getModuleError,
		setFinalizerError,
		getNodesError,
		getMLDError,
		shouldRunOnNodeError,
		shouldBeOnNode bool) {
		mod := kmmv1beta1.Module{}
		node := v1.Node{
			Status: v1.NodeStatus{
				NodeInfo: v1.NodeSystemInfo{KernelVersion: kernelVersion},
			},
		}
		mld := api.ModuleLoaderData{KernelVersion: kernelVersion}
		returnedError := fmt.Errorf("some error")
		if getModuleError {
			mockReconHelper.EXPECT().getRequestedModule(ctx, nsn).Return(nil, returnedError)
			goto executeTestFunction
		}
		mockReconHelper.EXPECT().getRequestedModule(ctx, nsn).Return(&mod, nil)
		if setFinalizerError {
			mockReconHelper.EXPECT().setFinalizer(ctx, &mod).Return(returnedError)
			goto executeTestFunction
		}
		mockReconHelper.EXPECT().setFinalizer(ctx, &mod).Return(nil)
		if getNodesError {
			mockReconHelper.EXPECT().getNodesList(ctx).Return(nil, returnedError)
			goto executeTestFunction
		}
		mockReconHelper.EXPECT().getNodesList(ctx).Return([]v1.Node{node}, nil)
		if getMLDError {
			mockKernel.EXPECT().GetModuleLoaderDataForKernel(&mod, kernelVersion).Return(nil, returnedError)
			goto executeTestFunction
		}
		mockKernel.EXPECT().GetModuleLoaderDataForKernel(&mod, kernelVersion).Return(&mld, nil)
		if shouldRunOnNodeError {
			mockReconHelper.EXPECT().shouldModuleRunOnNode(node, &mld).Return(shouldBeOnNode, returnedError)
			goto executeTestFunction
		}
		mockReconHelper.EXPECT().shouldModuleRunOnNode(node, &mld).Return(shouldBeOnNode, nil)
		if shouldBeOnNode {
			mockReconHelper.EXPECT().enableModuleOnNode(ctx, &mld, node.Name, kernelVersion).Return(returnedError)
		} else {
			mockReconHelper.EXPECT().disableModuleOnNode(ctx, mod.Namespace, mod.Name, node.Name).Return(returnedError)
		}

	executeTestFunction:
		res, err := mnr.Reconcile(ctx, req)

		Expect(res).To(Equal(reconcile.Result{}))
		Expect(err).To(HaveOccurred())

	},
		Entry("getRequestedModule failed", true, false, false, false, false, false),
		Entry("setFinalizer failed", false, true, false, false, false, false),
		Entry("getNodesList failed", false, false, true, false, false, false),
		Entry("getModuleLoaderDataForKernel failed", false, false, false, true, false, false),
		Entry("shouldModuleRunOnNode failed", false, false, false, false, true, false),
		Entry("enableModuleOnNode failed", false, false, false, false, false, true),
		Entry("disableModuleOnNode failed", false, false, false, false, false, false),
	)

	It("Good flow, kernel mapping exists, should run on node", func() {
		mod := kmmv1beta1.Module{}
		node := v1.Node{
			Status: v1.NodeStatus{
				NodeInfo: v1.NodeSystemInfo{KernelVersion: kernelVersion},
			},
		}
		mld := api.ModuleLoaderData{KernelVersion: kernelVersion}
		gomock.InOrder(
			mockReconHelper.EXPECT().getRequestedModule(ctx, nsn).Return(&mod, nil),
			mockReconHelper.EXPECT().setFinalizer(ctx, &mod).Return(nil),
			mockReconHelper.EXPECT().getNodesList(ctx).Return([]v1.Node{node}, nil),
			mockKernel.EXPECT().GetModuleLoaderDataForKernel(&mod, kernelVersion).Return(&mld, nil),
			mockReconHelper.EXPECT().shouldModuleRunOnNode(node, &mld).Return(true, nil),
			mockReconHelper.EXPECT().enableModuleOnNode(ctx, &mld, node.Name, kernelVersion).Return(nil),
		)

		res, err := mnr.Reconcile(ctx, req)

		Expect(res).To(Equal(reconcile.Result{}))
		Expect(err).NotTo(HaveOccurred())
	})

	It("Good flow, kernel mapping missing, should not run on node", func() {
		mod := kmmv1beta1.Module{}
		node := v1.Node{
			Status: v1.NodeStatus{
				NodeInfo: v1.NodeSystemInfo{KernelVersion: kernelVersion},
			},
		}
		gomock.InOrder(
			mockReconHelper.EXPECT().getRequestedModule(ctx, nsn).Return(&mod, nil),
			mockReconHelper.EXPECT().setFinalizer(ctx, &mod).Return(nil),
			mockReconHelper.EXPECT().getNodesList(ctx).Return([]v1.Node{node}, nil),
			mockKernel.EXPECT().GetModuleLoaderDataForKernel(&mod, kernelVersion).Return(nil, module.ErrNoMatchingKernelMapping),
			mockReconHelper.EXPECT().shouldModuleRunOnNode(node, nil).Return(false, nil),
			mockReconHelper.EXPECT().disableModuleOnNode(ctx, mod.Namespace, mod.Name, node.Name).Return(nil),
		)

		res, err := mnr.Reconcile(ctx, req)

		Expect(res).To(Equal(reconcile.Result{}))
		Expect(err).NotTo(HaveOccurred())
	})

})

var _ = Describe("ModuleReconciler_getRequestedModule", func() {
	var (
		ctrl *gomock.Controller
		clnt *client.MockClient
		mnrh moduleNMCReconcilerHelperAPI
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		mnrh = newModuleNMCReconcilerHelper(clnt, nil, nil)
	})

	ctx := context.Background()
	moduleName := "moduleName"
	moduleNamespace := "moduleNamespace"
	nsn := types.NamespacedName{Name: moduleName, Namespace: moduleNamespace}

	It("good flow", func() {
		clnt.EXPECT().Get(ctx, nsn, gomock.Any()).DoAndReturn(
			func(_ interface{}, _ interface{}, module *kmmv1beta1.Module, _ ...ctrlclient.GetOption) error {
				module.ObjectMeta = metav1.ObjectMeta{Name: moduleName, Namespace: moduleNamespace}
				return nil
			},
		)

		mod, err := mnrh.getRequestedModule(ctx, nsn)
		Expect(err).NotTo(HaveOccurred())
		Expect(mod.Name).To(Equal(moduleName))
		Expect(mod.Namespace).To(Equal(moduleNamespace))
	})

	It("error flow", func() {

		clnt.EXPECT().Get(ctx, nsn, gomock.Any()).Return(fmt.Errorf("some error"))
		mod, err := mnrh.getRequestedModule(ctx, nsn)
		Expect(err).To(HaveOccurred())
		Expect(mod).To(BeNil())
	})

})

var _ = Describe("ModuleReconciler_getRequestedModule", func() {
	var (
		ctrl *gomock.Controller
		clnt *client.MockClient
		mnrh moduleNMCReconcilerHelperAPI
		mod  kmmv1beta1.Module
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		mnrh = newModuleNMCReconcilerHelper(clnt, nil, nil)
		mod = kmmv1beta1.Module{}
	})

	ctx := context.Background()

	It("finalizer is already set", func() {
		controllerutil.AddFinalizer(&mod, constants.ModuleFinalizer)
		err := mnrh.setFinalizer(ctx, &mod)
		Expect(err).NotTo(HaveOccurred())
	})

	It("finalizer is not set", func() {
		clnt.EXPECT().Patch(ctx, &mod, gomock.Any()).Return(nil)

		err := mnrh.setFinalizer(ctx, &mod)
		Expect(err).NotTo(HaveOccurred())
	})

	It("finalizer is not set, failed to patch the Module", func() {
		clnt.EXPECT().Patch(ctx, &mod, gomock.Any()).Return(fmt.Errorf("some error"))

		err := mnrh.setFinalizer(ctx, &mod)
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("ModuleReconciler_getNodes", func() {
	var (
		ctrl *gomock.Controller
		clnt *client.MockClient
		mnrh moduleNMCReconcilerHelperAPI
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		mnrh = newModuleNMCReconcilerHelper(clnt, nil, nil)
	})

	ctx := context.Background()

	It("list failed", func() {
		clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).Return(fmt.Errorf("some error"))

		nodes, err := mnrh.getNodesList(ctx)

		Expect(err).To(HaveOccurred())
		Expect(nodes).To(BeNil())
	})

	It("Return nodes", func() {
		node1 := v1.Node{}
		node2 := v1.Node{}
		node3 := v1.Node{}
		clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ interface{}, list *v1.NodeList, _ ...interface{}) error {
				list.Items = []v1.Node{node1, node2, node3}
				return nil
			},
		)
		nodes, err := mnrh.getNodesList(ctx)

		Expect(err).NotTo(HaveOccurred())
		Expect(nodes).To(Equal([]v1.Node{node1, node2, node3}))

	})
})

var _ = Describe("finalizeModule", func() {
	var (
		ctx    context.Context
		ctrl   *gomock.Controller
		clnt   *client.MockClient
		helper *nmc.MockHelper
		mnrh   moduleNMCReconcilerHelperAPI
		mod    *kmmv1beta1.Module
	)

	BeforeEach(func() {
		ctx = context.Background()
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		helper = nmc.NewMockHelper(ctrl)
		mnrh = newModuleNMCReconcilerHelper(clnt, nil, helper)
		mod = &kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{Name: "moduleName", Namespace: "moduleNamespace"},
		}
	})

	It("failed to get list of NMCs", func() {
		clnt.EXPECT().List(ctx, gomock.Any()).Return(fmt.Errorf("some error"))

		err := mnrh.finalizeModule(ctx, mod)

		Expect(err).To(HaveOccurred())
	})

	It("multiple errors occurred", func() {
		nmc1 := kmmv1beta1.NodeModulesConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "nmc1"},
		}
		nmc2 := kmmv1beta1.NodeModulesConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "nmc2"},
		}

		gomock.InOrder(
			clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ interface{}, list *kmmv1beta1.NodeModulesConfigList, _ ...interface{}) error {
					list.Items = []kmmv1beta1.NodeModulesConfig{nmc1, nmc2}
					return nil
				},
			),
			clnt.EXPECT().Get(ctx, gomock.Any(), gomock.Any()).Return(fmt.Errorf("some error")),
			clnt.EXPECT().Get(ctx, gomock.Any(), gomock.Any()).Return(fmt.Errorf("some error")),
		)

		err := mnrh.finalizeModule(ctx, mod)

		Expect(err).To(HaveOccurred())

	})

	It("no nmcs, patch successfull", func() {
		gomock.InOrder(
			clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ interface{}, list *kmmv1beta1.NodeModulesConfigList, _ ...interface{}) error {
					list.Items = []kmmv1beta1.NodeModulesConfig{}
					return nil
				},
			),
			clnt.EXPECT().Patch(ctx, mod, gomock.Any()).Return(nil),
		)

		err := mnrh.finalizeModule(ctx, mod)

		Expect(err).NotTo(HaveOccurred())
	})

	It("no nmcs, failed", func() {
		gomock.InOrder(
			clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ interface{}, list *kmmv1beta1.NodeModulesConfigList, _ ...interface{}) error {
					list.Items = []kmmv1beta1.NodeModulesConfig{}
					return nil
				},
			),
			clnt.EXPECT().Patch(ctx, mod, gomock.Any()).Return(fmt.Errorf("some error")),
		)

		err := mnrh.finalizeModule(ctx, mod)

		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("shouldModuleRunOnNode", func() {
	var (
		mnrh          moduleNMCReconcilerHelperAPI
		kernelVersion string
	)

	BeforeEach(func() {
		mnrh = newModuleNMCReconcilerHelper(nil, nil, nil)
		kernelVersion = "some version"
	})

	It("kernel version not equal", func() {
		node := v1.Node{
			Status: v1.NodeStatus{
				NodeInfo: v1.NodeSystemInfo{
					KernelVersion: kernelVersion,
				},
			},
		}
		mld := &api.ModuleLoaderData{
			KernelVersion: "other kernelVersion",
		}

		res, err := mnrh.shouldModuleRunOnNode(node, mld)
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(BeFalse())
	})

	It("node not schedulable", func() {
		node := v1.Node{
			Spec: v1.NodeSpec{
				Taints: []v1.Taint{
					v1.Taint{
						Effect: v1.TaintEffectNoSchedule,
					},
				},
			},
			Status: v1.NodeStatus{
				NodeInfo: v1.NodeSystemInfo{
					KernelVersion: kernelVersion,
				},
			},
		}

		mld := &api.ModuleLoaderData{
			KernelVersion: kernelVersion,
		}

		res, err := mnrh.shouldModuleRunOnNode(node, mld)
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(BeFalse())
	})

	DescribeTable("selector vs node labels verification", func(mldSelector, nodeLabels map[string]string, expectedResult bool) {
		mld := &api.ModuleLoaderData{
			KernelVersion: kernelVersion,
			Selector:      mldSelector,
		}
		node := v1.Node{
			Status: v1.NodeStatus{
				NodeInfo: v1.NodeSystemInfo{
					KernelVersion: kernelVersion,
				},
			},
		}
		node.SetLabels(nodeLabels)

		res, err := mnrh.shouldModuleRunOnNode(node, mld)
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(expectedResult))
	},
		Entry("selector label not present in nodes'",
			map[string]string{"label_1": "label_1_value", "label_2": "label_2_value"},
			map[string]string{"label_1": "label_1_value", "label_3": "label_3_value"},
			false),
		Entry("selector labels present in nodes'",
			map[string]string{"label_1": "label_1_value", "label_2": "label_2_value"},
			map[string]string{"label_1": "label_1_value", "label_2": "label_2_value"},
			true),
		Entry("no selector labels'",
			nil,
			map[string]string{"label_1": "label_1_value", "label_2": "label_2_value"},
			true),
	)
})

var _ = Describe("enableModuleOnNode", func() {
	var (
		ctx                  context.Context
		ctrl                 *gomock.Controller
		clnt                 *client.MockClient
		rgst                 *registry.MockRegistry
		mnrh                 moduleNMCReconcilerHelperAPI
		helper               *nmc.MockHelper
		mld                  *api.ModuleLoaderData
		expectedModuleConfig *kmmv1beta1.ModuleConfig
		kernelVersion        string
		nodeName             string
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		helper = nmc.NewMockHelper(ctrl)
		rgst = registry.NewMockRegistry(ctrl)
		mnrh = newModuleNMCReconcilerHelper(clnt, rgst, helper)
		kernelVersion = "some version"
		nodeName = "node name"
		ctx = context.Background()
		mld = &api.ModuleLoaderData{
			KernelVersion:        kernelVersion,
			Name:                 "moduleName",
			Namespace:            "moduleNamespace",
			InTreeModuleToRemove: "InTreeModuleToRemove",
			ContainerImage:       "containerImage",
		}

		expectedModuleConfig = &kmmv1beta1.ModuleConfig{
			KernelVersion:        mld.KernelVersion,
			ContainerImage:       mld.ContainerImage,
			InTreeModuleToRemove: mld.InTreeModuleToRemove,
			Modprobe:             mld.Modprobe,
		}
	})

	It("Image does not exists", func() {
		rgst.EXPECT().ImageExists(ctx, mld.ContainerImage, gomock.Any(), gomock.Any()).Return(false, nil)
		err := mnrh.enableModuleOnNode(ctx, mld, nodeName, kernelVersion)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Failed to check if image exists", func() {
		rgst.EXPECT().ImageExists(ctx, mld.ContainerImage, gomock.Any(), gomock.Any()).Return(false, fmt.Errorf("some error"))
		err := mnrh.enableModuleOnNode(ctx, mld, nodeName, kernelVersion)
		Expect(err).To(HaveOccurred())
	})

	It("NMC does not exists", func() {
		nmc := &kmmv1beta1.NodeModulesConfig{
			ObjectMeta: metav1.ObjectMeta{Name: nodeName},
		}

		gomock.InOrder(
			rgst.EXPECT().ImageExists(ctx, mld.ContainerImage, gomock.Any(), gomock.Any()).Return(true, nil),
			clnt.EXPECT().Get(ctx, gomock.Any(), gomock.Any()).Return(apierrors.NewNotFound(schema.GroupResource{}, "whatever")),
			helper.EXPECT().SetModuleConfig(nmc, mld, expectedModuleConfig).Return(nil),
			clnt.EXPECT().Create(ctx, gomock.Any()).Return(nil),
		)

		err := mnrh.enableModuleOnNode(ctx, mld, nodeName, kernelVersion)
		Expect(err).NotTo(HaveOccurred())
	})

	It("NMC exists", func() {
		nmc := &kmmv1beta1.NodeModulesConfig{
			ObjectMeta: metav1.ObjectMeta{Name: nodeName},
		}
		gomock.InOrder(
			rgst.EXPECT().ImageExists(ctx, mld.ContainerImage, gomock.Any(), gomock.Any()).Return(true, nil),
			clnt.EXPECT().Get(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ interface{}, _ interface{}, nmc *kmmv1beta1.NodeModulesConfig, _ ...ctrlclient.GetOption) error {
					nmc.SetName(nodeName)
					return nil
				},
			),
			helper.EXPECT().SetModuleConfig(nmc, mld, expectedModuleConfig).Return(nil),
		)

		err := mnrh.enableModuleOnNode(ctx, mld, nodeName, kernelVersion)
		Expect(err).NotTo(HaveOccurred())
	})
})

var _ = Describe("disableModuleOnNode", func() {
	var (
		ctx             context.Context
		ctrl            *gomock.Controller
		clnt            *client.MockClient
		mnrh            moduleNMCReconcilerHelperAPI
		helper          *nmc.MockHelper
		nodeName        string
		moduleName      string
		moduleNamespace string
	)

	BeforeEach(func() {
		ctx = context.Background()
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		helper = nmc.NewMockHelper(ctrl)
		mnrh = newModuleNMCReconcilerHelper(clnt, nil, helper)
		nodeName = "node name"
		moduleName = "moduleName"
		moduleNamespace = "moduleNamespace"
	})

	It("NMC does not exists", func() {
		helper.EXPECT().Get(ctx, nodeName).Return(nil, apierrors.NewNotFound(schema.GroupResource{}, nodeName))

		err := mnrh.disableModuleOnNode(ctx, moduleNamespace, moduleName, nodeName)
		Expect(err).NotTo(HaveOccurred())
	})

	It("failed to get NMC", func() {
		helper.EXPECT().Get(ctx, nodeName).Return(nil, fmt.Errorf("some error"))

		err := mnrh.disableModuleOnNode(ctx, moduleNamespace, moduleName, nodeName)
		Expect(err).To(HaveOccurred())
	})

	It("NMC exists", func() {
		nmc := &kmmv1beta1.NodeModulesConfig{
			ObjectMeta: metav1.ObjectMeta{Name: nodeName},
		}
		gomock.InOrder(
			helper.EXPECT().Get(ctx, nodeName).Return(nmc, nil),
			clnt.EXPECT().Get(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ interface{}, _ interface{}, nmc *kmmv1beta1.NodeModulesConfig, _ ...ctrlclient.GetOption) error {
					nmc.SetName(nodeName)
					return nil
				},
			),
			helper.EXPECT().RemoveModuleConfig(nmc, moduleNamespace, moduleName).Return(nil),
		)

		err := mnrh.disableModuleOnNode(ctx, moduleNamespace, moduleName, nodeName)
		Expect(err).NotTo(HaveOccurred())
	})
})

var _ = Describe("removeModuleFromNMC", func() {
	var (
		ctx             context.Context
		ctrl            *gomock.Controller
		clnt            *client.MockClient
		mnrh            *moduleNMCReconcilerHelper
		helper          *nmc.MockHelper
		nmcName         string
		moduleName      string
		moduleNamespace string
	)

	BeforeEach(func() {
		ctx = context.Background()
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		helper = nmc.NewMockHelper(ctrl)
		mnrh = &moduleNMCReconcilerHelper{client: clnt, nmcHelper: helper}
		nmcName = "NMC name"
		moduleName = "moduleName"
		moduleNamespace = "moduleNamespace"
	})

	It("good flow", func() {
		nmc := &kmmv1beta1.NodeModulesConfig{
			ObjectMeta: metav1.ObjectMeta{Name: nmcName},
		}
		gomock.InOrder(
			clnt.EXPECT().Get(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ interface{}, _ interface{}, nmc *kmmv1beta1.NodeModulesConfig, _ ...ctrlclient.GetOption) error {
					nmc.SetName(nmcName)
					return nil
				},
			),
			helper.EXPECT().RemoveModuleConfig(nmc, moduleNamespace, moduleName).Return(nil),
		)

		err := mnrh.removeModuleFromNMC(ctx, nmc, moduleNamespace, moduleName)
		Expect(err).NotTo(HaveOccurred())
	})

	It("bad flow", func() {
		nmc := &kmmv1beta1.NodeModulesConfig{
			ObjectMeta: metav1.ObjectMeta{Name: nmcName},
		}
		gomock.InOrder(
			clnt.EXPECT().Get(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ interface{}, _ interface{}, nmc *kmmv1beta1.NodeModulesConfig, _ ...ctrlclient.GetOption) error {
					nmc.SetName(nmcName)
					return nil
				},
			),
			helper.EXPECT().RemoveModuleConfig(nmc, moduleNamespace, moduleName).Return(fmt.Errorf("some error")),
		)

		err := mnrh.removeModuleFromNMC(ctx, nmc, moduleNamespace, moduleName)
		Expect(err).To(HaveOccurred())
	})
})
