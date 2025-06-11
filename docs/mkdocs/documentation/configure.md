# Configuring

KMM is configured out of the box with sensible defaults.
To modify any setting, create a `ConfigMap` with name of `kmm-operator-manager-config` in the operator namespace with 
the relevant data and restart the controller with the following command:

```shell
kubectl delete pod -n "$namespace" -l app.kubernetes.io/component=kmm
```

The value of `$namespace` depends on your [installation method](install.md).

## Example
```yaml
apiVersion: v1
data:
  controller_config.yaml: |
    worker:
      firmwareHostPath: /example/different/firmware/path
kind: ConfigMap
metadata:
  name: kmm-operator-manager-config
  namespace: kmm-operator-system
```

### Note
If you want to configure `KMM Hub`, then create the `ConfigMap` with the name `kmm-operator-hub-manager-config` instead
in the KMM-hub controller's namespace.


## Reference

#### `healthProbeBindAddress`

Defines the address on which the operator should listen for kubelet health probes.  
Default value: `:8081`.

#### `job.gcDelay`

Defines the duration for which successful build pods should be preserved before they are deleted.  
Refer to the Go [`ParseDuration`](https://pkg.go.dev/time#ParseDuration) function documentation to understand valid
values for this setting.  
Default value: `0s`.

#### `leaderElection.enabled`

Determines whether [leader election](https://kubernetes.io/docs/concepts/architecture/leases/) is used to ensure that
only one replica of the KMM operator is running at any time.  
Default value: `true`.

#### `leaderElection.resourceID`

Determines the name of the resource that leader election will use for holding the leader lock.  
Default value: `kmm.sigs.x-k8s.io` for KMM and `kmm-hub.sigs.x-k8s.io` for KMM-hub.

#### `metrics.bindAddress`

Determines the bind address for the metrics server.
It will be defaulted to `:8080` if unspecified.
Set this to "0" to disable the metrics server.  
Default value: `0.0.0.0:8443`.

#### `metrics.enableAuthnAuthz`

Determines if metrics should be authenticated (via `TokenReviews`) and authorized (via `SubjectAccessReviews`) with the
kube-apiserver.  
For the authentication and authorization the controller needs a ClusterRole with the following rules:

  - `apiGroups: authentication.k8s.io, resources: tokenreviews, verbs: create`
  - `apiGroups: authorization.k8s.io, resources: subjectaccessreviews, verbs: create`

To scrape metrics e.g. via Prometheus the client needs a `ClusterRole` with the following rule:

  - `nonResourceURLs: "/metrics", verbs: get`

Default value: `true`.

#### `metrics.secureServing`

Determines whether the metrics should be served over HTTPS instead of HTTP.  
Default value: `true`.

#### `webhookPort`

Defines the port on which the operator should be listening for webhook requests.  
Default value: `9443`.

#### `worker.runAsUser`

Determines the value of the `runAsUser` field of the worker container's
[SecurityContext](https://kubernetes.io/docs/tasks/configure-pod-container/security-context/).  
Default value: `0`.

#### `worker.seLinuxType`

Determines the value of the `seLinuxOptions.type` field of the worker container's
[SecurityContext](https://kubernetes.io/docs/tasks/configure-pod-container/security-context/).  
Default value: `spc_t`.

#### `worker.firmwareHostPath`

If set, the value of this field will be written by the worker into the `/sys/module/firmware_class/parameters/path` file
on the node.
This sets the [kernel's firmware search path](firmwares.md#setting-the-kernels-firmware-search-path).  
Default value: `/lib/firmware`.
