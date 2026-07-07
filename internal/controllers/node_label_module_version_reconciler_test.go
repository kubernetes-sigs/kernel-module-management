package controllers

import (
	"context"
	"fmt"

	mock_client "github.com/kubernetes-sigs/kernel-module-management/internal/client"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
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
	nodeLabels := map[string]string{"some label": "some label value"}

	DescribeTable("reconciler flow", func(getSchedulePodsError, upDateNodeLabelsErrors, requeue bool) {
		labelsPerModules := map[string]*modulesVersionLabels{
			"moduleNameNamespace": {name: "name", namespace: "namespace"},
		}
		schedulePods := []v1.Pod{{}}
		loadedKernelModules := []types.NamespacedName{{Name: "some name", Namespace: "some namespace"}}
		reconcileLabelsResult := &reconcileLabelsResult{requeue: requeue}
		expectedRes := ctrl.Result{Requeue: requeue}
		mockHelper.EXPECT().getLabelsPerModules(ctx, nodeLabels).Return(labelsPerModules)
		if getSchedulePodsError {
			mockHelper.EXPECT().getSchedulePods(ctx, nodeName).Return(nil, fmt.Errorf("some error"))
			goto executeTestFunction
		}
		mockHelper.EXPECT().getSchedulePods(ctx, nodeName).Return(schedulePods, nil)
		mockHelper.EXPECT().getLoadedKernelModules(nodeLabels).Return(loadedKernelModules)
		mockHelper.EXPECT().reconcileLabels(labelsPerModules, schedulePods, loadedKernelModules).Return(reconcileLabelsResult)
		if upDateNodeLabelsErrors {
			mockHelper.EXPECT().updateNodeLabels(ctx, nodeName, reconcileLabelsResult).Return(fmt.Errorf("some error"))
			goto executeTestFunction
		}
		mockHelper.EXPECT().updateNodeLabels(ctx, nodeName, reconcileLabelsResult).Return(nil)
	executeTestFunction:
		node := v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Labels: nodeLabels,
				Name:   nodeName,
			},
		}

		res, err := nlmvr.Reconcile(ctx, &node)
		if upDateNodeLabelsErrors || getSchedulePodsError {
			Expect(err).To(HaveOccurred())
		} else {
			Expect(err).ToNot(HaveOccurred())
			Expect(res).To(Equal(expectedRes))
		}
	},
		Entry("good flow, no requeue", false, false, false),
		Entry("good flow, with requeue", false, false, true),
		Entry("get schedule pods failed", true, false, false),
		Entry("update node labels failed", false, true, false),
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
		"beta.kmm.node.kubernetes.io/version-worker-pod.namespace1.module1":   "1",
		"beta.kmm.node.kubernetes.io/version-schedule-pod.namespace1.module1": "1",
		"kmm.node.kubernetes.io/version-module.namespace1.module1":            "1",
		"beta.kmm.node.kubernetes.io/version-worker-pod.namespace2.module2":   "3",
		"kmm.node.kubernetes.io/version-module.namespace2.module2":            "3",
		"beta.kmm.node.kubernetes.io/version-schedule-pod.namespace3.module3": "4",
		"kmm.node.kubernetes.io/version-module.namespace5.module5":            "10",
		"beta.kmm.node.kubernetes.io/version-schedule-pod.namespace4.module4": "5",
		"beta.kmm.node.kubernetes.io/version-worker-pod.namespace4.module4":   "5",
		"kmm.node.kubernetes.io/version-module.namespace4.module4":            "5",
	}

	It("normal flow", func() {
		expectedRes := map[string]*modulesVersionLabels{
			"namespace1-module1": {
				name:                    "module1",
				namespace:               "namespace1",
				moduleVersionLabel:      "1",
				workerPodVersionLabel:   "1",
				schedulePodVersionLabel: "1",
			},
			"namespace2-module2": {
				name:                  "module2",
				namespace:             "namespace2",
				moduleVersionLabel:    "3",
				workerPodVersionLabel: "3",
			},
			"namespace3-module3": {
				name:                    "module3",
				namespace:               "namespace3",
				schedulePodVersionLabel: "4",
			},
			"namespace4-module4": {
				name:                    "module4",
				namespace:               "namespace4",
				moduleVersionLabel:      "5",
				workerPodVersionLabel:   "5",
				schedulePodVersionLabel: "5",
			},
			"namespace5-module5": {
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

var _ = Describe("getSchedulePods", func() {
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
		pod1 := v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					constants.ModuleNameLabel: "module name",
					constants.DaemonSetRole:   constants.DevicePluginRoleLabelValue,
				},
			},
		}

		kubeClient.EXPECT().List(ctx, gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ interface{}, list *v1.PodList, _ ...interface{}) error {
				list.Items = append(list.Items, pod1)
				return nil
			},
		)

		res, err := helper.getSchedulePods(ctx, nodeName)
		Expect(err).ToNot(HaveOccurred())
		Expect(len(res)).To(Equal(1))
		Expect(res[0]).To(Equal(pod1))
	})

	It("error flow", func() {
		kubeClient.EXPECT().List(ctx, gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(fmt.Errorf("some error"))

		res, err := helper.getSchedulePods(ctx, nodeName)
		Expect(err).To(HaveOccurred())
		Expect(res).To(BeNil())
	})

})

