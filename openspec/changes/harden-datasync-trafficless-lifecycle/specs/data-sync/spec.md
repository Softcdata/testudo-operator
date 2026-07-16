## ADDED Requirements

### Requirement: DataSync Trafficless Runtime Preflight

DataSync MUST validate the source and target runtime prerequisites needed by its Trafficless FSB path before creating the corresponding Backup or Restore. It MUST fail with a stable diagnostic reason when a prerequisite is not ready; it MUST NOT begin a restore that is known unable to run.

#### Scenario: Target Velero or node-agent is not ready

- **GIVEN** a DataSync run needs to restore data to a target cluster
- **AND** the target Cluster is NotReady, its Velero Deployment is unavailable, or its node-agent DaemonSet is not ready
- **WHEN** DataSync evaluates the restore preflight
- **THEN** DataSync MUST enter Failed with TargetVeleroRuntimeNotReady
- **AND** it MUST include the unavailable resource and observed readiness details
- **AND** it MUST NOT create a new AppRestore for that run

#### Scenario: Source runtime is not ready for FSB backup

- **GIVEN** a DataSync run needs to create a source FSB backup
- **AND** the source Cluster is not Ready for the required Velero runtime
- **WHEN** DataSync evaluates the backup preflight
- **THEN** DataSync MUST enter Failed with SourceVeleroRuntimeNotReady
- **AND** it MUST NOT submit a new Backup action

### Requirement: DataSync Trafficless Pod Ownership and Cleanup Scope

DataSync MUST assign a platform-managed owner token and a restore-run identifier to every Trafficless Pod it restores. It MUST use only those identifiers to select Pods for cleanup; it MUST NOT use a container image as cleanup ownership evidence.

#### Scenario: Stale Pod cleanup before a new restore

- **GIVEN** a previous Trafficless Pod belongs to the same DataSync owner token
- **WHEN** DataSync prepares a new restore run
- **THEN** it MUST request deletion only for Pods matching the platform-managed owner selector
- **AND** it MUST wait until those Pods are absent before creating the new AppRestore
- **AND** it MUST NOT delete a normal busybox Pod or a Trafficless Pod owned by another DataSync or Drill

#### Scenario: Ambiguous historical Trafficless Pod exists

- **GIVEN** a target namespace contains a Pod with trafficless=true
- **AND** the Pod lacks the required platform owner/run labels
- **WHEN** DataSync prepares a restore
- **THEN** DataSync MUST NOT delete that Pod by image or heuristic
- **AND** it MUST enter Failed with TrafficlessCleanupAmbiguous
- **AND** its message MUST identify the ambiguous namespace and Pod name

### Requirement: DataSync Cleanup Confirmation Is a Terminal Gate

DataSync MUST treat a successful Kubernetes Delete request as an intermediate action, not cleanup completion. It MUST remain InProgress until its run-scoped Trafficless Pods are confirmed absent. It MUST NOT report Ready while cleanup is pending, failed, or timed out.

#### Scenario: Cleanup deletion is delayed

- **GIVEN** the DataSync AppRestore reports successful data restore
- **AND** its run-scoped Trafficless Pod has a DeletionTimestamp or is still returned by the target selector
- **WHEN** DataSync reconciles the completion path
- **THEN** DataSync MUST remain InProgress and report cleanup progress
- **AND** it MUST requeue until the selector is empty or the cleanup deadline expires

#### Scenario: Cleanup confirmation times out

- **GIVEN** a run-scoped Trafficless Pod remains present after the effective cleanup timeout
- **WHEN** DataSync reconciles the cleanup gate
- **THEN** DataSync MUST enter Failed with TrafficlessCleanupTimeout
- **AND** it MUST NOT overwrite the result with Ready

### Requirement: DataSync Must Preserve Data-Restore Failure Diagnostics

DataSync MUST expose the most specific failure from its DataSync-owned AppRestore, Trafficless Pod, node-agent, PVR, Restore termination, or PVC readiness check through its own Conditions, events, and history.

#### Scenario: Trafficless Pod cannot start

- **GIVEN** a run-scoped Trafficless Pod remains Unschedulable or has an image pull, mount, configuration, or runtime container failure past the bounded observation window
- **WHEN** the DataSync-owned AppRestore reports that condition
- **THEN** DataSync MUST enter Failed with the corresponding stable reason
- **AND** its message MUST retain the target Pod, node, container, and underlying Kubernetes reason when available

#### Scenario: Node-agent cannot restore the scheduled Pod volume

- **GIVEN** a run-scoped Trafficless Pod is scheduled on a node without a Ready node-agent
- **OR** an associated PodVolumeRestore fails or exceeds its effective timeout
- **WHEN** the DataSync-owned AppRestore observes the failure
- **THEN** DataSync MUST enter Failed with NodeAgentUnavailable, PodVolumeRestoreFailed, or PodVolumeRestoreStalled as applicable
- **AND** it MUST NOT remain InProgress indefinitely

### Requirement: DataSync Restore Timeout Alignment

When a DisasterInstance configures OperationTimeoutMinutes, DataSync MUST apply the same duration to the AppRestore it creates for that run. When the instance does not configure a timeout, DataSync MUST preserve the existing RestoreRuntime default behavior.

#### Scenario: Instance timeout is configured

- **GIVEN** a DisasterInstance has OperationTimeoutMinutes set to 180
- **WHEN** DataSync builds its Trafficless AppRestore
- **THEN** the AppRestore timeout MUST be 180 minutes
- **AND** the DataSync-owned PVR and Restore termination checks MUST use that effective timeout
