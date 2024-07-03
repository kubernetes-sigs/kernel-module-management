package node

import (
	"context"
	"fmt"
	"github.com/kubernetes-sigs/kernel-module-management/internal/meta"
	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type Node interface {
	IsNodeSchedulable(node *v1.Node) bool
	GetNodesListBySelector(ctx context.Context, selector map[string]string) ([]v1.Node, error)
	GetNumTargetedNodes(ctx context.Context, selector map[string]string) (int, error)
	FindNodeCondition(cond []v1.NodeCondition, conditionType v1.NodeConditionType) *v1.NodeCondition
	UpdateLabels(ctx context.Context, node *v1.Node, toBeAdded, toBeRemoved []string) error
}

type node struct {
	client client.Client
}

func NewNode(client client.Client) Node {
	return &node{
		client: client,
	}
}

func (n *node) IsNodeSchedulable(node *v1.Node) bool {
	for _, taint := range node.Spec.Taints {
		if taint.Effect == v1.TaintEffectNoSchedule {
			return false
		}
	}
	return true
}

func (n *node) GetNodesListBySelector(ctx context.Context, selector map[string]string) ([]v1.Node, error) {
	logger := log.FromContext(ctx)
	logger.V(1).Info("Listing nodes", "selector", selector)

	selectedNodes := v1.NodeList{}
	opt := client.MatchingLabels(selector)
	if err := n.client.List(ctx, &selectedNodes, opt); err != nil {
		return nil, fmt.Errorf("could not list nodes: %v", err)
	}
	nodes := make([]v1.Node, 0, len(selectedNodes.Items))

	for _, node := range selectedNodes.Items {
		if n.IsNodeSchedulable(&node) {
			nodes = append(nodes, node)
		}
	}
	return nodes, nil
}

func (n *node) GetNumTargetedNodes(ctx context.Context, selector map[string]string) (int, error) {
	targetedNode, err := n.GetNodesListBySelector(ctx, selector)
	if err != nil {
		return 0, fmt.Errorf("could not list nodes: %v", err)
	}
	return len(targetedNode), nil
}

func (n *node) FindNodeCondition(cond []v1.NodeCondition, conditionType v1.NodeConditionType) *v1.NodeCondition {
	for i := 0; i < len(cond); i++ {
		c := cond[i]

		if c.Type == conditionType {
			return &c
		}
	}

	return nil
}

func (n *node) UpdateLabels(ctx context.Context, node *v1.Node, toBeAdded, toBeRemoved []string) error {
	patchFrom := client.MergeFrom(node.DeepCopy())

	addLabels(node, toBeAdded)
	removeLabels(node, toBeRemoved)

	if err := n.client.Patch(ctx, node, patchFrom); err != nil {
		return fmt.Errorf("could not patch node: %v", err)
	}
	return nil
}

func addLabels(node *v1.Node, labels []string) {
	for _, label := range labels {
		meta.SetLabel(
			node,
			label,
			"",
		)
	}
}

func removeLabels(node *v1.Node, labels []string) {
	for _, label := range labels {
		meta.RemoveLabel(
			node,
			label,
		)
	}
}
