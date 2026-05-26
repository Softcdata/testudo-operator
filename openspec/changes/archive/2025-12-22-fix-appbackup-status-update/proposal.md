# Fix AppBackup Status Update Logic

## Summary
修复 AppBackup 控制器在状态为空时未能正确更新状态字段的问题，确保当发生错误（如集群未找到）时，状态能够正确反映为 `Pending` 或 `Failed`，并持久化到 ETCD。

## Motivation
当前 AppBackup 控制器在 Reconcile 循环中，如果 `Status.Status` 为空，会在内存中将其默认为 `Pending` 阶段进行处理。然而，如果处理过程中发生错误（例如引用的集群不存在），控制器虽然更新了 `Status.Reason` 和 `Status.Message`，但由于内存中的 `phase`（默认为 Pending）与 `nextPhase`（也是 Pending）相同，导致 `statusChanged` 判定为 false，从而跳过了对 `Status.Status` 字段的更新。

这导致用户在查询 AppBackup 资源时，看到 `status.phase` 为空，尽管 `reason` 和 `message` 包含了错误信息。这不仅影响用户体验，也可能导致依赖状态字段的自动化工具失效。

## Proposed Changes
修改 `AppBackupReconciler.Reconcile` 方法中的状态更新逻辑：
1.  在判断是否需要更新状态时，显式检查 `appBackup.Status.Status` 是否为空。
2.  如果 `nextPhase` 不为空且 `appBackup.Status.Status` 为空，强制标记 `statusChanged` 为 true，确保状态被持久化。
3.  确保即使在 `PendingHandler` 返回错误时，状态也能被正确更新为 `Pending`（或根据错误类型更新为 `Failed`，如果适用）。

## Alternatives Considered
- **在 PendingHandler 中返回 Failed**: 如果集群不存在，可以视为配置错误，直接返回 `PhaseFailed`。但这可能导致控制器停止重试（取决于 FailedHandler 的实现），而集群可能只是暂时不可用。保持 `Pending` 并附带错误信息通常是更稳健的做法，允许在集群恢复后自动恢复。
- **总是更新 Status**: 无论状态是否变化都执行 Update。这会增加 API Server 的负担，不推荐。
