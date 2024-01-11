# Firmware support

Kernel modules sometimes need to load firmware files from the filesystem.
KMM supports copying firmware files from the [kmod image](kmod_image.md)
to the node's filesystem.  
The contents of `.spec.moduleLoader.container.modprobe.firmwarePath` are copied into `/var/lib/firmware` on the node
before `modprobe` is called to insert the kernel module.  
All files and empty directories are removed from that location before `modprobe -r` is called to unload the kernel
module, when the pod is terminated.

## Building a kmod image

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

## Setting the kernel's firmware search path

The Linux kernel accepts the `firmware_class.path` parameter as a
[search path for firmwares](https://www.kernel.org/doc/html/latest/driver-api/firmware/fw_search_path.html).
Since version 2.0.0, KMM workers can set that value on nodes by writing to sysfs, before attempting to load kmods.  
To enable that feature, set `worker.setFirmwareClassPath` to `/var/lib/firmware` in the
[operator configuration](configure.md#workersetfirmwareclasspath).  
