# Use cases

## A new module is added to the cluster
The operator lists all nodes required to build the module and looks at their kernels.
It uses the `Module`’s `.spec.kernelMappings` to create `DaemonSets` for each of those kernels.

If an image for one of these `DaemonSets` requires an in-cluster build, it creates the build object (build system TBD).
The `Module` owns the build object, so that it is deleted when the `Module` is deleted.

## Module update: kernel `$kernel` should run a new DriverContainer image
The OOTO would be triggered by an update to any `Module` instance.
That reconciliation generates a list of `DaemonSets` that need to be reconciled.

For each `DaemonSet` belonging to `Module`, we set the following labels:

| key                                          | value         |
|----------------------------------------------|---------------|
| `ooto.sigs.k8s.io/module-name`               | `$moduleName` |
| `oot.node.kubernetes.io/kernel-version.full` | `$kernel`     |

We either create those `DaemonSets` (if they do not already exist) or update them (if a `DaemonSet` already exists with 
the same labels).
If a `DaemonSet` already exists for `$moduleName` and `$kernel`, it is updated with the new DriverContainer image.
If that image requires a build, we create the build object and wait for its completion before we modify the `DaemonSet`.

## A new node joins the cluster (or is modified)
The OOTO lists all `Modules` and checks which ones should run on the node.  
For each `Module` that should run on the node (determined by the `.spec.selector` field), the operator tries to find
a suitable DriverContainer image for the node’s kernel.  
It creates a `DaemonSet` that runs that DriverContainer on all nodes running that kernel.

## A node leaves the cluster
If that node was targeted by one of the `DaemonSets`, said `DaemonSet`’s `.status.desiredNumberScheduled` is
decremented.  
The operator watches changes to those objects; if that field reaches 0, the `DaemonSet` is deleted.

## A node’s kernel is updated
Depending on the Kubernetes distribution used, this is functionally equivalent to either:

- Deleting the node and creating a new one, or;
- Modifying an existing node.

That use case is thus covered above.

## A node matches a Module’s .spec.selector, but not any of its kernel mappings
No DaemonSet is scheduled for this module and this kernel. The OOT kernel module will not be deployed on that node.
