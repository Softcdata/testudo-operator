---
name: implement-operation-retry
description: 为 DisasterOperation 添加重试机制，提高容灾操作的鲁棒性
status: draft
---

# 容灾操作重试机制

## 背景

当前的容灾操作（特别是组操作）对瞬时故障非常敏感。如果底层同步（Velero 备份等）因网络抖动等原因失败，`DisasterOperation` 会立即失败，导致整个组的编排中断。

需要引入重试机制，允许在失败时自动重试一定次数。

## 设计方案

### 1. API 变更

在 `DisasterOperationSpec` 中添加重试策略配置。同时在 `DisasterGroup` 的 Policy 中支持配置默认重试策略。

`pkg/apis/disaster/v1/disasteroperation_types.go`:

```go
type RetryPolicy struct {
    // MaxRetries 最大重试次数，默认为 0（不重试）
    MaxRetries int32 `json:"maxRetries,omitempty"`
    // RetryIntervalSeconds 重试间隔秒数，默认为 5
    RetryIntervalSeconds int32 `json:"retryIntervalSeconds,omitempty"`
}

type DisasterOperationSpec struct {
    // ...
    RetryPolicy *RetryPolicy `json:"retryPolicy,omitempty"`
}

type DisasterOperationStatus struct {
    // ...
    // RetryCount 当前已重试次数
    RetryCount int32 `json:"retryCount,omitempty"`
    
    // NextRetryTime 下次重试时间
    NextRetryTime *metav1.Time `json:"nextRetryTime,omitempty"`
}
```

`pkg/apis/disaster/v1/disastergroup_types.go`:

```go
type DisasterGroupPolicy struct {
    // ...
    RetryPolicy *RetryPolicy `json:"retryPolicy,omitempty"`
}
```

### 2. 控制器逻辑变更 (handleSync)

修改 `handleSync` 逻辑，当检测到底层失败时：

1. 检查 `RetryCount < MaxRetries`。
2. 如果可以重试：
   - 增加 `RetryCount`。
   - 设置 `NextRetryTime = now + RetryInterval`。
   - 记录事件 "Retrying"。
   - **清除底层的 Trigger 标记** (可选，或者直接依赖重新触发逻辑)。
   - 返回 `RequeueAfter: LastRetryTime - now`。
3. 当到达 `NextRetryTime`：
   - 更新 `Trigger.Manual = now` 重新触发底层同步。
   - 清除 `NextRetryTime`。
4. 如果超过重试次数：
   - 标记 Operation 为 Failed。

### 3. 处理流程

```
handleSync:
  1. 检查是否处于 "等待重试" 状态 (NextRetryTime != nil)
     - 如果 now < NextRetryTime: RequeueAfter(remaining)
     - 如果 now >= NextRetryTime:
         - 触发同步 (Trigger=manual)
         - 清除 NextRetryTime
         - Requeue

  2. 如果底层状态 == Failed:
     - 检查 RetryCount < MaxRetries?
       - YES: 
           - RetryCount++
           - NextRetryTime = now + Interval
           - Recoder Event "Retrying..."
           - Requeue
       - NO:
           - Operation Failed

  3. 如果底层状态 == Ready:
     - Operation Completed
```

## 实施步骤

1. [ ] 修改 CRD 定义 (Types)
2. [ ] 生成 CRD Manifests (`make manifests`)
3. [ ] 修改 `handleSync` 实现重试逻辑
4. [ ] 在 `DisasterGroup` 创建 Operation 时传递 Policy 配置
5. [ ] 验证测试

## 默认值

建议默认 `MaxRetries=3`, `Interval=10s`。
