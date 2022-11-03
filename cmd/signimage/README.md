A utility to pull down an image, extract named kernel modules from it, sign them with the provided keys, and add them back in as a new layer, then upload that new image under a new tag.

It operates as a wrapper around the sign-file binary provided as part of the kernel-devel package (a wrapper rather than a reimplementation to ensure its bug-for-bug compatabile, rather then having awhole new set of bugs of its own). sign-file is distributed as part of the kernel source so in theory is kernel version specific but in reality it hasn't changed to 5+ years, and for kernel modules to be whitelisted across major RHEL versions the signing format also has to be compatable so this is not a major concer.

Configuration is done via command line switches or failing that via environment variables

```
Usage of signimage:
  -cert string
        path to file containing public key for signing
  -filestosign string
        colon seperated list of kmods to sign
  -key string
        path to file containing private key for signing
  -pullsecret string
        path to file containing credentials for pulling images
  -pushsecret string
        path to file containing credentials for pushing images (defaults to the pullsecret)
  -signedimage string
        name of the signed image to produce (defaults to "${unsignedimage}-signed")
  -unsignedimage string
        name of the image to sign
```

Environment variables:


- UNSIGNEDIMAGE  the image to pull down
- SIGNEDIMAGE  the tag for the new (signed) image
- FILESTOSIGN  a colon seperated list of the full paths to files to sign
- KEYSECRET  the path to the private key
- CERTSECRET  the path to the public key 
- PULLSECRET  path to file containing push credentials
- PUSHSECRET  path to file containing pull credentials



## Examples
An example of its use as a Kubernetes job can be found in the ```kmod_signer_job.yaml``` file

Or the from the command line
```
./signimage -unsignedimage quay.io/<org>/<image>:<tag> \
	-pullsecret <pullsecretname> \
	-key <keyfilename> \
	-cert <certfilename> \
	-filestosign </var/lib/kmod1.ko>[:</var/lib/kmod2.ko>]...

