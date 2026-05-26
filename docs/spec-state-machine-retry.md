# 规范：状态机重试机制与指数退避策略

## 1. 背景与目标

目前 Operator 中的状态机（如 `AppBackup`, `AppRestore`）在处理耗时操作或等待外部资源（如 Velero 同步、BSL 创建）时，往往采用简单的 `Requeue: true` 或固定的 `RequeueAfter`。

这种方式存在以下问题：
1.  **资源浪费**：如果条件长期不满足，控制器会无限循环重试，消耗 CPU 和 API Server 资源。
2.  **缺乏超时控制**：没有明确的“最大重试时间”，导致任务可能永远卡在某个中间状态（如 `Pending` 或 `Restoring`），用户无法得到明确的失败反馈。
3.  **重试风暴**：固定间隔的重试在故障恢复时可能引发请求风暴。

本规范旨在定义一套全局统一的**重试、超时与退避（Backoff）机制**。

## 2. 核心设计

### 2.1 状态字段扩展

所有涉及状态机的 CRD（`AppBackup`, `AppRestore`, `DisasterConfig`）的 `Status` 结构中，建议包含以下标准字段用于控制重试逻辑：

```go
type StatusBase struct {
    // Phase 当前所处的阶段
    Phase string `json:"phase,omitempty"`

    // LastPhaseTransitionTime 上次状态变更的时间戳
    // 用于计算当前阶段已持续的时间，判断是否超时
    LastPhaseTransitionTime *metav1.Time `json:"lastPhaseTransitionTime,omitempty"`

    // RetryCount 当前阶段内的重试次数
    // 用于计算指数退避的延迟时间
    // 每次状态变更时重置为 0
    RetryCount int32 `json:"retryCount,omitempty"`
    
    // Message 存储最后一次重试的错误信息或等待原因
    Message string `json:"message,omitempty"`
}
```

### 2.2 全局配置常量

建议在 `pkg/common` 或 `pkg/config` 中定义全局默认值：

```go
const (
    // DefaultMaxRetryDuration 默认最大重试时间 (例如 30分钟)
    // 超过此时间仍未完成当前阶段，强制标记为 Failed
    DefaultMaxRetryDuration = 30 * time.Minute

    // DefaultBaseDelay 初始重试延迟 (例如 5秒)
    DefaultBaseDelay = 5 * time.Second

    // DefaultMaxDelay 最大重试延迟上限 (例如 5分钟)
    // 防止指数退避导致等待时间过长
    DefaultMaxDelay = 5 * time.Minute
)
```

## 3. 算法逻辑

### 3.1 超时检查 (Timeout Check)

在每个 Handler 的入口处，首先检查是否超时。

```go
duration := time.Since(status.LastPhaseTransitionTime.Time)
if duration > MaxRetryDuration {
    // 1. 记录错误事件
    // 2. 将状态流转为 Failed
    // 3. 返回不再重试
    return PhaseFailed, Result{}, fmt.Errorf("operation timed out after %s", duration)
}
```

### 3.2 指数退避计算 (Exponential Backoff)

如果操作未完成需要重试，计算下一次 Requeue 的时间。

公式：`NextDelay = min(MaxDelay, BaseDelay * 2 ^ RetryCount)`

```go
func CalculateBackoff(retryCount int32) time.Duration {
    delay := DefaultBaseDelay * time.Duration(math.Pow(2, float64(retryCount)))
    if delay > DefaultMaxDelay {
        return DefaultMaxDelay
    }
    return delay
}
```

### 3.3 状态更新流程

1.  **进入新阶段时**：
    *   更新 `Phase` = NewPhase
    *   更新 `LastPhaseTransitionTime` = Now()
    *   重置 `RetryCount` = 0

2.  **重试时**：
    *   保持 `Phase` 不变
    *   增加 `RetryCount`++
    *   更新 `Status`
    *   返回 `ctrl.Result{RequeueAfter: CalculateBackoff(RetryCount)}`

## 4. 工具函数封装建议

建议在 `internal/utils/retry` 包中封装通用逻辑，简化 Handler 代码。

```go
// RetryHelper 封装重试逻辑
type RetryHelper struct {
    Client client.Client
    // ...
}

// CheckOrUpdateRetry 检查是否超时，并计算下一次重试时间
// 返回: (shouldStop, result, error)
func (h *RetryHelper) CheckOrUpdateRetry(ctx context.Context, obj client.Object, status *StatusBase) (bool, ctrl.Result, error) {
    // 1. 检查超时
    if time.Since(status.LastPhaseTransitionTime.Time) > DefaultMaxRetryDuration {
        return true, ctrl.Result{}, fmt.Errorf("timeout")
    }

    // 2. 计算 Backoff
    delay := CalculateBackoff(status.RetryCount)

    // 3. 更新 RetryCount (内存中更新，调用方负责 Update Status)
    status.RetryCount++
    
    return false, ctrl.Result{RequeueAfter: delay}, nil
}
```

## 5. 改造示例 (AppRestore PendingHandler)

```go
func (h *PendingHandler) Handle(...) {
    // 1. 业务逻辑检查 (例如 BSL 是否就绪)
    if !isBSLReady {
        // 调用工具函数处理重试逻辑
        stop, res, err := retryHelper.CheckOrUpdateRetry(ctx, appRestore, &appRestore.Status)
        if stop {
            // 超时处理：标记为 Failed
            return PhaseFailed, ctrl.Result{}, err
        }
        
        // 记录原因并等待
        appRestore.Status.Message = "Waiting for BSL to be ready..."
        // 注意：这里需要保存 Status 更新 RetryCount
        r.Status().Update(ctx, appRestore) 
        
        return PhasePending, res, nil
    }
    
    // ... 成功逻辑 ...
}
```

## 6. 优势

1.  **系统保护**：指数退避减少了对 API Server 和外部服务（如 AWS S3）的调用频率。
2.  **确定性**：明确的超时机制防止了“僵尸任务”。
3.  **可观测性**：通过 `RetryCount` 和 `Message`，运维人员可以清楚地看到任务卡住了多久以及重试了多少次。
