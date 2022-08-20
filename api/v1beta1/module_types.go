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

// BuildArg represents a build argument used when building a container image.
type BuildArg struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type PullOptions struct {

	// +optional
	// If Insecure is true, images can be pulled from an insecure (plain HTTP) registry.
	Insecure bool `json:"insecure,omitempty"`

	// +optional
	// If InsecureSkipTLSVerify, the operator will accept any certificate provided by the registry.
	InsecureSkipTLSVerify bool `json:"insecureSkipTLSVerify,omitempty"`
}

type PushOptions struct {

	// +optional
	// If Insecure is true, built images can be pushed to an insecure (plain HTTP) registry.
	Insecure bool `json:"insecure,omitempty"`

	// +optional
	// If InsecureSkipTLSVerify, the operator will accept any certificate provided by the registry.
	InsecureSkipTLSVerify bool `json:"insecureSkipTLSVerify,omitempty"`
}

type Build struct {
	// +optional
	// BuildArgs is an array of build variables that are provided to the image building backend.
	BuildArgs []BuildArg `json:"buildArgs"`

	Dockerfile string `json:"dockerfile"`

	// +optional
	// Pull contains settings determining how to check if the DriverContainer image already exists.
	Pull PullOptions `json:"pull"`

	// +optional
	// Push contains settings determining how to push a built DriverContainer image.
	Push PushOptions `json:"push"`

	// +optional
	// Secrets is an optional list of secrets to be made available to the build system.
	// Those secrets should be used for private resources such as a private Github repo.
	// For container registries auth use module.spec.imagePullSecret instead.
	Secrets []v1.LocalObjectReference `json:"secrets"`
}

// KernelMapping pairs kernel versions with a DriverContainer image.
// Kernel versions can be matched literally or using a regular expression.
type KernelMapping struct {

	// +optional
	// Build enables in-cluster builds for this mapping and allows overriding the Module's build settings.
	Build *Build `json:"build"`

	// ContainerImage is the name of the DriverContainer image that should be used to deploy the module.
	ContainerImage string `json:"containerImage"`

	// +optional
	// Literal defines a literal target kernel version to be matched exactly against node kernels.
	Literal string `json:"literal"`

	// +optional
	// Regexp is a regular expression to be match against node kernels.
	Regexp string `json:"regexp"`
}

type ModprobeArgs struct {
	// Load is an optional list of arguments to be used when loading the kernel module.
	// +kubebuilder:validation:MinItems=1
	Load []string `json:"load,omitempty"`

	// Unload is an optional list of arguments to be used when unloading the kernel module.
	// +kubebuilder:validation:MinItems=1
	Unload []string `json:"unload,omitempty"`
}

type ModprobeSpec struct {
	// ModuleName is the name of the Module to be loaded.
	ModuleName string `json:"moduleName"`

	// Parameters is an optional list of kernel module parameters to be provided to modprobe.
	// They should be in the form of key=value and will be separated by spaces in the modprobe command.
	// The resulting loading command will be: `modprobe module_name ${Parameters}`.
	Parameters []string `json:"parameters,omitempty"`

	// DirName is the root directory for modules.
	// It adds `-d ${DirName}` to the modprobe command-line.
	// +kubebuilder:default=/opt
	DirName string `json:"dirName,omitempty"`

	// Args is an optional list of arguments to be passed to modprobe before the name of the kernel module.
	// The resulting commands will be: `modprobe ${Args} module_name`.
	// +optional
	Args *ModprobeArgs `json:"args,omitempty"`

	// If RawArgs are specified, they are passed straight to the modprobe binary; all other properties in this
	// object are ignored.
	// The resulting commands will be: `modprobe ${RawArgs}`.
	// +optional
	RawArgs *ModprobeArgs `json:"rawArgs,omitempty"`
}

type DriverContainerContainerSpec struct {
	// Build contains build instructions.
	// +optional
	Build *Build `json:"build,omitempty"`

	// ContainerImage is a top-level field
	// +optional
	ContainerImage string `json:"containerImage,omitempty"`

	// Image pull policy.
	// One of Always, Never, IfNotPresent.
	// Defaults to Always if :latest tag is specified, or IfNotPresent otherwise.
	// Cannot be updated.
	// More info: https://kubernetes.io/docs/concepts/containers/images#updating-images
	// +optional
	ImagePullPolicy v1.PullPolicy `json:"imagePullPolicy,omitempty" protobuf:"bytes,14,opt,name=imagePullPolicy,casttype=PullPolicy"`

	// KernelMappings is a list of kernel mappings.
	// When a node's labels match Selector, then the OOT Operator will look for the first mapping that matches its
	// kernel version, and use the corresponding container image to run the DriverContainer.
	// +kubebuilder:validation:MinItems=1
	KernelMappings []KernelMapping `json:"kernelMappings"`

	// Modprobe is a set of properties to customize which module modprobe loads and with which properties.
	Modprobe ModprobeSpec `json:"modprobe"`
}

type DriverContainerSpec struct {
	// Container holds the properties for the driver container that runs modprobe.
	Container DriverContainerContainerSpec `json:"container"`

	// +optional
	// ServiceAccountName is the name of the ServiceAccount to use to run this pod.
	// More info: https://kubernetes.io/docs/tasks/configure-pod-container/configure-service-account/
	ServiceAccountName string `json:"serviceAccountName,omitempty"`
}

