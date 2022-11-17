# Kernel Module Management Lab 

Kernel Module Management a.k.a KMM manages out of tree kernel modules in Kubernetes.

By installing the controller in a Kubernetes cluster we will get a new Namespace where a Deployment will run and create a ReplicaSet.

Once the ReplicaSet is running we can create and use a new Module object which will manage the build and/or run of a kernel module.

In this document we will find examples and YAML files including in-cluster module building, device plugins and use of module loaders with KMM for different Kubernetes environments : [EKS](#elastic-kubernetes-service-eks), [GKE](#google-kubernetes-engine-gke), [Azure](#microsoft-azure-aks), [Ubuntu bare metal](#ubuntu-bare-metal-cluster). 

## Installation

Please refer to https://github.com/kubernetes-sigs/kernel-module-management/blob/main/README.md#getting-started

## Kubernetes platforms tested on this lab :

- EKS Cluster in Amazon Web Services
- GKE Cluster in Google Cloud Platform
- AKS Cluster in Microsoft Azure
- Vanilla Kubernetes with Ubuntu Nodes

###  Elastic Kubernetes Service EKS

Elastic Kubernetes Service(EKS) at AWS run by default worker nodes based on Amazon Linux v2 (Ubuntu and maybe others could also be used and examples will be provided in such directory).
Kernel Management Module deploys at nodes labeled as `node-role.kubernetes.io/master=` or `node-role.kubernetes.io/control-plane=` depending on which Kubernetes version we are running, but Control Plane (master) in EKS nodes do not allow custom workloads per design as these nodes are managed by AWS.
So as a user workaround we could label worker nodes with said key to make deployment work.

As underlying node OS is [Amazon EKS Linux](https://github.com/awslabs/amazon-eks-ami) which is based in Amazon Linux v2, using amazonlinux images as builder images is the easiest way to match kernel versions between hosts and builders. Also Amazon Linux repositories keep a pretty extensive package archive of different headers versions which is really useful when dealing with not so updated EKS nodes.

At sometime in a near future AL2022 wil be released as an official Amazon Linux OS. It will be based on Fedora Linux and will have SELinux enabled by default so probably changes should be made to examples accordingly:
[Amazon Linux 2022](https://docs.aws.amazon.com/linux/al2022/release-notes/planned-changes.html)

EKS Linux AMI versions always stick with a specific kernel version that can be checked [here](https://github.com/awslabs/amazon-eks-ami/blob/master/CHANGELOG.md).

These are Kernel and other relevant software versions for EKS AMI Linux from Kubernetes 1.22:

**EKS AMI Linux VERSIONS** 
|      AMI      |kubelet|      Docker     |       Kernel        |   Packer    |  containerd  |
|:--------------|:------:|:--------------:|:-------------------:|:-----------:|-------------:|
|1.22.9-20220629| 1.22.9 |20.10.13-2.amzn2|5.4.196-108.356.amzn2|  v20220629  |1.4.13-3.amzn2|
|1.22.9-20220620| 1.22.9 |20.10.13-2.amzn2|5.4.196-108.356.amzn2|  v20220620  |1.4.13-3.amzn2|
|1.22.9-20220610| 1.22.9 |20.10.13-2.amzn2|5.4.196-108.356.amzn2|  v20220610  |1.4.13-3.amzn2|
|1.22.6-20220526| 1.22.6 |20.10.13-2.amzn2|5.4.190-107.353.amzn2|  v20220526  |1.4.13-2.amzn2.0.1|
|1.22.6-20220523| 1.22.6 |20.10.13-2.amzn2|5.4.190-107.353.amzn2|  v20220523  |1.4.13-2.amzn2.0.1|
|1.22.6-20220429| 1.22.6 |20.10.13-2.amzn2|5.4.188-104.359.amzn2|  v20220429  |1.4.13-2.amzn2.0.1|
|1.22.6-20220421| 1.22.6 |20.10.13-2.amzn2|5.4.188-104.359.amzn2|  v20220421  |1.4.13-2.amzn2.0.1|
|1.22.6-20220420| 1.22.6 |20.10.13-2.amzn2|5.4.188-104.359.amzn2|  v20220420  |1.4.13-2.amzn2.0.1|
|1.22.6-20220406| 1.22.6 |20.10.13-2.amzn2|5.4.181-99.354.amzn2 |  v20220406  |1.4.13-2.amzn2.0.1|
|1.22.6-20220317| 1.22.6 |20.10.7-5.amzn2 |5.4.181-99.354.amzn2 |  v20220317  |1.4.6-8.amzn2|

EKS can also be used with custom node groups based on [Ubuntu](https://cloud-images.ubuntu.com/docs/aws/eks/). In this case we could use Ubuntu 18.04 or Ubuntu 22.04 in our build step at the KMM CRD as a Docker image, as most likely we will find the kernel headers package matching the nodes within the distribution repository:

```console
apt update && apt-cache search linux-headers | grep aws
```

Please refer the [Ubuntu EKS](aws/ubuntu-kmm-kmod.yaml) or [Amazon Linux EKS](aws/kmm-kmod.yaml) examples for further details.
 
### Google Kubernetes Engine GKE

AutoPilot Clusters do limit which NodeSelectors and Linux Capabilities may be used in deployments, so deployments of KMMO in GKE platform should be done in standard clusters.

In Standard GKE clusters Control Plane (master) is administered by Google so labeling and/or user workloads are not allowed in those nodes. Therefore labeling a worker node as `node-role.kubernetes.io/master=` is needed before deploying the controller manager.

Underlying OS in nodes running at GKE is Container-Optimized OS by default but it could be Ubuntu also.

As stated in previous platform, in the case of Ubuntu Linux as node OS we should use Ubuntu Linux as the docker image in the same version running in the node for the build step in KMM CRD as it is easy to download contain the kernel headers package matching our nodes' kernel version. As of today, Ubuntu 20.04 focal LTS includes packages matching kernel version running in Ubuntu with containerd nodes at GKE. 
If you have access to your nodes you can check which Ubuntu version is currently running at your node so you can use the same Ubuntu version image for building:
```console
gcloud compute ssh gke-kmmo-lab-cluster-default-pool-073f8f9f-9z5s
```

```console
enrique@gke-kmmo-lab-cluster-default-pool-073f8f9f-9z5s:~$ lsb_release -a | grep Release
No LSB modules are available.
Release:	20.04
enrique@gke-kmmo-lab-cluster-default-pool-073f8f9f-9z5s:~$
```
This may change as newer versions of Ubuntu will be used on GKE nodes.


We could get the whole list of available linux-headers packages for GKE just by doing this:
```console
apt update && apt-cache search linux-headers | grep gke
```
The default option, Google's Container-optimized OS, comes with security settings that make it difficult to load out-of-tree kernel modules.
Using Ubuntu nodes with containerd in GKE is the straight, preferred and easier option if you are willing to use KMM.

Use case example for above can be found [here](gke/gke-ubuntu-nodes.yaml).

If worker nodes are based on the default GKE option which is COS, you will have to download the kernel sources for the same kernel version matching the node before the build step unless you find a kernel header package matching the exact same version as the one running in our nodes which is not the case today in 5.10.109 kernel used in examples. No major distribution delivers this specific kernel headers version in repositories.

In theory KMM could work in a cluster based on COS worker nodes if you could download kernel sources i.e. from Chromium OS [repository](https://chromium.googlesource.com/chromiumos/third_party/kernel/) as long as [Container-Optimized OS](https://cloud.google.com/container-optimized-os/docs/concepts/features-and-benefits) (COS) is based on the open source Chromium OS project. 
Apart from this you should disable [LoadPin](https://docs.kernel.org/admin-guide/LSM/LoadPin.html) which denies Module Loader to load kernel modules in nodes. This means you would also need to reboot worker nodes.
 
As described above, the easy and recommended way to use KMM in a GKE Google Cloud environment would be use a pool of Ubuntu based worker nodes so we could stick to build modules using kernel headers packages available in distribution repository.


**Ubuntu/COS GKE KERNEL VERSIONS**
+ 1.12.1-gke.57	v1.23.5-gke.1505	5.4.0.1049.50 (ubuntu), 5.10.123 (cos)
+ 1.12.0-gke.446	v1.23.5-gke.1504	5.4.0.1043.46 (ubuntu), 5.10.107 (cos)
+ 1.11.3-gke.45	v1.22.8-gke.204	5.4.0.1051.52 (ubuntu), 5.10.133 (cos)
+ 1.11.2-gke.53	v1.22.8-gke.204	5.4.0.1049.50 (ubuntu), 5.10.127 (cos)
+ 1.11.1-gke.53	v1.22.8-gke.200	5.4.0.1039.42 (ubuntu), 5.10.109 (cos)
+ 1.11.0-gke.543	v1.22.8-gke.200	5.4.0.1038.41 (ubuntu), 5.10.90 (cos)
+ 1.10.7-gke.15	v1.21.14-gke.2100	5.4.0.1051.52(ubuntu), 5.10.133 (cos)
+ 1.10.6-gke.36	v1.21.14-gke.2100	5.4.0.1049.50(ubuntu), 5.10.127 (cos)
+ 1.10.5-gke.26	v1.21.5-gke.1200	5.4.0.1043.46(ubuntu), 5.10.109 (cos)
+ 1.10.4-gke.32	v1.21.5-gke.1200	5.4.0.1039.42(ubuntu), 5.10.109 (cos)
+ 1.10.3-gke.49	v1.21.5-gke.1200	5.4.0.1038.41 (ubuntu), 5.10.90 (cos)
+ 1.10.2-gke.34	v1.21.5-gke.1200	5.4.0.1031.34 (ubuntu), 5.10.90 (cos)
+ 1.10.1-gke.19	v1.21.5-gke.1200	5.4.0.1031.34 (ubuntu), 5.10.90 (cos)
+ 1.10.0-gke.194	v1.21.5-gke.1200	5.4.0.1039.40 (ubuntu), 5.4.188 (cos)

Complete Release Notes for GKE, including kernel and Kubernetes versions can be reached [here](https://cloud.google.com/kubernetes-engine/docs/release-notes).


### Microsoft Azure AKS

AKS (Azure Kubernetes Service) is a cloud environment where control plane is administered by Azure and no workloads can be customized on such nodes. As we do in previous examples we can just tag a worker node as master or control-plane so KMM can be deployed.

Worker nodes are based by default on Ubuntu 18.04 LTS which has an extensive collection of kernel headers packages for many different versions used by those Azure nodes and other cloud environments as seen before.

A list of available kernel headers packages for our platform can be retrieved from an Ubuntu 18.04 LTS command line:

```console
apt update && apt-cache search linux-headers | grep azure
```

**KERNEL VERSIONS used in latest AKS RELEASES**

+ Linux version 5.4.0-1089-azure (buildd@lcy02-amd64-011) (gcc version 7.5.0 (Ubuntu 7.5.0-3ubuntu1~18.04)) #94~18.04.1-Ubuntu SMP Fri Aug 5 12:34:50 UTC 2022
+ Linux version 5.4.0-1086-azure (buildd@lcy02-amd64-092) (gcc version 7.5.0 (Ubuntu 7.5.0-3ubuntu1~18.04)) #91~18.04.1-Ubuntu SMP Thu Jun 23 20:33:05 UTC 2022
+ Linux version 5.4.0-1072-azure (buildd@lcy02-amd64-106) (gcc version 7.5.0 (Ubuntu 7.5.0-3ubuntu1~18.04)) #75~18.04.1-Ubuntu SMP Wed Mar 2 14:41:08 UTC 2022s
+ Linux version 5.4.0-1065-azure (buildd@lgw01-amd64-041) (gcc version 7.5.0 (Ubuntu 7.5.0-3ubuntu1~18.04)) #68~18.04.1-Ubuntu SMP Fri Dec 3 14:08:44 UTC 2021

Kernel versions used in every AKS Ubuntu 18.04 release can be reached [here](https://github.com/Azure/AKS/tree/master/vhd-notes/aks-ubuntu/AKSUbuntu-1804).

KMM examples for AKS clusters can be accessed [here](azure/kmm-kmod.yaml).

### Ubuntu Bare Metal Cluster

Examples in [ubuntu](ubuntu/kmm-kmod.yaml) directory are based on a bare-metal (or any kind of cloud instances) cluster with Ubuntu 22.04 LTS nodes.

This case is pretty straightforward as worker nodes and docker images used for building are the same OS so kernel headers should be included as a package and ready to install and use at the CRD build step.

Release notes for Ubuntu 22.04 can be found [here](https://discourse.ubuntu.com/t/jammy-jellyfish-release-notes/24668).

## Troubleshooting

A straight way to check if our module is effectively loaded is check within the Module Loader pod:
```console
04:17:00 enrique@fedora labs ±|wiplabs ✗|→ kubectl get po
NAME                         READY   STATUS      RESTARTS       AGE
kmm-ci-a-2lgkl-2t2ch         1/1     Running     0              70m
kmm-ci-a-build-z9b5x-h5qdq   0/1     Completed   0              72m
test                         1/1     Running     1 (168m ago)   170m
04:17:05 enrique@fedora labs ±|wiplabs ✗|→ kubectl exec -it kmm-ci-a-2lgkl-2t2ch -- lsmod | grep kmm
kmm_ci_a               16384  0
04:17:44 enrique@fedora labs ±|wiplabs ✗|→
```
If you encounter issues when using KMM there are some other places where you can check.

* No container is created after applying my CRD

  - Check if the kernelmapping matches.
  - Check the logs in the KMM controller.

* KMM fails at build

  - Check logs and/or describe build pod.
  
