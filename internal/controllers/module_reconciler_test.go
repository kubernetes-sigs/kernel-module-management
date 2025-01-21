package controllers

import (
	"context"
	"errors"
	"fmt"

	"github.com/kubernetes-sigs/kernel-module-management/internal/mic"
	"github.com/kubernetes-sigs/kernel-module-management/internal/node"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/api"
	"github.com/kubernetes-sigs/kernel-module-management/internal/client"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	"github.com/kubernetes-sigs/kernel-module-management/internal/meta"
	"github.com/kubernetes-sigs/kernel-module-management/internal/module"
	"github.com/kubernetes-sigs/kernel-module-management/internal/nmc"
	"github.com/kubernetes-sigs/kernel-module-management/internal/registry"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var _ = Describe("ModuleReconciler_Reconcile", func() {
	var (
		ctrl                *gomock.Controller
		mockNamespaceHelper *MocknamespaceLabeler
		mockReconHelper     *MockmoduleReconcilerHelperAPI
		mod                 *kmmv1beta1.Module
		mr                  *ModuleReconciler
		mn                  *node.MockNode
		mockMicAPI          *mic.MockModuleImagesConfigAPI
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockNamespaceHelper = NewMocknamespaceLabeler(ctrl)
		mockReconHelper = NewMockmoduleReconcilerHelperAPI(ctrl)
		mn = node.NewMockNode(ctrl)
		mockMicAPI = mic.NewMockModuleImagesConfigAPI(ctrl)

		mod = &kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{Name: moduleName, Namespace: namespace},
		}

		mr = &ModuleReconciler{
			nsLabeler:   mockNamespaceHelper,
			reconHelper: mockReconHelper,
			nodeAPI:     mn,
			micAPI:      mockMicAPI,
		}
	})

	const nodeName = "nodeName"

	ctx := context.Background()
	node := v1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: nodeName},
	}
	targetedNodes := []v1.Node{node}
	images := []kmmv1beta1.ModuleImageSpec{
		{
			Image: "example.registry.com/org/image:tag",
		},
	}
	currentNMCs := sets.New[string](nodeName)
	mld := api.ModuleLoaderData{KernelVersion: "some version"}
	enableSchedulingData := schedulingData{action: actionAdd, mld: &mld, node: &node}
	disableSchedulingData := schedulingData{action: actionDelete, mld: nil}

	It("should run finalization and try removing namespace label when module is being deleted", func() {
		mod.SetDeletionTimestamp(&metav1.Time{})

		gomock.InOrder(
			mockNamespaceHelper.EXPECT().tryRemovingLabel(ctx, namespace, moduleName),
			mockReconHelper.EXPECT().finalizeModule(ctx, mod).Return(nil),
		)

		res, err := mr.Reconcile(ctx, mod)

		Expect(res).To(Equal(reconcile.Result{}))
		Expect(err).NotTo(HaveOccurred())
	})

	type errorFlowTestCase struct {
		setFinalizerAndStatusError    bool
		getNodesError                 bool
		prepareImagesError            bool
		getNMCsMapError               bool
		prepareSchedulingError        bool
		handleModuleImagesConfigError bool
		shouldBeOnNode                bool
		disableEnableError            bool
		moduleUpdateStatusErr         bool
		setLabelError                 bool
	}

	DescribeTable("check error flows", func(c errorFlowTestCase) {

		nmcMLDConfigs := map[string]schedulingData{"nodeName": disableSchedulingData}
		if c.shouldBeOnNode {
			nmcMLDConfigs = map[string]schedulingData{"nodeName": enableSchedulingData}
		}
		returnedError := errors.New("some error")
		if c.setLabelError {
			mockNamespaceHelper.EXPECT().setLabel(ctx, mod.Namespace).Return(returnedError)
			goto executeTestFunction
		}
		mockNamespaceHelper.EXPECT().setLabel(ctx, mod.Namespace)
		if c.setFinalizerAndStatusError {
			mockReconHelper.EXPECT().setFinalizerAndStatus(ctx, mod).Return(returnedError)
			goto executeTestFunction
		}
		mockReconHelper.EXPECT().setFinalizerAndStatus(ctx, mod).Return(nil)
		if c.getNodesError {
			mn.EXPECT().GetNodesListBySelector(ctx, mod.Spec.Selector, nil).Return(nil, returnedError)
			goto executeTestFunction
		}
		mn.EXPECT().GetNodesListBySelector(ctx, mod.Spec.Selector, nil).Return(targetedNodes, nil)
		if c.prepareImagesError {
			mockReconHelper.EXPECT().prepareImages(ctx, mod, targetedNodes).Return(nil, returnedError)
			goto executeTestFunction
		} else {
			mockReconHelper.EXPECT().prepareImages(ctx, mod, targetedNodes).Return(images, nil)
		}
		if c.handleModuleImagesConfigError {
			mockMicAPI.EXPECT().HandleModuleImagesConfig(ctx, mod.Name, mod.Namespace, images,
				mod.Spec.ImageRepoSecret, mod).Return(returnedError)
			goto executeTestFunction
		} else {
			mockMicAPI.EXPECT().HandleModuleImagesConfig(ctx, mod.Name, mod.Namespace, images,
				mod.Spec.ImageRepoSecret, mod).Return(nil)
		}
		if c.getNMCsMapError {
			mockReconHelper.EXPECT().getNMCsByModuleSet(ctx, mod).Return(nil, returnedError)
			goto executeTestFunction
		}
		mockReconHelper.EXPECT().getNMCsByModuleSet(ctx, mod).Return(currentNMCs, nil)
		if c.prepareSchedulingError {
			mockReconHelper.EXPECT().prepareSchedulingData(ctx, mod, targetedNodes, currentNMCs).Return(nil, []error{returnedError})
			goto moduleStatusUpdateFunction
		}
		mockReconHelper.EXPECT().prepareSchedulingData(ctx, mod, targetedNodes, currentNMCs).Return(nmcMLDConfigs, []error{})
		if c.disableEnableError {
			if c.shouldBeOnNode {
				mockReconHelper.EXPECT().enableModuleOnNode(ctx, &mld, &node).Return(returnedError)
			} else {
				mockReconHelper.EXPECT().disableModuleOnNode(ctx, mod.Namespace, mod.Name, node.Name).Return(returnedError)
			}
			goto moduleStatusUpdateFunction
		}
		if c.shouldBeOnNode {
			mockReconHelper.EXPECT().enableModuleOnNode(ctx, &mld, &node).Return(nil)
		} else {
			mockReconHelper.EXPECT().disableModuleOnNode(ctx, mod.Namespace, mod.Name, node.Name).Return(nil)
		}

	moduleStatusUpdateFunction:
		if c.moduleUpdateStatusErr {
			mockReconHelper.EXPECT().moduleUpdateWorkerPodsStatus(ctx, mod, targetedNodes).Return(returnedError)
		} else {
			mockReconHelper.EXPECT().moduleUpdateWorkerPodsStatus(ctx, mod, targetedNodes).Return(nil)
		}

	executeTestFunction:
		res, err := mr.Reconcile(ctx, mod)

		Expect(res).To(Equal(reconcile.Result{}))
		Expect(err).To(HaveOccurred())
	},
		Entry("setFinalizerAndStatus failed", errorFlowTestCase{setFinalizerAndStatusError: true}),
		Entry("getNodesListBySelector failed", errorFlowTestCase{getNodesError: true}),
		Entry("prepareImages failed", errorFlowTestCase{prepareImagesError: true}),
		Entry("handleModuleImagesConfig failed", errorFlowTestCase{handleModuleImagesConfigError: true}),
		Entry("getNMCsByModuleMap failed", errorFlowTestCase{getNMCsMapError: true}),
		Entry("prepareSchedulingData failed", errorFlowTestCase{prepareSchedulingError: true}),
		Entry("enableModuleOnNode failed", errorFlowTestCase{shouldBeOnNode: true, disableEnableError: true}),
		Entry("disableModuleOnNode failed", errorFlowTestCase{disableEnableError: true}),
		Entry(".moduleUpdateWorkerPodsStatus failed", errorFlowTestCase{moduleUpdateStatusErr: true}),
		Entry("setLabel failed", errorFlowTestCase{setLabelError: true}),
	)

	It("Good flow, should run on node", func() {
		nmcMLDConfigs := map[string]schedulingData{nodeName: enableSchedulingData}
		gomock.InOrder(
			mockNamespaceHelper.EXPECT().setLabel(ctx, mod.Namespace),
			mockReconHelper.EXPECT().setFinalizerAndStatus(ctx, mod).Return(nil),
			mn.EXPECT().GetNodesListBySelector(ctx, mod.Spec.Selector, nil).Return(targetedNodes, nil),
			mockReconHelper.EXPECT().prepareImages(ctx, mod, targetedNodes).Return(images, nil),
			mockMicAPI.EXPECT().HandleModuleImagesConfig(ctx, mod.Name, mod.Namespace, images, mod.Spec.ImageRepoSecret, mod).Return(nil),
			mockReconHelper.EXPECT().getNMCsByModuleSet(ctx, mod).Return(currentNMCs, nil),
			mockReconHelper.EXPECT().prepareSchedulingData(ctx, mod, targetedNodes, currentNMCs).Return(nmcMLDConfigs, nil),
			mockReconHelper.EXPECT().enableModuleOnNode(ctx, &mld, &node).Return(nil),
			mockReconHelper.EXPECT().moduleUpdateWorkerPodsStatus(ctx, mod, targetedNodes).Return(nil),
		)

		res, err := mr.Reconcile(ctx, mod)

		Expect(res).To(Equal(reconcile.Result{}))
		Expect(err).NotTo(HaveOccurred())
	})

	It("Good flow, should not run on node", func() {
		nmcMLDConfigs := map[string]schedulingData{nodeName: disableSchedulingData}
		gomock.InOrder(
			mockNamespaceHelper.EXPECT().setLabel(ctx, mod.Namespace),
			mockReconHelper.EXPECT().setFinalizerAndStatus(ctx, mod).Return(nil),
			mn.EXPECT().GetNodesListBySelector(ctx, mod.Spec.Selector, nil).Return(targetedNodes, nil),
			mockReconHelper.EXPECT().prepareImages(ctx, mod, targetedNodes).Return(images, nil),
			mockMicAPI.EXPECT().HandleModuleImagesConfig(ctx, mod.Name, mod.Namespace, images, mod.Spec.ImageRepoSecret, mod).Return(nil),
			mockReconHelper.EXPECT().getNMCsByModuleSet(ctx, mod).Return(currentNMCs, nil),
			mockReconHelper.EXPECT().prepareSchedulingData(ctx, mod, targetedNodes, currentNMCs).Return(nmcMLDConfigs, nil),
			mockReconHelper.EXPECT().disableModuleOnNode(ctx, mod.Namespace, mod.Name, node.Name).Return(nil),
			mockReconHelper.EXPECT().moduleUpdateWorkerPodsStatus(ctx, mod, targetedNodes).Return(nil),
		)

		res, err := mr.Reconcile(ctx, mod)

		Expect(res).To(Equal(reconcile.Result{}))
		Expect(err).NotTo(HaveOccurred())
	})
})

