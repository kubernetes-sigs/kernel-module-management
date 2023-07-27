package utils

import (
	v1 "k8s.io/api/core/v1"
)

func IsNodeSchedulable(node *v1.Node) bool {
	for _, taint := range node.Spec.Taints {
		if taint.Effect == v1.TaintEffectNoSchedule {
			return false
		}
	}
	return true
}
