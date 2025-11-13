package node

import (
	"context"
	"fmt"
	"github.com/kubernetes-sigs/kernel-module-management/internal/client"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("IsNodeSchedulable", func() {
	var (
		ctrl              *gomock.Controller
		clnt              *client.MockClient
		mn                Node
		isNodeSchedulable bool
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		mn = NewNode(clnt)
	})

	It("Returns false, node is not schedulable", func() {

		node := v1.Node{
			Spec: v1.NodeSpec{
				Taints: []v1.Taint{
					{
						Effect: v1.TaintEffectNoSchedule,
					},
					{
						Effect: v1.TaintEffectPreferNoSchedule,
					},
				},
			},
		}
		isNodeSchedulable = mn.IsNodeSchedulable(&node, nil)
		Expect(isNodeSchedulable).To(BeFalse())

	})
	It("Returns true, node is schedulable", func() {

		node := v1.Node{
			Spec: v1.NodeSpec{
				Taints: []v1.Taint{
					{
						Effect: v1.TaintEffectPreferNoSchedule,
					},
				},
			},
		}
		isNodeSchedulable = mn.IsNodeSchedulable(&node, nil)
		Expect(isNodeSchedulable).To(BeTrue())

	})
})

var _ = Describe("GetNodesListBySelector", func() {
	var (
		ctrl *gomock.Controller
		clnt *client.MockClient
		mn   Node
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		mn = NewNode(clnt)
	})

	It("list failed", func() {
		clnt.EXPECT().List(context.Background(), gomock.Any(), gomock.Any()).Return(fmt.Errorf("some error"))

		nodes, err := mn.GetNodesListBySelector(context.Background(), map[string]string{}, nil)

		Expect(err).To(HaveOccurred())
		Expect(nodes).To(BeNil())
	})

	It("Return only schedulable nodes", func() {
		node1 := v1.Node{
			Spec: v1.NodeSpec{
				Taints: []v1.Taint{
					{
						Effect: v1.TaintEffectNoSchedule,
					},
				},
			},
		}
		node2 := v1.Node{}
		node3 := v1.Node{
			Spec: v1.NodeSpec{
				Taints: []v1.Taint{
					{
						Effect: v1.TaintEffectPreferNoSchedule,
					},
				},
			},
		}
		clnt.EXPECT().List(context.Background(), gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ interface{}, list *v1.NodeList, _ ...interface{}) error {
				list.Items = []v1.Node{node1, node2, node3}
				return nil
			},
		)
		nodes, err := mn.GetNodesListBySelector(context.Background(), map[string]string{}, nil)

		Expect(err).NotTo(HaveOccurred())
		Expect(nodes).To(Equal([]v1.Node{node2, node3}))

	})

	It("Select nodes that tolerate the taints of the node", func() {
		node1 := v1.Node{
			Spec: v1.NodeSpec{
				Taints: []v1.Taint{
					{
						Key:    "TestKey",
						Value:  "TestValue",
						Effect: v1.TaintEffectNoSchedule,
					},
				},
			},
		}
		node2 := v1.Node{
			Spec: v1.NodeSpec{
				Taints: []v1.Taint{
					{
						Effect: v1.TaintEffectNoSchedule,
					},
				},
			},
		}
		clnt.EXPECT().List(context.Background(), gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ interface{}, list *v1.NodeList, _ ...interface{}) error {
				list.Items = []v1.Node{node1, node2}
				return nil
			},
		)
		nodes, err := mn.GetNodesListBySelector(context.Background(),
			map[string]string{},
			[]v1.Toleration{
				{
					Key:    "TestKey",
					Value:  "TestValue",
					Effect: v1.TaintEffectNoSchedule,
				},
			})

		Expect(err).NotTo(HaveOccurred())
		Expect(nodes).To(Equal([]v1.Node{node1}))

	})
})

const (
	firstloadedKernelModuleReadyNodeLabel  = "kmm.node.kubernetes.io/loaded1-ns.loaded1-n.ready"
	secondloadedKernelModuleReadyNodeLabel = "kmm.node.kubernetes.io/loaded2-ns.loaded2-n.ready"
	unloadedKernelModuleReadyNodeLabel     = "kmm.node.kubernetes.io/unloaded-ns.unloaded-n.ready"
	notKernelModuleReadyNodeLabel          = "example.node.kubernetes.io/label-not-to-be-removed"
)

