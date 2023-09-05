package nmc

import (
	"context"
	"fmt"

	"github.com/kubernetes-sigs/kernel-module-management/internal/api"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
)

//go:generate mockgen -source=helper.go -package=nmc -destination=mock_helper.go

type Helper interface {
	Get(ctx context.Context, name string) (*kmmv1beta1.NodeModulesConfig, error)
	SetModuleConfig(nmc *kmmv1beta1.NodeModulesConfig, mld *api.ModuleLoaderData, moduleConfig *kmmv1beta1.ModuleConfig) error
	RemoveModuleConfig(nmc *kmmv1beta1.NodeModulesConfig, namespace, name string) error
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
			return nil, err
		}
		return nil, fmt.Errorf("failed to get NodeModulesConfig %s: %v", name, err)
	}
	return &nmc, nil
}

func (h *helper) SetModuleConfig(
	nmc *kmmv1beta1.NodeModulesConfig,
	mld *api.ModuleLoaderData,
	moduleConfig *kmmv1beta1.ModuleConfig) error {

	foundEntry, _ := h.GetModuleEntry(nmc, mld.Namespace, mld.Name)
	if foundEntry == nil {
		nms := kmmv1beta1.NodeModuleSpec{
			ModuleItem: kmmv1beta1.ModuleItem{
				Name:      mld.Name,
				Namespace: mld.Namespace,
			},
		}

		nmc.Spec.Modules = append(nmc.Spec.Modules, nms)
		foundEntry = &nmc.Spec.Modules[len(nmc.Spec.Modules)-1]
	}

	saName := mld.ServiceAccountName
	if saName == "" {
		saName = "default"
	}

	setLabel(nmc, mld.Namespace, mld.Name)
	foundEntry.Config = *moduleConfig
	foundEntry.ImageRepoSecret = mld.ImageRepoSecret
	foundEntry.ServiceAccountName = saName

	return nil
}

func (h *helper) RemoveModuleConfig(nmc *kmmv1beta1.NodeModulesConfig, namespace, name string) error {
	foundEntry, index := h.GetModuleEntry(nmc, namespace, name)
	if foundEntry != nil {
		nmc.Spec.Modules = append(nmc.Spec.Modules[:index], nmc.Spec.Modules[index+1:]...)
	}
	removeLabel(nmc, namespace, name)
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

func FindModuleStatus(statuses []kmmv1beta1.NodeModuleStatus, moduleNamespace, moduleName string) *kmmv1beta1.NodeModuleStatus {
	for i := 0; i < len(statuses); i++ {
		s := statuses[i]

		if s.Namespace == moduleNamespace && s.Name == moduleName {
			return &statuses[i]
		}
	}

	return nil
}

func RemoveModuleStatus(statuses *[]kmmv1beta1.NodeModuleStatus, modNamespace, modName string) {
	if statuses == nil || len(*statuses) == 0 {
		return
	}

	newStatuses := make([]kmmv1beta1.NodeModuleStatus, 0, len(*statuses)-1)

	for _, s := range *statuses {
		if s.Namespace != modNamespace || s.Name != modName {
			newStatuses = append(newStatuses, s)
		}
	}

	*statuses = newStatuses
}

func SetModuleStatus(statuses *[]kmmv1beta1.NodeModuleStatus, status kmmv1beta1.NodeModuleStatus) {
	if statuses == nil {
		return
	}

	s := FindModuleStatus(*statuses, status.Namespace, status.Name)

	if s != nil {
		*s = status
	} else {
		*statuses = append(*statuses, status)
	}
}

func setLabel(nmc *kmmv1beta1.NodeModulesConfig, namespace, name string) {
	moduleNMCLabel := utils.GetModuleNMCLabel(namespace, name)
	nmcLabels := nmc.GetLabels()
	if nmcLabels == nil {
		nmcLabels = map[string]string{}
	}
	nmcLabels[moduleNMCLabel] = ""
	nmc.SetLabels(nmcLabels)
}

func removeLabel(nmc *kmmv1beta1.NodeModulesConfig, namespace, name string) {
	moduleNMCLabel := utils.GetModuleNMCLabel(namespace, name)
	nmcLabels := nmc.GetLabels()
	if nmcLabels != nil {
		delete(nmcLabels, moduleNMCLabel)
		nmc.SetLabels(nmcLabels)
	}
}
