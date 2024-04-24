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
| KMM       | `kubectl logs -fn "$namespace" deployments/kmm-operator-webhook-server`     |
| KMM-Hub   | `kubectl logs -fn "$namespace" deployments/kmm-operator-hub-webhook-server` |

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
