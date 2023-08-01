package utils

import (
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
)

func IsNodeSchedulable(node *v1.Node) bool {
	for _, taint := range node.Spec.Taints {
		if taint.Effect == v1.TaintEffectNoSchedule {
			return false
		}
	}
	return true
}

func IsObjectSelectedByLabels(objectLabels map[string]string, selectorLabels map[string]string) (bool, error) {
	objectLabelsSet := labels.Set(objectLabels)
	sel := labels.NewSelector()

	for k, v := range selectorLabels {
		requirement, err := labels.NewRequirement(k, selection.Equals, []string{v})
		if err != nil {
			return false, fmt.Errorf("failed to create new label requirements: %v", err)
		}
		sel = sel.Add(*requirement)
	}

	return sel.Matches(objectLabelsSet), nil
}
