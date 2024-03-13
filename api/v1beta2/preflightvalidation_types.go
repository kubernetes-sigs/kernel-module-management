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

package v1beta2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	VerificationTrue          string = "True"
	VerificationFalse         string = "False"
	VerificationStageImage    string = "Image"
	VerificationStageBuild    string = "Build"
	VerificationStageSign     string = "Sign"
	VerificationStageRequeued string = "Requeued"
	VerificationStageDone     string = "Done"
)

// PreflightValidationSpec describes the desired state of the resource, such as the kernel version
// that Module CRs need to be verified against as well as the debug configuration of the logs
// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status
// +kubebuilder:validation:Required
type PreflightValidationSpec struct {
	// KernelVersion describes the kernel image that all Modules need to be checked against.
	// +kubebuilder:validation:Required
	KernelVersion string `json:"kernelVersion"`

	// Boolean flag that determines whether images build during preflight must also
	// be pushed to a defined repository
	// +optional
	PushBuiltImage bool `json:"pushBuiltImage"`
}

type CRBaseStatus struct {
	// Status of Module CR verification: true (verified), false (verification failed),
	// error (error during verification process), unknown (verification has not started yet)
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=True;False
	VerificationStatus string `json:"verificationStatus"`

	// StatusReason contains a string describing the status source.
	// +optional
	StatusReason string `json:"statusReason,omitempty"`

	// Current stage of the verification process:
	// image (image existence verification), build(build process verification)
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=Image;Build;Sign;Requeued;Done
	VerificationStage string `json:"verificationStage"`

	// LastTransitionTime is the last time the CR status transitioned from one status to another.
	// This should be when the underlying status changed.  If that is not known, then using the time when the API field changed is acceptable.
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Format=date-time
	LastTransitionTime metav1.Time `json:"lastTransitionTime" protobuf:"bytes,4,opt,name=lastTransitionTime"`
}

type PreflightValidationModuleStatus struct {
	CRBaseStatus `json:",inline"`

	// Name is the name of the Module resource.
	Name string `json:"name"`

	// Namespace is the namespace of the Module resource.
	Namespace string `json:"namespace"`
}

// PreflightValidationStatus is the most recently observed status of the PreflightValidation.
// It is populated by the system and is read-only.
// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status
type PreflightValidationStatus struct {
	// Modules contain observations about each Module's preflight upgradability validation
	// +listType=map
	// +listMapKey=namespace
	// +listMapKey=name
	// +optional
	Modules []PreflightValidationModuleStatus `json:"modules,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:storageversion

// PreflightValidation initiates a preflight validations for all Modules on the current Kubernetes cluster.
// +kubebuilder:resource:path=preflightvalidations,scope=Cluster,shortName=pfv
// +operator-sdk:csv:customresourcedefinitions:displayName="Preflight Validation"
type PreflightValidation struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// +kubebuilder:validation:Required

	Spec   PreflightValidationSpec   `json:"spec,omitempty"`
	Status PreflightValidationStatus `json:"status,omitempty"`
}

func (p *PreflightValidation) Hub() {}

// +kubebuilder:object:root=true

// PreflightValidationList is a list of PreflightValidation objects.
type PreflightValidationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	// List of PreflightValidation. More info:
	// https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md
	Items []PreflightValidation `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PreflightValidation{}, &PreflightValidationList{})
}
