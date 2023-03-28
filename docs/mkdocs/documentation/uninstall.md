# Uninstalling

## Using `kubectl`

```shell
kubectl delete -k https://github.com/kubernetes-sigs/kernel-module-management/config/default
kubectl delete -f https://github.com/cert-manager/cert-manager/releases/download/v1.11.0/cert-manager.yaml
```

## If installed using the bundle

### Using `operator-sdk`

```shell
operator-sdk cleanup kernel-module-management
```
