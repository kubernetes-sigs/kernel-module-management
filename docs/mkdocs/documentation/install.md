# Installing

## With [OLM](https://olm.operatorframework.io/) (recommended)

Follow the instructions on [OperatorHub.io](https://operatorhub.io/operator/kernel-module-management).  
This installs the operator in the `operators` namespace.

### Configuring tolerations for KMM

By default, the KMM Operator is installed on `master` nodes when possible and includes tolerations that allow it to be scheduled on them.
In environments where the control plane is not accessible (such as managed Kubernetes services), KMM is installed on worker nodes.
In such cases, when an upgrade flow is triggered, and nodes are tainted, workloads are removed from the nodes to allow the operator to remove the old kmods and insert the new kmod into the kernel. In order to do so, we need to make sure the operator's pods are not evicted if they are running on the tainted node.
In order to fix it, you can add additional tolerations to the operator.

When installing KMM using OLM, you can add tolerations to the Subscription resource using the `spec.config` field:

```yaml
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: kernel-module-management
  namespace: <namespace>
spec:
  channel: stable
  name: kernel-module-management
  source: operatorhubio-catalog          
  sourceNamespace: olm
  config:
    tolerations:
    - key: "node.kubernetes.io/unschedulable"
      operator: "Exists"
      effect: "NoSchedule"
```

To tolerate any taint, you can use an empty key with the `Exists` operator:

```yaml
  config:
    tolerations:
    - operator: "Exists"
```

If KMM is already installed, you can patch the existing Subscription to add tolerations:

```shell
kubectl patch subscription kernel-module-management -n <namespace> --type='merge' -p '
spec:
  config:
    tolerations:
    - key: "node.kubernetes.io/unschedulable"
      operator: "Exists"
      effect: "NoSchedule"
'
```

After patching, restart the operator deployment to apply the new tolerations:

```shell
kubectl rollout restart deploy/kmm-operator-controller -n <namespace>
```

## With `kubectl`

### Installing the cert-manager dependency

```shell
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.11.0/cert-manager.yaml
kubectl -n cert-manager wait --for=condition=Available deployment \
	cert-manager \
	cert-manager-cainjector \
	cert-manager-webhook
```

### Installing KMM

```shell
kubectl apply -k https://github.com/kubernetes-sigs/kernel-module-management/config/default
```

This installs the operator in the `kmm-operator-system` namespace.

### Configuring tolerations with kustomize

When deploying KMM with kustomize, you can add tolerations directly to the deployment spec in
[`config/manager-base/manager.yaml`](https://github.com/kubernetes-sigs/kernel-module-management/blob/main/config/manager-base/manager.yaml)
under `spec.template.spec.tolerations`.