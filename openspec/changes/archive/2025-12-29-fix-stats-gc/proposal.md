# Fix Cleanup of BackupRestoreStatistics

## Background

Currently, when an `AppBackup` resource is deleted, its associated `BackupRestoreStatistics` resource remains in the cluster (orphaned). This is because the `BackupRestoreStatistics` resource is created without an `ownerReference` pointing back to the `AppBackup`.

## Proposed Solution

1.  Update the `StatisticsHelper` interface and implementation to allow setting an owner for the created statistics resource.
    *   Add `SetOwner(owner metav1.Object, scheme *runtime.Scheme)` method or update `GetOrCreate` to accept owner info. Use `controller-runtime/pkg/controller/controllerutil.SetControllerReference`.
2.  Update `AppBackup` controller (and potentially `AppRestore` controller) to pass the owner object when creating statistics.
3.  Ensure `BackupRestoreStatistics` has the correct OwnerReference so that Kubernetes Garbage Collector deletes it when the owner is deleted.

## Impact

*   `disaster-operator`: `StatisticsHelper` and controllers.
*   No API change in terms of CRD fields, just implementation detail.
