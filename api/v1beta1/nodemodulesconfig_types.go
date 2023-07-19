/*


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

type ModuleConfig struct {
	ContainerImage string `json:"containerImage"`
	//+optional
	InTreeModuleToRemove string       `json:"inTreeModuleToRemove,omitempty"`
	Modprobe             ModprobeSpec `json:"modprobe"`
	KmodLoaded           bool         `json:"kmodLoaded"`
}

type NodeModuleSpec struct {
	Name      string       `json:"name"`
	Namespace string       `json:"namespace"`
	Config    ModuleConfig `json:"config"`
}

// NodeModulesConfigSpec describes the desired state of modules on the node
// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status
// +kubebuilder:validation:Required
type NodeModulesConfigSpec struct {
	// Modules list the spec of all the modules that need to be executed
	// on the node
	Modules []NodeModuleSpec `json:"modules,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

type NodeModuleStatus struct {
	Name       string `json:"name"`
	Namespace  string `json:"namespace"`
	KmodLoaded bool   `json:"kmodLoaded"`
}

// NodeModuleConfigStatus is the most recently observed status of the KMM modules on node.
// It is populated by the system and is read-only.
// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status
type NodeModulesConfigStatus struct {
	// Modules contain observations about each Module's node state status
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +optional
	Modules []NodeModuleStatus `json:"modules,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// NodeModulesConfig keeps spec and state of the KMM modules on a node.
// +kubebuilder:resource:path=nodemodulesconfig,scope=Cluster
// +kubebuilder:resource:path=nodemodulesconfig,scope=Cluster,shortName=nmc
// +operator-sdk:csv:customresourcedefinitions:displayName="Node Modules Config"
type NodeModulesConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NodeModulesConfigSpec   `json:"spec,omitempty"`
	Status NodeModulesConfigStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// NodeModulesConfigList is a list of NodeModulesConfig objects.
type NodeModulesConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	// List of NodeModulesConfig. More info:
	// https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md
	Items []NodeModulesConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NodeModulesConfig{}, &NodeModulesConfigList{})
}
