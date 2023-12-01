# Uninstalling

## If installed with [OLM](https://olm.operatorframework.io/)

Remove the `Subscription` resource:

```shell
kubectl delete -f https://operatorhub.io/install/kernel-module-management.yaml
```

## If installed with `kubectl`

```shell
kubectl delete -k https://github.com/kubernetes-sigs/kernel-module-management/config/default
kubectl delete -f https://github.com/cert-manager/cert-manager/releases/download/v1.11.0/cert-manager.yaml
```
