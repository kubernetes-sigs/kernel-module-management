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

const (
	// BuildImage means that image needs to be built
	BuildImage BuildOrSignAction = "BuildImage"

	// SignImage means that image needs to be built
	SignImage BuildOrSignAction = "SignImage"

	// ImageBuildFailed means that image does not exists and the Build failed
	ImageBuildFailed ImageState = "BuildFailed"

	// ImageBuildSucceeded means that image has been built and pushed succesfully
	ImageBuildSucceeded ImageState = "BuildSucceeded"

	// ImageSignFailed means that image does not exists and the Sign failed
	ImageSignFailed ImageState = "SignFailed"

	// ImageSignSucceeded means that image has been signed and pushed succesfully
	ImageSignSucceeded ImageState = "SignSucceeded"
)

// ModuleImageSpec describes the image whose state needs to be queried
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
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// ModuleBuildSignConfig keeps the request for images' build/sign for a KMM Module.
// +kubebuilder:resource:path=modulebuildsignconfigs,scope=Namespaced,shortName=mbsc
// +operator-sdk:csv:customresourcedefinitions:displayName="Module Build Sign Config"
type ModuleBuildSignConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ModuleBuildSignConfigSpec `json:"spec,omitempty"`
	Status ModuleImagesConfigStatus  `json:"status,omitempty"`
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
