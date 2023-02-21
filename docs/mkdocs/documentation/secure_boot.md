# Signing kernel modules with KMM

For more details on using Secure-boot see [here](https://access.redhat.com/documentation/en-us/red_hat_enterprise_linux/9/html/managing_monitoring_and_updating_the_kernel/signing-kernel-modules-for-secure-boot_managing-monitoring-and-updating-the-kernel) or [here](https://wiki.debian.org/SecureBoot)

## Using Signing with KMM

On a secure-boot enabled system all kernel modules (kmods) must be signed with a public/private key-pair enrolled into
the Machine Owner's Key or MOK database.
Drivers distributed as part of a distribution should already be signed by the distributions private key, but for kernel
modules build out-of-tree KMM supports signing kernel modules using the `sign` section of the kernel mapping.

To use this functionality you need:

- A public private key pair in the correct (der) format
- At least one secure-boot enabled nodes with the public key enrolled in its MOK database
- Either a pre-built driver container image, or the source code and `Dockerfile` needed to build one in-cluster.

If you have a pre-built image, such as one distributed by a hardware vendor, or already built elsewhere please see the
[Signing](#signing-a-pre-built-driver-container) docs for signing with KMM.

Alternatively if you have source code and need to build your image first, please see the
[Build and Sign](#building-and-signing-a-moduleloader-container-image) docs for deploying with KMM.

If all goes well KMM should load your driver into the node's kernel.
If not, see [Common Issues](#debugging--troubleshooting).

# Adding the Keys for secureboot

To use KMM to sign kernel modules a certificate and private key are required.
For details on how to create these see [here](https://access.redhat.com/documentation/en-us/red_hat_enterprise_linux/9/html/managing_monitoring_and_updating_the_kernel/signing-kernel-modules-for-secure-boot_managing-monitoring-and-updating-the-kernel#generating-a-public-and-private-key-pair_signing-kernel-modules-for-secure-boot)

For example:
```shell
openssl req -x509 -new -nodes -utf8 -sha256 -days 36500 -batch -config configuration_file.config -outform DER -out my_signing_key_pub.der -keyout my_signing_key.priv
```

The two files created (`my_signing_key_pub.der` containing the cert and `my_signing_key.priv` containing the private
key) can then be added as [secrets](https://kubernetes.io/docs/concepts/configuration/secret/) either directly by:

```shell
kubectl create secret generic my-signing-key --from-file=key=<my_signing_key.priv>
kubectl create secret generic my-signing-key-pub --from-file=key=<my_signing_key_pub.der>
```
OR

by base64 encoding them:
```shell
cat my_signing_key.priv | base64 -w 0  > my_signing_key2.base64
cat my_signing_key_pub.der | base64 -w 0 > my_signing_key_pub.base64
```

Adding the encoded text in to a yaml file:
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: my-signing-key-pub
  namespace: default
type: Opaque
data:
  cert: <base64 encoded secureboot public key>

---
apiVersion: v1
kind: Secret
metadata:
  name: my-signing-key
  namespace: default
type: Opaque
data:
  key: <base64 encoded secureboot private key>
```

and then applying the yaml file using:
```shell
kubectl apply -f <yaml filename>
```

## Checking the keys

To check the public key secret is set correctly:
```shell
kubectl  get secret -o yaml <certificate secret name> | awk '/cert/{print $2; exit}' | base64 -d  | openssl x509 -inform der -text
```

This should display a certificate with a Serial Number, Issuer, Subject etc.

And to check the private key:
```shell
kubectl  get secret -o yaml <private key secret name> | awk '/key/{print $2; exit}' | base64 -d
```

Which should display a key, including `-----BEGIN PRIVATE KEY-----` and `-----END PRIVATE KEY-----` lines

# Signing a pre-built driver container

The YAML below will add the public/private key-pair as secrets with the required key names (`key` for the private key,
`cert` for the public key).
It will then pull down the `unsignedImage` image, open it up, sign the kernel modules listed in `filesToSign`, add them
back and push the resulting image as `containerImage`.

KMM should then deploy the DaemonSet that loads the signed kmods onto all the nodes with that match the selector.
The driver containers should run successfully on any nodes that have the public key in their MOK database, and any
nodes that are not secure-boot enabled (which will just ignore the signature).
They should fail to load on any that have secure-boot enabled but do not have that key in their MOK database.

## Example

Before applying this, ensure that the `keySecret` and `certSecret` secrets have been created (see
[here](#adding-the-keys-for-secureboot)).

```yaml
---
apiVersion: kmm.sigs.x-k8s.io/v1beta1
kind: Module
metadata:
  name: example-module
spec:
  moduleLoader:
    serviceAccountName: default
    container:
      modprobe:
        # the name of the kmod to load
        moduleName: '<your module name>'
      kernelMappings:
        # the kmods will be deployed on all nodes in the cluster with a kernel that matches the regexp
        - regexp: '^.*\.x86_64$'
          # the container to produce containing the signed kmods
          containerImage: <image name e.g. quay.io/myuser/my-driver:<kernelversion>-signed>
          sign:
            # the image containing the unsigned kmods (we need this because we are not building the kmods within the cluster)
            unsignedImage: <image name e.g. quay.io/myuser/my-driver:<kernelversion> >
            keySecret: # a secret holding the private secureboot key with the key 'key'
              name: <private key secret name>
            certSecret: # a secret holding the public secureboot key with the key 'cert'
              name: <certificate secret name>
            filesToSign: # full path within the unsignedImage container to the kmod(s) to sign
              - /opt/lib/modules/4.18.0-348.2.1.el8_5.x86_64/kmm_ci_a.ko
  imageRepoSecret:
    # the name of a secret containing credentials to pull unsignedImage and push containerImage to the registry
    name: repo-pull-secret
  selector:
    kubernetes.io/arch: amd64
```

# Building and signing a ModuleLoader container image

The YAML below should build a new container image using the
[source code from the repo](https://github.com/kubernetes-sigs/kernel-module-management/tree/main/ci/kmm-kmod) (this
kernel module does nothing useful but provides a good example).
The image produced is saved back in the registry with a temporary name, and this temporary image is then signed using
the parameters in the `sign` section.

The temporary image name is based on the final image name and is set to be
`<containerImage>:<tag>-<namespace>_<module name>_kmm_unsigned`.

For example, given the YAML below KMM would build an image named
`quay.io/chrisp262/minimal-driver:final-default_example-module_kmm_unsigned` containing the build but unsigned kmods,
and push it to the registry.
Then it would create a second image, `quay.io/chrisp262/minimal-driver:final` containing the signed kmods.
It is this second image that will be loaded by the DaemonSet and will deploy the kmods to the cluster nodes.

Once it is signed the temporary image can be safely deleted from the registry (it will be rebuilt if needed).


## Example
Before applying this, ensure that the `keySecret` and `certSecret` secrets have been created (see
[here](#adding-the-keys-for-secureboot)).

```yaml
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: example-module-dockerfile
  namespace: default
data:
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
---
apiVersion: kmm.sigs.x-k8s.io/v1beta1
kind: Module
metadata:
  name: example-module
  namespace: default
spec:
  moduleLoader:
    serviceAccountName: default
    container:
      modprobe:
        moduleName: simple_kmod
      kernelMappings:
        - regexp: '^.*\.x86_64$'
          containerImage: < the name of the final driver container to produce>
          build:
            dockerfileConfigMap:
              name: example-module-dockerfile
          sign:
            keySecret:
              name: <private key secret name>
            certSecret:
              name: <certificate secret name>
            filesToSign:
              - /opt/lib/modules/4.18.0-348.2.1.el8_5.x86_64/kmm_ci_a.ko
  imageRepoSecret: # used as imagePullSecrets in the DaemonSet and to pull / push for the build and sign features
    name: repo-pull-secret
  selector: # top-level selector
    kubernetes.io/arch: amd64
```

# Debugging & troubleshooting

If your driver containers end up in `PostStartHookError` or `CrashLoopBackOff` status, and `kubectl describe` shows an
event: `modprobe: ERROR: could not insert '<your kmod name>': Required key not available` then the kmods are either not
signed, or signed with the wrong key.

