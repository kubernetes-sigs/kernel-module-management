# Home

The OOT Operator optionally builds and runs DriverContainers on Kubernetes clusters.

## Getting started

Install the bleeding edge version of the OOT Operator by running the following command:
```shell
kubectl apply -k https://github.com/qbarrand/oot-operator/config/default
```

Alternatively, we create [bundle](https://github.com/qbarrand/oot-operator/pkgs/container/oot-operator-bundle) and
[catalog](https://github.com/qbarrand/oot-operator/pkgs/container/oot-operator-catalog) images to be used by OLM.  
To install the OOT Operator from a bundle, run the following command:
```shell
operator-sdk run bundle docker pull ghcr.io/qbarrand/oot-operator-bundle:main
```