var _ = Describe("setFinalizerAndStatus", func() {
	var (
		ctrl         *gomock.Controller
		clnt         *client.MockClient
		statusWriter *client.MockStatusWriter
		mrh          moduleReconcilerHelperAPI
		mod          kmmv1beta1.Module
		expectedMod  *kmmv1beta1.Module
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		statusWriter = client.NewMockStatusWriter(ctrl)
		mrh = newModuleReconcilerHelper(clnt, nil, nil, nil, scheme)
		mod = kmmv1beta1.Module{}
		expectedMod = mod.DeepCopy()
	})

	ctx := context.Background()

	It("finalizer is already set", func() {
		controllerutil.AddFinalizer(&mod, constants.ModuleFinalizer)
		err := mrh.setFinalizerAndStatus(ctx, &mod)
		Expect(err).NotTo(HaveOccurred())
	})

	It("finalizer is not set", func() {
		controllerutil.AddFinalizer(expectedMod, constants.ModuleFinalizer)
		clnt.EXPECT().Patch(ctx, &mod, gomock.Any()).Return(nil)
		clnt.EXPECT().Status().Return(statusWriter)
		statusWriter.EXPECT().Update(ctx, expectedMod).Return(nil)

		err := mrh.setFinalizerAndStatus(ctx, &mod)
		Expect(err).NotTo(HaveOccurred())
	})

	It("finalizer is not set, failed to patch the Module", func() {
		clnt.EXPECT().Patch(ctx, &mod, gomock.Any()).Return(fmt.Errorf("some error"))

		err := mrh.setFinalizerAndStatus(ctx, &mod)
		Expect(err).To(HaveOccurred())
	})

	It("finalizer is not set, failed to update Module's status", func() {
		controllerutil.AddFinalizer(expectedMod, constants.ModuleFinalizer)
		clnt.EXPECT().Patch(ctx, &mod, gomock.Any()).Return(nil)
		clnt.EXPECT().Status().Return(statusWriter)
		statusWriter.EXPECT().Update(ctx, expectedMod).Return(fmt.Errorf("some error"))

		err := mrh.setFinalizerAndStatus(ctx, &mod)
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("finalizeModule", func() {
	const (
		moduleName      = "moduleName"
		moduleNamespace = "moduleNamespace"
	)

	var (
		ctx                    context.Context
		ctrl                   *gomock.Controller
		clnt                   *client.MockClient
		helper                 *nmc.MockHelper
		mrh                    moduleReconcilerHelperAPI
		mod                    *kmmv1beta1.Module
		matchConfiguredModules = map[string]string{nmc.ModuleConfiguredLabel(moduleNamespace, moduleName): ""}
		matchLoadedModules     = map[string]string{nmc.ModuleInUseLabel(moduleNamespace, moduleName): ""}
	)

	BeforeEach(func() {
		ctx = context.Background()
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		helper = nmc.NewMockHelper(ctrl)
		mrh = newModuleReconcilerHelper(clnt, nil, nil, helper, scheme)
		mod = &kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{Name: moduleName, Namespace: moduleNamespace},
		}
	})

	It("failed to get list of NMCs", func() {
		clnt.
			EXPECT().
			List(ctx, &kmmv1beta1.NodeModulesConfigList{}, matchConfiguredModules).
			Return(fmt.Errorf("some error"))

		err := mrh.finalizeModule(ctx, mod)

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

		err := mrh.finalizeModule(ctx, mod)

		Expect(err).To(HaveOccurred())

	})

	It("no nmcs, patch successfull", func() {
		gomock.InOrder(
			clnt.EXPECT().List(ctx, &kmmv1beta1.NodeModulesConfigList{}, matchConfiguredModules).DoAndReturn(
				func(_ interface{}, list *kmmv1beta1.NodeModulesConfigList, _ ...interface{}) error {
					list.Items = []kmmv1beta1.NodeModulesConfig{}
					return nil
				},
			),
			clnt.EXPECT().List(ctx, &kmmv1beta1.NodeModulesConfigList{}, matchLoadedModules),
			clnt.EXPECT().Patch(ctx, mod, gomock.Any()).Return(nil),
		)

		err := mrh.finalizeModule(ctx, mod)

		Expect(err).NotTo(HaveOccurred())
	})

	It("some nmcs have the Module loaded, does not patch", func() {
		gomock.InOrder(
			clnt.EXPECT().List(ctx, &kmmv1beta1.NodeModulesConfigList{}, matchConfiguredModules).DoAndReturn(
				func(_ interface{}, list *kmmv1beta1.NodeModulesConfigList, _ ...interface{}) error {
					list.Items = make([]kmmv1beta1.NodeModulesConfig, 0)
					return nil
				},
			),
			clnt.EXPECT().List(ctx, &kmmv1beta1.NodeModulesConfigList{}, matchLoadedModules).DoAndReturn(
				func(_ interface{}, list *kmmv1beta1.NodeModulesConfigList, _ ...interface{}) error {
					list.Items = make([]kmmv1beta1.NodeModulesConfig, 1)
					return nil
				},
			),
		)

		Expect(
			mrh.finalizeModule(ctx, mod),
		).NotTo(
			HaveOccurred(),
		)
	})

	It("no nmcs, patch failed", func() {
		gomock.InOrder(
			clnt.EXPECT().List(ctx, &kmmv1beta1.NodeModulesConfigList{}, matchConfiguredModules).DoAndReturn(
				func(_ interface{}, list *kmmv1beta1.NodeModulesConfigList, _ ...interface{}) error {
					list.Items = []kmmv1beta1.NodeModulesConfig{}
					return nil
				},
			),
			clnt.EXPECT().List(ctx, &kmmv1beta1.NodeModulesConfigList{}, matchLoadedModules),
			clnt.EXPECT().Patch(ctx, mod, gomock.Any()).Return(fmt.Errorf("some error")),
		)

		err := mrh.finalizeModule(ctx, mod)

		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("prepareImages", func() {

	const (
		moduleName      = "moduleName"
		moduleNamespace = "moduleNamespace"
	)

	var (
		ctx              context.Context
		ctrl             *gomock.Controller
		clnt             *client.MockClient
		mockKernelMapper *module.MockKernelMapper
		helper           *nmc.MockHelper
		mrh              moduleReconcilerHelperAPI
		mod              *kmmv1beta1.Module
		targetedNodes    []v1.Node
	)

	BeforeEach(func() {
		ctx = context.Background()
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		mockKernelMapper = module.NewMockKernelMapper(ctrl)
		helper = nmc.NewMockHelper(ctrl)
		mrh = newModuleReconcilerHelper(clnt, mockKernelMapper, nil, helper, scheme)
		mod = &kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{
				Name:      moduleName,
				Namespace: moduleNamespace,
			},
		}
		targetedNodes = []v1.Node{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: nodeName,
				},
			},
		}
	})

	It("should return errors if we failed to get moduleLoaderData for kernel", func() {

		mockKernelMapper.EXPECT().GetModuleLoaderDataForKernel(mod, gomock.Any()).Return(nil, errors.New("some error"))

		_, err := mrh.prepareImages(ctx, mod, targetedNodes)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to get moduleLoaderData for kernel"))
	})

	It("should work as expected", func() {

		img := "example.registry.com/org/image:tag"
		mld := &api.ModuleLoaderData{ContainerImage: img}
		mockKernelMapper.EXPECT().GetModuleLoaderDataForKernel(mod, gomock.Any()).Return(mld, nil)

		images, err := mrh.prepareImages(ctx, mod, targetedNodes)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(images)).To(Equal(1))
		Expect(images[0].Image).To(Equal(img))
	})
})

var _ = Describe("getNMCsByModuleSet", func() {
	var (
		ctrl *gomock.Controller
		clnt *client.MockClient
		mrh  moduleReconcilerHelperAPI
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		mrh = newModuleReconcilerHelper(clnt, nil, nil, nil, scheme)
	})

	ctx := context.Background()

	It("list failed", func() {
		clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).Return(fmt.Errorf("some error"))

		nodes, err := mrh.getNMCsByModuleSet(ctx, &kmmv1beta1.Module{})

		Expect(err).To(HaveOccurred())
		Expect(nodes).To(BeNil())
	})

	It("Return NMCs", func() {
		nmc1 := kmmv1beta1.NodeModulesConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "nmc1"},
		}
		nmc2 := kmmv1beta1.NodeModulesConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "nmc2"},
		}
		nmc3 := kmmv1beta1.NodeModulesConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "nmc3"},
		}
		clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ interface{}, list *kmmv1beta1.NodeModulesConfigList, _ ...interface{}) error {
				list.Items = []kmmv1beta1.NodeModulesConfig{nmc1, nmc2, nmc3}
				return nil
			},
		)

		nmcsSet, err := mrh.getNMCsByModuleSet(ctx, &kmmv1beta1.Module{})

		expectedSet := sets.New[string]([]string{"nmc1", "nmc2", "nmc3"}...)

		Expect(err).NotTo(HaveOccurred())
		Expect(nmcsSet.Equal(expectedSet)).To(BeTrue())

	})
})

