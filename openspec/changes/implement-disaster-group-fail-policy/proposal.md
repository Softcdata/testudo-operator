# Proposal: DisasterGroup 失败处理策略逻辑设计

## 背景
UI 新增了 `DisasterGroup` 的策略配置：
- `failPolicy`: 失败处理策略 (Continue/Stop)
- `timeoutMin`: 执行超时时间
- `parallelism`: 全局并行度

我们需要在 Operator 中实现这些策略的响应逻辑。

## 1. 原理
`DisasterOperation` Controller 负责 orchestrate 整个组的切换流程。它会监控 `Levels` 的执行进度。

### 1.1 状态追踪
在 `DisasterGroup` 的 `Status` 中，或者更准确地说是生成的 `DisasterOperation` CR 中，需要记录每个 Level 的执行状态和开始时间。

## 2. 策略逻辑

### 2.1 TimeoutMin (超时控制)
- **范围**: 该超时针对的是 **单个 Level** 的执行。
- **逻辑**: 
  - 当 Level N 开始执行时，记录 `LevelStartTime`。
  - Controller 轮询检查: `if Now - LevelStartTime > TimeoutMin`:
    - 标记该 Level 为 `TimedOut`。
    - 触发 `FailPolicy` 逻辑。

### 2.2 FailPolicy (失败/超时处理)
- **Stop (默认)**: 
  - 只要 Level 中有任何一个 Instance 操作失败 (Failure) 或超时 (TimedOut):
  - 立即停止后续 Level 的调度。
  - 将整个 Operation 标记为 `Failed`。
  - 已成功的实例保持原样，失败的保持原样（人工介入）。
- **Continue**:
  - 即使当前 Level 有实例失败或超时，记录错误日志/事件。
  - **继续** 调度下一个 Level。
  - 整个 Operation 最终标记为 `CompletedWithErrors`。

### 2.3 Parallelism (全局流控)
- **逻辑**: 
  - 即使 Level 定义了 `["A", "B", "C"]` (并行3个)，如果 `Parallelism=2`:
  - Controller 只能先调度 A, B。
  - 等 A 或 B 结束后，再调度 C。
  - 这需要一个简单的信号量或计数器机制：`RunningInstances < Parallelism`。

## 3. 实现计划
1.  修改 `DisasterOperation` Controller 的 Group Reconcile 逻辑。
2.  引入 `WorkQueue` 或简单的 Loop 来管理并行度。
3.  在 Status 中增加 `LevelStates` 记录。
