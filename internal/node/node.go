package node

import (
	"context"
	"fmt"
	"github.com/kubernetes-sigs/kernel-module-management/internal/meta"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

//go:generate mockgen -source=node.go -package=node -destination=mock_node.go

type Node interface {
	IsNodeSchedulable(node *v1.Node, tolerations []v1.Toleration) bool
	GetNodesListBySelector(ctx context.Context, selector map[string]string, tolerations []v1.Toleration) ([]v1.Node, error)
	GetNumTargetedNodes(ctx context.Context, selector map[string]string, tolerations []v1.Toleration) (int, error)
	UpdateLabels(ctx context.Context, node *v1.Node, toBeAdded, toBeRemoved []string) error
	NodeBecomeReadyAfter(node *v1.Node, checkTime metav1.Time) bool
	RemoveNodeReadyLabels(ctx context.Context, node *v1.Node) error
}

type node struct {
	client client.Client
}

func NewNode(client client.Client) Node {
	return &node{
		client: client,
	}
}

func (n *node) IsNodeSchedulable(node *v1.Node, tolerations []v1.Toleration) bool {
	for _, taint := range node.Spec.Taints {
		toleranceFound := false
		for _, toleration := range tolerations {
			if toleration.ToleratesTaint(&taint) {
				toleranceFound = true
				break
			}
		}
		if !toleranceFound && (taint.Effect == v1.TaintEffectNoSchedule || taint.Effect == v1.TaintEffectNoExecute) {
			return false
		}
	}
	return true
}

func (n *node) GetNodesListBySelector(ctx context.Context, selector map[string]string, tolerations []v1.Toleration) ([]v1.Node, error) {
	logger := log.FromContext(ctx)
	logger.V(1).Info("Listing nodes", "selector", selector)

	selectedNodes := v1.NodeList{}
	opt := client.MatchingLabels(selector)
	if err := n.client.List(ctx, &selectedNodes, opt); err != nil {
		return nil, fmt.Errorf("could not list nodes: %v", err)
	}
	nodes := make([]v1.Node, 0, len(selectedNodes.Items))

	for _, node := range selectedNodes.Items {
		if n.IsNodeSchedulable(&node, tolerations) {
			nodes = append(nodes, node)
		}
	}
	return nodes, nil
}

func (n *node) GetNumTargetedNodes(ctx context.Context, selector map[string]string, tolerations []v1.Toleration) (int, error) {
	targetedNode, err := n.GetNodesListBySelector(ctx, selector, tolerations)
	if err != nil {
		return 0, fmt.Errorf("could not list nodes: %v", err)
	}
	return len(targetedNode), nil
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

func (n *node) RemoveNodeReadyLabels(ctx context.Context, node *v1.Node) error {
	var labelsToRemove []string
	for label := range node.GetLabels() {
		if ok, _, _ := utils.IsKernelModuleReadyNodeLabel(label); ok ||
			utils.IsDeprecatedKernelModuleReadyNodeLabel(label) {
			labelsToRemove = append(labelsToRemove, label)
		}
	}
	if err := n.UpdateLabels(ctx, node, []string{}, labelsToRemove); err != nil {
		return fmt.Errorf("could update node %s labels: %v", node.Name, err)
	}
	return nil
}

func (n *node) NodeBecomeReadyAfter(node *v1.Node, timestamp metav1.Time) bool {
	conds := node.Status.Conditions
	for i := 0; i < len(conds); i++ {
		c := conds[i]
		if c.Type == v1.NodeReady && c.Status == v1.ConditionTrue && timestamp.Before(&c.LastTransitionTime) {
			return true
		}
	}
	return false
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
