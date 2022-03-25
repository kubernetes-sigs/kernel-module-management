package controllers

import (
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

func SkipDeletions() predicate.Predicate {
	return predicate.Funcs{
		DeleteFunc: func(_ event.DeleteEvent) bool { return false },
	}
}
