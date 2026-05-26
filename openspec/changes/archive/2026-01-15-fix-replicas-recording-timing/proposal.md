---
created: 2026-01-15
status: completed
---

# 提案：修复副本数记录时机错误

## Why

当前 Failover 流程存在严重 bug，导致目标集群无法正确恢复工作负载副本数：

**问题流程**:
1. `executeScaleDownSource`: 先将源集群副本数缩容到 **0**
2. `executeFinalSync`: 触发 ResourceSync
3. `ResourceSync.recordReplicasToConfigMap`: 记录副本数（**此时已经是 0**）
4. `executeScaleUpTarget`: 尝试恢复副本数，但 ConfigMap 中记录的是 0

**结果**: 目标集群的 Deployment/StatefulSet 副本数始终为 0，应用无法启动。

## What Changes

### 方案 A: 在 ScaleDown 之前记录副本数 (推荐)
在 `executeScaleDownSource` 中，缩容之前**先记录**原始副本数到 ConfigMap。

**优点**: 逻辑清晰，副本数在真正缩容前被保存。

### 方案 B: 添加新步骤 RecordReplicas
在 Failover 步骤中添加一个新步骤 `RecordReplicas`，放在 `ScaleDownSource` 之前。

```go
steps := []string{
    FailoverStepPauseSchedules,
    FailoverStepRecordReplicas,    // 新增: 先记录副本数
    FailoverStepScaleDownSource,
    FailoverStepFinalSync,
    FailoverStepScaleUpTarget,
    FailoverStepSwitchRoles,
}
```

**优点**: 步骤明确，易于审计和调试。

### 推荐方案: 方案 A
方案 A 更简单，只需要修改 `executeScaleDownSource` 函数。

## Impact
*   **修复 Failover 流程**: 确保目标集群能正确恢复工作负载副本数。
*   **修复 Undo (撤销) 流程**: Undo 操作也使用相同的函数，同样受益。
*   **无 API 变更**: 不需要修改 CRD。
