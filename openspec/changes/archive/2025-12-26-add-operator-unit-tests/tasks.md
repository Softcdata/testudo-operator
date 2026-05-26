# Tasks: Add Operator Unit Tests

- [x] **Setup & ClusterReconciler** <!-- id: 0 -->
    - [x] Refactor `ClusterReconciler` to make external commands (Helm/Velero) mockable. <!-- id: 1 -->
    - [x] Set up `envtest` suite for controller testing. <!-- id: 2 -->
    - [x] Implement `TestClusterReconciler_Reconcile` covering: <!-- id: 3 -->
        - [x] Successful creation and status initialization.
        - [x] Velero installation logic (mocked).
        - [x] Stats collection.
    - [x] Implement `TestClusterReconciler_Delete` covering: <!-- id: 4 -->
        - [x] Dependency checks (blocking deletion).
        - [x] Finalizer removal.

- [x] **Remaining Controllers** <!-- id: 5 -->
    - [x] Add tests for `AppBackupReconciler`. <!-- id: 6 -->
    - [x] Add tests for `AppRestoreReconciler`. <!-- id: 7 -->
    - [x] Add tests for `DisasterConfigReconciler`. <!-- id: 8 -->

- [x] **Utils & Packages** <!-- id: 9 -->
    - [x] Add tests for `pkg/tools`. <!-- id: 10 -->
