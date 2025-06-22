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
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type BuildOrSignAction string
type BuildOrSignStatus string

const (
	// BuildImage means that image needs to be built
	BuildImage BuildOrSignAction = "BuildImage"

	// SignImage means that image needs to be built
	SignImage BuildOrSignAction = "SignImage"

	// ActionSuccess means that action (sign or build, depending on the action field) has succeeded
	ActionSuccess BuildOrSignStatus = "Success"

	// ActionFailure means that action (sign or build, depending on the action field) has failed
	ActionFailure BuildOrSignStatus = "Failure"
)

// ModuleBuildSignSpec describes the image whose state needs to be queried
type ModuleBuildSignSpec struct {
	ModuleImageSpec `json:",inline"`

	// +kubebuilder:validation:Enum=BuildImage;SignImage
	Action BuildOrSignAction `json:"action"`
}

// ModuleBuildSignConfigSpec describes the images that need to be built/signed
// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status
// +kubebuilder:validation:Required
type ModuleBuildSignConfigSpec struct {
	Images []ModuleBuildSignSpec `json:"images"`

	// ImageRepoSecret contains pull secret for the image's repo, if needed
	// +optional
	ImageRepoSecret *v1.LocalObjectReference `json:"imageRepoSecret,omitempty"`

	// Boolean flag that determines whether images built must also
	// be pushed to a defined repository
	// +optional
	PushBuiltImage bool `json:"pushBuiltImage"`
}

// BuildSignImageState contains the status of the image that was requested to be built/signed
type BuildSignImageState struct {
	Image string `json:"image"`

	// +kubebuilder:validation:Enum=Success;Failure
	Status BuildOrSignStatus `json:"status"`

	// +kubebuilder:validation:Enum=BuildImage;SignImage
	Action BuildOrSignAction `json:"action"`
}

// ModuleBuildSignConfigStatus describes the status of the images that needed to be built/signed
// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status
type ModuleBuildSignConfigStatus struct {
	Images []BuildSignImageState `json:"images"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// ModuleBuildSignConfig keeps the request for images' build/sign for a KMM Module.
// +kubebuilder:resource:path=modulebuildsignconfigs,scope=Namespaced,shortName=mbsc
// +operator-sdk:csv:customresourcedefinitions:displayName="Module Build Sign Config"
type ModuleBuildSignConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ModuleBuildSignConfigSpec   `json:"spec,omitempty"`
	Status ModuleBuildSignConfigStatus `json:"status,omitempty"`
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
