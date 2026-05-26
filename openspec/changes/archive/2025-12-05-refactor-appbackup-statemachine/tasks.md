# Tasks: AppBackup State Machine Refactoring

## 1. Core Infrastructure
- [x] 1.1 **Define Constants & Interface**:
    - Define `Phase` constants: `PhasePending`, `PhaseReady`, `PhaseFailed`, `PhaseDeleting`.
    - Define `StateHandler` interface in `internal/controller/appbackup/state.go` (or similar).
    - Define `StateContext` struct if needed to pass common dependencies (Client, Recorder, etc.).

## 2. Handlers Implementation
- [x] 2.1 **Implement `PendingHandler`**:
    - Validate `Spec.Cluster` and `Spec.Template.StorageLocation`.
    - Check dependencies (KubeClient, StorageRepository).
    - Ensure Finalizer (`testudo.softcdata.com/finalizer`) is present.
    - Transition to `PhaseReady` on success, `PhaseFailed` on error.

- [x] 2.2 **Implement `ReadyHandler` (The Core)**:
    - [x] 2.2.1 **Provisioning Logic**: Ensure Velero Schedule or one-off Backup exists based on `Spec.Schedule`.
    - [x] 2.2.2 **Observation Logic**: Implement `ListAppBackups` to find the latest Velero Backup (sort by `CreationTimestamp` desc).
    - [x] 2.2.3 **Action Handling**:
        - Implement `Backup` action: Create new Velero Backup.
        - Implement `Retry` action: Re-create Velero Backup.
        - Implement **`Cancel`** action: Delete the currently running Velero Backup.
    - [x] 2.2.4 **Status Sync**: Update `Status.BackupStatus` and `Status.History` based on the observed latest backup.

- [x] 2.3 **Implement `FailedHandler`**:
    - Record failure events.
    - Monitor `Generation` or Spec hash to detect configuration changes.
    - Transition back to `PhasePending` if config changes.

- [x] 2.4 **Implement `DeletingHandler`**:
    - Execute `deleteExternalResources` (clean up Velero Schedules/Backups).
    - Remove Finalizer.

## 3. Controller Integration
- [x] 3.1 **Refactor Reconcile Loop**:
    - Replace the monolithic logic in `AppBackupReconciler.Reconcile` with the State Machine switch.
    - Initialize the appropriate Handler based on `Status.Phase`.
- [x] 3.2 **Event Watching**:
    - Update `SetupWithManager` to watch `velerov1.Backup`.
    - Implement `EnqueueRequestsFromMapFunc` to map Velero Backups back to AppBackup requests (using `app-backup-uid` label).

## 4. Verification
- [x] 4.1 **Unit Tests**: Test state transitions for each Handler.
- [x] 4.2 **Manual Verification**:
    - Verify normal backup flow.
    - Verify `Cancel` action stops a running backup.
    - Verify `Retry` action.
    - Verify Schedule auto-creation and observation.
