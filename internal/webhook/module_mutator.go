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

package webhook

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
)

const defaultTag = "latest"

type ModuleDefaulter struct {
	logger logr.Logger
}

func NewModuleDefaulter(logger logr.Logger) *ModuleDefaulter {
	return &ModuleDefaulter{logger: logger}
}

func (md *ModuleDefaulter) SetupWebhookWithManager(mgr ctrl.Manager) error {
	// controller-runtime will set the path to `mutate-<group>-<version>-<resource> so we
	// need to make sure it is set correctly in the +kubebuilder annotation below.
	return ctrl.NewWebhookManagedBy(mgr).
		For(&kmmv1beta1.Module{}).
		WithDefaulter(md).
		Complete()
}

//+kubebuilder:webhook:path=/mutate-kmm-sigs-x-k8s-io-v1beta1-module,mutating=true,failurePolicy=fail,sideEffects=None,groups=kmm.sigs.x-k8s.io,resources=modules,verbs=create;update,versions=v1beta1,name=vmodule.kb.io,admissionReviewVersions=v1

// Default implements webhook.Default so a webhook will be registered for the type
func (md *ModuleDefaulter) Default(ctx context.Context, obj runtime.Object) error {

	mod, ok := obj.(*kmmv1beta1.Module)
	if !ok {
		return fmt.Errorf("bad type for the object; expected %T, got %T", mod, obj)
	}

	md.logger.Info("Mutating Module creation", "name", mod.Name, "namespace", mod.Namespace)

	SetDefaultContainerImageTagIfNeeded(&mod.Spec)
	SetDefaultKernelMappingTagsIfNeeded(&mod.Spec)

	return nil
}

func SetDefaultContainerImageTagIfNeeded(modSpec *kmmv1beta1.ModuleSpec) {

	setDefaultTagIfNeeded(&modSpec.ModuleLoader.Container.ContainerImage)
}

func SetDefaultKernelMappingTagsIfNeeded(modSpec *kmmv1beta1.ModuleSpec) {

	for i := range modSpec.ModuleLoader.Container.KernelMappings {
		setDefaultTagIfNeeded(&modSpec.ModuleLoader.Container.KernelMappings[i].ContainerImage)
	}
}

func setDefaultTagIfNeeded(image *string) {

	if *image == "" {
		return
	}

	if !strings.Contains(*image, ":") && !strings.Contains(*image, "@") {
		*image = fmt.Sprintf("%s:%s", *image, defaultTag)
	}
}
