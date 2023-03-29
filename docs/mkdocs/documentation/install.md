# Installing

## Using `kubectl`

First install [cert-manager](https://github.com/cert-manager/cert-manager) which is a dependency.
```shell
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.11.0/cert-manager.yaml
kubectl -n cert-manager wait --for=condition=Available deployment \
	cert-manager \
	cert-manager-cainjector \
	cert-manager-webhook
```

Install KMM.
```shell
kubectl apply -k https://github.com/kubernetes-sigs/kernel-module-management/config/default
```

## Using the bundle

### Using `operator-sdk`

```shell
operator-sdk run bundle gcr.io/k8s-staging-kmm/kernel-module-management-operator-bundle:latest
```
