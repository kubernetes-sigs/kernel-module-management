# Uninstalling

## Using `kubectl`

```shell
kubectl delete -k https://github.com/kubernetes-sigs/kernel-module-management/config/default
```

## If installed using the bundle

### Using `operator-sdk`

```shell
operator-sdk cleanup kernel-module-management
```