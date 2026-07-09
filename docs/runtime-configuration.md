# Operator Runtime Configuration

`OperatorRuntimeConfig` provides hot-reloadable controller runtime settings for backup, restore, sync, storage, cluster, and disaster-operation control loops.

The singleton object is:

```yaml
apiVersion: testudo.softcdata.com/v1
kind: OperatorRuntimeConfig
metadata:
  name: default
  namespace: disaster-system
```

The namespace must match the operator management namespace. The object name must be `default`; other names do not affect active runtime behavior.

## Configuration Layers

Runtime behavior resolves configuration in this order:

1. Resource or operation `spec`, such as `AppBackup.spec.timeout`, `AppRestore.spec.timeout`, `DisasterOperation.spec.timeoutMinutes`, and `DisasterOperation.spec.retryPolicy`.
2. Instance or policy `spec`, such as `DisasterInstance.spec.operationTimeoutMinutes`.
3. `OperatorRuntimeConfig/default` hot runtime settings.
4. Startup env or flags, including the compatible `APPRESTORE_*` env values.
5. Built-in code defaults.

Business defaults used by the frontend or server are outside the operator runtime layer. The frontend/server may read its own strongly typed default configuration and write final values into CRD `spec`; the operator only consumes the final `spec`.

## Hot Reload

The following fields are hot-reloaded by the operator on the next reconcile or next runtime check:

- `backupRuntime`: backup max waits and observe poll interval.
- `restoreRuntime`: restore max waits, poll intervals, stall grace values, PVR pending wait, retry backoff, and retry limits.
- `operationRuntime`: default operation timeout, step requeue intervals, and default retry interval.
- `instanceRuntime`: transition watchdog and instance state requeue intervals.
- `syncRuntime`: scheduler update timeout, backup/restore observe intervals, and history retention.
- `storageRuntime`: storage validation/statistics requeue interval.
- `clusterRuntime`: cluster reconcile interval, delete retry interval, Velero install timeout for future install calls, and zombie Helm lock threshold.

If `OperatorRuntimeConfig/default` is deleted, the operator falls back to startup env/flag defaults and built-in code defaults. If the object exists but contains semantic errors, the operator keeps the last valid active snapshot and records an `Invalid` condition on the object.

## Validation Boundary

The CRD OpenAPI schema validates object shape and field types. It intentionally does not enforce runtime ranges such as min/max duration or count values.

Controller semantic validation rejects dangerous values from activation and writes status conditions:

- `Ready=True` means the latest valid config snapshot is active.
- `Invalid=True` means the object was accepted by Kubernetes but not activated by the operator.

Examples of controller-level invalid values:

- `restoreRuntime.retryBackoff: 0s`
- `backupRuntime.pollInterval: 0s`
- `operationRuntime.defaultTimeoutMinutes: 0`
- `syncRuntime.historyRetention: 0`
- `instanceRuntime.transitionWatchdogTimeout` smaller than `instanceRuntime.minTransitionWatchdogTimeout`

## Startup Env Compatibility

Existing `APPRESTORE_*` env values remain supported as startup defaults:

- `APPRESTORE_IN_PROGRESS_MAX_WAIT`
- `APPRESTORE_UNKNOWN_MAX_WAIT`
- `APPRESTORE_PROGRESS_COMPLETE_GRACE`
- `APPRESTORE_STARTUP_GRACE`
- `APPRESTORE_MISSING_GRACE`
- `APPRESTORE_EMPTY_STATUS_GRACE`
- `APPRESTORE_PVR_PENDING_MAX_WAIT`
- `APPRESTORE_RETRY_BACKOFF`
- `APPRESTORE_RETRY_LIMIT`
- `APPRESTORE_RETRY_LIMIT_PROGRESS`
- `APPRESTORE_RETRY_LIMIT_STARTUP`
- `APPRESTORE_RETRY_LIMIT_MISSING`
- `APPRESTORE_RETRY_LIMIT_EMPTY`

When `OperatorRuntimeConfig/default` sets the corresponding restore runtime field, the CRD value takes precedence over env defaults without restarting the operator.

## Rollout Boundary

Velero workload parameters are not hot runtime config:

- Velero image and plugin images.
- Velero workload `extraArgs`.
- node-agent settings.
- BackupStorageLocation validation frequency.

These require a separate Velero rollout or upgrade flow. `clusterRuntime.veleroInstallTimeout` only affects future Helm install/upgrade calls made by the operator; it does not modify an already running Helm command or Velero workload.

## Sample

A default sample is available at `config/samples/disaster_v1_operatorruntimeconfig.yaml`.
