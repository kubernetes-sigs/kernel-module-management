# Deploying kernel modules

KMM watches `Node` and `Module` resources in the cluster to determine if a kernel module should be loaded on or unloaded
from a node.  
To be eligible for a `Module`, a `Node` must:

- have labels that match the `Module`'s `.spec.selector` field;
- run a kernel version matching one of the items in the `Module`'s `.spec.moduleLoader.container.kernelMappings`;
- if [ordered upgrade](ordered_upgrade.md) is configured in the `Module`, have a label that matches its
  `.spec.moduleLoader.container.version` field.

When KMM needs to reconcile nodes with the desired state as configured in the `Module` resource, it creates worker Pods
on the target node(s) to run the necessary action.  
The operator monitors the outcome of those Pods and records that information.
It uses it to label `Node` objects when the module was successfully loaded, and to run the device plugin (if
configured).
The label can be found in the node's label with the following format:
```
kmm.node.kubernetes.io/<namespace>.<modulename>.ready
```
but it is strongly recommended to use `GetKernelModuleReadyNodeLabel` function from the `labels` package in order to construct the correct label

### Worker Pods

Worker pods run the KMM `worker` binary that

- pulls the kmod image configured in the `Module` resource;
- extract it in the Pod's filesystem;
- runs `modprobe` with the right arguments to perform the necessary action.

kmod images are standard OCI images that contains `.ko` files.
Learn more about [how to build a kmod image](kmod_image.md).

### Device plugin

