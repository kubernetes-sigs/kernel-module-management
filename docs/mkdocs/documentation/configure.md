# Configuring

KMM should be configured out of the box with sensible defaults.
The operator configuration is set in the `kmm-operator-manager-config` `ConfigMap` in the operator namespace.
To modify any setting, edit the `ConfigMap` data and restart the controller with the following command:

```shell
kubectl delete pod -n "$namespace" -l app.kubernetes.io/component=kmm
```

The value of `$namespace` depends on your [installation method](install.md).

## Reference

#### `healthProbeBindAddress`

Defines the address on which the operator should listen for kubelet health probes.  
Recommended value: `:8081`.

#### `job.gcDelay`

Defines the duration for which successful build pods should be preserved before they are deleted.  
Refer to the Go [`ParseDuration`](https://pkg.go.dev/time#ParseDuration) function documentation to understand valid
values for this setting.  
There is no recommended value for this setting.

#### `leaderElection.enabled`

Determines whether [leader election](https://kubernetes.io/docs/concepts/architecture/leases/) is used to ensure that
only one replica of the KMM operator is running at any time.  
Recommended value: `true`.

#### `leaderElection.resourceID`

Determines the name of the resource that leader election will use for holding the leader lock.  
Recommended value: `kmm.sigs.x-k8s.io`.

#### `metrics.bindAddress`

Determines the bind address for the metrics server.
It will be defaulted to `:8080` if unspecified.
Set this to "0" to disable the metrics server.  
Recommended value: `0.0.0.0:8443`.

#### `metrics.enableAuthnAuthz`

Determines if metrics should be authenticated (via `TokenReviews`) and authorized (via `SubjectAccessReviews`) with the
kube-apiserver.  
For the authentication and authorization the controller needs a ClusterRole with the following rules:

  - `apiGroups: authentication.k8s.io, resources: tokenreviews, verbs: create`
  - `apiGroups: authorization.k8s.io, resources: subjectaccessreviews, verbs: create`

To scrape metrics e.g. via Prometheus the client needs a `ClusterRole` with the following rule:

  - `nonResourceURLs: "/metrics", verbs: get`

Recommended value: `true`.

#### `metrics.secureServing`

Determines whether the metrics should be served over HTTPS instead of HTTP.  
Recommended value: `true`.

#### `webhookPort`

Defines the port on which the operator should be listening for webhook requests.  
Recommended value: `9443`.

#### `worker.runAsUser`

Determines the value of the `runAsUser` field of the worker container's
[SecurityContext](https://kubernetes.io/docs/tasks/configure-pod-container/security-context/).  
Recommended value: `9443`.

#### `worker.seLinuxType`

Determines the value of the `seLinuxOptions.type` field of the worker container's
[SecurityContext](https://kubernetes.io/docs/tasks/configure-pod-container/security-context/).  
Recommended value: `spc_t`.

#### `worker.setFirmwareClassPath`

If set, the value of this field will be written by the worker into the `/sys/module/firmware_class/parameters/path` file
on the node.
This sets the [kernel's firmware search path](firmwares.md#setting-the-kernels-firmware-search-path).  
Recommended value: `/var/lib/firmware` if you need to set that value through the worker app; otherwise, unset.
