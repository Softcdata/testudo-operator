# Proposal: Backup and Restore Statistics

## Summary

This proposal introduces a mechanism to track and aggregate statistics for backup and restore operations in the Disaster Operator. It defines a new Custom Resource Definition (CRD) `BackupRestoreStatistics` and a client-side aggregation strategy.

## Motivation

Users need visibility into the status of their backup and restore operations. While individual `AppBackup` and `AppRestore` resources provide status, aggregating this information (e.g., "How many backups failed today?", "What is the success rate of restores?") requires a dedicated statistics mechanism.

## Design

### 1. Custom Resource Definition: `BackupRestoreStatistics`

A new namespaced CRD `BackupRestoreStatistics` is introduced to hold the statistics for a specific scope (e.g., a single AppBackup or AppRestore).

**API Version**: `testudo.softcdata.com/v1`
**Kind**: `BackupRestoreStatistics`

#### Spec

```go
type BackupRestoreStatisticsSpec struct {
    // ScopeType defines the scope of the statistics (e.g., "app", "namespace", "cluster")
    ScopeType ScopeType `json:"scopeType"`

    // ScopeRef defines the reference to the scope object (e.g., the AppBackup instance)
    ScopeRef ScopeReference `json:"scopeRef"`
}
```

#### Status

```go
type BackupRestoreStatisticsStatus struct {
    // Statistics contains the counters
    Statistics StatisticsData `json:"statistics"`

    // LastUpdateTime is the last time the statistics were updated
    LastUpdateTime *metav1.Time `json:"lastUpdateTime,omitempty"`

    // ... other metadata
}

type StatisticsData struct {
    Total      int32 `json:"total"`
    InProgress int32 `json:"inProgress"`
    Completed  int32 `json:"completed"`
    Failed     int32 `json:"failed"`
    Canceled   int32 `json:"canceled"`
    Unknown    int32 `json:"unknown"`
}
```

### 2. Architecture: Level-Triggered Sync

Instead of an event-driven approach (incrementing counters on events), we use a **Level-Triggered Sync** strategy (Snapshot & Sync) for robustness.

1.  **Controllers (AppBackup/AppRestore)**:
    *   In their Reconcile loop, they calculate the *current* statistics snapshot based on the state of their managed resources (e.g., listing child Velero Backups or checking their own status).
    *   They call `StatisticsHelper.SyncStats` to update the `BackupRestoreStatistics` CR.

2.  **StatisticsHelper**:
    *   Encapsulates the logic to `Get` or `Create` the Statistics CR.
    *   Compares the current CR state with the calculated snapshot.
    *   If different, patches the CR with the new values.
    *   Handles concurrency using `RetryOnConflict`.

### 3. Aggregation Strategy: Client-Side

To avoid the complexity and race conditions of a central "Aggregator Controller", aggregation is performed **on-demand by the client** (e.g., API Server, CLI, Dashboard).

#### Labeling

To facilitate efficient querying and aggregation, `BackupRestoreStatistics` CRs are labeled with:

*   `disaster.io/scope-type`: e.g., "app"
*   `disaster.io/scope-uid`: UID of the source object.
*   `testudo.softcdata.com/owner-kind`: The Kind of the owner resource (e.g., "AppBackup", "AppRestore").

#### Aggregation Logic

The upper layer (client) performs aggregation by:

1.  **List**: Listing `BackupRestoreStatistics` in a namespace, filtering by `testudo.softcdata.com/owner-kind` (e.g., "AppBackup").
2.  **Sum**: Iterating through the list and summing the fields (`Total`, `Completed`, `Failed`, etc.).

**Example (Go):**

```go
// Aggregate statistics for all AppBackups in "disaster-system"
stats, err := helper.AggregateStatistics(ctx, cli, "disaster-system", map[string]string{
    "testudo.softcdata.com/owner-kind": "AppBackup",
})
```

## Implementation Details

*   **Helper Package**: `pkg/helper/statistics_helper.go` contains the `StatisticsHelper` interface and implementation, including `SyncStats` and `AggregateStatistics`.
*   **Integration**:
    *   `AppBackup` controller syncs statistics for its managed Velero Backups.
    *   `AppRestore` controller syncs statistics for itself (1:1 mapping).

## Future Work

*   **Global Aggregation**: If cross-namespace aggregation is needed, a Cluster-scoped CR or a dedicated aggregator component might be considered, but for now, client-side aggregation per namespace is sufficient.
*   **History/Retention**: The `EventLog` in the status provides a short history of changes. Long-term retention should be handled by external monitoring systems (Prometheus) scraping these CRs.
