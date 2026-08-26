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
- The modprobe.d capability works correctly together with the existing
  `ModulesLoadingOrder` capability, with no degradation to either.
  A Module may enable both on the same resource.
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

### 2.3 Approach

User modprobe.d files are copied from the driver image into
`/etc/modprobe.d/` on the worker pod (the same pattern as Firmware), so
they are present before `modprobe` runs. `modprobe` only reads files
that end with `.conf`; user files should use that suffix so they take
effect. KMM copies the directory as-is and does not rename or filter by
extension.

`ModulesLoadingOrder` continues to be a `softdep.conf` file in that
directory. When both capabilities are enabled, KMM mounts that file
read-only at `/etc/modprobe.d/softdep.conf` and copies the user's files
next to it. The directory stays writable, so `modprobe` reads both.
If the user image already contains a file named `softdep.conf`, the copy
fails rather than overwriting KMM's file (FR-9).

When `modprobedDir` is not set, `ModulesLoadingOrder` is unchanged.

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
- **FR-8:** A Module may enable the modprobe.d capability and set
  `ModulesLoadingOrder` at the same time. Both take effect: the user's
  init/de-init configuration and the configured load order. KMM must not
  reject this combination.
- **FR-9:** If a Module enables both capabilities and the user's
  modprobe.d files include a file whose name collides with the file KMM
  uses for `ModulesLoadingOrder`, the module must fail to load rather
  than silently overwriting either file.

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
  FR-4, FR-5, or FR-9), the user must be able to observe the failure and
  its cause through the Module's status or a Kubernetes event, without
  having to inspect pod logs to discover it.

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
- [ ] A user can enable the modprobe.d capability on a module that also
      sets `ModulesLoadingOrder`, and both take effect: load order from
      `ModulesLoadingOrder` and the init/de-init sequences from the
      user's modprobe.d files.
- [ ] A user who enables both capabilities and whose modprobe.d files
      include a file that collides with KMM's `ModulesLoadingOrder`
      file sees the module fail to load and can observe the failure via
      the Module's status or a Kubernetes event.
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
  the modprobe.d file format and that files should be named `*.conf`;
  KMM provides no authoring assistance or content validation.
