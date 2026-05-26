---
name: implement-group-sync-wait
description: 实现组操作同步等待机制，确保编排按配置的串行/并行模式正确执行
status: draft
---

# 组操作同步等待机制

## 背景

DisasterGroup 的核心设计目的就是**编排**：根据 Policy 配置，按串行或并行模式执行多个实例的容灾操作。但当前 `handleSync` 存在问题：

```go
// 当前行为：触发同步后立即标记完成
operation.Status.State = disasterv1.OperationStateCompleted
operation.Status.Message = "同步已触发"
```

这导致所有层级的同步操作几乎同时开始执行，**编排完全失效**。

## 问题本质

**组操作的子 Operation 必须等待实际操作完成**，无论是串行还是并行：

| 场景 | 期望行为 |
|-----|---------|
| 串行 (Layer-by-Layer) | Level-0 所有实例完成 → Level-1 开始 |
| 并行 (All at Once) | 所有实例同时开始，全部完成后组操作才算完成 |

只有子 Operation 正确报告完成状态，父 Operation 才能做出正确的编排决策。

## 设计方案

### 核心思路

**所有组内子 Operation 必须等待实际完成**，不仅仅是"触发"。

判断标准：子 Operation 关联到父 Operation（通过 OwnerRef 或 label）。

### 操作类型完成条件

| 操作类型 | 完成条件 |
|---------|---------|
| `failover` | 已有步骤化逻辑，无需修改 |
| `reprotect` | 已有步骤化逻辑，无需修改 |
| `undo` | 已有步骤化逻辑，无需修改 |
| `pause` | 设置 Paused=true 后立即完成，无需等待 |
| `resume` | 设置 Paused=false 后立即完成，无需等待 |
| `syncdata` | 等待 DataSync.Status.State == Ready |
| `syncresource` | 等待 ResourceSync.Status.State == Ready |
| `synconce` | 等待 DataSync 和 ResourceSync 都 Ready |

### 修改 handleSync

```go
func (r *DisasterOperationReconciler) handleSync(ctx context.Context, log logr.Logger, 
    operation *disasterv1.DisasterOperation, syncData bool, syncResource bool) (ctrl.Result, error) {
    
    instance := &disasterv1.DisasterInstance{}
    if err := r.Get(ctx, client.ObjectKey{...}, instance); err != nil {
        operation.Status.State = disasterv1.OperationStateFailed
        operation.Status.Message = "DisasterInstance 未找到"
        return ctrl.Result{}, r.Status().Update(ctx, operation)
    }
    
    now := time.Now().Format(time.RFC3339)
    triggered := false
    
    // 1. 触发同步（如果尚未触发）
    if syncData && instance.Status.DataSyncName != "" {
        ds := &disasterv1.DataSync{}
        if err := r.Get(ctx, client.ObjectKey{...}, ds); err == nil {
            // 检查是否需要触发（避免重复触发）
            if ds.Spec.Trigger.Manual != now {
                ds.Spec.Trigger.Manual = now
                // 传递 TraceID...
                r.Update(ctx, ds)
                triggered = true
            }
        }
    }
    
    if syncResource && instance.Status.ResourceSyncName != "" {
        rs := &disasterv1.ResourceSync{}
        if err := r.Get(ctx, client.ObjectKey{...}, rs); err == nil {
            if rs.Spec.Trigger.Manual != now {
                rs.Spec.Trigger.Manual = now
                // 传递 TraceID...
                r.Update(ctx, rs)
                triggered = true
            }
        }
    }
    
    // 首次触发后记录时间戳
    if triggered && operation.Status.Message == "" {
        operation.Status.Message = fmt.Sprintf("同步已于 %s 触发，等待完成...", now)
        r.Status().Update(ctx, operation)
    }
    
    // 2. 检查同步状态
    allReady := true
    var pendingInfo string
    
    if syncData && instance.Status.DataSyncName != "" {
        ds := &disasterv1.DataSync{}
        if err := r.Get(ctx, client.ObjectKey{...}, ds); err == nil {
            if ds.Status.State != disasterv1.SyncStateReady {
                allReady = false
                pendingInfo = fmt.Sprintf("DataSync: %s", ds.Status.State)
            }
        }
    }
    
    if syncResource && instance.Status.ResourceSyncName != "" {
        rs := &disasterv1.ResourceSync{}
        if err := r.Get(ctx, client.ObjectKey{...}, rs); err == nil {
            if rs.Status.State != disasterv1.SyncStateReady {
                allReady = false
                pendingInfo = fmt.Sprintf("ResourceSync: %s", rs.Status.State)
            }
        }
    }
    
    // 3. 超时检查
    if operation.Spec.TimeoutMinutes > 0 && operation.Status.StartTime != nil {
        elapsed := time.Since(operation.Status.StartTime.Time)
        if elapsed > time.Duration(operation.Spec.TimeoutMinutes)*time.Minute {
            operation.Status.State = disasterv1.OperationStateFailed
            operation.Status.Message = fmt.Sprintf("同步超时 (已等待 %v)", elapsed.Round(time.Second))
            r.Recorder.Event(operation, "Warning", "Timeout", operation.Status.Message)
            return ctrl.Result{}, r.Status().Update(ctx, operation)
        }
    }
    
    // 4. 等待或完成
    if !allReady {
        log.V(1).Info("等待同步完成", "pending", pendingInfo)
        return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
    }
    
    // 所有同步已完成
    operation.Status.State = disasterv1.OperationStateCompleted
    operation.Status.CompletionTime = &metav1.Time{Time: time.Now()}
    operation.Status.Message = "同步已完成"
    r.Recorder.Event(operation, "Normal", "SyncCompleted", "同步已完成")
    return ctrl.Result{}, r.Status().Update(ctx, operation)
}
```

