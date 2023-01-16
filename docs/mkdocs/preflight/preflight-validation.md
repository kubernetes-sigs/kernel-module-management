# Preflight validition of the cluster with installed KMM Module

Before executing upgrade on the cluster with applied KMM Modules, admin needs to verify that installed kernel modules (via KMM)
will be able to be installed on the nodes after the cluster upgrade and possible kernel upgrade. 
Preflight will try to validate every Module loaded in the cluster, in parallel (it does not wait for validation of one Module to complete, before starting
validation of another Module).

### Validation kick-off
Preflight validation is triggered by uploading PreflightValidation CR into the cluster. This CR's Spec contains 2 fields:
```
type PreflightValidationSpec struct {
        // KernelVersion describes the kernel image that all Modules need to be checked against.
        // +kubebuilder:validation:Required
        KernelVersion string `json:"kernelVersion"`

        // Boolean flag that determines whether images build during preflight must also
        // be pushed to a defined repository
        // +optional
        PushBuiltImage bool `json:"pushBuiltImage"`
}
```

1. KernelVersion - the version of the kernel that the cluster will be upgraded to. Manadatory field
2. PushBuiltImage - if true, then the images created during the Build and Sign validation will be pushed to their repositories (false by default).

### Validation lifecycle
Preflight validation will try to validate every module loaded in the cluster. Preflight will stop running validation on a module, once its validation
has been successfull. In case module validation has failed, admin can change the module definitions , and Preflight will try to validate the module again in the next loop.
If admin want to run Preflight validation for additional kernel, then another Preflight CR should be created.
Once all the modules has been validated, it is recommended to delete Preflight CR

### Validation status
Preflight will report that status and progress of each module in the cluster that it tries/tried to validate.

```
type CRStatus struct {
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
```


Per each module you will see the following fields:
1. Verification status - true or false, validated or not
2. Status reason - verbal explanation reagrding the status. Why it is not validated, etc'
3. Verification stage - describe the validation stage being executed (Image, Build, Sign)
4. Last transition time - the tinme of the last update to the status



### Preflight validation stages per Module

On every KMM Module present in the cluster, preflight will run the following validations:
1. [Image validation](#Image-validation-stage)
2. [Build validation](#Build-validation-stage)
3. [Sign validation](#Sign-validation-stage)

### Image validation stage

Image validation is always the first stage of the preflight validation that is being executed. In case image validation
is successful, no other validations will be run on that specific module.
Image validation consists of 2 stages:
1. image existences and accessability. The code tries to access the image defined for the upgraded kernel in the module, and get its manifests
2  verify the presence of the kernel module defined in the Module in the correct path for future modprobe execution. If this validation is successfull
   it probably means that the kernel module was compiled with the correct linux headers. The correct path is:
   <DirName>/lib/modules/<UpgradedKernel>/  

### Build validation stage

Build validation is executed only in case image validation has failed, and there is a Build section in the Module that is relevent for the
upgraded kernel. Build validation will try to run build job and validate that it finishes successfully. In case "Push" flag is defined in the Preflight CR,
it will also try to push the resulting image into its repo. The resulting image name is taken from the definition of the ContainerImage field of the Module CR
Note: if Sign section is defined for the upgraded kernel, then the resulting image will not be ContainerImage field of the Module CR, but a temporary image name,
since the resulting image should be the product of Sign flow

### Sign validation stage
Sign validation is executed only in case image validation has failed, there is a Sign section in the Module that is relevent for the upgrade kernel, and build validation finished successfully in case there was a Build section in the Module relevent for the upgraded kernel. Sign validation will try to run sign job and validate that it finishes successfully. In case "Push" flag is defined in the Preflight CR, it will also try to push the resulting image into its repo.The result image is always the image defined in the ContainerImage field of the Module. The input image is either the output of the Build stage, or an image defined in the UnsignedImage field.
Note: in case Build section exists, Sign section input image is the Build's section output image. Therefore, in order for the input image to be available for the Sign section, the "Push" flag must be defined in the preflight CR


### Example CR
Below is an example of the Preflight Validation CR in a yaml format. In the example, we want to verify all the currently present modules vesrsus the 5.8.18-101.fc31.x86_64
kernel, and push the resulting images of Build/Sign into the defined repositores
```
apiVersion: kmm.sigs.x-k8s.io/v1beta1
kind: PreflightValidation
metadata:
  name: preflight
spec:
  kernelVersion: 5.8.18-101.fc31.x86_64
  pushBuiltImage: true
```
