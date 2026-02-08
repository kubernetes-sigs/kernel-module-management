# Installing

## With [OLM](https://olm.operatorframework.io/) (recommended)

Follow the instructions on [OperatorHub.io](https://operatorhub.io/operator/kernel-module-management).  
This installs the operator in the `operators` namespace.

### Running KMM on infra nodes

By default, KMM Operator includes tolerations that allow it to be scheduled on control-plane nodes.
In environments where the control plane is not accessible (such as HyperShift or managed Kubernetes services),
or where worker nodes have custom taints, you may need to add additional tolerations.

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
    - key: "node-role.kubernetes.io/infra"
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
    - key: "node-role.kubernetes.io/infra"
      operator: "Exists"
      effect: "NoSchedule"
'
```

After patching, you may need to delete the existing pods to trigger new ones with the updated tolerations:

```shell
kubectl delete pods -n <namespace> -l app.kubernetes.io/component=kmm
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
