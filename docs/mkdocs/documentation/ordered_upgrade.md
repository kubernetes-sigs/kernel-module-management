# Ordered upgrade of kernel module without reboot

Sometimes customer needs to update the kernel module code for a specific version,
due to new feature added to the kernel module's code, or a bug fix was pushed
into the source of the kernel module.
The upgrade should not require node reboot.
It should be executed sequentially, one node at a time,
so as not to disturb the workload of the cluster. The order of the 
upgrade is managed by the admin/user.

## Prerequisite

In order to successfully upgrade a kernel module to a new version, the currently loaded kernel module
should be unloaded, and a new kernel module file should be loaded. That means that all user-mode 
application/workloads accessing kernel module should be deleted prior to attempting to unload kernel 
module. In k8s cluster there are 2 type of workloads that can access KMM kernel module:
1. user/customer workload: application running on k8s cluster that is accessing kernel module. It is
   deployed by the customer/admin, and it is customer/admin responsibility to make sure that the workload
   is not running on the node prior to kernel module unloading, and to make sure that the workload
   is back running on the node once the new kernel module version has been loaded
2. device plugin: managed by KMM operator, that will also manage the unloading/loading of the device plugin
   on a node being upgraded

## Upgrade flow

1. in case customer wants to use an ordered upgrade, he needs to use:
   `kmm.node.kubernetes.io/version-module-<module-namespace>-<module-name>=$moduleVersion` label on all the nodes that
   will be used by the kernel module. `$moduleVersion` value should be the same as the `version`
   field in the KMM CR. 
2. customer needs to update the appropriate KMM CR. The following fields need to be updated:
   - `containerImage` (for appropriate kernel version)
   - `version`
   the update should be atomic: both `containerImage` and `version` fields should be updated simultaneously
2. customer has to remove their workload on the node that should be upgraded
3. customer removes `kmm.node.kubernetes.io/version-module-<module-namespace>-<module-name>` on the node,
   which will trigger unloading the current kernel module
4. customer executes additional upgrading actions on the node (if need be)
5. customer adds `kmm.node.kubernetes.io/version-module-<module-namespace>-<module-name>=<module version>` label on the node.
   <module version> value should be the new Version field value set in the CR (item 2).
6. customer restores their workload on the node.

In case customer does not need to to execute additional upgrading actions (point 4), then all he needs to do is just to 
change the `kmm.node.kubernetes.io/version-module-<module-namespace>-<module-name>=$moduleVersion` label.
Meaning: steps 3,4 and 5 are unified into one step: customer changes `kmm.node.kubernetes.io/version-module-<module-namespace>-<module-name>`
label value to new `$moduleVersion`, as the one set in the KMM CR.

## Inner implementation

### Components:

1. Dedicated controller that watches for `kmm.node.kubernetes.io/version-module-<module-namespace>-<module-name>`. Once it is notified of a change in that label,
   it will use inner labels: `beta.kmm.node.kubernetes.io/version-module-loader-<module-namespace>-<module-name>` and
   `beta.kmm.node.kubernetes.io/version-device-plugin-<module-namespace>-<module-name>` to initiate the upgrade process
2. ModuleLoader and DevicePlugin daemonsets now have additional node selector value - `beta.kmm.node.kubernetes.io/version-module-loader--<module-namespace>-<module-name>`
   and `kmm.node.kubernetes.io/version-device-plugin-<module-namespace>-<module-name>` accordingly. Manipulation of those labels in the nodes will allow the above mentioned
   controller to remove/add pods on the node
3. upon change of the Version field in the Module CR, a new ModuleLoader and DevicePlugin daemonsets will be created with appropriate labels in the `nodeSelector` field

### Flow

1. In case the `version` field was changed in the `Module` CR, new ModuleLoader and DevicePlugin daemonsets are created with appropriate labels:
   `beta.kmm.node.kubernetes.io/version-module-loader-<module-namespace>-<module-name>=$moduleVersion` and 
   `beta.kmm.node.kubernetes.io/version-device-plugin-<module-namespace>-<module-name>=$moduleVersion` set in the `nodeSelector`s
2. Once "version label controller" detects change/removal of the `kmm.node.kubernetes.io/version-module-<module-namespace>-<module-name>` label on the node,
   it will remove `beta.kmm.node.kubernetes.io/version-device-plugin-<module-namespace>-<module-name>` from the node,
   which will cause the device-plugin pod to be removed
4. Once device plugin is removed, the controller will remove the `beta.kmm.node.kubernetes.io/version-module-loader-<module-namespace>-<module-name>` label,
   which will cause the current version of the ModuleLoader pod to be removed
5. Once the current ModuleLoader pod is removed, the controller will set `beta.kmm.node.kubernetes.io/version-module-loader-<module-namespace>-<module-name>=$moduleVersion`
   on the node which will cause the new ModuleLoader pod to start on the controller. 
6. Once the new ModuleLoader pod is running, the controller will set `beta.kmm.node.kubernetes.io/version-device-plugin-<module-namespace>-<module-name>=$moduleVersion` label
   on the node, which will cause the DevicePlugin pod to start again on the node.