var _ = Describe("prepareSchedulingData", func() {
	const (
		kernelVersion   = "some kernel version"
		nodeName        = "nodeName"
		moduleName      = "moduleName"
		moduleNamespace = "moduleNamespace"
	)
	var (
		ctrl          *gomock.Controller
		clnt          *client.MockClient
		mockKernel    *module.MockKernelMapper
		mockHelper    *nmc.MockHelper
		mrh           moduleReconcilerHelperAPI
		node          v1.Node
		targetedNodes []v1.Node
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		mockKernel = module.NewMockKernelMapper(ctrl)
		mockHelper = nmc.NewMockHelper(ctrl)
		mrh = newModuleReconcilerHelper(clnt, mockKernel, nil, mockHelper, scheme)
		node = v1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: nodeName},
			Status: v1.NodeStatus{
				NodeInfo: v1.NodeSystemInfo{KernelVersion: kernelVersion},
			},
		}
		targetedNodes = []v1.Node{node}
	})

	ctx := context.Background()
	mod := kmmv1beta1.Module{}
	mld := api.ModuleLoaderData{KernelVersion: "some version", Name: moduleName, Namespace: moduleNamespace}

	It("failed to determine mld", func() {
		currentNMCs := sets.New[string](nodeName)
		mockKernel.EXPECT().GetModuleLoaderDataForKernel(&mod, kernelVersion).Return(nil, fmt.Errorf("some error"))

		scheduleData, errs := mrh.prepareSchedulingData(ctx, &mod, targetedNodes, currentNMCs)

		Expect(len(errs)).To(Equal(1))
		Expect(scheduleData).To(Equal(map[string]schedulingData{}))
	})

	DescribeTable(
		"mld for kernel version does not exists",
		func(moduleCurrentlyEnabled bool, expectedAction string) {
			currentNMCs := sets.New[string]()
			expectedScheduleData := map[string]schedulingData{nodeName: {action: expectedAction}}

			if moduleCurrentlyEnabled {
				currentNMCs.Insert(nodeName)
			}

			mockKernel.EXPECT().GetModuleLoaderDataForKernel(&mod, kernelVersion).Return(nil, module.ErrNoMatchingKernelMapping)

			scheduleData, errs := mrh.prepareSchedulingData(ctx, &mod, targetedNodes, currentNMCs)

			Expect(errs).To(BeEmpty())
			Expect(scheduleData).To(Equal(expectedScheduleData))
		},
		EntryDescription("module currently enabled in MLD: %t, expected action: %q"),
		Entry(nil, true, actionDelete),
		Entry(nil, false, ""),
	)

	It("mld exists", func() {
		currentNMCs := sets.New[string](nodeName)
		mockKernel.EXPECT().GetModuleLoaderDataForKernel(&mod, kernelVersion).Return(&mld, nil)

		scheduleData, errs := mrh.prepareSchedulingData(ctx, &mod, targetedNodes, currentNMCs)

		expectedScheduleData := map[string]schedulingData{nodeName: schedulingData{action: actionAdd, mld: &mld, node: &node}}
		Expect(errs).To(BeEmpty())
		Expect(scheduleData).To(Equal(expectedScheduleData))
	})

	It("mld exists, nmc exists for other node", func() {
		currentNMCs := sets.New[string]("some other node")
		mockKernel.EXPECT().GetModuleLoaderDataForKernel(&mod, kernelVersion).Return(&mld, nil)

		scheduleData, errs := mrh.prepareSchedulingData(ctx, &mod, targetedNodes, currentNMCs)

		Expect(errs).To(BeEmpty())
		Expect(scheduleData).To(HaveKeyWithValue(nodeName, schedulingData{action: actionAdd, mld: &mld, node: &node}))
		Expect(scheduleData).To(HaveKeyWithValue("some other node", schedulingData{action: actionDelete}))
	})

	It("failed to determine mld for one of the nodes/nmcs", func() {
		currentNMCs := sets.New[string]("some other node")
		mockKernel.EXPECT().GetModuleLoaderDataForKernel(&mod, kernelVersion).Return(nil, fmt.Errorf("some error"))

		scheduleData, errs := mrh.prepareSchedulingData(ctx, &mod, targetedNodes, currentNMCs)

		Expect(errs).NotTo(BeEmpty())
		expectedScheduleData := map[string]schedulingData{"some other node": schedulingData{action: actionDelete}}
		Expect(scheduleData).To(Equal(expectedScheduleData))
	})

	It("should produce correct scheduling data when there are two nodes", func() {
		const (
			otherNodeName          = "other-node-name"
			otherNodeKernelVersion = "some other kernel version"
		)

		otherNode := node
		otherNode.ObjectMeta.Name = otherNodeName
		otherNode.Status.NodeInfo.KernelVersion = otherNodeKernelVersion

		targetedNodes = append(targetedNodes, otherNode)

		otherNodeMLD := mld
		otherNodeMLD.KernelVersion = otherNodeKernelVersion

		gomock.InOrder(
			mockKernel.EXPECT().GetModuleLoaderDataForKernel(&mod, kernelVersion).Return(&mld, nil),
			mockKernel.EXPECT().GetModuleLoaderDataForKernel(&mod, otherNodeKernelVersion).Return(&otherNodeMLD, nil),
		)

		scheduleData, errs := mrh.prepareSchedulingData(ctx, &mod, targetedNodes, sets.New[string]())
		Expect(errs).To(BeEmpty())

		expected := map[string]schedulingData{
			nodeName:      {action: actionAdd, mld: &mld, node: &node},
			otherNodeName: {action: actionAdd, mld: &otherNodeMLD, node: &otherNode},
		}

		Expect(scheduleData).To(Equal(expected))
	})

	It("module version exists, workerPod version label exists, versions are equal", func() {
		node.SetLabels(map[string]string{utils.GetWorkerPodVersionLabelName(moduleNamespace, moduleName): "moduleVersion1"})
		targetedNodes[0] = node
		currentNMCs := sets.New[string](nodeName)
		mld.ModuleVersion = "moduleVersion1"
		mockKernel.EXPECT().GetModuleLoaderDataForKernel(&mod, kernelVersion).Return(&mld, nil)

		scheduleData, errs := mrh.prepareSchedulingData(ctx, &mod, targetedNodes, currentNMCs)

		Expect(errs).To(BeEmpty())
		Expect(scheduleData).To(HaveKeyWithValue(nodeName, schedulingData{action: actionAdd, mld: &mld, node: &node}))
	})

	It("module version exists, workerPod version label exists, versions are different", func() {
		node.SetLabels(map[string]string{utils.GetWorkerPodVersionLabelName(moduleNamespace, moduleName): "moduleVersion1"})
		targetedNodes[0] = node
		currentNMCs := sets.New[string](nodeName)
		mld.ModuleVersion = "moduleVersion2"
		mockKernel.EXPECT().GetModuleLoaderDataForKernel(&mod, kernelVersion).Return(&mld, nil)

		scheduleData, errs := mrh.prepareSchedulingData(ctx, &mod, targetedNodes, currentNMCs)

		Expect(errs).To(BeEmpty())
		Expect(scheduleData).To(HaveKeyWithValue(nodeName, schedulingData{}))
	})

	It("module version exists, moduleLoader version label does not exist", func() {
		currentNMCs := sets.New[string](nodeName)
		mld.ModuleVersion = "moduleVersion2"
		mockKernel.EXPECT().GetModuleLoaderDataForKernel(&mod, kernelVersion).Return(&mld, nil)

		scheduleData, errs := mrh.prepareSchedulingData(ctx, &mod, targetedNodes, currentNMCs)

		Expect(errs).To(BeEmpty())
		Expect(scheduleData).To(HaveKeyWithValue(nodeName, schedulingData{action: actionDelete}))
	})
})

