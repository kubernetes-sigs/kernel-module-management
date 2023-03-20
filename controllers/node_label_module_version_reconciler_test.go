package controllers

import (
	"context"
	"fmt"

	"github.com/golang/mock/gomock"
	mock_client "github.com/kubernetes-sigs/kernel-module-management/internal/client"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Reconcile", func() {
	const (
		nodeName = "node-name"
	)

	var (
		kubeClient *mock_client.MockClient
		mockHelper *MocknodeLabelModuleVersionHelperAPI
		nlmvr      *NodeLabelModuleVersionReconciler
	)

	BeforeEach(func() {
		ctrl := gomock.NewController(GinkgoT())
		kubeClient = mock_client.NewMockClient(ctrl)
		mockHelper = NewMocknodeLabelModuleVersionHelperAPI(ctrl)
		nlmvr = &NodeLabelModuleVersionReconciler{
			client:    kubeClient,
			helperAPI: mockHelper,
		}
	})

	ctx := context.Background()
	nn := types.NamespacedName{
		Name: nodeName,
	}
	req := ctrl.Request{NamespacedName: nn}
	nodeLabels := map[string]string{"some label": "some label value"}

	DescribeTable("reconciler flow", func(getNodeError, getMLPodsError, upDateNodeLabelsErrors, requeue bool) {
		labelsPerModules := map[string]*modulesVersionLabels{
			"moduleNameNamespace": &modulesVersionLabels{name: "name", namespace: "namespace"},
		}
		moduleLoaderPods := []v1.Pod{v1.Pod{}}
		reconcileLabelsResult := &reconcileLabelsResult{requeue: requeue}
		expectedRes := ctrl.Result{Requeue: requeue}
		if getNodeError {
			kubeClient.EXPECT().Get(ctx, nn, gomock.AssignableToTypeOf(&v1.Node{})).Return(fmt.Errorf("some error"))
			goto executeTestFunction
		}
		kubeClient.EXPECT().Get(ctx, req.NamespacedName, gomock.Any()).Do(
			func(_ interface{}, _ interface{}, node *v1.Node, _ ...client.GetOption) {
				node.SetLabels(nodeLabels)
				node.Name = nodeName
			},
		)
		mockHelper.EXPECT().getLabelsPerModules(ctx, nodeLabels).Return(labelsPerModules)
		if getMLPodsError {
			mockHelper.EXPECT().getModuleLoaderAndDevicePluginPods(ctx, nodeName).Return(nil, fmt.Errorf("some error"))
			goto executeTestFunction
		}
		mockHelper.EXPECT().getModuleLoaderAndDevicePluginPods(ctx, nodeName).Return(moduleLoaderPods, nil)
		mockHelper.EXPECT().reconcileLabels(labelsPerModules, moduleLoaderPods).Return(reconcileLabelsResult)
		if upDateNodeLabelsErrors {
			mockHelper.EXPECT().updateNodeLabels(ctx, nodeName, reconcileLabelsResult).Return(fmt.Errorf("some error"))
			goto executeTestFunction
		}
		mockHelper.EXPECT().updateNodeLabels(ctx, nodeName, reconcileLabelsResult).Return(nil)
	executeTestFunction:

		res, err := nlmvr.Reconcile(ctx, req)
		if upDateNodeLabelsErrors || getMLPodsError || getNodeError {
			Expect(err).To(HaveOccurred())
		} else {
			Expect(err).ToNot(HaveOccurred())
			Expect(res).To(Equal(expectedRes))
		}
	},
		Entry("good flow, no requeue", false, false, false, false),
		Entry("good flow, with requeue", false, false, false, true),
		Entry("get node failed", true, false, false, false),
		Entry("get module loader pods failed", false, true, false, false),
		Entry("update node labels failed", false, false, true, false),
	)
})

var _ = Describe("getLabelsPerModules", func() {
	var (
		helper nodeLabelModuleVersionHelperAPI
	)

	BeforeEach(func() {
		helper = newNodeLabelModuleVersionHelper(nil)
	})

	nodeLabels := map[string]string{
		"some label 1": "some value1",
		"some label 2": "",
		"beta.kmm.node.kubernetes.io/version-module-loader.namespace1.module1": "1",
		"beta.kmm.node.kubernetes.io/version-device-plugin.namespace1.module1": "1",
		"kmm.node.kubernetes.io/version-module.namespace1.module1":             "1",
		"beta.kmm.node.kubernetes.io/version-module-loader.namespace2.module2": "3",
		"kmm.node.kubernetes.io/version-module.namespace2.module2":             "3",
		"beta.kmm.node.kubernetes.io/version-device-plugin.namespace3.module3": "4",
		"kmm.node.kubernetes.io/version-module.namespace5.module5":             "10",
	}

	It("normal flow", func() {
		expectedRes := map[string]*modulesVersionLabels{
			"namespace1-module1": &modulesVersionLabels{
				name:                     "module1",
				namespace:                "namespace1",
				moduleVersionLabel:       "1",
				moduleLoaderVersionLabel: "1",
				devicePluginVersionLabel: "1",
			},
			"namespace2-module2": &modulesVersionLabels{
				name:                     "module2",
				namespace:                "namespace2",
				moduleVersionLabel:       "3",
				moduleLoaderVersionLabel: "3",
			},
			"namespace3-module3": &modulesVersionLabels{
				name:                     "module3",
				namespace:                "namespace3",
				devicePluginVersionLabel: "4",
			},
			"namespace5-module5": &modulesVersionLabels{
				name:               "module5",
				namespace:          "namespace5",
				moduleVersionLabel: "10",
			},
		}

		res := helper.getLabelsPerModules(context.Background(), nodeLabels)
		Expect(len(res)).To(Equal(len(expectedRes)))
		for key, value := range expectedRes {
			Expect(res).Should(HaveKeyWithValue(key, value))
		}
	})
})

