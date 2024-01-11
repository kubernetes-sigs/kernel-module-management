# Installing

## With [OLM](https://olm.operatorframework.io/) (recommended)

Follow the instructions on [OperatorHub.io](https://operatorhub.io/operator/kernel-module-management).  
This installs the operator in the `operators` namespace.

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
