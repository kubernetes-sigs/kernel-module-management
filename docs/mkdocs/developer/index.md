# Design notes

The OOT Operator brings a new `Module` CRD.
`Module` represents an out-of-tree kernel module that must be inserted into the cluster nodes’ kernel through a
DriverContainer scheduled by a `DaemonSet`.

The OOTO optionally builds and runs DriverContainers on the cluster.
It picks the right DriverContainer image by leveraging kernel mappings in the `Module` CRD that describe which image
should be used for which kernel.

A kernel mapping maps either a literal kernel name or a regex with a kernel’s node.
This allows for more flexibility when targeting a set of kernels (e.g. “for Ubuntu nodes, build from that repository”).
Variables available at build time still reflect the actual kernel version.

## `Module` CRD
```yaml
apiVersion: ooto.sigs.k8s.io/v1beta1
kind: Module
metadata:
  name: module-sample
spec:
  build:
    # Reference instructions to build the module
    git:
      repository: https://github.com/vendor/driver
      ref: some-tag
    buildArgs:
      PARAM1: value1
      PARAM2: value2
    containerFile: Dockerfile (or something else)
    contextDir: some/path/in/the/sources
    to:
      imageName: image-registry.openshift-image-registry.svc:5000/driver:${KERNEL_VERSION}
      pushSecret:
        name: push-secret-name
        namespace: push-secret-namespace
  devicePlugin: # is a Container spec
  driverContainer: # is a Container spec
  kernelMappings:
    - literal: 5.16.11-200.fc35.x86_64
      containerImage: quay.io/vendor/module-sample:fedora-5.16.11-200.fc35.x86_64
    - literal: 5.4.0-1054-gke
      containerImage: quay.io/vendor/module-sample:ubuntu-5.4.0-1054-gke
    - regexp: '^.*\-gke$'
      build:
        git:
          ref: gke
  selector:  # top-level selector
    feature.node.kubernetes.io/cpu-cpuid.VMX: true
status:
  conditions:
    - lastTransitionTime: "2022-03-07T14:01:00Z"
      status: False
      type: Progressing
    - lastTransitionTime: "2022-03-07T14:01:01Z"
      status: True
      type: Ready
```

## In-cluster builds
Virtually any Kubernetes distribution ships with its own kernel.
It is thus challenging for a kernel module vendor to make DriverContainer images available for all kernels available
in all Kubernetes distributions out there (or even for a subset of them).

The OOTO supports in-cluster builds of DriverContainer images when those are not made available by the vendor.
The in-cluster build system is able to build an image from any Git repository that contains a `Dockerfile`.
It can then push said image to a user-provided repository, optionally using authentication provided in a Kubernetes secret.

**Optional:** on upstream Kubernetes, we may want to deploy an in-cluster registry to host in-cluster built images.  
**On OCP**, the build mechanism would be BuildConfig (maybe Shipwright in the future) and we can leverage the
integrated in-cluster registry.

## Unloading modules
Users must provide a [preStop lifecycle hook](https://kubernetes.io/docs/concepts/containers/container-lifecycle-hooks/)
to their DriverContainer pod template to make sure that their module is unloaded when the DriverContainer pod exits.

## Security

### DriverContainer privileges
By default, the OOTO would only grant the [`CAP_SYS_MODULE`](https://man7.org/linux/man-pages/man7/capabilities.7.html)
capability to DriverContainer `DaemonSets`.

### Kubernetes API privileges
The OOTO would only be granted a limited set of Kubernetes API privileges:
- Read and watch `Nodes`;
- Read and watch `Modules`, update their status;
- Read, create, modify and watch `DaemonSets`;
- Read, create, modify and watch `Build` objects (from whatever system we agree on);
- Read `Secrets`.
