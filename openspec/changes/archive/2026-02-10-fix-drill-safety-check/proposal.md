# Change: Drill (灾备演练) 安全性增强 - 主备切换保护

## Why (背景)

在当前的演练流程中，`DisasterDrill` 在创建阶段（`Pending` -> `Ready`）就确定并锁定了目标集群 (`TargetCluster`)。如果在演练处于 `Ready` 状态但尚未执行期间发生了主备切换 (`Failover` 或 `Reprotect`)，原备集群可能会变为生产主集群。

此时如果用户继续确认执行演练，演练将会在**新的主集群**上执行恢复和扩容操作。这会导致：
1.  **资源抢占**: 演练负载与生产业务竞争资源。
2.  **潜在风险**: 虽然有命名空间隔离，但在生产集群上进行大规模恢复操作是不符合演练初衷的，且增加了误操作风险。

## What Changes (核心变更)

### 1. 二次校验机制 (Re-validation)

在 `DisasterDrill` 控制器的 `handleReady` 方法中（即用户确认执行 `spec.confirmed=true` 的时刻），增加一个强制的“二次校验”逻辑：

1.  重新获取关联的 `DisasterInstance` 最新状态。
2.  检查当前锁定的 `drill.Status.TargetCluster` 是否等于 `instance.Status.PrimaryCluster`。
3.  如果相等，说明目标集群已变为主集群（发生了拓扑变更），此时**拦截操作，标记演练失败**。

### 2. 代码实现

```go
// internal/controller/disasterdrill/controller.go

// Double Check: 在执行前再次校验拓扑结构
if drill.Spec.InstanceName != "" {
    instance := &disasterv1.DisasterInstance{}
    if err := r.Get(ctx, client.ObjectKey{Namespace: drill.Namespace, Name: drill.Spec.InstanceName}, instance); err != nil {
        // ... handle error ...
    }

    // 核心校验：目标集群不能是当前的主集群
    if drill.Status.TargetCluster == instance.Status.PrimaryCluster {
        drill.Status.State = disasterv1.DrillStateFailed
        drill.Status.Message = fmt.Sprintf("危险操作拦截：目标集群 %s 已变更为实例为主集群（可能发生主备切换），请删除并重建演练", drill.Status.TargetCluster)
        r.Recorder.Event(drill, "Warning", "TopologyChanged", drill.Status.Message)
        return ctrl.Result{}, r.Status().Update(ctx, drill)
    }
}
```

## Impact (影响)

### disaster-operator
- **DisasterDrill Controller**: 
  - 修改 `handleReady` 逻辑，增加对 `DisasterInstance` 的读取和校验。
  - 需要在 `SetupWithManager` 中确保有权限读取 `DisasterInstance` (已有)。

### 用户体验
- 如果在演练 Pending 期间发生主备切换，用户点击确认后会看到演练状态变为 `Failed`，并在 Event/Message 中看到明确的错误提示：“危险操作拦截...”。
- 用户需要删除并重建演练以适应新的拓扑结构。

## Non-Goals (非目标)
- 本次变更仅针对“目标变为主集群”的危险场景。对于“目标变为第三方集群”等其他拓扑变更暂不处理（通常不涉及安全风险）。
- 不自动更新 `TargetCluster`：为了保持状态的可预测性，我们选择拦截报错而不是自动修正目标，让用户显式感知拓扑变化。
