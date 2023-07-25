package nmc

import (
	"context"
	"fmt"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
)

//go:generate mockgen -source=helper.go -package=nmc -destination=mock_helper.go

type Helper interface {
	Get(ctx context.Context, name string) (*kmmv1beta1.NodeModulesConfig, error)
	SetNMCAsDesired(ctx context.Context, nmc *kmmv1beta1.NodeModulesConfig, namespace, name string, moduleConfig *kmmv1beta1.ModuleConfig) error
	GetModuleEntry(nmc *kmmv1beta1.NodeModulesConfig, modNamespace, modName string) (*kmmv1beta1.NodeModuleSpec, int)
}

type helper struct {
	client client.Client
}

func NewHelper(client client.Client) Helper {
	return &helper{
		client: client,
	}
}

func (h *helper) Get(ctx context.Context, name string) (*kmmv1beta1.NodeModulesConfig, error) {
	nmc := kmmv1beta1.NodeModulesConfig{}
	err := h.client.Get(ctx, types.NamespacedName{Name: name}, &nmc)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get NodeModulesConfig %s: %v", name, err)
	}
	return &nmc, nil
}

func (h *helper) SetNMCAsDesired(ctx context.Context,
	nmc *kmmv1beta1.NodeModulesConfig,
	namespace string,
	name string,
	moduleConfig *kmmv1beta1.ModuleConfig) error {
	foundEntry, index := h.GetModuleEntry(nmc, namespace, name)
	// remove entry
	if moduleConfig == nil {
		if foundEntry != nil {
			nmc.Spec.Modules = append(nmc.Spec.Modules[:index], nmc.Spec.Modules[index+1:]...)
		}
		return nil
	}

	// add/change entry
	if foundEntry == nil {
		nmc.Spec.Modules = append(nmc.Spec.Modules, kmmv1beta1.NodeModuleSpec{Name: name, Namespace: namespace})
		foundEntry = &nmc.Spec.Modules[len(nmc.Spec.Modules)-1]
	}
	foundEntry.Config = *moduleConfig

	return nil
}

func (h *helper) GetModuleEntry(nmc *kmmv1beta1.NodeModulesConfig, modNamespace, modName string) (*kmmv1beta1.NodeModuleSpec, int) {
	for i, moduleSpec := range nmc.Spec.Modules {
		if moduleSpec.Namespace == modNamespace && moduleSpec.Name == modName {
			return &nmc.Spec.Modules[i], i
		}
	}
	return nil, 0
}
