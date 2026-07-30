# Modprobe.d Configuration Files for Driver Container Images

| Field       | Value   |
|-------------|---------|
| Author(s)   | Natali Shemtov |
| Date        | 2026-07-29 |

## 1. Problem Statement

Kernel modules can subscribe to multiple kernel subsystems during
initialization (for example, PCI). Each subscription increments the
module's reference count. When the module needs to be unloaded, its exit
function isn't called until that reference count reaches zero — which
typically requires running a user-space script to unwind those
subscriptions first. Today, KMM users have no way to run such a script
before or after `modprobe` loads or unloads their kernel module, so
modules that rely on this pattern cannot be reliably unloaded through KMM.

## 2. Goals and Non-Goals

### 2.1 Goals

- Users can supply a set of modprobe.d configuration files in their driver
  container image, and have KMM apply them so that the corresponding
  init/de-init scripts run automatically when the kernel module is loaded
  and unloaded.
- The modprobe.d capability works correctly together with the existing
  Firmware loading capability, with no degradation to either.
- Users are always informed when their modprobe.d configuration could not
  be applied, rather than experiencing a silent failure.
- The modprobe.d capability introduces no degradation to the existing
  `ModulesLoadingOrder` capability — achieved by preventing the two from
  being enabled together on the same module (see Non-Goals), rather than
  allowing them to silently conflict.
- Users can specify where in their driver container image their
  modprobe.d files live, via a new field on the Module API. KMM always
  copies those files to the same fixed location on the worker pod
  (`/etc/modprobe.d/`), regardless of the configured source path.
  Modules that don't set the field are unaffected (see NFR-2).

### 2.2 Non-Goals

- Live reconfiguration: applying a new or changed set of modprobe.d files
  to an already-running module requires the user to restart/recreate the
  module's worker pods.
- Validation of modprobe.d file contents: KMM does not check or validate
  the syntax of the modprobe.d files a user provides — malformed files
  fail at `modprobe` execution time, not as a KMM-reported error.
- Nested directory structures: only modprobe.d files placed directly in
  the configured directory are supported. Files placed in sub-directories
  are not picked up.
- Combined use with `ModulesLoadingOrder`: `ModulesLoadingOrder` is
  implemented today by mounting a read-only volume at `/etc/modprobe.d/`
  in the worker Pod, which collides with this capability's use of the
  same path. Merging both into a single input (for example, generating
  and mounting the combined configuration from an init container) is a
  long-term solution that is out of scope for this enhancement. Users who
  need both softdep-style module ordering and other modprobe.d directives
  can express the ordering directly in their own modprobe.d files.

## 3. Requirements

### 3.1 Functional Requirements

- **FR-1:** Users must be able to enable the modprobe.d capability on a
  module by setting a new field on the Module API that specifies the
  directory in their driver container image containing their modprobe.d
  configuration files. The field accepts any well-formed absolute path;
  KMM's validating webhook must reject the Module if the value isn't a
  well-formed absolute path, but otherwise does not restrict which
  directory the user chooses.
- **FR-2:** When a module has the modprobe.d capability enabled, the
  configuration files in `/etc/modprobe.d/` must take effect before
  `modprobe` is invoked to load the kernel module, so that any load-time
  init sequence defined in those files runs automatically.
- **FR-3:** When a module has the modprobe.d capability enabled, the
  configuration files in `/etc/modprobe.d/` must take effect before
  `modprobe` is invoked to unload the kernel module, so that any de-init
  sequence defined in those files runs automatically (including
  sequences needed to release kernel subsystem reference counts before
  unload can proceed).
- **FR-4:** If the directory configured for the modprobe.d capability
  does not exist in the driver container image, or exists but contains no
  files, the module must fail to load rather than silently proceeding
  without the modprobe.d configuration.
- **FR-5:** If the directory configured for the modprobe.d capability
  contains a sub-directory, the module must fail to load.
- **FR-6:** Applying an updated set of modprobe.d files requires the user
  to restart or recreate the module's worker pods; a running pod does not
  pick up changes to the configured directory automatically.
- **FR-7:** The modprobe.d capability must continue to work correctly for
  modules that also use Firmware loading.
- **FR-8:** KMM must reject, via validating webhook, any Module that
  enables the modprobe.d capability while also setting
  `ModulesLoadingOrder`, since the two currently cannot coexist (see
  Non-Goals). The rejection must clearly state why the combination isn't
  supported and point the user to expressing module ordering directly in
  their own modprobe.d files instead.

### 3.2 Non-Functional Requirements

- **NFR-1:** Enabling the modprobe.d capability on a module requires that
  module's worker pods to run with a Privileged security context, applied
  automatically by KMM. Users must be able to determine, before deploying,
  that enabling the modprobe.d capability implies this elevated privilege
  level for the module's worker pods.
- **NFR-2:** Modules that do not enable the modprobe.d capability continue
  to behave exactly as they do today — this capability introduces no
  change for existing modules.
- **NFR-3:** When the modprobe.d configuration cannot be applied (per
  FR-4 or FR-5), the user must be able to observe the failure and its
  cause through the Module's status or a Kubernetes event, without having
  to inspect pod logs to discover it.

## 4. Acceptance Criteria

- [ ] A user can enable the modprobe.d capability on a module and place
      configuration files in the directory they configured in their
      driver container image, and after deploying the Module, the
      load-time init sequence defined in those files runs as part of
      loading the kernel module.
- [ ] The de-init sequence defined in the user's modprobe.d files runs as
      part of unloading the kernel module, including for modules that
      require this sequence to release subsystem reference counts before
      `modprobe -r` can succeed.
- [ ] A user who enables the modprobe.d capability but whose image has no
      directory at the configured path, or an empty one, sees the module
      fail to load and can observe the failure via the Module's status or
      a Kubernetes event.
- [ ] A user who enables the modprobe.d capability and whose configured
      directory contains a sub-directory sees the module fail to load and
      can observe the failure via the Module's status or a Kubernetes
      event.
- [ ] A user can use the modprobe.d capability on a module that also uses
      Firmware loading, and both capabilities work correctly together.
- [ ] A user who tries to enable the modprobe.d capability on a module
      that also sets `ModulesLoadingOrder` is rejected by the validating
      webhook, with a message explaining the conflict and the
      recommended workaround.
- [ ] A user who sets the modprobe.d field to a value that isn't a
      well-formed absolute path is rejected by the validating webhook.

## 5. Assumptions

- Clusters that adopt this feature permit Privileged pods for modules
  that use it — clusters that uniformly block Privileged pods (e.g., via
  a "restricted" PodSecurity standard applied without exception) would be
  unable to use this capability at all. This mirrors an existing
  precondition for modules that use Firmware loading today, which is
  already documented.
- Users authoring modprobe.d configuration files are already familiar with
  the modprobe.d file format; KMM provides no authoring assistance or
  content validation.

## 6. Future Work

- **Long-term merge:** revisit combining KMM-generated configuration
  (for example, the softdep config produced for `ModulesLoadingOrder`)
  and user-supplied modprobe.d files into a single input, so the two
  capabilities can be used together. This likely requires moving away
  from the current read-only DownwardAPI-based mount for
  `ModulesLoadingOrder` toward generating configuration into a shared,
  writable volume (for example, from an init container).
- **Remove `ModulesLoadingOrder`:** once the modprobe.d capability is
  available, users can express module load ordering directly in their
  own modprobe.d files, making the `ModulesLoadingOrder` field redundant.
  Track a follow-up task to remove it from the API in KMM 3.0 (the next
  version where breaking API changes for existing users are permitted),
  once this enhancement's work is complete.
