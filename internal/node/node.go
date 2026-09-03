package node

import (
	"context"
	"fmt"
	"github.com/kubernetes-sigs/kernel-module-management/internal/meta"
	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

//go:generate mockgen -source=node.go -package=node -destination=mock_node.go

type Node interface {
	IsNodeSchedulable(node *v1.Node, tolerations []v1.Toleration) bool
	GetAllNodesBySelector(ctx context.Context, selector map[string]string) ([]v1.Node, error)
	GetAllNodesByLabelKey(ctx context.Context, labelKey string) ([]v1.Node, error)
	GetSchedulableNodesBySelector(ctx context.Context, selector map[string]string, tolerations []v1.Toleration) ([]v1.Node, error)
	GetNumTargetedNodes(ctx context.Context, selector map[string]string, tolerations []v1.Toleration) (int, error)
	UpdateLabels(ctx context.Context, node *v1.Node, toBeAdded, toBeRemoved map[string]string) error
	UpdateLabelsWithOptimisticLock(ctx context.Context, node *v1.Node, toBeAdded, toBeRemoved map[string]string) error
	IsNodeRebooted(node *v1.Node, statusBootId string) bool
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
			if toleration.ToleratesTaint(klog.Background(), &taint, false) {
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

func (n *node) GetAllNodesBySelector(ctx context.Context, selector map[string]string) ([]v1.Node, error) {
	logger := log.FromContext(ctx)
	logger.V(1).Info("Listing nodes", "selector", selector)

	selectedNodes := v1.NodeList{}
	opt := client.MatchingLabels(selector)
	if err := n.client.List(ctx, &selectedNodes, opt); err != nil {
		return nil, fmt.Errorf("could not list nodes: %v", err)
	}
	return selectedNodes.Items, nil
}

// GetAllNodesByLabelKey lists the nodes carrying labelKey whatever its value, unlike
// GetAllNodesBySelector, which matches on the value too.
func (n *node) GetAllNodesByLabelKey(ctx context.Context, labelKey string) ([]v1.Node, error) {
	logger := log.FromContext(ctx)
	logger.V(1).Info("Listing nodes", "label key", labelKey)

	selectedNodes := v1.NodeList{}
	if err := n.client.List(ctx, &selectedNodes, client.HasLabels{labelKey}); err != nil {
		return nil, fmt.Errorf("could not list nodes: %v", err)
	}
	return selectedNodes.Items, nil
}

func (n *node) GetSchedulableNodesBySelector(ctx context.Context, selector map[string]string, tolerations []v1.Toleration) ([]v1.Node, error) {
	allNodes, err := n.GetAllNodesBySelector(ctx, selector)
	if err != nil {
		return nil, fmt.Errorf("could not get all nodes by selector: %v", err)
	}

	nodes := make([]v1.Node, 0, len(allNodes))
	for i := range allNodes {
		if n.IsNodeSchedulable(&allNodes[i], tolerations) {
			nodes = append(nodes, allNodes[i])
		}
	}
	return nodes, nil
}

func (n *node) GetNumTargetedNodes(ctx context.Context, selector map[string]string, tolerations []v1.Toleration) (int, error) {
	targetedNode, err := n.GetSchedulableNodesBySelector(ctx, selector, tolerations)
	if err != nil {
		return 0, fmt.Errorf("could not list nodes: %v", err)
	}
	return len(targetedNode), nil
}

func (n *node) UpdateLabels(ctx context.Context, node *v1.Node, toBeAdded, toBeRemoved map[string]string) error {
	return n.patchLabels(ctx, node, toBeAdded, toBeRemoved, client.MergeFrom(node.DeepCopy()))
}

// UpdateLabelsWithOptimisticLock is UpdateLabels for a caller whose decision would be wrong if the
// node had changed since it was read: the patch then fails with a conflict instead of landing.
func (n *node) UpdateLabelsWithOptimisticLock(ctx context.Context, node *v1.Node, toBeAdded, toBeRemoved map[string]string) error {
	patchFrom := client.MergeFromWithOptions(node.DeepCopy(), client.MergeFromWithOptimisticLock{})

	return n.patchLabels(ctx, node, toBeAdded, toBeRemoved, patchFrom)
}

func (n *node) patchLabels(
	ctx context.Context,
	node *v1.Node,
	toBeAdded, toBeRemoved map[string]string,
	patchFrom client.Patch,
) error {
	addLabels(node, toBeAdded)
	removeLabels(node, toBeRemoved)

	if err := n.client.Patch(ctx, node, patchFrom); err != nil {
		return fmt.Errorf("could not patch node: %w", err)
	}
	return nil
}

func (n *node) IsNodeRebooted(node *v1.Node, statusBootId string) bool {
	conds := node.Status.Conditions
	for i := 0; i < len(conds); i++ {
		c := conds[i]
		if c.Type == v1.NodeReady && c.Status == v1.ConditionTrue && (statusBootId != node.Status.NodeInfo.BootID) {
			return true
		}
	}
	return false
}

func addLabels(node *v1.Node, labels map[string]string) {
	for label, value := range labels {
		meta.SetLabel(
			node,
			label,
			value,
		)
	}
}

func removeLabels(node *v1.Node, labels map[string]string) {
	for label := range labels {
		meta.RemoveLabel(
			node,
			label,
		)
	}
}
