# Signing kernel modules using KMM

### Secure-boot with KMM

For more details on using Secure-boot see [here](https://access.redhat.com/documentation/en-us/red_hat_enterprise_linux/9/html/managing_monitoring_and_updating_the_kernel/signing-kernel-modules-for-secure-boot_managing-monitoring-and-updating-the-kernel) or [here](https://wiki.debian.org/SecureBoot)

### Using Signing with KMM

On a secure-boot enabled system all kernel modules (kmods) must be signed with a public/private key-pair enrolled into the Machine Owner's Key or MOK database. Drivers distributed as part of a distribution should already be signed by the distributions private key, but for kernel modules build out-of-tree KMM supports signing kernel modules using the "sign" section of the kernel mapping.

To use this functionality you need:

 * A public private key pair in the correct (der) format
 * At least one secure-boot enabled nodes with the public key enrolled in its MOK database
 * Either a pre-built driver container image, or the source code and dockerfile needed to build one in-cluster.

If you have a pre-built image, such as one distributed by a hardware vendor, or already built elsewhere please see the [Signing](secureboot-signing.md) docs for signing with KMM.

Alternatively if you have source code and need to build your image first, please see the [Build and Sign](secureboot-build-and-sign.md) docs for deploying with KMM.

If all goes well KMM should load your driver into the nodes kernel. If not see the list of [Common Issues](debugging.md)

