package nmc

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/api"
	"github.com/kubernetes-sigs/kernel-module-management/internal/client"
	"go.uber.org/mock/gomock"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("handleModuleNMC", func() {
	var (
		ctrl          *gomock.Controller
		ctx           context.Context
		mnh           *moduleNMCHandler
		mnhh          *MockmoduleNMCHandlerHelperAPI
		kernelVersion string
		nodeName      string
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mnhh = NewMockmoduleNMCHandlerHelperAPI(ctrl)
		mnh = &moduleNMCHandler{reconHelper: mnhh}
		ctx = context.Background()
		kernelVersion = "some kernel version"
		nodeName = "node name"
	})

	It("module should be on node", func() {
		node := v1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: nodeName},
			Status: v1.NodeStatus{
				NodeInfo: v1.NodeSystemInfo{
					KernelVersion: kernelVersion,
				},
			},
		}
		nodes := []v1.Node{node}
		mld := &api.ModuleLoaderData{}
		mnhh.EXPECT().shouldModuleRunOnNode(nodes[0], mld, kernelVersion).Return(true, nil)
		mnhh.EXPECT().enableModuleOnNode(ctx, mld, "node name", kernelVersion).Return(nil)

		err := mnh.handleModuleNMC(ctx, mld, nodes)

		Expect(err).NotTo(HaveOccurred())
	})

	It("module should not be on node", func() {
		node := v1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: nodeName},
			Status: v1.NodeStatus{
				NodeInfo: v1.NodeSystemInfo{
					KernelVersion: kernelVersion,
				},
			},
		}
		nodes := []v1.Node{node}
		mld := &api.ModuleLoaderData{
			Name:      "some name",
			Namespace: "some namespace",
		}

		By("shouldModuleRunOnNode failed")
		mnhh.EXPECT().shouldModuleRunOnNode(nodes[0], mld, kernelVersion).Return(false, nil)
		mnhh.EXPECT().disableModuleOnNode(ctx, mld.Namespace, mld.Name, nodeName).Return(nil)

		err := mnh.handleModuleNMC(ctx, mld, nodes)

		Expect(err).NotTo(HaveOccurred())
	})

	It("error flow", func() {
		node := v1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: nodeName},
			Status: v1.NodeStatus{
				NodeInfo: v1.NodeSystemInfo{
					KernelVersion: kernelVersion,
				},
			},
		}
		nodes := []v1.Node{node}
		mld := &api.ModuleLoaderData{
			Name:      "some name",
			Namespace: "some namespace",
		}

		By("shouldModuleRunOnNode failed")
		mnhh.EXPECT().shouldModuleRunOnNode(nodes[0], mld, kernelVersion).Return(false, fmt.Errorf("some error"))

		err := mnh.handleModuleNMC(ctx, mld, nodes)

		Expect(err).To(HaveOccurred())

		By("enableModuleOnNode failed")
		mnhh.EXPECT().shouldModuleRunOnNode(nodes[0], mld, kernelVersion).Return(true, nil)
		mnhh.EXPECT().enableModuleOnNode(ctx, mld, nodeName, kernelVersion).Return(fmt.Errorf("some error"))

		err = mnh.handleModuleNMC(ctx, mld, nodes)

		Expect(err).To(HaveOccurred())

		By("disableModuleOnNode failed")
		mnhh.EXPECT().shouldModuleRunOnNode(nodes[0], mld, kernelVersion).Return(false, nil)
		mnhh.EXPECT().disableModuleOnNode(ctx, mld.Namespace, mld.Name, nodeName).Return(fmt.Errorf("some error"))

		err = mnh.handleModuleNMC(ctx, mld, nodes)

		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("shouldModuleRunOnNode", func() {
	var (
		//ctrl          *gomock.Controller
		//clnt          *client.MockClient
		mnhh moduleNMCHandlerHelperAPI
		//helper        *MockHelper
		kernelVersion string
	)

	BeforeEach(func() {
		//ctrl = gomock.NewController(GinkgoT())
		//clnt = client.NewMockClient(ctrl)
		//helper = NewMockHelper(ctrl)
		mnhh = newModuleNMCHandlerHelper(nil, nil)
		kernelVersion = "some version"
	})

	It("kernel version not equal", func() {
		node := v1.Node{}
		mld := &api.ModuleLoaderData{
			KernelVersion: "other kernelVersion",
		}

		res, err := mnhh.shouldModuleRunOnNode(node, mld, kernelVersion)
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
		}

		mld := &api.ModuleLoaderData{
			KernelVersion: kernelVersion,
		}

		res, err := mnhh.shouldModuleRunOnNode(node, mld, kernelVersion)
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(BeFalse())
	})

	DescribeTable("selector vs node labels verification", func(mldSelector, nodeLabels map[string]string, expectedResult bool) {
		mld := &api.ModuleLoaderData{
			KernelVersion: kernelVersion,
			Selector:      mldSelector,
		}
		node := v1.Node{}
		node.SetLabels(nodeLabels)

		res, err := mnhh.shouldModuleRunOnNode(node, mld, kernelVersion)
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
		mnhh                 moduleNMCHandlerHelperAPI
		helper               *MockHelper
		mld                  *api.ModuleLoaderData
		expectedModuleConfig *kmmv1beta1.ModuleConfig
		kernelVersion        string
		nodeName             string
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		helper = NewMockHelper(ctrl)
		mnhh = newModuleNMCHandlerHelper(clnt, helper)
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

	It("NMC does not exists", func() {
		nmc := &kmmv1beta1.NodeModulesConfig{
			ObjectMeta: metav1.ObjectMeta{Name: nodeName},
		}

		gomock.InOrder(
			clnt.EXPECT().Get(ctx, gomock.Any(), gomock.Any()).Return(apierrors.NewNotFound(schema.GroupResource{}, "whatever")),
			helper.EXPECT().SetModuleConfig(ctx, nmc, mld.Namespace, mld.Name, expectedModuleConfig).Return(nil),
			clnt.EXPECT().Create(ctx, gomock.Any()).Return(nil),
		)

		err := mnhh.enableModuleOnNode(ctx, mld, nodeName, kernelVersion)
		Expect(err).NotTo(HaveOccurred())
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
			helper.EXPECT().SetModuleConfig(ctx, nmc, mld.Namespace, mld.Name, expectedModuleConfig).Return(nil),
		)

		err := mnhh.enableModuleOnNode(ctx, mld, nodeName, kernelVersion)
		Expect(err).NotTo(HaveOccurred())
	})
})

