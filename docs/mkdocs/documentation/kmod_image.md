# Creating a kmod image

Kernel Module Management works with purpose-built kmod images, which are standard OCI images that contain `.ko` files.  
The location of those files must match the following pattern:
```
<prefix>/lib/modules/[kernel-version]/
```

Where:

- `<prefix>` should be equal to `/opt` in most cases, as it is the `Module` CRD's default value;
- `kernel-version` must be non-empty and equal to the kernel version the kernel modules were built for.

In addition to the present of the `.ko` files, the kmod image also requires
the `cp` binary to be present as the `.ko` files will be copied from this image to
the image-loader worker pod created by the operator.
This is a minimal requirement and we do not require any other binary tool to be
present in the image.

## `depmod`

It is recommended to run `depmod` at the end of the build process to generate `modules.dep` and map files.
This is especially useful if your kmod image contains several kernel modules and if one of the modules depend on
another.
To generate dependencies and map files for a specific kernel version, run `depmod -b /opt ${KERNEL_FULL_VERSION}`.

## Example `Dockerfile`

The `Dockerfile` below can accommodate any kernel available in the Ubuntu repositories.
Pass the kernel version you are building an image from using the `--build-arg KERNEL_FULL_VERSION=1.2.3` Docker CLI switch.

```dockerfile
FROM ubuntu as builder

ARG KERNEL_FULL_VERSION

RUN apt-get update && apt-get install -y bc \
    bison \
    flex \
    libelf-dev \
    gnupg \
    wget \
    git \
    make \
    gcc \
    linux-headers-${KERNEL_FULL_VERSION}

WORKDIR /usr/src
RUN ["git", "clone", "https://github.com/kubernetes-sigs/kernel-module-management.git"]

WORKDIR /usr/src/kernel-module-management/ci/kmm-kmod
RUN ["make"]

FROM ubuntu

ARG KERNEL_FULL_VERSION

RUN apt-get update && apt-get install -y kmod

COPY --from=builder /usr/src/kernel-module-management/ci/kmm-kmod/kmm_ci_a.ko /opt/lib/modules/${KERNEL_FULL_VERSION}/
COPY --from=builder /usr/src/kernel-module-management/ci/kmm-kmod/kmm_ci_b.ko /opt/lib/modules/${KERNEL_FULL_VERSION}/

RUN depmod -b /opt ${KERNEL_FULL_VERSION}
```

## Building in cluster

KMM is able to build kmod images in cluster.
Build instructions must be provided using the `build` section of a kernel mapping.
The `Dockerfile` for your container image should be copied into a `ConfigMap` object, under the `dockerfile` key.
The `ConfigMap` needs to be located in the same namespace as the `Module`.

KMM will first check if the image name specified in the `containerImage` field exists.
If it does, the build will be skipped.
Otherwise, KMM will create a Pod to build your image using [kaniko](https://github.com/GoogleContainerTools/kaniko).

The following build arguments are automatically set by KMM:

| Name                  | Description                            | Example                 |
|-----------------------|----------------------------------------|-------------------------|
| `KERNEL_FULL_VERSION` | The kernel version we are building for | `6.3.5-200.fc38.x86_64` |
| `MOD_NAME`            | The `Module`'s name                    | `my-mod`                |
| `MOD_NAMESPACE`       | The `Module`'s namespace               | `my-namespace`          |

Successful build pods are garbage-collected immediately, unless [`job.gcDelay`](./configure.md#jobgcdelay) is set in the
operator configuration.  
Failed build pods are always preserved and must be deleted manually by the administrator for the build to be restarted.

Once the image is built, KMM proceeds with the `Module` reconciliation.

```yaml
# ...
- regexp: '^.+$'
  containerImage: "some.registry/org/my-kmod:${KERNEL_FULL_VERSION}"
  build:
    buildArgs:  # Optional
      - name: ARG_NAME
        value: some-value
    secrets:  # Optional
      - name: some-kubernetes-secret  # Will be mounted in the build pod as /run/secrets/some-kubernetes-secret.
    baseImageRegistryTLS:
      # Optional and not recommended! If true, the build will be allowed to pull the image in the Dockerfile's
      # FROM instruction using plain HTTP.
      insecure: false
      # Optional and not recommended! If true, the build will skip any TLS server certificate validation when
      # pulling the image in the Dockerfile's FROM instruction using plain HTTP.
      insecureSkipTLSVerify: false
    dockerfileConfigMap:  # Required
      name: my-kmod-dockerfile
  registryTLS:
    # Optional and not recommended! If true, KMM will be allowed to check if the container image already exists
    # using plain HTTP.
    insecure: false
    # Optional and not recommended! If true, KMM will skip any TLS server certificate validation when checking if
    # the container image already exists.
    insecureSkipTLSVerify: false
```

### Depending on in-tree kernel modules

Some kernel modules depend on other kernel modules shipped with the node's distribution.
To avoid copying those dependencies into the kmod image, KMM mounts the host's `/lib/modules`
directory into Pod filesystems:

- **Build Pods**: mounted at `/host/lib/modules`
- **Worker Pods**: mounted at both `/lib/modules` and `/host/lib/modules`

By creating a symlink from `/opt/lib/modules/[kernel-version]/[symlink-name]` to
`/host/lib/modules/[kernel-version]`, `depmod` can use the in-tree kmods on the building node's filesystem to resolve
dependencies.
At runtime, the worker Pod extracts the entire image, including the `[symlink-name]` symbolic link.
That link points to `/host/lib/modules/[kernel-version]` in the worker Pod, which is mounted from the node's filesystem.
`modprobe` can then follow that link and load the in-tree dependencies as needed.

In the example below, we use `host` as the symbolic link name under `/opt/lib/modules/[kernel-version]`:

```dockerfile
FROM ubuntu as builder

#
# Build steps
#

FROM ubuntu

ARG KERNEL_FULL_VERSION

RUN apt-get update && apt-get install -y kmod

COPY --from=builder /usr/src/kernel-module-management/ci/kmm-kmod/kmm_ci_a.ko /opt/lib/modules/${KERNEL_FULL_VERSION}/
COPY --from=builder /usr/src/kernel-module-management/ci/kmm-kmod/kmm_ci_b.ko /opt/lib/modules/${KERNEL_FULL_VERSION}/

# Create the symbolic link to host modules (mounted at /host/lib/modules by KMM)
RUN ln -s /host/lib/modules/${KERNEL_FULL_VERSION} /opt/lib/modules/${KERNEL_FULL_VERSION}/host

RUN depmod -b /opt ${KERNEL_FULL_VERSION}
```

!!! warning
    `depmod` will generate dependency files based on the kernel modules present on the node that runs the kmod image
    build.  
    On the node on which KMM loads the kernel modules, `modprobe` will expect the files to be present under
    `/host/lib/modules/[kernel-version]`, and the same filesystem layout.  
    It is highly recommended that the build and the target nodes share the same distribution and release.
