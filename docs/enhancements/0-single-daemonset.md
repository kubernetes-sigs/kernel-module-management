# Single DaemonSet for KMM

Authors: @qbarrand, @mresvanis, @yevgeny-shnaidman

## Introduction

This enhancement aims at eliminating several pain points related to how KMM loads kernel modules on the nodes.

### Resource utilization

KMM currently creates one DaemonSet per `Module` and compatible kernel version found in the cluster.
Formally, the number of DaemonSets created $S$ is defined by:
$$S = \sum_{i=1}^nk_i$$

where:
- $n$ is the number of `Modules` in the cluster, and;
- $k_i$ is the number of kernel versions running in the cluster that `Module` $i$ is compatible with.

A big $S$ leads to unnecessary workload for the cluster:
- on nodes, one ModuleLoader Pod is created per `Module`. Although those just sleep after the `modprobe`
  `postStart` hook, each running pod incurs an overhead for the container runtime and the kubelet;
- on the control-plane, a higher number of DaemonSets and Pods creates more events for kube-controller-manager, which
  trigger more reconciliations and status updates.

### Reliability

Running user-provided ModuleLoader images as DaemonSets makes it difficult for KMM to determine if a module was
successfully unloaded or not.  
KMM v1 uses the `preStop` hook to run `modprobe -r` and unload modules.
`preStop` hook failures cannot prevent the Pod from being terminated.
If `modprobe` cannot to unload the module for any reason (e.g. userspace applications still using it), then the Pod will
be terminated anyway and the `kmm.node.kubernetes.io/$namespace.$module.ready` label will be removed from the Node, even
though the module is still loaded.

## Goals

1. Reduce the overall KMM footprint
2. Improve the reliability of module loading and unloading

## Non-goals

1. Add further control on the deployment of kmods on nodes

## Design

### KMM: one DaemonSet pulling all kmod images

This proposal reduces the number of DaemonSets run by KMM to one.
KMM is now composed of two components, communicating over the network:
- the control-plane is the current operator Deployment;
- the daemon pods belong to a new DaemonSet that is created and owned by the control-plane Deployment.

After it is installed, the operator creates a DaemonSet targeting on all worker nodes.
The daemon pods run a new KMM component that pulls kmod images, extracts them locally and loads / unloads kmods.

```
                                                     ┌─────────────────────────────────────────┐
                                                     │ Random namespace                        │
                                                     │                                         │
                          gets                       │            ┌─────────────┐              │
                        ┌────────────────────────────┼────────────► Pull secret │              │
                        │                            │            └───────▲─────┘              │
                        │         reconciles         │                    │                    │
                        │   ┌────────────────────────┼────┐               │                    │
                        │   │                        │    │               │                    │
                        │   │                        │  ┌─▼──────┐        │                    │
                        │   │                        │  │ Module ├────────┘                    │
                        │   │                        │  └────────┘  references                 │
                        │   │                        │                                         │
┌───────────────────────┼───┼───────────────────┐    │                                         │
│ KMM namespace         │   │                   │    │                                         │
│                       │   │                   │    │                    ┌───────────┐        │
│                       │   │                   │    │   ┌────────────────► Build Job │        │
│                       │   │                   │    │   │                └─────┬─────┘        │
│           ┌───────────┴───┴───┐   creates &   │    │   │                      │              │
│           │ KMM control-plane │      owns     │    │   │                      │              │
│           │   (Deployment)    ├───────────────┼────┼───┤  ┌─────────────┐     │              │
│           └────▲────▲────▲────┘               │    │   └──► Signing Job │     │              │
│                │    │    │                    │    │      └─────────┬───┘     │              │
│                │    │    │                    │    │                │         │              │
│                │    │    │                    │    └────────────────┼─────────┼──────────────┘
│                │    │    │                    │                     │         │
│                │    │    │                    │                     │         │
│                │    │    │                    │                     │ push to │
│                │   g│    │                    │                     │         │
│          grpc  │   r│    │    grpc            │                     │         │
│         ┌──────┘   p│    └───────────┐        │                     │         │
│         │          c│                │        │                     │         │
│         │           │                │        │               ┌─────▼─────────▼──────┐
│         │           │                │        │               │ registry.company.com │
│         │           │                │        │               └───────────▲──────────┘
│         │           │                │        │                           │
│  ┌──────▼─────┐ ┌───▼────────┐ ┌─────▼──────┐ │                           │
│  │ Daemon pod │ │ Daemon pod │ │ Daemon pod │ │                           │
│  │ Node 0     │ │ Node 1     │ │ Node 2     │ │                           │
│  └────────┬───┘ └────────┬───┘ └───────┬────┘ │                           │
│           │              │             │      │                           │
└───────────┼──────────────┼─────────────┼──────┘                           │
            │              │             │                                  │
            └──────────────┴─────────────┴──────────────────────────────────┘
                                      pull from
```

