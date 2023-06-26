# Ordered upgrade of kernel module without reboot

It is sometimes desirable to upgrade the kernel module on a node without rebooting it.
To minimize the impact on the workload running in the cluster, the upgrade process should be executed sequentially, one
node at a time.  
Because it requires knowledge of workload utilizing the kernel module, this process is managed by the cluster
administrator.

## Prerequisite

In order to successfully upgrade a kernel module to a new version, the currently loaded kernel module should be
unloaded, and a new kernel module file should be loaded.
This means that all user-mode application/workloads using the kernel module should be terminated before attempting to
unload the kernel module.  
There are 2 type of workloads that can use a kernel module:

- user workload: application running in the cluster that is accessing kernel module.
  It is deployed by the administrator, and it is their responsibility to make sure that the workload is not running on
  the node prior to kernel module unloading, and to make sure that the workload is back running on the node once the new
  kernel module version has been loaded.
- device plugin managed by KMM.
  The operator will manage the unloading/loading of the device plugin on a node being upgraded.

Due to Kubernetes limitations in label names, the combined length of `Module` name and namespace may not exceed 39
characters.

## Upgrade steps

1. To run an ordered upgrade, set the
   `kmm.node.kubernetes.io/version-module.<module-namespace>.<module-name>=$moduleVersion` label on all the nodes that
   will be used by the kernel module.
   `$moduleVersion` value should be equal to the value of the `version` field in the `Module`. 
2. The `Module` can now be upgraded.
   Update the following fields:
     - `containerImage` (to appropriate kernel version)
     - `version`  
   The update should be atomic: both `containerImage` and `version` fields must be updated simultaneously.

3. Terminate any workload using the kmod on the node being upgraded.
4. Remove the `kmm.node.kubernetes.io/version-module.<module-namespace>.<module-name>` label on the node.
   This will unload the kernel module from the node.
5. If required, perform any additional maintenance required on the node for the kmod upgrade.
6. Add the `kmm.node.kubernetes.io/version-module.<module-namespace>.<module-name>=$moduleVersion` label to the node.
   `$moduleVersion` should be equal to the new value of the `version` field in the `Module` (after step 2).
7. Restore any workload that would leverage the kernel module on the node.

In case no additional upgrading action (step 4) is needed, then the process boils down to changing the
`kmm.node.kubernetes.io/version-module.<module-namespace>.<module-name>=$moduleVersion` label.
Steps 3, 4 and 5 are then unified into one step: update the
`kmm.node.kubernetes.io/version-module.<module-namespace>.<module-name>` label value to new `$moduleVersion` as set in
the `Module`.

## Inner implementation

### Components:

1. A controller watches for `kmm.node.kubernetes.io/version-module.<module-namespace>.<module-name>`.
   Once it that label is modified on a node, it uses the internal
   `beta.kmm.node.kubernetes.io/version-module-loader.<module-namespace>.<module-name>` and
   `beta.kmm.node.kubernetes.io/version-device-plugin.<module-namespace>.<module-name>` labels to initiate the upgrade
   process.
2. ModuleLoader and DevicePlugin DaemonSets now have additional node selector value:
  `beta.kmm.node.kubernetes.io/version-module-loader.<module-namespace>.<module-name>`
   and `kmm.node.kubernetes.io/version-device-plugin.<module-namespace>.<module-name>` accordingly.
   Manipulation of those labels in the nodes will allow the controller to remove the old pod from the node and add the
   new one.
3. Upon change to the `version` field in the `Module`, new ModuleLoader and DevicePlugin DaemonSets will be created with
   the appropriate labels in the `nodeSelector` field.

### Flow

1. In case the `version` field was modified in the `Module`, new ModuleLoader and DevicePlugin DaemonSets are created
   with the appropriate node selectors:
   `beta.kmm.node.kubernetes.io/version-module-loader.<module-namespace>.<module-name>=$moduleVersion` and 
   `beta.kmm.node.kubernetes.io/version-device-plugin.<module-namespace>.<module-name>=$moduleVersion`.
2. Once the "version label controller" detects change/removal of the
   `kmm.node.kubernetes.io/version-module.<module-namespace>.<module-name>` label on the node,
   it removes the `beta.kmm.node.kubernetes.io/version-device-plugin.<module-namespace>.<module-name>` label from the
   node, which terminates the device-plugin pod.
3. Once the device plugin pod is terminated, the controller removes the
   `beta.kmm.node.kubernetes.io/version-module-loader.<module-namespace>.<module-name>` label, which causes the current
   version of the ModuleLoader pod to be removed.
4. Once the current ModuleLoader pod is removed, the controller sets the
   `beta.kmm.node.kubernetes.io/version-module-loader.<module-namespace>.<module-name>=$moduleVersion` label on the
   node, which causes the new ModuleLoader pod to start.
5. Once the new ModuleLoader pod is running, the controller sets the
   `beta.kmm.node.kubernetes.io/version-device-plugin.<module-namespace>.<module-name>=$moduleVersion` label on the
   node, which will cause the DevicePlugin pod to start again.
