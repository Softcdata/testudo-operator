# Tasks: Fix Stats GC

- [x] Update `StatisticsHelper` interface signature for `GetOrCreate` to optionally accept `owner metav1.Object` and `scheme *runtime.Scheme`. <!-- id: update-helper-interface -->
- [x] Implement `SetControllerReference` logic in `statisticsHelper.GetOrCreate`. <!-- id: implement-set-owner -->
- [x] Update `AppBackupReconciler` to pass `appBackup` and `scheme` when calling `GetOrCreate`. <!-- id: update-appbackup-controller -->
- [x] Verify `AppRestore` usage of `StatisticsHelper` and update if necessary. <!-- id: update-apprestore-controller -->
- [x] Add unit test verifying `OwnerReference` is set on created statistics. <!-- id: add-test-case -->
