# Add Operator Unit Tests

## Summary
This proposal aims to introduce a comprehensive unit testing strategy for the `disaster-operator`, starting with the `ClusterReconciler`. The goal is to ensure system stability and prevent regressions by covering critical logic with tests.

## Motivation
Currently, the operator lacks sufficient unit test coverage. As the project grows, manual verification becomes inefficient and error-prone. Adding unit tests will:
- Improve code reliability.
- Facilitate refactoring and feature additions.
- Catch bugs early in the development cycle.

## Proposed Changes
1.  **Establish Testing Infrastructure**: Set up necessary test dependencies (e.g., `envtest`, `ginkgo`/`gomega` if preferred, or standard `testing` with `testify`).
2.  **ClusterReconciler Tests**: Implement unit tests for `ClusterReconciler` covering:
    -   Reconciliation logic (creation, update, deletion).
    -   Status updates.
    -   Velero installation checks (mocked).
    -   Dependency checks during deletion.
3.  **Expand Coverage**: Systematically add tests for other controllers and packages.

## Implementation Plan
The implementation will be phased:
1.  **Phase 1**: Setup and `ClusterReconciler` tests.
2.  **Phase 2**: `AppBackup` and `AppRestore` controller tests.
3.  **Phase 3**: Webhook and utility package tests.

## Testing Strategy
-   **Controller Tests**: Use `sigs.k8s.io/controller-runtime/pkg/envtest` to spin up a local control plane for integration-like unit tests.
-   **Unit Tests**: Use `fake` clients and mocks for pure logic testing where a full API server isn't needed.
-   **Mocking**: Abstract external command executions (like `helm` or `velero` CLI calls) to allow testing without external dependencies.