#### Control-plane

The control-plane watches all resources in the cluster that are necessary to determine which module needs to be loaded
on which node.
In addition to its current duties such as build, signing and node labeling, it also listens for connections from daemon
pods.
Once the connection is established, the control-plane maintains a map of the desired state of each node with daemon Pod
connections, allowing it to notify a specific Pod when that state is modified and some action is needed.  
To reduce the amount of computation for each reconciliation, we may decide to persist the desired state of each node
into a new Custom Resource.
The `Module` and `Node` reconcilers would update that resources, and a new, separate reconciler would watch it and
notify the corresponding daemon Pod when an update is required.

#### Daemon Pods

The DaemonSet for the daemon Pods is created and owned by the control-plane.
Daemon pods are running a new KMM image that contains both `modprobe` and a new binary that runs `modprobe` and
communicates with the control-plane.
They get their configuration from the control-plane over a network connection and notify it of any event, such as
successful or failed kmod loading.  
The daemon Pod configuration contains everything a daemon Pod needs to load a module:
- container image name;
- `modprobe` configuration, such as the module name or the additional arguments;
- pull secret, if specified;
- module loading order, if specified;
- list of firmware files, if specified.

When instructed to by the control-plane, daemon pods keep trying to load or unload kernel modules.
The control-plane only adds or removes the `kmm.node.kubernetes.io/$namespace.$module.ready` label when it receives the
appropriate success event from the daemon pods, which guarantees a consistent label.

Locally, daemon Pod should maintain a state with the same lifetime as the kernel modules it loads.
It is desirable for this state to persist during application restarts, but not node reboots, because by definition all
KMM-loaded kmods are gone as well.  
We propose to write that state in a temporary filesystem such as `/run`, which:
- is not garbage-collected at runtime like `/tmp` is;
- is emptied during reboots.

#### Connectivity

The control-plane and the daemon Pods communicate over the network, using [gRPC] over TLS.  
Although connections are initiated by the daemon Pods, communication is bi-directional:
- daemon Pods send request their config from and send events to the control-plane;
- the control-plane notifies daemon Pods by sending updates.

