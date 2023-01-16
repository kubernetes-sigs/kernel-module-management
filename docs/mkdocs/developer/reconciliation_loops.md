# Reconciliation loop

## Modules

Each time a new `Module` is created, we need to find to which nodes it applies.

1. A first filtering is performed using the `.spec.selector` field, then we go through the module’s kernel mappings to find a container image that matches the node’s kernel.

1. We end up with a certain number of (kernel, image) pairs; for each of these pairs, there should be a `DaemonSet`.

1. We first look for a `DaemonSet` that would already be targeting the same kernel and DriverContainer image (that data is stored in the `DaemonSet`’s labels).

1. If there is already such a `DaemonSet`, we patch it, if needed.

1. If there is not already a matching `DaemonSet`, we create it and set the `Module` as owner.

1. When a `Module` is deleted, we have nothing to do: because we set it as owner of all `DaemonSets`, Kubernetes garbage
   collection will take care of deleting them.

1. We watch `Module`, owned `DaemonSet` and build objects as well as nodes to make sure that we are not missing any change
   in the cluster.

![Modules reconciliation](diagrams/reconciliation-module.png)
