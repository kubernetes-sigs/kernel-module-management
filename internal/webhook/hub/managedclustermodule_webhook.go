/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package hub

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/kubernetes-sigs/kernel-module-management/api-hub/v1beta1"
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/webhook"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

type ManagedClusterModuleValidator struct {
	logger logr.Logger
	m      admission.CustomValidator
}

func NewManagedClusterModuleValidator(logger logr.Logger) *ManagedClusterModuleValidator {
	return &ManagedClusterModuleValidator{
		logger: logger,
		m:      webhook.NewModuleValidator(logger),
	}
}

func (m *ManagedClusterModuleValidator) SetupWebhookWithManager(mgr ctrl.Manager) error {
	// controller-runtime will set the path to `validate-<group>-<version>-<resource> so we
	// need to make sure it is set correctly in the +kubebuilder annotation below.
	return ctrl.NewWebhookManagedBy(mgr).
		For(&v1beta1.ManagedClusterModule{}).
		WithValidator(m).
		Complete()
}

//+kubebuilder:webhook:path=/validate-hub-kmm-sigs-x-k8s-io-v1beta1-managedclustermodule,mutating=false,failurePolicy=fail,sideEffects=None,groups=hub.kmm.sigs.x-k8s.io,resources=managedclustermodules,verbs=create;update,versions=v1beta1,name=vmanagedclustermodule.kb.io,admissionReviewVersions=v1

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (m *ManagedClusterModuleValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	mcm, ok := obj.(*v1beta1.ManagedClusterModule)
	if !ok {
		return nil, fmt.Errorf("bad type for the object; expected %T, got %T", mcm, obj)
	}

	m.logger.Info("Validating ManagedClusterModule creation", "name", mcm.Name, "namespace", mcm.Namespace)

	return m.m.ValidateCreate(ctx, &kmmv1beta1.Module{Spec: mcm.Spec.ModuleSpec})
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (m *ManagedClusterModuleValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	oldMCM, ok := oldObj.(*v1beta1.ManagedClusterModule)
	if !ok {
		return nil, fmt.Errorf("bad type for the object; expected %T, got %T", oldMCM, oldObj)
	}

	newMCM, ok := newObj.(*v1beta1.ManagedClusterModule)
	if !ok {
		return nil, fmt.Errorf("bad type for the object; expected %T, got %T", newMCM, newObj)
	}

	m.logger.Info("Validating ManagedClusterModule update", "name", oldMCM.Name, "namespace", oldMCM.Namespace)

	return m.m.ValidateUpdate(ctx, &kmmv1beta1.Module{Spec: oldMCM.Spec.ModuleSpec}, &kmmv1beta1.Module{Spec: newMCM.Spec.ModuleSpec})
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (m *ManagedClusterModuleValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	return nil, webhook.NotImplemented
}
