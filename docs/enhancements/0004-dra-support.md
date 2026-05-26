# Adding DRA Support to KMM

| Field       | Value   |
|-------------|---------|
| Author(s)   | Tomer Newman |
| Date        | 2026-05-26 |

## 1. Problem Statement

KMM manages the lifecycle of out-of-tree kernel modules and their associated Device Plugins. Device Plugins use a static integer counting model for hardware resource advertisement — a mechanism Kubernetes is replacing with Dynamic Resource Allocation (DRA). As the ecosystem adopts DRA, KMM users who need modern hardware orchestration (structured parameters, resource claims, device classes) have no way to deploy DRA drivers through KMM. Without DRA support, KMM users must manage DRA driver DaemonSets and DeviceClass resources outside of KMM, losing the operator's lifecycle management, ordered upgrades, and integration with kernel module loading.

## 2. Goals and Non-Goals

### 2.1 Goals

- KMM users can deploy a DRA driver alongside a kernel module by specifying `spec.dra` on a Module resource.
- The KMM operator manages the full lifecycle of DRA DaemonSets (create, update, delete) and DeviceClass resources on behalf of the user.
- DRA modules participate in ordered upgrades with the same guarantees as Device Plugin modules.
- The admission webhook prevents users from creating invalid DRA configurations (unsupported cluster version, missing or invalid driver name).

### 2.3 Non-Goals

- Hub/multi-cluster DRA support — DRA operates in spoke mode only, matching DevicePlugin behavior.
- In-place migration from DevicePlugin to DRA — users delete the existing Module and create a new one.
- DRA-specific Prometheus metrics.

## 3. Requirements

### 3.1 Functional Requirements

#### CRD and API

- **FR-1:** The Module CRD must include a `spec.dra` field that accepts a DRA configuration: container spec, init container, service account, volumes, `driverName` (required, DNS subdomain), and `deviceClasses` (optional list of DeviceClass definitions).
- **FR-2:** DRA and DevicePlugin container/pod properties must share common base types (`PluginSpec`, `PluginContainerSpec`). `DevicePluginContainerSpec` must remain as a type alias for backward compatibility.
- **FR-3:** `spec.dra` and `spec.devicePlugin` must be mutually exclusive. The CRD must reject a Module that sets both fields.
- **FR-4:** `status.dra` must report the DRA DaemonSet's availability using the same `DaemonSetStatus` structure as `status.devicePlugin`.

#### Webhook Validation

- **FR-5:** The admission webhook must reject creation of a Module with `spec.dra` when the cluster Kubernetes version is below 1.34.
- **FR-6:** The admission webhook must reject creation of a Module with `spec.dra` when `driverName` is empty or is not a valid DNS subdomain.

#### DRA Controller

- **FR-7:** A DRA reconciler must watch Module resources and create a DRA DaemonSet when `spec.dra` is set. The DaemonSet must be updated when the Module spec changes and deleted when `spec.dra` is removed or the Module is deleted.
- **FR-8:** The DRA DaemonSet must auto-mount `/var/lib/kubelet/plugins/` (HostPathDirectoryOrCreate) and `/var/lib/kubelet/plugins_registry/` (HostPathDirectory). Users may specify additional volumes via the spec.
- **FR-9:** DRA DaemonSets must be named `{module-name}-dra-{suffix}`, following the existing device plugin naming convention.
- **FR-10:** The DRA DaemonSet must target nodes where the kernel module is loaded (using the kernel-module-ready node label as its nodeSelector).

#### DeviceClass Management

- **FR-11:** The DRA controller must create, update, and delete Kubernetes DeviceClass resources based on `spec.dra.deviceClasses`.
- **FR-12:** DeviceClass ownership must be tracked via labels on the DeviceClass (Module name and namespace), since DeviceClass is cluster-scoped and Module is namespaced.
- **FR-13:** When a Module is deleted, all DeviceClasses managed by that Module must be deleted.

#### Ordered Upgrades

- **FR-14:** DRA must participate in KMM ordered upgrades with the same behavior as DevicePlugin: DRA-specific version labels on nodes, a DRA pod reconciler that labels nodes on pod readiness, extension of the label action table for DRA label transitions, and garbage collection of old-version DRA DaemonSets.

### 3.2 Non-Functional Requirements

- **NFR-1:** All CRD changes must be backward compatible. Existing Modules using `spec.devicePlugin` must continue to work without modification.
- **NFR-2:** DRA features require Kubernetes >= 1.34. The webhook enforces this at admission time.

## 4. Acceptance Criteria

- [ ] A Module with `spec.dra` set results in a running DRA DaemonSet on all nodes matching the module selector where the kernel module is loaded.
- [ ] The DRA DaemonSet mounts `/var/lib/kubelet/plugins/` and `/var/lib/kubelet/plugins_registry/` automatically.
- [ ] DeviceClass resources listed in `spec.dra.deviceClasses` are created in the cluster. Removing an entry from the list deletes the corresponding DeviceClass. Editing an entry updates the DeviceClass.
- [ ] The webhook rejects a Module with `spec.dra` on a cluster running Kubernetes < 1.34.
- [ ] The webhook rejects a Module with `spec.dra` when `driverName` is empty or not a valid DNS subdomain.
- [ ] A Module cannot have both `spec.dra` and `spec.devicePlugin` set simultaneously.
- [ ] DRA DaemonSets participate in ordered upgrades: version labels are set on nodes, old-version DaemonSets are garbage-collected when no longer scheduled, and version transitions follow the same rules as DevicePlugin.
- [ ] Deleting a Module with `spec.dra` removes all associated DRA DaemonSets and DeviceClasses.
- [ ] Existing Modules with `spec.devicePlugin` (and no `spec.dra`) continue to work without any changes.

## 5. Assumptions

- The Kubernetes DRA API (`k8s.io/api/resource/v1`) is stable and available on clusters running version 1.34+.
- DRA kubelet plugins register via Unix sockets under `/var/lib/kubelet/plugins/` and discover through `/var/lib/kubelet/plugins_registry/`, following the standard Kubernetes plugin registration mechanism.

## 6. Dependencies

- Kubernetes >= 1.34 with DRA support enabled.
- `k8s.io/api/resource/v1` Go module for DeviceClass types.
