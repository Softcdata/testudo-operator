# Proposal: 容灾实例操作超时机制 (Instance Operation Timeout)

## 1. 背景与问题 (Background)

当前的容灾操作（`DisasterOperation`）缺乏超时控制机制。在实际场景中，可能会出现以下情况导致操作无限期挂起：
- **Velero 备份/恢复卡死**: Velero 任务一直处于 `InProgress` 状态。
- **K8s 资源无法就绪**: Pod 因镜像拉取失败或资源不足一直处于 `Pending`。
- **网络分区**: 跨集群通信中断，导致状态无法更新。

如果没有超时机制，用户只能通过手动删除或强制停止来干预，缺乏自动化的熔断保护，这对于生产环境的自动化容灾是不安全的。

## 2. 目标 (Goals)

1.  允许为 `DisasterInstance` 配置默认的操作超时时间。
2.  允许在发起单次操作（`DisasterOperation`）时指定/覆盖超时时间。
3.  控制器 (`DisasterOperationReconciler`) 能够检测超时并自动将操作状态流转为 `Failed` (Reason: TimedOut)。

## 3. 设计方案 (Design)

### 3.1 CRD 变更

#### 3.1.1 DisasterInstance (Spec)
在 `DisasterInstance` 的 Spec 中增加全局默认超时配置。

```yaml
type DisasterInstanceSpec struct {
    // ...
    
    // OperationTimeoutMinutes 定义该实例执行容灾操作的默认超时时间(分钟)
    // 默认值: 60 (1小时)
    // +optional
    OperationTimeoutMinutes int32 `json:"operationTimeoutMinutes,omitempty"`
}
```

#### 3.1.2 DisasterOperation (Spec)
在 `DisasterOperation` 中记录本次执行的具体超时时间。

```yaml
type DisasterOperationSpec struct {
    // ...
    
    // TimeoutMinutes 本次操作的超时时间(分钟)。
    // 如果未指定，则继承 DisasterInstance 的 OperationTimeoutMinutes。
    // +optional
    TimeoutMinutes int32 `json:"timeoutMinutes,omitempty"`
}
```

#### 3.1.3 DisasterOperation (Status)
状态中增加超时原因的枚举。

```go
const (
    // OperationReasonTimedOut 操作因超时被系统终止
    OperationReasonTimedOut = "TimedOut"
)
```

### 3.2 控制器逻辑 (Controller Logic)

修改 `internal/controller/disasteroperation/controller.go`:

1.  **初始化阶段**: 
    - 当创建 `DisasterOperation` 时，如果 `Spec.TimeoutMinutes` 为空，从关联的 `DisasterInstance` 读取配置并填充。
    - 如果实例也未配置，使用系统默认值 (如 60分钟)。

2.  **Reconcile 循环**:
    - 对于处于 `Running` 或 `Pending` 状态的操作：
    - 计算 `Deadline = CreationTimestamp + TimeoutMinutes`。
    - 检查 `CurrentTime > Deadline` ?
        - **Yes**: 
            - 标记 `Status.State = Failed`。
            - 设置 `Status.Message = "Operation timed out after X minutes"`。
            - 触发 Event `Warning: OperationTimedOut`。
            - (Option) 尝试触发 Cancellation 逻辑（如取消正在运行的 Velero 任务）。
        - **No**:
            - 计算剩余时间 `RequeueAfter = Deadline - CurrentTime`。
            - 返回 `ctrl.Result{RequeueAfter: ...}` 确保在超时时刻能被唤醒。

### 3.3 Server API 变更

更新 `ExecuteAction` 接口 (POST `/apis/.../instances/:name/actions`)，支持传入超时参数。

**Request Body**:
```json
{
  "operation": "failover",
  "config": {
    "force": true,
    "timeoutMinutes": 30  // 可选，覆盖默认配置
  }
}
```

## 4. 交互流程

1.  用户在 UI 设置 `DisasterInstance` 的 "默认超时时间" (比如 30分钟)。
2.  用户点击 "一键切换"。
3.  Server 创建 `DisasterOperation`，`Spec.TimeoutMinutes` 被置为 30。
4.  Operator 开始执行。
5.  如果 30分钟内未完成 (`Status != Completed`)，Operator 强制置为 Failed。
6.  UI 显示 "执行超时"。

## 5. 后续规划
- **超时回调 (Timeout Hooks)**: 超时后是否自动执行回滚 (Undo)？目前建议先仅做报警和停止，由人工介入决定回滚，避免二次破坏。
