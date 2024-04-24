# Preflight validation for Modules

Before executing upgrade on the cluster with applied KMM Modules, admin needs to verify that installed kernel modules
(via KMM) will be able to be installed on the nodes after the cluster upgrade and possible kernel upgrade. 
Preflight will try to validate every `Module` loaded in the cluster, in parallel (it does not wait for validation of one
`Module` to complete, before starting validation of another `Module`).

## Validation kick-off

Preflight validation is triggered by creating a `PreflightValidation` resource in the cluster. This Spec contains two
fields:

#### `kernelVersion`

The version of the kernel that the cluster will be upgraded to.  
This field is required.

#### `pushBuiltImage`

If true, then the images created during the Build and Sign validation will be pushed to their repositories.  
Default value: `false`.

## Validation lifecycle

Preflight validation will try to validate every module loaded in the cluster. Preflight will stop running validation on
a `Module`, once its validation is successful.
In case module validation has failed, admin can change the module definitions, and Preflight will try to validate the
module again in the next loop.
If admin want to run Preflight validation for additional kernel, then another `PreflightValidation` resource should be
created.
Once all the modules have been validated, it is recommended to delete the `PreflightValidation` resource.

## Validation status

A `PreflightValidation` resource will report that status and progress of each module in the cluster that it tries / has
tried to validate in its `.status.modules` list.  
Elements of that list contain the following fields:

#### `lastTransitionTime`

The last time when the `Module` status transitioned from one status to another.  
This should be when the underlying status changed.
If that is not known, then using the time when the API field changed is acceptable.

#### `name`

The name of the `Module` resource.

#### `namespace`

The namespace of the `Module` resource.

#### `statusReason`

A string describing the status source.

#### `verificationStage`

The current stage of the verification process, either:

- `image` (image existence verification), or;
- `build` (build process verification), or;
- `sign` (sign process verification), or;

#### `verificationStatus`

The status of the `Module` verification, either:

- `true` (verified), or;
- `false` (verification failed), or;
- `error` (error during the verification process), or;
- `unknown` (verification has not started yet).

## Preflight validation stages per Module

On every KMM Module present in the cluster, preflight will run the following validations:

1. [Image validation](#Image-validation-stage)
2. [Build validation](#Build-validation-stage)
3. [Sign validation](#Sign-validation-stage)

### Image validation stage

Image validation is always the first stage of the preflight validation that is being executed.
In case image validation is successful, no other validations will be run on that specific module.
Image validation consists of 2 stages:

1. image existence and accessibility. The code tries to access the image defined for the upgraded kernel in the module,
   and get its manifests.
2. verify the presence of the kernel module defined in the `Module` in the correct path for future `modprobe` execution.
   If this validation is successful, it probably means that the kernel module was compiled with the correct linux
   headers.
   The correct path is `<DirName>/lib/modules/<UpgradedKernel>/`.

### Build validation stage

Build validation is executed only in case image validation has failed, and there is a `build` section in the `Module`
that is relevant for the upgraded kernel.
Build validation will try to run build pod and validate that it finishes successfully.
If the `PushBuiltImage` flag is defined in the `PreflightValidation` CR, it will also try to push the resulting image
into its repo.
The resulting image name is taken from the definition of the `containerImage` field of the `Module` CR.

!!! note
    If the `sign` section is defined for the upgraded kernel, then the resulting image will not be `containerImage`
    field of the `Module` CR, but a temporary image name, since the resulting image should be the product of Sign flow.

### Sign validation stage

Sign validation is executed only in case image validation has failed, there is a `sign` section in the `Module` that is
relevant for the upgrade kernel, and build validation finished successfully in case there was a `build` section in the
`Module` relevant for the upgraded kernel.
Sign validation will try to run the sign pod and validate that it finishes successfully.
In case the `PushBuiltImage` flag is defined in the `PreflightValidation` CR, it will also try to push the resulting
image to its registry.
The result image is always the image defined in the `ContainerImage` field of the `Module`.
The input image is either the output of the Build stage, or an image defined in the `UnsignedImage` field.

!!! note
    In case a `build` section exists, the `sign` section input image is the `build` section's output image.
    Therefore, in order for the input image to be available for the `sign` section, the `PushBuiltImage` flag must be
    defined in the `PreflightValidation` CR.

## Example CR
Below is an example of the `PreflightValidation` resource in the YAML format.
In the example, we want to verify all the currently present modules against the upcoming `5.8.18-101.fc31.x86_64`
kernel, and push the resulting images of Build/Sign into the defined repositories.
```yaml
apiVersion: kmm.sigs.x-k8s.io/v1beta2
kind: PreflightValidation
metadata:
  name: preflight
spec:
  kernelVersion: 5.8.18-101.fc31.x86_64
  pushBuiltImage: true
```
