# Fix AppBackup Deletion Stuck on Missing Cluster

## Summary
修复当引用的 Cluster 资源不存在时，AppBackup 无法被删除的问题。

## Motivation
当前 AppBackup 的删除逻辑（`DeletingHandler`）尝试连接到目标集群以清理外部资源（如 Velero Backup）。如果引用的 Cluster 资源不存在，`GetKubeClient` 会返回错误，导致 Reconcile 循环失败并重试，从而阻止了 Finalizer 的移除。这使得用户无法删除指向无效集群的 AppBackup 资源。

## Proposed Changes
修改 `internal/controller/appbackup/appbackup_state.go` 中的 `DeletingHandler.Handle` 方法：
1.  在调用 `r.ClientFactory.GetKubeClient` 后检查错误。
2.  如果错误类型为 "NotFound" (即 Cluster 资源不存在) 或其他表明无法连接的错误（视情况而定，但主要是 NotFound），则：
    -   记录一条警告日志，说明由于集群不存在无法清理外部资源。
    -   跳过 `deleteExternalResources` 调用。
    -   继续执行移除 Finalizer 的操作。
3.  对于其他类型的错误（如临时网络问题），保持当前的重试行为。

## Alternatives Considered
- **强制删除**: 用户可以使用 `kubectl patch` 手动移除 finalizer。但这不应该是常规操作流程。
- **无限重试**: 保持现状。但这会导致资源无法被清理，除非用户重新创建该 Cluster。
