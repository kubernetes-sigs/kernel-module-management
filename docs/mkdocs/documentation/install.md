# Installing

## Using `kubectl`

```shell
kubectl apply -k https://github.com/kubernetes-sigs/kernel-module-management/config/default
```

## Using the bundle

### Using `operator-sdk`

```shell
operator-sdk run bundle gcr.io/k8s-staging-kmm/kernel-module-management-operator-bundle:latest
```