package test

import (
	"errors"
	"slices"

	"github.com/kubernetes-sigs/kernel-module-management/api/v1beta2"
	"github.com/kubernetes-sigs/kernel-module-management/internal/preflight"
	"k8s.io/apimachinery/pkg/types"
)

func DeleteModuleStatus(statuses *[]v1beta2.PreflightValidationModuleStatus, nsn types.NamespacedName) {
	*statuses = slices.DeleteFunc(*statuses, func(status v1beta2.PreflightValidationModuleStatus) bool {
		return status.Namespace == nsn.Namespace && status.Name == nsn.Name
	})
}

func UpsertModuleStatus(statuses *[]v1beta2.PreflightValidationModuleStatus, s v1beta2.PreflightValidationModuleStatus) error {
	if s.Name == "" || s.Namespace == "" {
		return errors.New("name and namespace may not be empty")
	}

	found, ok := preflight.FindModuleStatus(*statuses, types.NamespacedName{Name: s.Name, Namespace: s.Namespace})
	if ok {
		found.CRBaseStatus = s.CRBaseStatus
	} else {
		*statuses = append(*statuses, s)
	}

	return nil
}
