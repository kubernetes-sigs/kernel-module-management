package predicates

import (
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

func HasLabel(label string) predicate.Predicate {
	return predicate.NewPredicateFuncs(func(o client.Object) bool {
		return o.GetLabels()[label] != ""
	})
}

func Namespace(namespace string) predicate.Predicate {
	return predicate.NewPredicateFuncs(func(o client.Object) bool {
		return o.GetNamespace() == namespace
	})
}

var SkipDeletions predicate.Predicate = predicate.Funcs{
	DeleteFunc: func(_ event.DeleteEvent) bool { return false },
}
