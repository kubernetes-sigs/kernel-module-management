package node

import (
	"context"
	"fmt"
	"github.com/kubernetes-sigs/kernel-module-management/internal/client"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
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
		isNodeSchedulable = mn.IsNodeSchedulable(&node)
		Expect(isNodeSchedulable).To(BeFalse())

	})
	It("Returns true, node is schedulable", func() {

		node := v1.Node{
			Spec: v1.NodeSpec{
				Taints: []v1.Taint{
					{
						Effect: v1.TaintEffectNoExecute,
					},
					{
						Effect: v1.TaintEffectPreferNoSchedule,
					},
				},
			},
		}
		isNodeSchedulable = mn.IsNodeSchedulable(&node)
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

		nodes, err := mn.GetNodesListBySelector(context.Background(), map[string]string{})

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
		nodes, err := mn.GetNodesListBySelector(context.Background(), map[string]string{})

		Expect(err).NotTo(HaveOccurred())
		Expect(nodes).To(Equal([]v1.Node{node2, node3}))

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

	It("There are not schedulable nodes", func() {
		clnt.EXPECT().List(context.Background(), gomock.Any(), gomock.Any()).Return(fmt.Errorf("some error"))

		numOfNodes, err := mn.GetNumTargetedNodes(context.Background(), map[string]string{})

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
		numOfNodes, err := mn.GetNumTargetedNodes(context.Background(), map[string]string{})

		Expect(err).NotTo(HaveOccurred())
		Expect(numOfNodes).To(Equal(2))

	})
})

var _ = Describe("FindNodeCondition", func() {
	var (
		ctrl           *gomock.Controller
		clnt           *client.MockClient
		mn             Node
		conditionFound *v1.NodeCondition
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		mn = NewNode(clnt)
	})

	It("Finds condition", func() {
		condition := v1.NodeCondition{
			Type: v1.NodeReady,
		}
		node := v1.Node{
			Status: v1.NodeStatus{
				Conditions: []v1.NodeCondition{
					condition,
				},
			},
		}
		conditionFound = mn.FindNodeCondition(node.Status.Conditions, v1.NodeReady)
		Expect(conditionFound).To(Equal(&condition))
	})

	It("Does not find condition", func() {
		node := v1.Node{
			Status: v1.NodeStatus{
				Conditions: []v1.NodeCondition{
					{
						Type: v1.NodeDiskPressure,
					},
					{
						Type: v1.NodeMemoryPressure,
					},
				},
			},
		}
		conditionFound = mn.FindNodeCondition(node.Status.Conditions, v1.NodeReady)
		Expect(conditionFound).To(BeNil())
	})
})