type DevicePluginContainerSpec struct {
	// Entrypoint array. Not executed within a shell.
	// The container image's ENTRYPOINT is used if this is not provided.
	// Variable references $(VAR_NAME) are expanded using the container's environment. If a variable
	// cannot be resolved, the reference in the input string will be unchanged. Double $$ are reduced
	// to a single $, which allows for escaping the $(VAR_NAME) syntax: i.e. "$$(VAR_NAME)" will
	// produce the string literal "$(VAR_NAME)". Escaped references will never be expanded, regardless
	// of whether the variable exists or not. Cannot be updated.
	// More info: https://kubernetes.io/docs/tasks/inject-data-application/define-command-argument-container/#running-a-command-in-a-shell
	// +optional
	Command []string `json:"command,omitempty" protobuf:"bytes,3,rep,name=command"`

	// Arguments to the entrypoint.
	// The container image's CMD is used if this is not provided.
	// Variable references $(VAR_NAME) are expanded using the container's environment. If a variable
	// cannot be resolved, the reference in the input string will be unchanged. Double $$ are reduced
	// to a single $, which allows for escaping the $(VAR_NAME) syntax: i.e. "$$(VAR_NAME)" will
	// produce the string literal "$(VAR_NAME)". Escaped references will never be expanded, regardless
	// of whether the variable exists or not. Cannot be updated.
	// More info: https://kubernetes.io/docs/tasks/inject-data-application/define-command-argument-container/#running-a-command-in-a-shell
	// +optional
	Args []string `json:"args,omitempty" protobuf:"bytes,4,rep,name=args"`

	// List of environment variables to set in the container.
	// Cannot be updated.
	// +optional
	// +patchMergeKey=name
	// +patchStrategy=merge
	Env []v1.EnvVar `json:"env,omitempty" patchStrategy:"merge" patchMergeKey:"name" protobuf:"bytes,7,rep,name=env"`

	// Image is the name of the container image that the device plugin container will run.
	Image string `json:"image"`

	// Image pull policy.
	// One of Always, Never, IfNotPresent.
	// Defaults to Always if :latest tag is specified, or IfNotPresent otherwise.
	// Cannot be updated.
	// More info: https://kubernetes.io/docs/concepts/containers/images#updating-images
	// +optional
	ImagePullPolicy v1.PullPolicy `json:"imagePullPolicy,omitempty" protobuf:"bytes,14,opt,name=imagePullPolicy,casttype=PullPolicy"`

	// Compute Resources required by this container.
	// Cannot be updated.
	// More info: https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/
	// +optional
	Resources v1.ResourceRequirements `json:"resources,omitempty" protobuf:"bytes,8,opt,name=resources"`

	// VolumeMounts is a list of volume mounts that are appended to the default ones.
	// +optional
	VolumeMounts []v1.VolumeMount `json:"volumeMounts,omitempty"`
}

type DevicePluginSpec struct {
	Container DevicePluginContainerSpec `json:"container"`

	// +optional
	// ServiceAccountName is the name of the ServiceAccount to use to run this pod.
	// More info: https://kubernetes.io/docs/tasks/configure-pod-container/configure-service-account/
	ServiceAccountName string `json:"serviceAccountName,omitempty"`

	Volumes []v1.Volume `json:"volumes,omitempty"`
}

// ModuleSpec describes how the OOT operator should deploy a Module on those nodes that need it.
type ModuleSpec struct {
	// DevicePlugin allows overriding some properties of the container that deploys the device plugin on the node.
	// Name is ignored and is set automatically by the OOT Operator.
	// +optional
	DevicePlugin *DevicePluginSpec `json:"devicePlugin"`

	// DriverContainer allows overriding some properties of the container that deploys the driver on the node.
	// Name and image are ignored and are set automatically by the OOT Operator.
	DriverContainer DriverContainerSpec `json:"driverContainer"`

	// ImageRepoSecret is an optional secret that is used to pull both the driver container and the device plugin, and
	// to push the resulting image from the driver container build, if enabled.
	// +optional
	ImageRepoSecret *v1.LocalObjectReference `json:"imageRepoSecret,omitempty"`

	// Selector describes on which nodes the Module should be loaded and optionally built.
	Selector map[string]string `json:"selector"`
}

// DaemonSetStatus contains the status for a daemonset deployed during
// reconciliation loop
type DaemonSetStatus struct {
	// number of nodes that are targeted by the module selector
	NodesMatchingSelectorNumber int32 `json:"nodesMatchingSelectorNumber"`
	// number of the pods that should be deployed for daemonset
	DesiredNumber int32 `json:"desiredNumber"`
	// number of the actually deployed and running pods
	AvailableNumber int32 `json:"availableNumber"`
}

// ModuleStatus defines the observed state of Module.
type ModuleStatus struct {
	// DevicePlugin contains the status of the Device Plugin daemonset
	// if it was deployed during reconciliation
	DevicePlugin DaemonSetStatus `json:"devicePlugin,omitempty"`
	// DriverContainer contains the status of the DriverContainer daemonset
	DriverContainer DaemonSetStatus `json:"driverContainer"`
}

//+kubebuilder:object:root=true
//+kubebuilder:resource:scope=Namespaced
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
