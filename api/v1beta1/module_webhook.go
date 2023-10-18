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

package v1beta1

import (
	"errors"
	"fmt"
	"regexp"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// maxCombinedLength is the maximum combined length of Module name and namespace when the version field is set.
const maxCombinedLength = 40

// log is for logging in this package.
var modulelog = logf.Log.WithName("module-resource")

func (m *Module) SetupWebhookWithManager(mgr ctrl.Manager) error {
	// controller-runtime will set the path to `validate-<group>-<version>-<resource> so we
	// need to make sure it is set correctly in the +kubebuilder annotation below.
	return ctrl.NewWebhookManagedBy(mgr).
		For(m).
		Complete()
}

//+kubebuilder:webhook:path=/validate-kmm-sigs-x-k8s-io-v1beta1-module,mutating=false,failurePolicy=fail,sideEffects=None,groups=kmm.sigs.x-k8s.io,resources=modules,verbs=create;update,versions=v1beta1,name=vmodule.kb.io,admissionReviewVersions=v1

var _ webhook.Validator = &Module{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (m *Module) ValidateCreate() (admission.Warnings, error) {
	modulelog.Info("Validating Module creation", "name", m.Name, "namespace", m.Namespace)

	return m.validate()
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (m *Module) ValidateUpdate(old runtime.Object) (admission.Warnings, error) {
	modulelog.Info("Validating Module update", "name", m.Name, "namespace", m.Namespace)

	if old != nil {
		oldModule, ok := old.(*Module)
		if !ok {
			return nil, fmt.Errorf("old object %v is not of the expected type *Module", old)
		}

		if (oldModule.Spec.ModuleLoader.Container.Version == "" && m.Spec.ModuleLoader.Container.Version != "") ||
			(oldModule.Spec.ModuleLoader.Container.Version != "" && m.Spec.ModuleLoader.Container.Version == "") {
			return nil, errors.New("cannot update to or from an empty version; please delete the Module and create it again")
		}
	}

	return m.validate()
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (m *Module) ValidateDelete() (admission.Warnings, error) {
	return nil, nil
}

func (m *Module) validate() (admission.Warnings, error) {
	nameLength := len(m.Name + m.Namespace)

	if nameLength > maxCombinedLength {
		return nil, fmt.Errorf(
			"module name and namespace have a combined length of %d characters, which exceeds the maximum of %d when version is set",
			nameLength,
			maxCombinedLength,
		)
	}

	if err := m.validateKernelMapping(); err != nil {
		return nil, fmt.Errorf("failed to validate kernel mappings: %v", err)
	}

	return nil, m.validateModprobe()
}

func (m *Module) validateKernelMapping() error {
	container := m.Spec.ModuleLoader.Container

	for idx, km := range container.KernelMappings {
		if km.Regexp != "" && km.Literal != "" {
			return fmt.Errorf("regexp and literal are mutually exclusive properties at kernelMappings[%d]", idx)
		}

		if km.Regexp == "" && km.Literal == "" {
			return fmt.Errorf("regexp or literal must be set at kernelMappings[%d]", idx)
		}

		if _, err := regexp.Compile(km.Regexp); err != nil {
			return fmt.Errorf("invalid regexp at index %d: %v", idx, err)
		}

		if container.ContainerImage == "" && km.ContainerImage == "" {
			return fmt.Errorf("missing spec.moduleLoader.container.kernelMappings[%d].containerImage", idx)
		}
	}

	return nil
}

func (m *Module) validateModprobe() error {
	modprobe := m.Spec.ModuleLoader.Container.Modprobe
	moduleName := modprobe.ModuleName
	moduleNameDefined := moduleName != ""
	rawLoadArgsDefined := modprobe.RawArgs != nil && len(modprobe.RawArgs.Load) > 0
	rawUnloadArgsDefined := modprobe.RawArgs != nil && len(modprobe.RawArgs.Unload) > 0

	if moduleNameDefined {
		if rawLoadArgsDefined || rawUnloadArgsDefined {
			return errors.New("rawArgs cannot be set when moduleName is set")
		}
	} else if !rawLoadArgsDefined || !rawUnloadArgsDefined {
		return errors.New("load and unload rawArgs must be set when moduleName is unset")
	}

	if modprobe.ModulesLoadingOrder != nil {
		if len(modprobe.ModulesLoadingOrder) < 2 {
			return errors.New("if a loading order is defined, at least two values must be defined")
		}

		if !moduleNameDefined {
			return errors.New("if a loading order is defined, moduleName must be set")
		}

		if modprobe.ModulesLoadingOrder[0] != moduleName {
			return errors.New("if a loading order is defined, the first element must be moduleName")
		}

		s := sets.New[string]()

		for _, modName := range modprobe.ModulesLoadingOrder {
			if s.Has(modName) {
				return fmt.Errorf("%q: duplicate value in the loading order list", modName)
			}

			s.Insert(modName)
		}
	}

	return nil
}
