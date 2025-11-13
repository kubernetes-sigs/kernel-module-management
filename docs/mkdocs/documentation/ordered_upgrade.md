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

### Indicator that the new version is ready to be used

The operator will label the node with a "version.ready" label to indicate that the new version of the kernel module is loaded
and ready to be used:
`kmm.node.kubernetes.io/<module-namespace>.<module-name>.version.ready=<module-version>`

## Implementation details

### Components

1. A dedicated controller in the operator watches
   `kmm.node.kubernetes.io/version-module.<module-namespace>.<module-name>` labels on nodes.
   When it detects a change in the value of that label, it sets the following internal labels to initiate the upgrade
   process:
     - `beta.kmm.node.kubernetes.io/version-worker-pod.<module-namespace>.<module-name>`
     - `beta.kmm.node.kubernetes.io/version-device-plugin.<module-namespace>.<module-name>`
2. The operator manipulates its internal `NodeModulesConfig` resources to create worker Pods that unload the current
   version of a module and load the new one.
3. The operator creates new device plugin DaemonSets with a new node selector component:
   `kmm.node.kubernetes.io/version-device-plugin.<module-namespace>.<module-name>`
   This makes it possible to terminate the device plugin Pod before unloading the current module, and to create them
   again when the new module is loaded.

### Flow

1. When the `version` field is modified in the `Module`, a new device plugin DaemonSet is created with the following
   internal node selector:
   `beta.kmm.node.kubernetes.io/version-device-plugin.<module-namespace>.<module-name>=$moduleVersion`
2. When the `kmm.node.kubernetes.io/version-module.<module-namespace>.<module-name>` label is changed on the node, KMM's
   version label controller removes the
   `beta.kmm.node.kubernetes.io/version-device-plugin.<module-namespace>.<module-name>` label, which terminates the
   device plugin Pod;
3. Once the device plugin Pod is terminated, the controller removes the
   `beta.kmm.node.kubernetes.io/version-worker-pod.<module-namespace>.<module-name>` label, which removes the entry from
   the corresponding `NodeModulesConfig` resource.
   This results in a worker Pod being created to unload the current kernel module.
4. Once the current kernel module is unloaded, the controller sets the
   `beta.kmm.node.kubernetes.io/version-worker-pod.<module-namespace>.<module-name>=$moduleVersion` label on the
   node, which adds back an updated entry into the corresponding `NodeModulesConfig` resource.
   This results in a worker Pod being created to load the new kernel module.
5. Once the new kernel module is loaded, the controller sets the
   `beta.kmm.node.kubernetes.io/version-device-plugin.<module-namespace>.<module-name>=$moduleVersion` label on the
   node, which will cause the device plugin Pod to start again.
