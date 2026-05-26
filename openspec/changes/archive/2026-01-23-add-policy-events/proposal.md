# 变更：为策略 (DisasterPolicy) 添加全局事件上报

## Why

当前全局事件规范中尚未包含 `DisasterPolicy` 资源的事件定义。策略是用户管理备份行为的核心配置对象，其生命周期操作（创建、启用、禁用、编辑、删除）同样需要纳入全局事件体系，以便用户在事件历史中追踪策略变更记录。

## What Changes

### 1. 更新全局事件规范

在 `openspec/specs/global-events/spec.md` 中添加 Policy 事件定义：

| 资源类型 | Action | Task 名称格式 | 备注 |
|----------|--------|--------------|------|
| **DisasterPolicy** | 创建策略 | `创建策略 <name>` | Finalizer 添加后 Started → Reconcile 成功后 Finished |
| | 编辑策略 | `编辑策略 <name>` | Generation 变化时 |
| | 启用策略 | `启用策略 <name>` | State: Disabled → Enabled |
| | 禁用策略 | `禁用策略 <name>` | State: Enabled → Disabled |
| | 删除策略 | `删除策略 <name>` | 删除前发射事件 |

### 2. 修改 DisasterPolicyStatus

添加以下字段用于追踪状态变化：

```go
type DisasterPolicyStatus struct {
    // ...existing fields...
    
    // ObservedGeneration 记录上次处理的 Generation
    ObservedGeneration int64 `json:"observedGeneration,omitempty"`
    
    // LastEventPhase 记录上次发射事件时的状态，用于防抖
    LastEventPhase string `json:"lastEventPhase,omitempty"`
    
    // LastState 记录上次的 State，用于检测启用/禁用
    LastState PolicyState `json:"lastState,omitempty"`
}
```

### 3. 修改 DisasterPolicyReconciler

在 `disasterpolicy_controller.go` 中添加事件发射逻辑：

1. **创建事件**: Finalizer 添加后发射 Started，首次 Reconcile 成功后发射 Finished
2. **编辑事件**: `Generation > ObservedGeneration` 时发射
3. **启用/禁用事件**: 检测 `Spec.State` 变化
4. **删除事件**: 在 `handleDelete` 中发射

### 4. 简化的事件模式（可选）

由于策略是简单的配置对象（无外部依赖），可以简化为**仅发射 Finished 事件**：

- 创建：Reconcile 成功后直接发射 "创建策略 Success"
- 编辑/启用/禁用：状态变化时直接发射 "编辑策略 Success"
- 删除：移除 Finalizer 前发射 "删除策略 Success"

## Impact

- **disaster-operator**: 
  - `pkg/apis/disaster/v1/disasterpolicy_types.go` - 添加 Status 字段
  - `internal/controller/disasterpolicy_controller.go` - 添加事件发射逻辑
- **disaster-operator**: `openspec/specs/global-events/spec.md` - 添加 Policy 事件目录

## 设计决策

1. **Operator 端发射事件**：与 Cluster/Storage 保持一致，无论 API 还是 kubectl 操作都能触发事件
2. **使用 ObservedGeneration**：追踪 Spec 变化，避免重复发射
3. **区分启用/禁用**：通过记录 `LastState` 来检测状态切换
