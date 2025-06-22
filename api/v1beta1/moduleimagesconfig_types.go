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

type ImageState string

const (
	// ImageExists means that image exists in the specified repo
	ImageExists ImageState = "Exists"
	// ImageDoesNotExist means that image does not exist in the specified repo
	ImageDoesNotExist ImageState = "DoesNotExist"
	// ImageNeedsBuilding means that image does not exists, but has Build section and can be built
	ImageNeedsBuilding ImageState = "NeedsBuilding"
	// ImageNeedsSigning means that images needs signing, because it was pre-built, or in-cluster build succeeded
	ImageNeedsSigning ImageState = "NeedsSigning"
)

// ModuleImageSpec describes the image whose state needs to be queried
type ModuleImageSpec struct {
	// image
	Image string `json:"image"`

	// kernel version for which this image is targeted
	KernelVersion string `json:"kernelVersion"`

	// Build contains build instructions, in case image needs building
	// +optional
	Build *Build `json:"build,omitempty"`

	// Sign contains sign instructions, in case image needs signing
	// +optional
	Sign *Sign `json:"sign,omitempty"`

	// SkipWaitMissingImage signals to MIC to stop waiting for image to be present
	// in case Build andSign not define, and report the image as DoesNotExist
	SkipWaitMissingImage bool `json:"skipWaitMissingImage,omitempty"`

	// +optional
	// RegistryTLS set the TLS configs for accessing the registry of the image.
	RegistryTLS *TLSOptions `json:"registryTLS,omitempty"`
}

// ModuleImagesConfigSpec describes the images of the Module whose status needs to be verified
// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status
type ModuleImagesConfigSpec struct {
	Images []ModuleImageSpec `json:"images,omitempty"`

	// ImageRepoSecret contains pull secret for the image's repo, if needed
	// +optional
	ImageRepoSecret *v1.LocalObjectReference `json:"imageRepoSecret,omitempty"`

	// +kubebuilder:default=IfNotPresent
	// ImagePullPolicy defines the pull policy used for verifying the presence of the image
	//+optional
	ImagePullPolicy v1.PullPolicy `json:"imagePullPolicy"`

	// Boolean flag that determines whether images built must also
	// be pushed to a defined repository
	// +optional
	PushBuiltImage bool `json:"pushBuiltImage"`
}

type ModuleImageState struct {
	// image
	Image string `json:"image"`
	// status of the image
	// one of: Exists, notExists
	Status ImageState `json:"status"`
}

// ModuleImagesConfigStatus describes the status of the images that need to be verified (defined in the spec)
// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status
// +kubebuilder:validation:Required
type ModuleImagesConfigStatus struct {
	ImagesStates []ModuleImageState `json:"imagesStates"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// ModuleImagesConfig keeps the request for images' state for a KMM Module.
// +kubebuilder:resource:path=moduleimagesconfigs,scope=Namespaced,shortName=mic
// +operator-sdk:csv:customresourcedefinitions:displayName="Module Images Config"
type ModuleImagesConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ModuleImagesConfigSpec   `json:"spec,omitempty"`
	Status ModuleImagesConfigStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ModuleImagesConfigList is a list of ModuleImagesConfig objects.
type ModuleImagesConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	// List of ModuleImagesConfig. More info:
	// https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md
	Items []ModuleImagesConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ModuleImagesConfig{}, &ModuleImagesConfigList{})
}