### 状态流转图

```
                    ┌─────────────────────────────────┐
                    │     DisasterOperation (Sync)    │
                    └─────────────────────────────────┘
                                    │
                           Pending → Running
                                    │
                    ┌───────────────┴───────────────┐
                    │           触发同步            │
                    │  ds.Spec.Trigger.Manual = now │
                    │  rs.Spec.Trigger.Manual = now │
                    └───────────────┬───────────────┘
                                    │
                    ┌───────────────▼───────────────┐
                    │         检查同步状态          │
                    │  DataSync.Status.State?       │
                    │  ResourceSync.Status.State?   │
                    └───────────────┬───────────────┘
                                    │
              ┌─────────────────────┼─────────────────────┐
              │                     │                     │
              ▼                     ▼                     ▼
        InProgress              Failed                 Ready
              │                     │                     │
              │                     ▼                     ▼
              │           Operation.State=Failed   Operation.State=Completed
              │                                           │
              └────── RequeueAfter 5s ────────────────────┘
```

## 文件变更

| 文件 | 变更内容 |
|-----|---------|
| `internal/controller/disasteroperation/controller.go` | 重写 `handleSync` 方法 |

## 对现有行为的影响

| 场景 | 变更前 | 变更后 |
|-----|-------|-------|
| 独立实例执行 sync-resource | 触发后立即完成 | 触发后等待 Ready |
| 组操作执行 sync-resource | 触发后立即完成（Bug） | 触发后等待 Ready |

**注意**: 这会改变独立实例操作的行为，但这是**更合理的语义** - "sync" 操作意味着"同步完成"，而不仅仅是"触发同步"。

如果用户只想触发同步而不等待，可以：
1. 直接通过修改 DataSync/ResourceSync CRD 的 `Trigger.Manual` 字段触发
2. (未来) 添加 `trigger-sync` 操作类型

## 测试计划

1. **单元测试**: 验证 handleSync 等待逻辑
2. **E2E 测试**:
   - 两层串行组执行 sync-resource，验证 Level-0 完成后 Level-1 才开始
   - 超时测试
   - 失败传播测试

## 实施步骤

1. [ ] 重写 handleSync 方法
2. [ ] 测试独立实例 sync 操作
3. [ ] 测试两层串行组 sync 操作
4. [ ] 验证超时机制
