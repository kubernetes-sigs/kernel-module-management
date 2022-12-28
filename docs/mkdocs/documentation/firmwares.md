# Firmware support

Kernel modules sometimes need to load firmware files from the filesystem.
KMM supports copying firmware files from the [Module Loader image](module_loader_image.md)
to the node's filesystem.  
The contents of `.spec.moduleLoader.container.modprobe.firmwarePath` are copied into `/var/lib/firmware` on the node
before `modprobe` is called to insert the kernel module.  
All files and empty directories are removed from that location before `modprobe -r` is called to unload the kernel
module, when the pod is terminated.

## Building a ModuleLoader image

In addition to building the kernel module itself, include the binary firmware in the builder image.

```dockerfile
FROM ubuntu as builder

# Build the kmod

RUN ["mkdir", "/firmware"]
RUN ["curl", "-o", "/firmware/firmware.bin", "https://artifacts.example.com/firmware.bin"]

FROM ubuntu

# Copy the kmod, install modprobe, run depmod

COPY --from=builder /firmware /firmware
```

## Tuning the `Module` resource

Set `.spec.moduleLoader.container.modprobe.firmwarePath` in the `Module` CR:

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

        # Optional. Will copy /firmware/* into /var/lib/firmware/ on the node.
        firmwarePath: /firmware
        
        # Add kernel mappings
  selector:
    node-role.kubernetes.io/worker: ""
```