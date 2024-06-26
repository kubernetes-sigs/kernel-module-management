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
	"github.com/kubernetes-sigs/kernel-module-management/internal/webhook"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
)

// FIXME: remove the code that returned an error until now
type ManagedClusterModuleDefaulter struct {
	logger          logr.Logger
	moduleDefaulter *webhook.ModuleDefaulter
}

func NewManagedClusterModuleDefaulter(logger logr.Logger) *ManagedClusterModuleDefaulter {
	return &ManagedClusterModuleDefaulter{
		logger:          logger,
		moduleDefaulter: webhook.NewModuleDefaulter(logger),
	}
}

func (mcmd *ManagedClusterModuleDefaulter) SetupWebhookWithManager(mgr ctrl.Manager) error {
	// controller-runtime will set the path to `mutate-<group>-<version>-<resource> so we
	// need to make sure it is set correctly in the +kubebuilder annotation below.
	return ctrl.NewWebhookManagedBy(mgr).
		For(&v1beta1.ManagedClusterModule{}).
		WithDefaulter(mcmd).
		Complete()
}

//+kubebuilder:webhook:path=/mutate-hub-kmm-sigs-x-k8s-io-v1beta1-managedclustermodule,mutating=true,failurePolicy=fail,sideEffects=None,groups=hub.kmm.sigs.x-k8s.io,resources=managedclustermodules,verbs=create;update,versions=v1beta1,name=vmanageclustermodule.kb.io,admissionReviewVersions=v1

// Default implements webhook.Default so a webhook will be registered for the type
func (mcmd *ManagedClusterModuleDefaulter) Default(ctx context.Context, obj runtime.Object) error {

	mcm, ok := obj.(*v1beta1.ManagedClusterModule)
	if !ok {
		return fmt.Errorf("bad type for the object; expected %T, got %T", mcm, obj)
	}

	mcmd.logger.Info("Mutating ManagedClusterModule creation", "name", mcm.Name, "namespace", mcm.Namespace)

	webhook.SetDefaultContainerImageTagIfNeeded(&mcm.Spec.ModuleSpec)
	webhook.SetDefaultKernelMappingTagsIfNeeded(&mcm.Spec.ModuleSpec)

	return nil
}
