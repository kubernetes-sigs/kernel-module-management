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
	cfg "sigs.k8s.io/controller-runtime/pkg/config/v1alpha1"
)

type Feature struct {
	Enabled bool `json:"enabled"`
}

type ModuleReconcilerFeature struct {
	Feature `json:",inline"`

	// If true, allows KMM to build images.
	AllowBuilds bool `json:"allowBuilds"`

	// If true, allows KMM to start kmod signing jobs.
	AllowSigning bool `json:"allowSigning"`
}

type Reconcilers struct {
	// ManagedClusterModule contains settings for the ManagedClusterModule reconciler.
	ManagedClusterModule Feature `json:"managedClusterModule"`

	// Module contains settings for the Module reconciler.
	Module ModuleReconcilerFeature `json:"module"`

	// NodeKernelClusterClaim contains settings for the NodeKernelClusterClaim reconciler.
	NodeKernelClusterClaim Feature `json:"nodeKernelClusterClaim"`

	// 	NodeKernel contains settings for the NodeKernel reconciler.
	NodeKernel Feature `json:"nodeKernel"`

	// 	PodNodeModule contains settings for the PodNodeModule reconciler.
	PodNodeModule Feature `json:"podNodeModule"`

	// 	PreflightValidation contains settings for the PreflightValidation reconciler.
	PreflightValidation Feature `json:"preflightValidation"`
}

type ConfigSpec struct {
	// Reconcilers contains settings related to the reconcilers.
	Reconcilers Reconcilers `json:"reconcilers"`
}

type ConfigStatus struct{}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// Config is the Schema for the configs API
type Config struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ConfigSpec   `json:"spec,omitempty"`
	Status ConfigStatus `json:"status,omitempty"`

	// ControllerManagerConfigurationSpec returns the configurations for controllers
	cfg.ControllerManagerConfigurationSpec `json:",inline"`
}

//+kubebuilder:object:root=true

func init() {
	SchemeBuilder.Register(&Config{})
}