var _ = Describe("getLabelAndAction", func() {
	DescribeTable("should return correct label and action", func(moduleVersionValue, workerPodVersionValue, schedulePodVersionValue string,
		expectedLabelFunc func(string, string) string, expectedLabelValue, expectedAction string) {
		moduleLabels := &modulesVersionLabels{
			name:                    "moduleName",
			namespace:               "moduleNamespace",
			moduleVersionLabel:      moduleVersionValue,
			workerPodVersionLabel:   workerPodVersionValue,
			schedulePodVersionLabel: schedulePodVersionValue,
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
		Entry("module version missing, worker pod present, schedule pod present", "", "1", "1", utils.GetSchedulePodVersionLabelName, "", deleteAction),
		Entry("module version missing, worker pod present, schedule pod missing", "", "1", "", utils.GetWorkerPodVersionLabelName, "", deleteAction),
		Entry("all labels missing", "", "", "", nil, "", noneAction),
		Entry("module version present, worker pod missing, schedule pod missing", "1", "", "", utils.GetWorkerPodVersionLabelName, "1", addAction),
		Entry("module version present, worker pod present, schedule pod missing", "1", "1", "", utils.GetSchedulePodVersionLabelName, "1", addAction),
		Entry("module version present, worker pod different, schedule pod different", "2", "1", "1", utils.GetSchedulePodVersionLabelName, "", deleteAction),
		Entry("module version present, worker pod different, schedule pod missing", "2", "1", "", utils.GetWorkerPodVersionLabelName, "", deleteAction),
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

	It("delete schedule pod label, schedule pod not present", func() {
		moduleLabels := &modulesVersionLabels{
			name:                    "moduleName",
			namespace:               "moduleNamespace",
			moduleVersionLabel:      "",
			workerPodVersionLabel:   "1",
			schedulePodVersionLabel: "1",
		}

		res := helper.reconcileLabels(map[string]*modulesVersionLabels{"key": moduleLabels}, []v1.Pod{pod}, nil)

		Expect(res.requeue).To(BeFalse())
		Expect(len(res.labelsToAdd)).To(Equal(0))
		Expect(res.labelsToDelete).To(Equal([]string{utils.GetSchedulePodVersionLabelName("moduleNamespace", "moduleName")}))
	})

	It("delete worker pod label, schedule pod not present", func() {
		moduleLabels := &modulesVersionLabels{
			name:                    "moduleName",
			namespace:               "moduleNamespace",
			moduleVersionLabel:      "",
			workerPodVersionLabel:   "1",
			schedulePodVersionLabel: "",
		}

		res := helper.reconcileLabels(map[string]*modulesVersionLabels{"key": moduleLabels}, []v1.Pod{pod}, nil)

		Expect(res.requeue).To(BeFalse())
		Expect(len(res.labelsToAdd)).To(Equal(0))
		Expect(res.labelsToDelete).To(Equal([]string{utils.GetWorkerPodVersionLabelName("moduleNamespace", "moduleName")}))
	})

	It("delete schedule pod label, schedule pod present", func() {
		moduleLabels := &modulesVersionLabels{
			name:                    "moduleName",
			namespace:               "moduleNamespace",
			moduleVersionLabel:      "",
			workerPodVersionLabel:   "1",
			schedulePodVersionLabel: "1",
		}
		pod.SetLabels(map[string]string{constants.ModuleNameLabel: "moduleName"})

		res := helper.reconcileLabels(map[string]*modulesVersionLabels{"key": moduleLabels}, []v1.Pod{pod}, nil)

		Expect(res.requeue).To(BeFalse())
		Expect(res.labelsToDelete).To(Equal([]string{utils.GetSchedulePodVersionLabelName("moduleNamespace", "moduleName")}))
		Expect(len(res.labelsToAdd)).To(Equal(0))
	})

	It("delete worker pod label, schedule pod present", func() {
		moduleLabels := &modulesVersionLabels{
			name:                    "moduleName",
			namespace:               "moduleNamespace",
			moduleVersionLabel:      "",
			workerPodVersionLabel:   "1",
			schedulePodVersionLabel: "",
		}
		pod.SetLabels(map[string]string{constants.ModuleNameLabel: "moduleName"})

		res := helper.reconcileLabels(map[string]*modulesVersionLabels{"key": moduleLabels}, []v1.Pod{pod}, nil)

		Expect(res.requeue).To(BeTrue())
		Expect(len(res.labelsToAdd)).To(Equal(0))
		Expect(len(res.labelsToDelete)).To(Equal(0))
	})

	It("add worker pod label, kernel module is not loaded", func() {
		moduleLabels := &modulesVersionLabels{
			name:                    "moduleName",
			namespace:               "moduleNamespace",
			moduleVersionLabel:      "1",
			workerPodVersionLabel:   "",
			schedulePodVersionLabel: "",
		}

		res := helper.reconcileLabels(map[string]*modulesVersionLabels{"key": moduleLabels}, []v1.Pod{pod}, nil)

		Expect(res.requeue).To(BeFalse())
		Expect(res.labelsToAdd).To(Equal(map[string]string{utils.GetWorkerPodVersionLabelName("moduleNamespace", "moduleName"): "1"}))
		Expect(len(res.labelsToDelete)).To(Equal(0))
	})

	It("add worker pod label, kernel module is loaded", func() {
		moduleLabels := &modulesVersionLabels{
			name:                    "moduleName",
			namespace:               "moduleNamespace",
			moduleVersionLabel:      "1",
			workerPodVersionLabel:   "",
			schedulePodVersionLabel: "",
		}

		loadedKernelModules := []types.NamespacedName{{Name: "moduleName", Namespace: "moduleNamespace"}}

		res := helper.reconcileLabels(map[string]*modulesVersionLabels{"key": moduleLabels}, []v1.Pod{pod}, loadedKernelModules)

		Expect(res.requeue).To(BeTrue())
		Expect(len(res.labelsToDelete)).To(Equal(0))
		Expect(len(res.labelsToAdd)).To(Equal(0))
	})

	It("add schedule pod label, kernel module is not loaded", func() {
		moduleLabels := &modulesVersionLabels{
			name:                    "moduleName",
			namespace:               "moduleNamespace",
			moduleVersionLabel:      "1",
			workerPodVersionLabel:   "1",
			schedulePodVersionLabel: "",
		}

		res := helper.reconcileLabels(map[string]*modulesVersionLabels{"key": moduleLabels}, []v1.Pod{pod}, nil)

		Expect(res.requeue).To(BeFalse())
		Expect(res.labelsToAdd).To(Equal(map[string]string{utils.GetSchedulePodVersionLabelName("moduleNamespace", "moduleName"): "1"}))
		Expect(len(res.labelsToDelete)).To(Equal(0))
	})

	It("add schedule pod label, kernel module is loaded", func() {
		moduleLabels := &modulesVersionLabels{
			name:                    "moduleName",
			namespace:               "moduleNamespace",
			moduleVersionLabel:      "1",
			workerPodVersionLabel:   "1",
			schedulePodVersionLabel: "",
		}

		loadedKernelModules := []types.NamespacedName{{Name: "moduleName", Namespace: "moduleNamespace"}}

		res := helper.reconcileLabels(map[string]*modulesVersionLabels{"key": moduleLabels}, []v1.Pod{pod}, loadedKernelModules)

		Expect(res.requeue).To(BeFalse())
		Expect(res.labelsToAdd).To(Equal(map[string]string{utils.GetSchedulePodVersionLabelName("moduleNamespace", "moduleName"): "1"}))
		Expect(len(res.labelsToDelete)).To(Equal(0))
	})

	It("no label needs to be added due to none action", func() {
		moduleLabels := &modulesVersionLabels{
			name:                    "moduleName",
			namespace:               "moduleNamespace",
			moduleVersionLabel:      "1",
			workerPodVersionLabel:   "1",
			schedulePodVersionLabel: "1",
		}
		pod := v1.Pod{}
		pod.Namespace = "moduleNamespace"
		pod.SetLabels(map[string]string{constants.ModuleNameLabel: "moduleName"})

		res := helper.reconcileLabels(map[string]*modulesVersionLabels{"key": moduleLabels}, []v1.Pod{pod}, nil)

		Expect(res.requeue).To(BeFalse())
		Expect(len(res.labelsToAdd)).To(Equal(0))
		Expect(len(res.labelsToDelete)).To(Equal(0))
	})
})

var _ = Describe("updateNodeLabels", func() {
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
