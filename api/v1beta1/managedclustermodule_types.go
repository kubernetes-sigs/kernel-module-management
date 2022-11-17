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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ManagedClusterModuleSpec defines the desired state of ManagedClusterModule
type ManagedClusterModuleSpec struct {
	// ModuleSpec describes how the KMM operator should deploy a Module on those nodes that need it.
	ModuleSpec ModuleSpec `json:"moduleSpec,omitempty"`

	// Namespace describes the Spoke namespace, in which the ModuleSpec should be applied.
	Namespace string `json:"namespace,omitempty"`

	// Selector describes on which managed clusters the ModuleSpec should be applied.
	Selector map[string]string `json:"selector"`
}

//+kubebuilder:object:root=true
//+kubebuilder:resource:path=managedclustermodules,scope=Cluster
//+kubebuilder:subresource:status

// ManagedClusterModule is the Schema for the managedclustermodules API
type ManagedClusterModule struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec ManagedClusterModuleSpec `json:"spec,omitempty"`
}

//+kubebuilder:object:root=true

// ManagedClusterModuleList contains a list of ManagedClusterModule
type ManagedClusterModuleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ManagedClusterModule `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ManagedClusterModule{}, &ManagedClusterModuleList{})
}
