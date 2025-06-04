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

#### `name`

The name of the `Module` resource.

#### `namespace`

The namespace of the `Module` resource.

#### `CRBaseStatus.statusReason`

A string describing the status source.

#### `CRBaseStatus.verificationStage`

The current stage of the verification process, either:

- `Image` (image existence verification), or;
- `Done` (verification is done)

#### `CRBaseStatus.verificationStatus`

The status of the `Module` verification, either:

- `Success` (verified), or;
- `Failure` (verification failed), or;
- `InProgress` (verification is in-progress).

### Image validation stage

Image validation is always the first stage of the preflight validation that is being executed.
In case image validation is successful, no other validations will be run on that specific module.
The operator will check, using the container-runtime, the image existence and accessibility for the updaded kernel in the module.

If the image validation has failed, and there is a `build`/`sign` section in the `Module` that is relevant for the upgraded kernel,
the controller will try to build and/or sign the image.
If the `PushBuiltImage` flag is defined in the `PreflightValidation` CR, it will also try to push the resulting image
into its repo.
The resulting image name is taken from the definition of the `containerImage` field of the `Module` CR.

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