var _ = Describe("disableModuleOnNode", func() {
	var (
		ctx             context.Context
		ctrl            *gomock.Controller
		clnt            *client.MockClient
		mnhh            moduleNMCHandlerHelperAPI
		helper          *MockHelper
		nodeName        string
		moduleName      string
		moduleNamespace string
	)

	BeforeEach(func() {
		ctx = context.Background()
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		helper = NewMockHelper(ctrl)
		mnhh = newModuleNMCHandlerHelper(clnt, helper)
		nodeName = "node name"
		moduleName = "moduleName"
		moduleNamespace = "moduleNamespace"
	})

	It("NMC does not exists", func() {
		helper.EXPECT().Get(ctx, nodeName).Return(nil, nil)

		err := mnhh.disableModuleOnNode(ctx, moduleNamespace, moduleName, nodeName)
		Expect(err).NotTo(HaveOccurred())
	})

	It("failed to get NMC", func() {
		helper.EXPECT().Get(ctx, nodeName).Return(nil, fmt.Errorf("some error"))

		err := mnhh.disableModuleOnNode(ctx, moduleNamespace, moduleName, nodeName)
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
			helper.EXPECT().RemoveModuleConfig(ctx, nmc, moduleNamespace, moduleName).Return(nil),
		)

		err := mnhh.disableModuleOnNode(ctx, moduleNamespace, moduleName, nodeName)
		Expect(err).NotTo(HaveOccurred())
	})
})