This can be achieved with either a combination of
[Unary RPCs](https://grpc.io/docs/what-is-grpc/core-concepts/#unary-rpc) and
[Server streaming RPC](https://grpc.io/docs/what-is-grpc/core-concepts/#server-streaming-rpc), or
[Bidirectional streaming RPCs](https://grpc.io/docs/what-is-grpc/core-concepts/#bidirectional-streaming-rpc).

#### Security

##### Preventing external connections to the control-plane

The control-plane runs sensitive tasks, such as node labeling, and distributes secret data, such as pull secrets, to the
daemon pods.  
To make sure that only daemon Pods can communicate with the control-plane, we leverage
[cert-manager's CSI Driver](https://cert-manager.io/docs/usage/csi/) to authenticate both components.  
Further restrictions may be configured at the TCP level using a
[Network Policy](https://kubernetes.io/docs/concepts/services-networking/network-policies/); unfortunately not all
[network plugins](https://kubernetes.io/docs/concepts/extend-kubernetes/compute-storage-net/network-plugins/) support
them.

##### Restricting daemon Pods' privileges

Daemon Pods run on all nodes.
If one of those nodes is compromised, then an attacker could use the Pod's credentials against the API Server for
malicious purposes.
We intend to run the daemon Pods with a dedicated ServiceAccount with zero API Server privileges.
The only sensitive data that the daemon Pod will potentially be processing is pull secrets from other namespaces, which
will only be used to pull the kmod image once and will not be persisted in any way.

### Hub & Spoke: one proxy in each Spoke

In Hub & Spoke setups, we introduce a third KMM component, the proxy, which is both a [gRPC] proxy for the daemon pods
and a controller providing doing everything the Hub cannot do remotely:
- label nodes when a module is successfully loaded;
- notify the Hub of any change in the Nodes' kernel versions.

```
     ┌───────────────────────────────┐
     │ Hub                           │
     │                               │
     │  ┌──────────────────────┐     │
     │  │ ManagedClusterModule │     │
     │  └──────────▲───────────┘     │
     │             │                 │
     │             │                 │
     │             │watches          │
     │             │                 │
     │        ┌────┴────┐            │
┌────┼────────► KMM-Hub │            │
│    │        └────┬────┘            │
│    │             │                 │
│    │             │ creates & owns  │
│    │             │                 │
│    │       ┌─────┴─────┐           │
│    │       │           │           │
│    │       │     ┌─────▼─────┐     │
│    │       │     │ Build Job ├─────┼─┐
│    │       │     └───────────┘     │ │         ┌──────────────────────┐
│    │       │                       │ ├─────────► registry.company.com ◄─────┐
│    │  ┌────▼─────┐                 │ │ push to └──────────────────────┘     │
│    │  │ Sign Job ├─────────────────┼─┘                                      │
│    │  └──────────┘                 │                                        │
│    │                               │                              pull from │
│    └───────────────────────────────┘                                        │
│                                                                             │
│                 ┌────────────────────────────────────────────────────────┐  │
│                 │ Spoke                                                  │  │
│                 │              ┌───────┐                                 │  │
└─────────────────┼──────────────► Proxy ◄─────────────┐                   │  │
  pushes node     │              └───┬───┘             │  ┌──────────────┐ │  │
  data and gets   │                  │                 ├──► Daemon pod 0 ├─┼──┤
  daemon config   │                  │                 │  └──────────────┘ │  │
                  │                  │                 │                   │  │
                  │        watches & │                 │  ┌──────────────┐ │  │
                  │        labels    │                 ├──► Daemon pod 1 ├─┼──┤
                  │                  │                 │  └──────────────┘ │  │
                  │                  │                 │                   │  │
                  │        ┌─────────┼─────────────┐   │  ┌──────────────┐ │  │
                  │        │         │             │   └──► Daemon pod 2 ├─┼──┘
                  │        │         │             │      └──────────────┘ │
                  │        │         │             │                       │
                  │   ┌────▼───┐  ┌──▼─────┐  ┌────▼───┐                   │
                  │   │ Node 0 │  │ Node 1 │  │ Node 2 │                   │
                  │   └────────┘  └────────┘  └────────┘                   │
                  │                                                        │
                  └────────────────────────────────────────────────────────┘
```

The daemon Pods - proxy and proxy - KMM-Hub communication channels are powered by [gRPC].

We will target the Open Cluster Management [Add-on](https://open-cluster-management.io/concepts/addon/) model and
framework to deploy and manage the full solution.  
KMM-Hub would act as the addon manager, while the proxy would be the agent.

## Additional benefits

In addition to addressing the goals outlined at the beginning of this document, this proposal brings the following
benefits:
- reduced requirements on the images provided by kmod vendors: we no longer require images to bundle the `sleep` and
  `modprobe` binaries;
- eliminated risk for incompatible / buggy `modprobe` & `sleep` binaries in the kmod image;
- improved robustness for the firmware copy feature by providing better error messages and making it possible to detect
  conflicts across `Modules`;
- immediate pod termination as the daemon application can be setup to handle signals properly.

## API changes

We foresee no API change for this enhancement.

## Semantics

We propose to move away from "ModuleLoader images" to "kernel module images" or "kmod images", since the user-provided
images are only a container for the `.ko` files and are not loading the modules themselves anymore.

## Alternatives considered

### Daemon Pods running a small controller

In that design, instead of delegating all communication with the API Server to the control-plane, each daemon Pod would
run a small controller watching `Module` resources and their own Node to determine what kmod they have to download and
load.
Instead of communicating over a [gRPC] channel, the daemon Pods would notify the control-plane over the status of a new
custom resource.  
However the current `Module` API allows users to reference Secret objects as pull secrets in the `Module`'s namespace.
The daemon pods are all running in the KMM namespace, and thus we would have to run them with a ServiceAccount that
has permissions to `get` all Secrets in all namespaces.
That makes any privileged workload on any node in the cluster that runs the KMM daemon pod able to get any Secret; it is
thus undesirable.

<!-- Links -->

[gRPC]: https://grpc.io/