var _ = Describe("prepareImages", func() {
})

var _ = Describe("enableModuleOnNode", func() {
	const (
		moduleNamespace = "moduleNamespace"
		moduleName      = "moduleName"
	)

	var (
		ctx                  context.Context
		ctrl                 *gomock.Controller
		clnt                 *client.MockClient
		rgst                 *registry.MockRegistry
		mrh                  moduleReconcilerHelperAPI
		helper               *nmc.MockHelper
		mld                  *api.ModuleLoaderData
		node                 v1.Node
		expectedModuleConfig *kmmv1beta1.ModuleConfig
		kernelVersion        string
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		helper = nmc.NewMockHelper(ctrl)
		rgst = registry.NewMockRegistry(ctrl)
		mrh = newModuleReconcilerHelper(clnt, nil, rgst, helper, scheme)
		node = v1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "nodeName"},
		}
		kernelVersion = "some version"
		ctx = context.Background()
		mld = &api.ModuleLoaderData{
			KernelVersion:         kernelVersion,
			Name:                  moduleName,
			Namespace:             moduleNamespace,
			InTreeModulesToRemove: []string{"InTreeModuleToRemove"},
			ContainerImage:        "containerImage",
		}

		expectedModuleConfig = &kmmv1beta1.ModuleConfig{
			KernelVersion:         mld.KernelVersion,
			ContainerImage:        mld.ContainerImage,
			InTreeModulesToRemove: mld.InTreeModulesToRemove,
			Modprobe:              mld.Modprobe,
		}
	})

	It("Build configured and image does not exist", func() {
		mld.Build = &kmmv1beta1.Build{}
		rgst.EXPECT().ImageExists(ctx, mld.ContainerImage, gomock.Any(), gomock.Any()).Return(false, nil)
		err := mrh.enableModuleOnNode(ctx, mld, &node)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Sign configured and image does not exist", func() {
		mld.Sign = &kmmv1beta1.Sign{}
		rgst.EXPECT().ImageExists(ctx, mld.ContainerImage, gomock.Any(), gomock.Any()).Return(false, nil)
		err := mrh.enableModuleOnNode(ctx, mld, &node)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Build configured and failed to check if image exists", func() {
		mld.Build = &kmmv1beta1.Build{}
		rgst.EXPECT().ImageExists(ctx, mld.ContainerImage, gomock.Any(), gomock.Any()).Return(false, fmt.Errorf("some error"))
		err := mrh.enableModuleOnNode(ctx, mld, &node)
		Expect(err).To(HaveOccurred())
	})

	It("NMC does not exists", func() {
		nmc := &kmmv1beta1.NodeModulesConfig{
			ObjectMeta: metav1.ObjectMeta{Name: node.Name},
		}

		gomock.InOrder(
			clnt.EXPECT().Get(ctx, gomock.Any(), gomock.Any()).Return(apierrors.NewNotFound(schema.GroupResource{}, "whatever")),
			helper.EXPECT().SetModuleConfig(nmc, mld, expectedModuleConfig).Return(nil),
			clnt.EXPECT().Create(ctx, gomock.Any()).Return(nil),
		)

		err := mrh.enableModuleOnNode(ctx, mld, &node)
		Expect(err).NotTo(HaveOccurred())
	})

	It("NMC exists", func() {
		nmcObj := &kmmv1beta1.NodeModulesConfig{
			ObjectMeta: metav1.ObjectMeta{Name: node.Name},
		}

		nmcWithLabels := *nmcObj
		nmcWithLabels.SetLabels(map[string]string{
			nmc.ModuleConfiguredLabel(moduleNamespace, moduleName): "",
			nmc.ModuleInUseLabel(moduleNamespace, moduleName):      "",
		})

		Expect(
			controllerutil.SetOwnerReference(&node, &nmcWithLabels, scheme),
		).NotTo(
			HaveOccurred(),
		)

		gomock.InOrder(
			clnt.EXPECT().Get(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ interface{}, _ interface{}, nmc *kmmv1beta1.NodeModulesConfig, _ ...ctrlclient.GetOption) error {
					nmc.SetName(node.Name)
					return nil
				},
			),
			helper.EXPECT().SetModuleConfig(nmcObj, mld, expectedModuleConfig).Return(nil),
			clnt.EXPECT().Patch(ctx, &nmcWithLabels, gomock.Any()).Return(nil),
		)

		err := mrh.enableModuleOnNode(ctx, mld, &node)
		Expect(err).NotTo(HaveOccurred())
	})
})

