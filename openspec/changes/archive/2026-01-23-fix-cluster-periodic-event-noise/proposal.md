# 变更: 修复集群定期检查重复发射事件问题

## Why

当前 `ClusterReconciler` 在定期健康检查（每分钟）成功后，会重复发射"创建集群完成"或"编辑集群完成"事件。这与 `StorageRepositoryReconciler` 的行为不一致——后者已使用 `wasAvailable` 变量区分"首次创建/恢复"和"定期检查"，避免了事件噪音。

**具体问题**：
1. 集群首次创建后进入 Ready 状态，正确发射"创建集群完成"事件
2. 但每次定期检查（`RequeueAfter: 1 * time.Minute`）成功后，由于 `ObservedGeneration` 和 `Generation` 相等且 `LastEventPhase == Ready`，虽然当前逻辑不会重复发射，但如果集群曾经短暂离线后恢复，会再次发射事件
3. 更重要的是：当前逻辑复杂且与 StorageRepository 实现不一致，增加维护成本

## What Changes

1. **引入 `wasReady` 变量**：参考 `storagerepository_controller.go:145` 的实现，在 Reconcile 入口处记录初始状态
2. **精确控制事件发射**：仅在状态从 非Ready → Ready 转变时发射"创建集群完成"事件
3. **统一代码风格**：使 Cluster 控制器的事件防抖逻辑与 StorageRepository 保持一致

## Impact

- **受影响代码**: `internal/controller/cluster_controller.go`
- **受影响规范**: `specs/development-standards/spec.md` (事件防抖规范)

## 关联问题

- 参考归档提案: `archive/2026-01-20-add-cluster-storage-events/`
- StorageRepository 已实现的正确模式: `storagerepository_controller.go:145-192`
