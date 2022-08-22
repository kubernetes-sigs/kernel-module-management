# How to build module-loaders to be used by KMMO

### Pre-build module-loaders

When a `Module` CR is applied to the cluster, KMMO will create a DaemonSet to run the module-loader specified in it.

In order for module-loaders that depend on in-tree kernel-modules to work, KMMO is mounting `/lib/modules/${KVER}` from the node to the container, which means that it will override all files located in `/lib/modules/${KVER}` inside the module-loader.

Therefore, when building a module-loader that is going to be used by KMMO, there are few things that needs to be done in the module-loader:
* Put the `*.ko` files in `/opt/lib/modules/${KVER}` instead of `/lib/modules/${KVER}`
* Link `/lib/modules/${KVER}` inside `/opt/lib/modules/$(KVER)/system` in case the module-loader depend on in-tree kernel-modules
* Run `depmod -b /opt` in order to generate the dependency file correctly