var _ = Describe("disableModuleOnNode", func() {
	var (
		ctx             context.Context
		ctrl            *gomock.Controller
		clnt            *client.MockClient
		mrh             moduleReconcilerHelperAPI
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
		mrh = newModuleReconcilerHelper(clnt, nil, nil, helper, scheme)
		nodeName = "node name"
		moduleName = "moduleName"
		moduleNamespace = "moduleNamespace"
	})

	It("NMC exists", func() {
		nmc := &kmmv1beta1.NodeModulesConfig{
			ObjectMeta: metav1.ObjectMeta{Name: nodeName},
		}
		gomock.InOrder(
			clnt.EXPECT().Get(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ interface{}, _ interface{}, nmc *kmmv1beta1.NodeModulesConfig, _ ...ctrlclient.GetOption) error {
					nmc.SetName(nodeName)
					return nil
				},
			),
			helper.EXPECT().RemoveModuleConfig(nmc, moduleNamespace, moduleName).Return(nil),
		)

		err := mrh.disableModuleOnNode(ctx, moduleNamespace, moduleName, nodeName)
		Expect(err).NotTo(HaveOccurred())
	})
})

var _ = Describe("removeModuleFromNMC", func() {
	var (
		ctx             context.Context
		ctrl            *gomock.Controller
		clnt            *client.MockClient
		mrh             *moduleReconcilerHelper
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
		mrh = &moduleReconcilerHelper{client: clnt, nmcHelper: helper}
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

		err := mrh.removeModuleFromNMC(ctx, nmc, moduleNamespace, moduleName)
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

		err := mrh.removeModuleFromNMC(ctx, nmc, moduleNamespace, moduleName)
		Expect(err).To(HaveOccurred())
	})

	It("removes the configured label", func() {
		nmc := &kmmv1beta1.NodeModulesConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:   nmcName,
				Labels: map[string]string{nmc.ModuleConfiguredLabel(moduleNamespace, moduleName): ""},
			},
		}

		nmcWithoutLabel := *nmc
		nmcWithoutLabel.SetLabels(make(map[string]string))

		gomock.InOrder(
			clnt.EXPECT().Get(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ interface{}, _ interface{}, nmc *kmmv1beta1.NodeModulesConfig, _ ...ctrlclient.GetOption) error {
					nmc.SetName(nmcName)
					return nil
				},
			),
			helper.EXPECT().RemoveModuleConfig(nmc, moduleNamespace, moduleName).Return(nil),
			clnt.EXPECT().Patch(ctx, &nmcWithoutLabel, gomock.Any()),
		)

		err := mrh.removeModuleFromNMC(ctx, nmc, moduleNamespace, moduleName)
		Expect(err).NotTo(HaveOccurred())
	})
})

