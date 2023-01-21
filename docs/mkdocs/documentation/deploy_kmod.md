# Deploying kernel modules

For each `Module`, KMM can create a number of `DaemonSets`:

- one ModuleLoader DaemonSet per compatible kernel version running in the cluster;
- one device plugin DaemonSet, if configured.

### ModuleLoader

The ModuleLoader DaemonSets run ModuleLoader images to load kernel modules.  
A ModuleLoader image is an OCI image that contains the `.ko` files and both the `modprobe` and `sleep` binaries.

When the ModuleLoader pod is created, it will run `modprobe` to insert the specified module into the kernel.
It will then `sleep` until it is terminated.
When that happens, the `ExecPreStop` hook will run `modprobe -r` to unload the kernel module.

Learn more about [how to build a ModuleLoader image](module_loader_image.md).

### Device plugin

If `.spec.devicePlugin` is configured in a `Module`, then KMM will create a [device plugin](https://kubernetes.io/docs/concepts/extend-kubernetes/compute-storage-net/device-plugins/)
DaemonSet in the cluster.
That DaemonSet will target nodes:

- that match the `Module`'s `.spec.selector`;
- on which the kernel module is loaded (where the ModuleLoader pod has the `Ready` condition).

## `Module` CRD

The `Module` Custom Resource Definition represents a kernel module that should be loaded on all or select nodes in the
cluster, through a ModuleLoader image.
A Module specifies one or more kernel versions it is compatible with, as well as a node selector.

The compatible versions for a `Module` are listed under `.spec.moduleLoader.container.kernelMappings`.
A kernel mapping can either match a `literal` version, or use `regexp` to match many of them at the same time.

The reconciliation loop for `Module` runs the following steps:

1. list all nodes matching `.spec.selector`;
2. build a set of all kernel versions running on those nodes;
3. for each kernel version:
    1. go through `.spec.moduleLoader.container.kernelMappings` and find the appropriate container image name.
       If the kernel mapping has `build` or `sign` defined and the container image does not already exist, run the build
       and / or signing job as required;
    2. create a ModuleLoader `DaemonSet` with the container image determined at the previous step;
    3. if `.spec.devicePlugin` is defined, create a device plugin `DaemonSet` using the configuration specified under
       `.spec.devicePlugin.container`;
4. garbage-collect:
    1. existing `DaemonSets` targeting kernel versions that are not run by any node in the cluster;
    2. successful build jobs;
    3. successful signing jobs.

## Example resource

Below is an annotated `Module` example with most options set.
More information about specific features is available in the dedicated pages:

- [in-cluster builds](module_loader_image.md#building-in-cluster)
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

        # Optional. Will copy /firmware/* into /var/lib/firmware/ on the node.
        firmwarePath: /firmware
        
        parameters:  # Optional
          - param=1

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

  imageRepoSecret:  # Optional. Used to pull ModuleLoader and device plugin images
    name: secret-name

  selector:
    node-role.kubernetes.io/worker: ""
```