var _ = Describe("getModuleLoaderAndDevicePluginPods", func() {
	const (
		nodeName = "node-name"
	)

	var (
		kubeClient *mock_client.MockClient
		helper     nodeLabelModuleVersionHelperAPI
	)

	BeforeEach(func() {
		ctrl := gomock.NewController(GinkgoT())
		kubeClient = mock_client.NewMockClient(ctrl)
		helper = newNodeLabelModuleVersionHelper(kubeClient)
	})

	ctx := context.Background()

	It("good flow", func() {
		fieldSelector := client.MatchingFields{"spec.nodeName": nodeName}
		labelSelector := client.HasLabels{constants.DaemonSetRole}
		kubeClient.EXPECT().List(ctx, gomock.Any(), labelSelector, fieldSelector)

		_, err := helper.getModuleLoaderAndDevicePluginPods(ctx, nodeName)
		Expect(err).ToNot(HaveOccurred())
	})

	It("error flow", func() {
		fieldSelector := client.MatchingFields{"spec.nodeName": nodeName}
		labelSelector := client.HasLabels{constants.DaemonSetRole}
		kubeClient.EXPECT().List(ctx, gomock.Any(), labelSelector, fieldSelector).Return(fmt.Errorf("some error"))

		res, err := helper.getModuleLoaderAndDevicePluginPods(ctx, nodeName)
		Expect(err).To(HaveOccurred())
		Expect(res).To(BeNil())
	})

})

var _ = Describe("getLabelAndAction", func() {
	DescribeTable("reconciler flow", func(moduleVersionValue, moduleLoaderVersionValue, devicePluginVersionValue string,
		expectedLabelFunc func(string, string) string, expectedLabelValue, expectedAction string) {
		moduleLabels := &modulesVersionLabels{
			name:                     "moduleName",
			namespace:                "moduleNamespace",
			moduleVersionLabel:       moduleVersionValue,
			moduleLoaderVersionLabel: moduleLoaderVersionValue,
			devicePluginVersionLabel: devicePluginVersionValue,
		}
		expectedLabel := ""
		if expectedLabelFunc != nil {
			expectedLabel = expectedLabelFunc("moduleNamespace", "moduleName")
		}

		label, labelValue, action := getLabelAndAction(moduleLabels)

		Expect(label).To(Equal(expectedLabel))
		Expect(labelValue).To(Equal(expectedLabelValue))
		Expect(action).To(Equal(expectedAction))
	},
		Entry("all labels present with the same label", "1", "1", "1", nil, "", noneAction),
		Entry("module version missing, module loader present, device plugin present", "", "1", "1", utils.GetDevicePluginVersionLabelName, "", deleteAction),
		Entry("module version missing, module loader present, device plugin missing", "", "1", "", utils.GetModuleLoaderVersionLabelName, "", deleteAction),
		Entry("all labels missing", "", "", "", nil, "", noneAction),
		Entry("module version present, module loader missing, device plugin missing", "1", "", "", utils.GetModuleLoaderVersionLabelName, "1", addAction),
		Entry("module version present, module loader present, device plugin missing", "1", "1", "", utils.GetDevicePluginVersionLabelName, "1", addAction),
		Entry("module version present, module loader different, device plugin different", "2", "1", "1", utils.GetDevicePluginVersionLabelName, "", deleteAction),
		Entry("module version present, module loader different, device plugin missing", "2", "1", "", utils.GetModuleLoaderVersionLabelName, "", deleteAction),
	)
})

