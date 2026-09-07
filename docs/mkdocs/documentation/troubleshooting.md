# Troubleshooting

## Reading logs

In the commands below, the value of `$namespace` depends on your [installation method](install.md).

### Operator

| Component | Command                                                                 |
|-----------|-------------------------------------------------------------------------|
| KMM       | `kubectl logs -fn "$namespace" deployments/kmm-operator-controller`     |
| KMM-Hub   | `kubectl logs -fn "$namespace" deployments/kmm-operator-hub-controller` |

### Webhook server

| Component | Command                                                                     |
|-----------|-----------------------------------------------------------------------------|
| KMM       | `kubectl logs -fn "$namespace" deployments/kmm-operator-webhook`     |
| KMM-Hub   | `kubectl logs -fn "$namespace" deployments/kmm-operator-hub-webhook` |

## Observing events

### Build & Sign

KMM publishes events whenever it starts a kmod image build or observes its outcome.  
Those events are attached to `Module` objects and are available at the very end of `kubectl describe module`:

```text
$> kubectl describe modules.kmm.sigs.x-k8s.io kmm-ci-a
[...]
Events:
  Type    Reason          Age                From  Message
  ----    ------          ----               ----  -------
  Normal  BuildCreated    2m29s              kmm   Build created for kernel 6.6.2-201.fc39.x86_64
  Normal  BuildSucceeded  63s                kmm   Build job succeeded for kernel 6.6.2-201.fc39.x86_64
  Normal  SignCreated     64s (x2 over 64s)  kmm   Sign created for kernel 6.6.2-201.fc39.x86_64
  Normal  SignSucceeded   57s                kmm   Sign job succeeded for kernel 6.6.2-201.fc39.x86_64
```

### Module load or unload

KMM publishes events whenever it successfully loads or unloads a kernel module on a node.  
Those events are attached to `Node` objects and are available at the very end of `kubectl describe node`:

```text
$> kubectl describe node my-node
[...]
Events:
  Type    Reason          Age    From  Message
  ----    ------          ----   ----  -------
[...]
  Normal  ModuleLoaded    4m17s  kmm   Module default/kmm-ci-a loaded into the kernel
  Normal  ModuleUnloaded  2s     kmm   Module default/kmm-ci-a unloaded from the kernel
```

## A Module that stays in `Terminating`

KMM holds a `Module` until the resources it created for that `Module` have gone.
Deleting a `Module` therefore does not remove it straight away.

| Finalizer                                      | Waits for                                                      |
|------------------------------------------------|----------------------------------------------------------------|
| `kmm.node.kubernetes.io/module-finalizer`      | no `NodeModulesConfig` still lists the kernel module as in use  |
| `kmm.node.kubernetes.io/dra-cleanup`           | the DRA DaemonSets, their Pods and the `DeviceClass` objects    |
| `kmm.node.kubernetes.io/device-plugin-cleanup` | the device plugin DaemonSets, their Pods and their node labels  |

The finalizers still on the object say which of those has not finished:

```shell
kubectl get module "$name" -n "$namespace" -o jsonpath='{.metadata.finalizers}'
```

A `Module` that keeps `dra-cleanup` is often waiting on a `DeviceClass` that cannot
finish being deleted. That one is worth checking first, because a `DeviceClass` is
cluster scoped and is not garbage collected along with the `Module`:

```shell
kubectl get deviceclass -l "kmm.node.kubernetes.io/module.name=$name,kmm.node.kubernetes.io/module.namespace=$namespace" -o yaml
```

A `Module` that keeps `device-plugin-cleanup` is usually waiting on a Pod. The
DaemonSet is deleted in the foreground, so it stays listed with a deletion
timestamp until the Pods it owns have gone, and a Pod goes only once its
`NodeLabelerFinalizer` has been released.

The operator logs what each pass is still waiting for, under `Waiting for the ...
resources to go before releasing the Module`: the DaemonSet count, whether any Pod
is still there (`podsLeft`), and for DRA the DeviceClass count.

Removing a finalizer by hand leaves those resources behind with nothing left to
clean them up, so prefer clearing whatever is holding the resource itself.
