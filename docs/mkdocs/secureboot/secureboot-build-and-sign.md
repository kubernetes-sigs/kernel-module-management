# Signing kernel modules using KMM

### Using Build and Sign together with KMM

The yaml below should build a new container image using the source code from the [](https://github.com/kubernetes-sigs/kernel-module-management/tree/main/ci/kmm-kmod) repo (this kernel module does nothing useful but provides a good example). The image produced is saved back the the registry with a temporary name, and this temporary image is then signed using the parameters in the `sign` section.

The temporary image name is based on the final image name and is set to be `<containerImage>:<tag>-<namespace>_<module name>_kmm_unsigned`. 

For example, given the yaml below KMM would build an image named `quay.io/chrisp262/minimal-driver:final-default_example-module_kmm_unsigned` containing the build but unsigned kmods, and push it to the registry. Then it would create a second image, quay.io/chrisp262/minimal-driver:final containing the signed kmods. It is this second image that will be loaded by the daemonset and will deploy the kmods to the cluster nodes.

Once it is signed the temporary image can be safely deleted from the registry (it will be rebuilt if needed).


### Example
Before applying this ensure that the `keySecret` and `certSecret` secrets have been created (see [here](secureboot-secrets.md)

```
---
apiVersion: kmm.sigs.k8s.io/v1beta1
kind: Module
metadata:
  name: example-module
  namespace: default
spec:
  moduleLoader:
    serviceAccountName: default
    container:
      modprobe:
        moduleName: 'simple_kmod'
      kernelMappings:
        - regexp: '^.*\.x86_64$'
          containerImage: < the name of the final driver container to produce>
          build:
            dockerfile: |
              ARG DTK_AUTO
              ARG KERNEL_VERSION
              FROM ${DTK_AUTO} as builder
              WORKDIR /build/
              RUN git clone -b main --single-branch https://github.com/kubernetes-sigs/kernel-module-management.git
              WORKDIR kernel-module-management/ci/kmm-kmod/
              RUN make 
              FROM docker.io/redhat/ubi8:latest
              ARG KERNEL_VERSION
              RUN yum -y install kmod && yum clean all
              RUN mkdir -p /opt/lib/modules/${KERNEL_VERSION}
              COPY --from=builder /build/kernel-module-management/ci/kmm-kmod/*.ko /opt/lib/modules/${KERNEL_VERSION}/
              RUN /usr/sbin/depmod -b /opt
          sign:
            keySecret:
              name: <private key secret name>
            certSecret:
              name: <certificate secret name>
            filesToSign:
              - /opt/lib/modules/4.18.0-348.2.1.el8_5.x86_64/kmm_ci_a.ko
  imageRepoSecret:  # used as imagePullSecrets in the DaemonSet and to pull / push for the build and sign features
    name: "repo-pull-secret"
  selector:  # top-level selector
    kubernetes.io/arch: amd64
```

A list of common issues can be found [here](debugging.md)
