//go:build !ignore_autogenerated

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

// Code generated by controller-gen. DO NOT EDIT.

package v1beta2

import (
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *CRBaseStatus) DeepCopyInto(out *CRBaseStatus) {
	*out = *in
	in.LastTransitionTime.DeepCopyInto(&out.LastTransitionTime)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new CRBaseStatus.
func (in *CRBaseStatus) DeepCopy() *CRBaseStatus {
	if in == nil {
		return nil
	}
	out := new(CRBaseStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PreflightValidation) DeepCopyInto(out *PreflightValidation) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	out.Spec = in.Spec
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PreflightValidation.
func (in *PreflightValidation) DeepCopy() *PreflightValidation {
	if in == nil {
		return nil
	}
	out := new(PreflightValidation)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *PreflightValidation) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PreflightValidationList) DeepCopyInto(out *PreflightValidationList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]PreflightValidation, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PreflightValidationList.
func (in *PreflightValidationList) DeepCopy() *PreflightValidationList {
	if in == nil {
		return nil
	}
	out := new(PreflightValidationList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *PreflightValidationList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PreflightValidationModuleStatus) DeepCopyInto(out *PreflightValidationModuleStatus) {
	*out = *in
	in.CRBaseStatus.DeepCopyInto(&out.CRBaseStatus)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PreflightValidationModuleStatus.
func (in *PreflightValidationModuleStatus) DeepCopy() *PreflightValidationModuleStatus {
	if in == nil {
		return nil
	}
	out := new(PreflightValidationModuleStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PreflightValidationSpec) DeepCopyInto(out *PreflightValidationSpec) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PreflightValidationSpec.
func (in *PreflightValidationSpec) DeepCopy() *PreflightValidationSpec {
	if in == nil {
		return nil
	}
	out := new(PreflightValidationSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PreflightValidationStatus) DeepCopyInto(out *PreflightValidationStatus) {
	*out = *in
	if in.Modules != nil {
		in, out := &in.Modules, &out.Modules
		*out = make([]PreflightValidationModuleStatus, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PreflightValidationStatus.
func (in *PreflightValidationStatus) DeepCopy() *PreflightValidationStatus {
	if in == nil {
		return nil
	}
	out := new(PreflightValidationStatus)
	in.DeepCopyInto(out)
	return out
}