var _ = Describe("moduleUpdateWorkerPodsStatus", func() {
	var (
		ctx          context.Context
		ctrl         *gomock.Controller
		clnt         *client.MockClient
		mod          kmmv1beta1.Module
		mrh          *moduleReconcilerHelper
		helper       *nmc.MockHelper
		statusWriter *client.MockStatusWriter
	)

	BeforeEach(func() {
		ctx = context.Background()
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		helper = nmc.NewMockHelper(ctrl)
		statusWriter = client.NewMockStatusWriter(ctrl)
		mod = kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "modName",
				Namespace: "modNamespace",
			},
		}
		mrh = &moduleReconcilerHelper{client: clnt, nmcHelper: helper}
	})

	It("faled to get configured NMCs", func() {
		clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).Return(fmt.Errorf("some error"))
		err := mrh.moduleUpdateWorkerPodsStatus(ctx, &mod, nil)
		Expect(err).To(HaveOccurred())
	})

	It("module missing from spec", func() {
		nmc1 := kmmv1beta1.NodeModulesConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "nmc1"},
		}
		targetedNodes := []v1.Node{v1.Node{}, v1.Node{}}
		expectedMod := mod.DeepCopy()
		expectedMod.Status.ModuleLoader.NodesMatchingSelectorNumber = int32(2)
		expectedMod.Status.ModuleLoader.DesiredNumber = int32(1)
		expectedMod.Status.ModuleLoader.AvailableNumber = int32(0)
		gomock.InOrder(
			clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ interface{}, list *kmmv1beta1.NodeModulesConfigList, _ ...interface{}) error {
					list.Items = []kmmv1beta1.NodeModulesConfig{nmc1}
					return nil
				},
			),
			helper.EXPECT().GetModuleSpecEntry(&nmc1, mod.Namespace, mod.Name).Return(nil, 0),
			clnt.EXPECT().Status().Return(statusWriter),
			statusWriter.EXPECT().Patch(ctx, expectedMod, gomock.Any()),
		)

		err := mrh.moduleUpdateWorkerPodsStatus(ctx, &mod, targetedNodes)
		Expect(err).NotTo(HaveOccurred())
	})

	DescribeTable("module present in spec", func(numTargetedNodes int,
		modulePresentInStatus,
		configsEqual bool,
		expectedNodesMatchingSelectorNumber,
		expectedDesiredNumber,
		expectedAvailableNumber int) {
		expectedMod := mod.DeepCopy()
		expectedMod.Status.ModuleLoader.NodesMatchingSelectorNumber = int32(expectedNodesMatchingSelectorNumber)
		expectedMod.Status.ModuleLoader.DesiredNumber = int32(expectedDesiredNumber)
		expectedMod.Status.ModuleLoader.AvailableNumber = int32(expectedAvailableNumber)

		targetedNodes := []v1.Node{}
		for i := 0; i < numTargetedNodes; i++ {
			targetedNodes = append(targetedNodes, v1.Node{})
		}
		nmc1 := kmmv1beta1.NodeModulesConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "nmc1"},
		}
		moduleConfig1 := kmmv1beta1.ModuleConfig{ContainerImage: "some image1"}
		moduleConfig2 := kmmv1beta1.ModuleConfig{ContainerImage: "some image2"}
		nmcModuleSpec := kmmv1beta1.NodeModuleSpec{
			Config: moduleConfig1,
		}
		nmcModuleStatus := kmmv1beta1.NodeModuleStatus{}
		if configsEqual {
			nmcModuleStatus.Config = moduleConfig1
		} else {
			nmcModuleStatus.Config = moduleConfig2
		}
		clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ interface{}, list *kmmv1beta1.NodeModulesConfigList, _ ...interface{}) error {
				list.Items = []kmmv1beta1.NodeModulesConfig{nmc1}
				return nil
			},
		)
		helper.EXPECT().GetModuleSpecEntry(&nmc1, mod.Namespace, mod.Name).Return(&nmcModuleSpec, 0)
		if modulePresentInStatus {
			helper.EXPECT().GetModuleStatusEntry(&nmc1, mod.Namespace, mod.Name).Return(&nmcModuleStatus)
		} else {
			helper.EXPECT().GetModuleStatusEntry(&nmc1, mod.Namespace, mod.Name).Return(nil)
		}
		clnt.EXPECT().Status().Return(statusWriter)
		statusWriter.EXPECT().Patch(ctx, expectedMod, gomock.Any())

		err := mrh.moduleUpdateWorkerPodsStatus(ctx, &mod, targetedNodes)
		Expect(err).NotTo(HaveOccurred())
	},
		Entry("2 targeted nodes, module not in status", 2, false, false, 2, 1, 0),
		Entry("3 targeted nodes, module in status, configs not equal", 2, true, false, 2, 1, 0),
		Entry("3 targeted nodes, module in status, configs equal", 2, true, true, 2, 1, 1),
	)

	It("multiple module in spec and status", func() {
		moduleConfig1 := kmmv1beta1.ModuleConfig{ContainerImage: "some image1"}
		//moduleConfig2 := kmmv1beta1.ModuleConfig{ContainerImage: "some image2",}
		nmcModuleSpec := kmmv1beta1.NodeModuleSpec{
			Config: moduleConfig1,
		}
		nmcModuleStatus := kmmv1beta1.NodeModuleStatus{
			Config: moduleConfig1,
		}
		nmc1 := kmmv1beta1.NodeModulesConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "nmc1"},
		}
		targetedNodes := []v1.Node{v1.Node{}, v1.Node{}}
		expectedMod := mod.DeepCopy()
		expectedMod.Status.ModuleLoader.NodesMatchingSelectorNumber = int32(2)
		expectedMod.Status.ModuleLoader.DesiredNumber = int32(1)
		expectedMod.Status.ModuleLoader.AvailableNumber = int32(1)
		gomock.InOrder(
			clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ interface{}, list *kmmv1beta1.NodeModulesConfigList, _ ...interface{}) error {
					list.Items = []kmmv1beta1.NodeModulesConfig{nmc1}
					return nil
				},
			),
			helper.EXPECT().GetModuleSpecEntry(&nmc1, mod.Namespace, mod.Name).Return(&nmcModuleSpec, 0),
			helper.EXPECT().GetModuleStatusEntry(&nmc1, mod.Namespace, mod.Name).Return(&nmcModuleStatus),
			clnt.EXPECT().Status().Return(statusWriter),
			statusWriter.EXPECT().Patch(ctx, expectedMod, gomock.Any()),
		)

		err := mrh.moduleUpdateWorkerPodsStatus(ctx, &mod, targetedNodes)
		Expect(err).NotTo(HaveOccurred())
	})

})

