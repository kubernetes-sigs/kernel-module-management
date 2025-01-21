/*
Copyright 2025.

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

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// ModuleBuildSignConfig keeps the request for images' build/sign for a KMM Module.
// +kubebuilder:resource:path=modulebuildsignconfig,scope=Namespaced,shortName=mbsc
// +operator-sdk:csv:customresourcedefinitions:displayName="Module Build Sign Config"
type ModuleBuildSignConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ModuleImagesConfigSpec   `json:"spec,omitempty"`
	Status ModuleImagesConfigStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ModuleBuildSignConfigList is a list of ModuleBuildSignConfig objects.
type ModuleBuildSignConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	// List of ModuleBuildSignConfig. More info:
	// https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md
	Items []ModuleBuildSignConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ModuleBuildSignConfig{}, &ModuleBuildSignConfigList{})
}