var _ = Describe("reconcileLabels", func() {
	var (
		helper nodeLabelModuleVersionHelperAPI
	)

	BeforeEach(func() {
		helper = newNodeLabelModuleVersionHelper(nil)
	})

	pod := v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: "moduleNamespace"},
	}

	It("delete label scenario with device plugin pod not present", func() {
		moduleLabels := &modulesVersionLabels{
			name:                     "moduleName",
			namespace:                "moduleNamespace",
			moduleVersionLabel:       "",
			moduleLoaderVersionLabel: "1",
			devicePluginVersionLabel: "1",
		}

		res := helper.reconcileLabels(map[string]*modulesVersionLabels{"key": moduleLabels}, []v1.Pod{pod})

		Expect(res.requeue).To(BeFalse())
		Expect(len(res.labelsToAdd)).To(Equal(0))
		Expect(res.labelsToDelete).To(Equal([]string{utils.GetDevicePluginVersionLabelName("moduleNamespace", "moduleName")}))
	})

	It("delete label scenario with device plugin pod present", func() {
		moduleLabels := &modulesVersionLabels{
			name:                     "moduleName",
			namespace:                "moduleNamespace",
			moduleVersionLabel:       "",
			moduleLoaderVersionLabel: "1",
			devicePluginVersionLabel: "1",
		}
		pod.SetLabels(map[string]string{constants.ModuleNameLabel: "moduleName", constants.DaemonSetRole: constants.DevicePluginRoleLabelValue})

		res := helper.reconcileLabels(map[string]*modulesVersionLabels{"key": moduleLabels}, []v1.Pod{pod})

		Expect(res.requeue).To(BeTrue())
		Expect(len(res.labelsToDelete)).To(Equal(0))
		Expect(len(res.labelsToAdd)).To(Equal(0))
	})

	It("add label scenario with module loader pod not present", func() {
		moduleLabels := &modulesVersionLabels{
			name:                     "moduleName",
			namespace:                "moduleNamespace",
			moduleVersionLabel:       "1",
			moduleLoaderVersionLabel: "",
			devicePluginVersionLabel: "",
		}

		res := helper.reconcileLabels(map[string]*modulesVersionLabels{"key": moduleLabels}, []v1.Pod{pod})

		Expect(res.requeue).To(BeFalse())
		Expect(res.labelsToAdd).To(Equal(map[string]string{utils.GetModuleLoaderVersionLabelName("moduleNamespace", "moduleName"): "1"}))
		Expect(len(res.labelsToDelete)).To(Equal(0))
	})

	It("add label scenario with module loader pod still present", func() {
		moduleLabels := &modulesVersionLabels{
			name:                     "moduleName",
			namespace:                "moduleNamespace",
			moduleVersionLabel:       "1",
			moduleLoaderVersionLabel: "",
			devicePluginVersionLabel: "",
		}
		pod.SetLabels(map[string]string{constants.ModuleNameLabel: "moduleName", constants.DaemonSetRole: constants.ModuleLoaderRoleLabelValue})

		res := helper.reconcileLabels(map[string]*modulesVersionLabels{"key": moduleLabels}, []v1.Pod{pod})

		Expect(res.requeue).To(BeTrue())
		Expect(len(res.labelsToAdd)).To(Equal(0))
		Expect(len(res.labelsToDelete)).To(Equal(0))
	})

	It("no label needs to be added due to none action", func() {
		moduleLabels := &modulesVersionLabels{
			name:                     "moduleName",
			namespace:                "moduleNamespace",
			moduleVersionLabel:       "1",
			moduleLoaderVersionLabel: "1",
			devicePluginVersionLabel: "1",
		}
		pod := v1.Pod{}
		pod.Namespace = "moduleNamespace"
		pod.SetLabels(map[string]string{constants.ModuleNameLabel: "moduleName"})

		res := helper.reconcileLabels(map[string]*modulesVersionLabels{"key": moduleLabels}, []v1.Pod{pod})

		Expect(res.requeue).To(BeFalse())
		Expect(len(res.labelsToAdd)).To(Equal(0))
		Expect(len(res.labelsToDelete)).To(Equal(0))
	})
})

var _ = Describe("reconcileLabels", func() {
	var (
		kubeClient *mock_client.MockClient
		helper     nodeLabelModuleVersionHelperAPI
	)

	BeforeEach(func() {
		ctrl := gomock.NewController(GinkgoT())
		kubeClient = mock_client.NewMockClient(ctrl)
		helper = newNodeLabelModuleVersionHelper(kubeClient)
	})

	It("check presence of all needed labels", func() {
		ctx := context.Background()
		reconLabels := reconcileLabelsResult{
			labelsToAdd:    map[string]string{"labels1": "value1", "label2": "value2"},
			labelsToDelete: []string{"label3", "label4"},
		}
		kubeClient.EXPECT().Get(ctx, types.NamespacedName{Name: "nodeName"}, gomock.Any()).Do(
			func(_ interface{}, _ interface{}, node *v1.Node, _ ...client.GetOption) {
				node.SetLabels(map[string]string{"label3": "value3", "label4": "value4", "label5": "value5"})
				node.Name = "nodeName"
			},
		)
		node := v1.Node{}
		node.Name = "nodeName"
		node.SetLabels(map[string]string{"labels1": "value1", "label2": "value2", "label5": "value5"})
		kubeClient.EXPECT().Patch(ctx, &node, gomock.Any()).Return(nil)

		res := helper.updateNodeLabels(context.Background(), "nodeName", &reconLabels)

		Expect(res).ToNot(HaveOccurred())
	})
})
