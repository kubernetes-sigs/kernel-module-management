# Signing kernel modules using KMM

### Using Signing with a pre-built driver container

The yaml below will add the public/private key-pair as secrets with the required key names ("key" for the private key, "cert" for the public key). It will then pull down the `unsignedImage` image, open it up, sign the kernel modules listed in `filesToSign`, add them back and push the resulting image as `containerImage`.

KMM should then deploy the daemonset that loads the signed kmods onto all the nodes with that match the selector.
The driver containers should run successfully on any nodes that have the public key in their MOK database, and any nodes that are not secure-boot enabled (which will just ignore the signature). They should fail to load on any that have secure-boot enabled but do not have that key in their MOK database.

### Example

Before applying this ensure that the `keySecret` and `certSecret` secrets have been created (see [here](secureboot-secrets.md)

```yaml
---
apiVersion: kmm.sigs.k8s.io/v1beta1
kind: Module
metadata:
  name: example-module
spec:
  moduleLoader:
    serviceAccountName: default
    container:
      modprobe:
        # the name of the kmod to load
        moduleName: "<your module name>"
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
    name: "repo-pull-secret"
  selector:
    kubernetes.io/arch: amd64
```

A list of common issues can be found [here](debugging.md)
