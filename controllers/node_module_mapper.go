package controllers

import (
	"context"

	"github.com/go-logr/logr"
	ootov1alpha1 "github.com/qbarrand/oot-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type NodeModuleMapper struct {
	client client.Client
	logger logr.Logger
}

func NewNodeModuleMapper(client client.Client, logger logr.Logger) *NodeModuleMapper {
	return &NodeModuleMapper{
		client: client,
		logger: logger,
	}
}

func (nmm *NodeModuleMapper) FindModulesForNode(node client.Object) []reconcile.Request {
	logger := nmm.logger.WithValues("node", node.GetName())

	reqs := make([]reconcile.Request, 0)

	logger.Info("Listing all modules")

	mods := ootov1alpha1.ModuleList{}

	if err := nmm.client.List(context.Background(), &mods); err != nil {
		logger.Error(err, "could not list modules")
		return reqs
	}

	logger.Info("Listed modules", "count", len(mods.Items))

	nodeLabelsSet := labels.Set(node.GetLabels())

	for _, mod := range mods.Items {
		logger := logger.WithValues("module name", mod.Name)

		logger.V(1).Info("Processing module")

		sel := labels.NewSelector()

		for k, v := range mod.Spec.Selector {
			logger.V(1).Info("Processing selector item", "key", k, "value", v)

			requirement, err := labels.NewRequirement(k, selection.Equals, []string{v})
			if err != nil {
				logger.Error(err, "could not generate requirement: %v", err)
				return reqs
			}

			sel = sel.Add(*requirement)
		}

		if !sel.Matches(nodeLabelsSet) {
			logger.V(1).Info("Node labels do not match the module's selector; skipping")
			continue
		}

		nsn := types.NamespacedName{Name: mod.Name, Namespace: mod.Namespace}

		reqs = append(reqs, reconcile.Request{NamespacedName: nsn})
	}

	logger.Info("Adding reconciliation requests", "count", len(reqs))
	logger.V(1).Info("New requests", "requests", reqs)

	return reqs
}