var _ = Describe("UpdateLabels", func() {
	var (
		ctrl *gomock.Controller
		n    Node
		ctx  context.Context
		clnt *client.MockClient
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		ctx = context.TODO()
		n = NewNode(clnt)
	})

	It("Should work as expected", func() {
		node := v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{},
			},
		}
		loaded := map[string]string{firstloadedKernelModuleReadyNodeLabel: ""}
		unloaded := map[string]string{unloadedKernelModuleReadyNodeLabel: ""}

		clnt.EXPECT().Patch(ctx, gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)

		err := n.UpdateLabels(ctx, &node, loaded, unloaded)
		Expect(err).ToNot(HaveOccurred())
		Expect(node.Labels).To(HaveKey(firstloadedKernelModuleReadyNodeLabel))
	})

	It("Should fail to patch node", func() {
		node := v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{},
			},
		}
		loaded := map[string]string{firstloadedKernelModuleReadyNodeLabel: ""}
		unloaded := map[string]string{unloadedKernelModuleReadyNodeLabel: ""}

		clnt.EXPECT().Patch(ctx, gomock.Any(), gomock.Any(), gomock.Any()).Return(fmt.Errorf("some error"))

		err := n.UpdateLabels(ctx, &node, loaded, unloaded)
		Expect(err).To(HaveOccurred())

	})
})

var _ = Describe("GetNumTargetedNodes", func() {
	var (
		ctrl *gomock.Controller
		clnt *client.MockClient
		mn   Node
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		mn = NewNode(clnt)
	})

	It("There are no schedulable nodes", func() {
		clnt.EXPECT().List(context.Background(), gomock.Any(), gomock.Any()).Return(fmt.Errorf("some error"))

		numOfNodes, err := mn.GetNumTargetedNodes(context.Background(), map[string]string{}, nil)

		Expect(err).To(HaveOccurred())
		Expect(numOfNodes).To(Equal(0))
	})

	It("Return the number of schedulable nodes only", func() {
		node1 := v1.Node{
			Spec: v1.NodeSpec{
				Taints: []v1.Taint{
					{
						Effect: v1.TaintEffectNoSchedule,
					},
				},
			},
		}
		node2 := v1.Node{}
		node3 := v1.Node{
			Spec: v1.NodeSpec{
				Taints: []v1.Taint{
					{
						Effect: v1.TaintEffectPreferNoSchedule,
					},
				},
			},
		}
		clnt.EXPECT().List(context.Background(), gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ interface{}, list *v1.NodeList, _ ...interface{}) error {
				list.Items = []v1.Node{node1, node2, node3}
				return nil
			},
		)
		numOfNodes, err := mn.GetNumTargetedNodes(context.Background(), map[string]string{}, nil)

		Expect(err).NotTo(HaveOccurred())
		Expect(numOfNodes).To(Equal(2))

	})
})

var _ = Describe("IsNodeRebooted", func() {
	var (
		n        Node
		testNode v1.Node
	)

	BeforeEach(func() {
		n = NewNode(nil)
		testNode = v1.Node{
			Status: v1.NodeStatus{
				Conditions: []v1.NodeCondition{
					{
						Type: v1.NodeMemoryPressure,
					},
					{
						Type: v1.NodeDiskPressure,
					},
					{
						Type: v1.NodeReady,
					},
					{
						Type: v1.NodeNetworkUnavailable,
					},
				},
			},
		}
	})

	It("ready condition is true, boot id changed", func() {
		statusBootId := "1"
		testNode.Status.Conditions[2].Status = v1.ConditionTrue
		testNode.Status.NodeInfo.BootID = "2"
		res := n.IsNodeRebooted(&testNode, statusBootId)
		Expect(res).To(BeTrue())
	})

	It("ready condition is true, boot id did not change", func() {
		statusBootId := "1"
		testNode.Status.Conditions[2].Status = v1.ConditionTrue
		testNode.Status.NodeInfo.BootID = "1"

		res := n.IsNodeRebooted(&testNode, statusBootId)
		Expect(res).To(BeFalse())
	})
})

var _ = Describe("removeLabels", func() {
	var node v1.Node

	BeforeEach(func() {
		node = v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					firstloadedKernelModuleReadyNodeLabel: "",
				},
			},
		}
	})

	It("Should remove labels", func() {
		labels := map[string]string{firstloadedKernelModuleReadyNodeLabel: ""}
		removeLabels(&node, labels)
		Expect(node.Labels).ToNot(HaveKey(firstloadedKernelModuleReadyNodeLabel))
	})
})
