# Creating a ModuleLoader image

Kernel Module Management works with purpose-built ModuleLoader images.
Those are standard OCI images that satisfy a few requirements:

- `.ko` files must be located under `/opt/lib/modules/${KERNEL_VERSION}`
- the `modprobe` and `sleep` binaries must be in the `$PATH`.

## `depmod`

It is recommended to run `depmod` at the end of the build process to generate `modules.dep` and map files.
This is especially useful if your ModuleLoader image contains several kernel modules and if one of the modules depend on
another.
To generate dependencies and map files for a specific kernel version, run `depmod -b /opt ${KERNEL_VERSION}`.

## Example `Dockerfile`

The `Dockerfile` below can accommodate any kernel available in the Ubuntu repositories.
Pass the kernel version you are building an image from using the `--build-arg KERNEL_VERSION=1.2.3` Docker CLI switch.

```dockerfile
FROM ubuntu as builder

ARG KERNEL_VERSION

RUN apt-get update && apt-get install -y bc \
    bison \
    flex \
    libelf-dev \
    gnupg \
    wget \
    git \
    make \
    gcc \
    linux-headers-${KERNEL_VERSION}

WORKDIR /usr/src
RUN ["git", "clone", "https://github.com/kubernetes-sigs/kernel-module-management.git"]

WORKDIR /usr/src/kernel-module-management/ci/kmm-kmod
RUN ["make"]

FROM ubuntu

ARG KERNEL_VERSION

RUN apt-get update && apt-get install -y kmod

COPY --from=builder /usr/src/kernel-module-management/ci/kmm-kmod/kmm_ci_a.ko /opt/lib/modules/${KERNEL_VERSION}/
COPY --from=builder /usr/src/kernel-module-management/ci/kmm-kmod/kmm_ci_b.ko /opt/lib/modules/${KERNEL_VERSION}/

RUN depmod -b /opt ${KERNEL_VERSION}
```

## Building in cluster

KMM is able to build ModuleLoader images in cluster.
Build instructions must be provided using the `build` section of a kernel mapping.
The `Dockerfile` for your container image should be copied into a `ConfigMap` object, under the `Dockerfile` key.
The `ConfigMap` needs to be located in the same namespace as the `Module`.

KMM will first check if the image name specified in the `containerImage` field exists.
If it does, the build will be skipped.
Otherwise, KMM will create a Job to build your image.
The [kaniko](https://github.com/GoogleContainerTools/kaniko) build system is used.
KMM monitors the health of the build job, retrying if necessary.

The following build arguments are automatically set by KMM:

| Name                          | Description                            | Example                 |
|-------------------------------|----------------------------------------|-------------------------|
| `KERNEL_FULL_VERSION`         | The kernel version we are building for | `6.3.5-200.fc38.x86_64` |
| `KERNEL_VERSION` (deprecated) | The kernel version we are building for | `6.3.5-200.fc38.x86_64` |
| `MOD_NAME`                    | The `Module`'s name                    | `my-mod`                |
| `MOD_NAMESPACE`               | The `Module`'s namespace               | `my-namespace`          |

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