var _ = Describe("namespaceHelper_setLabel", func() {
	var (
		ctx = context.TODO()

		mockClient *client.MockClient
		nh         namespaceLabeler
	)

	BeforeEach(func() {
		ctrl := gomock.NewController(GinkgoT())
		mockClient = client.NewMockClient(ctrl)
		nh = newNamespaceLabeler(mockClient)
	})

	DescribeTable(
		"should work as expected",
		func(labelSet bool) {
			getNS := mockClient.
				EXPECT().
				Get(ctx, types.NamespacedName{Name: namespace}, &v1.Namespace{}).
				Do(func(_ context.Context, _ types.NamespacedName, ns *v1.Namespace, _ ...ctrlclient.GetOption) {
					if labelSet {
						meta.SetLabel(ns, constants.NamespaceLabelKey, "")
					}
				})

			if !labelSet {
				ns := v1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{constants.NamespaceLabelKey: ""},
					},
				}

				mockClient.
					EXPECT().
					Patch(ctx, &ns, gomock.Any()).
					After(getNS)
			}

			Expect(
				nh.setLabel(ctx, namespace),
			).NotTo(
				HaveOccurred(),
			)
		},
		Entry("when label is already set", true),
		Entry("when label is not set", false),
	)
})

var _ = Describe("namespaceHelper_tryRemovingLabel", func() {
	var (
		ctx = context.TODO()

		mockClient *client.MockClient
		nh         namespaceLabeler
	)

	BeforeEach(func() {
		ctrl := gomock.NewController(GinkgoT())
		mockClient = client.NewMockClient(ctrl)
		nh = newNamespaceLabeler(mockClient)
	})

	It("should do nothing if several modules remain", func() {
		mockClient.
			EXPECT().
			List(ctx, &kmmv1beta1.ModuleList{}, ctrlclient.InNamespace(namespace)).
			Do(func(_ context.Context, modList *kmmv1beta1.ModuleList, _ ...ctrlclient.ListOption) {
				modList.Items = []kmmv1beta1.Module{
					{
						ObjectMeta: metav1.ObjectMeta{Name: moduleName},
					},
					{
						ObjectMeta: metav1.ObjectMeta{Name: "some-other-module-name"},
					},
				}
			})

		Expect(
			nh.tryRemovingLabel(ctx, namespace, moduleName),
		).NotTo(
			HaveOccurred(),
		)
	})

	It("should remove the label if it's the only module remaining", func() {
		gomock.InOrder(
			mockClient.
				EXPECT().
				List(ctx, &kmmv1beta1.ModuleList{}, ctrlclient.InNamespace(namespace)).
				Do(func(_ context.Context, modList *kmmv1beta1.ModuleList, _ ...ctrlclient.ListOption) {
					modList.Items = []kmmv1beta1.Module{
						{
							ObjectMeta: metav1.ObjectMeta{Name: moduleName},
						},
					}
				}),
			mockClient.
				EXPECT().
				Get(ctx, types.NamespacedName{Name: namespace}, &v1.Namespace{}),
			mockClient.
				EXPECT().
				Patch(ctx, &v1.Namespace{}, gomock.Any()),
		)

		Expect(
			nh.tryRemovingLabel(ctx, namespace, moduleName),
		).NotTo(
			HaveOccurred(),
		)
	})

})