If `.spec.devicePlugin` is configured in a `Module`, then KMM will create a [device plugin](https://kubernetes.io/docs/concepts/extend-kubernetes/compute-storage-net/device-plugins/)
DaemonSet in the cluster.
That DaemonSet will target nodes:

- that match the `Module`'s `.spec.selector`;
- on which the kernel module is loaded.

There is also support for running an init-container as part of the device-plugin by setting `.spec.devicePlugin.initContainer`.

For example, for some devices, a successful load of the kernel module into the kernel might not constitute the indication of
successful loading. For those devices, the indication is the appearance of the device file under /dev filesystem.
For those cases, using an init-container looping over files verification instead of adding that verification to the device-plugin
image is preferable for debuggability and code re-usage reasons.

K8s automatically mounts the SA token and root CAs into the /var/run/secrets/kubernetes.io/serviceaccount of the
device-plugin pods using projected volumes.
As a result, mounting/overriding additional files into that directory is not allowed.
In some cases, users may want to use additional custom CAs or tokens for the device plugin but k8s won't allow mounting
them to the same path unless the auto-mount is disable.
In such cases, the `module.spec.devicePlugin.automountServiceAccountToken` field can be set to false to disable the auto-mounting
into device plugin pod, and allow users to mount what they need for the device plugin application.
!!! note
    In this case, it is the user's responsibility to mount the necessary tokens and CAs into the device plugin pods, otherwise the device plugin may not work properly.

## `Module` CRD

The `Module` Custom Resource Definition represents a kernel module that should be loaded on all or select nodes in the
cluster, through a kmod image.
A Module specifies one or more kernel versions it is compatible with, as well as a node selector.

The compatible versions for a `Module` are listed under `.spec.moduleLoader.container.kernelMappings`.
A kernel mapping can either match a `literal` version, or use `regexp` to match many of them at the same time.

The reconciliation loop for `Module` runs the following steps:

1. list all nodes matching `.spec.selector`;
2. build a set of all kernel versions running on those nodes;
3. for each kernel version:
    1. go through `.spec.moduleLoader.container.kernelMappings` and find the appropriate container image name.
       If the kernel mapping has `build` or `sign` defined and the container image does not already exist, run the build
       and / or signing pod as required;
    2. create a worker Pod pulling the container image determined at the previous step and running `modprobe`;
    3. if `.spec.devicePlugin` is defined, create a device plugin `DaemonSet` using the configuration specified under
       `.spec.devicePlugin.container`;
4. garbage-collect:
    1. obsolete device plugin `DaemonSets` that do not target any node;
    2. successful build pods;
    3. successful signing pods.

### Soft dependencies between kernel modules

Some setups may require that several kernel modules be loaded in a specific order to work properly, although the modules
do not directly depend on each other through symbols.
`depmod` is usually not aware of those dependencies, and they do not appear in the files it produces.  
If `mod_a` has a soft dependency on `mod_b`, `modprobe mod_a` will not load `mod_b`.

Soft dependencies can be declared in the `Module` CRD via the
`.spec.moduleLoader.container.modprobe.modulesLoadingOrder` field:

```yaml
modulesLoadingOrder:  # optional
  - mod_a
  - mod_b
```

With the configuration above:

- the loading order will be `mod_b`, then `mod_a`;
- the unloading order will be `mod_a`, then `mod_b`.

The first value in the list, to be loaded last, must be equivalent to the `moduleName`.

### Replacing an in-tree module

Some modules loaded by KMM may replace in-tree modules already loaded on the node.  
To unload in-tree modules before loading your module, set `.spec.moduleLoader.container.inTreeModulesToRemove`:

```yaml
spec:
  moduleLoader:
    container:
      modprobe:
        moduleName: mod_a
        # ...

      # Other fields removed for brevity
      inTreeModulesToRemove: [mod_a, mod_b]
```

The worker Pod will first try to unload the in-tree `mod_b` before loading `mod_a` from the kmod image.  
When the worker Pod is terminated and `mod_a` is unloaded, `mod_b` will not be loaded again.

### Forcing module image rebuilds

When KMM builds a kmod image in-cluster, it first checks if the target image already exists in the registry.
If it does, the build is skipped.
However, in some situations the images may no longer exist in the registry even though KMM believes they do.
This can happen when using an image registry with ephemeral storage: registry contents are lost on restart, node drain,
or rollout, but KMM still believes the images exist and attempts to use them, causing failures on the nodes.

To force KMM to re-verify image existence and rebuild images if necessary, set or change the
`.spec.imageRebuildTriggerGeneration` field in the `Module` resource:

```yaml
apiVersion: kmm.sigs.x-k8s.io/v1beta1
kind: Module
metadata:
  name: my-kmod
spec:
  imageRebuildTriggerGeneration: 1  # Change this value, or set it if it is not set, to trigger a rebuild
  moduleLoader:
    # ...
```

When KMM detects that the value of `.spec.imageRebuildTriggerGeneration` differs from the value stored in
`.status.imageRebuildTriggerGeneration`, it will:

1. re-verify image existence for all kernel mappings;
2. trigger builds for images that need to be rebuilt.

After the rebuild process completes, KMM updates `.status.imageRebuildTriggerGeneration` to match the spec value.

!!! note
    This field is optional. If not set, KMM behaves as before and only builds images that do not exist in the registry.

### Supporting Modules without OOT kmods
In some cases, there is a need to configure the KMM Module to avoid loading an out-of-tree kernel module and
instead use the in-tree one, running only the device plugin.
In such cases, the moduleLoader can be omitted from the Module custom resource, leaving only the devicePlugin section.

```yaml
apiVersion: kmm.sigs.x-k8s.io/v1beta1
kind: Module
metadata:
  name: my-kmod
spec:
  selector:
    node-role.kubernetes.io/worker: ""
  devicePlugin:
    container:
      image: some.registry/org/my-device-plugin:latest
```

### Example resource

Below is an annotated `Module` example with most options set.
More information about specific features is available in the dedicated pages:

- [in-cluster builds](kmod_image.md#building-in-cluster)
- [kernel module signing](secure_boot.md)

```yaml
apiVersion: kmm.sigs.x-k8s.io/v1beta1
kind: Module
metadata:
  name: my-kmod
spec:
  moduleLoader:
    container:
      modprobe:
        moduleName: my-kmod  # Required

        dirName: /opt  # Optional

        # Optional. Will copy /firmware/* on the node into the path specified
        # in the `kmm-operator-manager-config` at `worker.setFirmwareClassPath`
        # before `modprobe` is called to insert the kernel module..
        firmwarePath: /firmware
        
        parameters:  # Optional
          - param=1

        modulesLoadingOrder:  # optional
          - my-kmod
          - my_dep_a
          - my_dep_b

      imagePullPolicy: Always  # optional

      inTreeModuleToRemove: my-intree-kmod  # optional and deprecated, use inTreeModulesToRemove
      inTreeModulesToRemove: [my-intree-kmod, my-other-intree-kmod] # optional

      kernelMappings:  # At least one item is required
        - literal: 6.0.15-300.fc37.x86_64
          containerImage: some.registry/org/my-kmod:6.0.15-300.fc37.x86_64

        # For each node running a kernel matching the regexp below,
        # KMM will create a DaemonSet running the image specified in containerImage
        # with ${KERNEL_FULL_VERSION} replaced with the kernel version.
        - regexp: '^.+\fc37\.x86_64$'
          containerImage: "some.other.registry/org/my-kmod:${KERNEL_FULL_VERSION}"

        # For any other kernel, build the image using the Dockerfile in the my-kmod ConfigMap.
        - regexp: '^.+$'
          containerImage: "some.registry/org/my-kmod:${KERNEL_FULL_VERSION}"
          inTreeModuleToRemove: my-intree-kmod  # optional and deprecated, use inTreeModulesToRemove
          inTreeModulesToRemove: [my-intree-kmod, my-other-intree-kmod] # optional
          build:
            buildArgs:  # Optional
              - name: ARG_NAME
                value: some-value
            secrets:  # Optional
              - name: some-kubernetes-secret  # Will be available in the build environment at /run/secrets/some-kubernetes-secret.
            baseImageRegistryTLS:
              # Optional and not recommended! If true, the build will be allowed to pull the image in the Dockerfile's
              # FROM instruction using plain HTTP.
              insecure: false
              # Optional and not recommended! If true, the build will skip any TLS server certificate validation when
              # pulling the image in the Dockerfile's FROM instruction using plain HTTP.
              insecureSkipTLSVerify: false
            dockerfileConfigMap:  # Required
              name: my-kmod-dockerfile
          sign:
            certSecret:
              name: cert-secret  # Required
            keySecret:
              name: key-secret  # Required
            # Required when sign is set. Use absolute paths or Ash shell absolute globs (e.g. /opt/lib/modules/<kernel-version>/*.ko), see Secure Boot docs.
            filesToSign:
              - /opt/lib/modules/${KERNEL_FULL_VERSION}/my-kmod.ko
          registryTLS:
            # Optional and not recommended! If true, KMM will be allowed to check if the container image already exists
            # using plain HTTP.
            insecure: false
            # Optional and not recommended! If true, KMM will skip any TLS server certificate validation when checking if
            # the container image already exists.
            insecureSkipTLSVerify: false

  devicePlugin:  # Optional
    container:
      image: some.registry/org/device-plugin:latest  # Required if the devicePlugin section is present

      env:  # Optional
        - name: MY_DEVICE_PLUGIN_ENV_VAR
          value: SOME_VALUE

      volumeMounts:  # Optional
        - mountPath: /some/mountPath
          name: device-plugin-volume

    volumes:  # Optional
      - name: device-plugin-volume
        configMap:
          name: some-configmap

    serviceAccountName: sa-device-plugin  # Optional; created automatically if not set

  imageRepoSecret:  # Optional. Used to pull kmod and device plugin images
    name: secret-name

  # Optional. Change this value to force KMM to re-verify and potentially rebuild all module images.
  # See "Forcing module image rebuilds" section for details.
  imageRebuildTriggerGeneration: 1

  selector:
    node-role.kubernetes.io/worker: ""
```

#### Variable substitution

The following `Module` fields support shell-like variable substitution:

- `.spec.moduleLoader.container.containerImage`;
- `.spec.moduleLoader.container.kernelMappings[*].containerImage`;
- `.spec.moduleLoader.container.sign.filesToSign`;
- `.spec.moduleLoader.container.kernelMappings[*].sign.filesToSign`;

The following variables will be substituted:

| Name                  | Description                            | Example                 |
|-----------------------|----------------------------------------|-------------------------|
| `KERNEL_FULL_VERSION` | The kernel version we are building for | `6.3.5-200.fc38.x86_64` |
| `MOD_NAME`            | The `Module`'s name                    | `my-mod`                |
| `MOD_NAMESPACE`       | The `Module`'s namespace               | `my-namespace`          |

### Unloading the kernel module

To unload a module loaded with KMM from nodes, simply delete the corresponding `Module` resource.
KMM will then create worker Pods where required to run `modprobe -r` and unload the kernel module from nodes.

!!! warning
    To create unloading worker Pods, KMM needs all the resources it used when loading the kernel module.
    This includes the `ServiceAccount` that are referenced in the `Module` as well as any RBAC you may have defined to
    allow privileged KMM worker Pods to run.
    It also includes any pull secret referenced in `.spec.imageRepoSecret`.  
    To avoid situations where KMM is unable to unload the kernel module from nodes, make sure those resources are not
    deleted while the `Module` resource is still present in the cluster in any state, including `Terminating`.  
    KMM ships with a validating admission webhook that rejects the deletion of namespaces that contain at least one
    `Module` resource.

### Kernel modules events on Nodes
Due to an event anti-spam mechanism embedded in Kubernetes,
some events may not necessarily be shown when loading or unloading kernel modules in quick succession.

## Security and permissions

Loading kernel modules is a highly sensitive operation.
Once loaded, kernel modules have all possible permissions to do any kind of operation on the node.

### Pod Security standards

KMM creates privileged workload to load the kernel modules on nodes.  
If [Pod Security Admission](https://kubernetes.io/docs/concepts/security/pod-security-admission/) is enabled in the
cluster, an administrator needs to configure the isolation level for the namespace where the `Module` is deployed for
the worker and device plugin pods to work.

```shell
kubectl label --overwrite ns "${namespace}" pod-security.kubernetes.io/enforce=privileged
```
