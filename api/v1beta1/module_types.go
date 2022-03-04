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
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type KernelMapping struct {
	ContainerImage string `json:"containerImage"`

	// +optional
	Literal string `json:"literal"`

	// +optional
	Regexp string `json:"regexp"`
}

// ModuleSpec defines the desired state of Module
type ModuleSpec struct {
	// +optional
	DevicePlugin v1.Container `json:"devicePlugin"`

	// +optional
	DriverContainer v1.Container `json:"driverContainer"`

	KernelMappings []KernelMapping `json:"kernelMappings"`

	// Selector is a first-level filter to check if the module applies to a specific node.
	// In the case of a hardware accelerator, Selector could contain a PCI device label set by Node Feature Discovery.
	Selector map[string]string `json:"selector"`
}

// ModuleStatus defines the observed state of Module
type ModuleStatus struct {
	Conditions []metav1.Condition `json:"conditions"`

	// +optional
	KernelDaemonSetsMap map[string]string `json:"kernelDaemonSetsMap"`
}

//+kubebuilder:object:root=true
//+kubebuilder:resource:scope=Cluster
//+kubebuilder:subresource:status

// Module is the Schema for the modules API
type Module struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ModuleSpec   `json:"spec,omitempty"`
	Status ModuleStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ModuleList contains a list of Module
type ModuleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Module `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Module{}, &ModuleList{})
}
