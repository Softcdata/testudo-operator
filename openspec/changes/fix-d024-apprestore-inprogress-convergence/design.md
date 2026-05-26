# D-024 / D-025 设计：AppRestore 收敛增强

## 1. 现状问题

当前 `AppRestore` 在恢复运行态异常时主要依赖 1 小时硬超时退出，导致两类场景长期悬挂：

1. D-024：`InProgress` 且 `itemsRestored==totalItems`，但无 `completionTimestamp`。
2. D-025：恢复对象长期“未启动”（phase 空/`New`，且无 start/completion），或命中 Velero 重启窗口的瞬态失败（`during the server starting`）。

## 2. 目标

1. D-024 / D-025 都能在有界时间内收敛到终态。
2. 保持对正常恢复路径的兼容。
3. 不引入新的 API 入参或 CRD schema 变更。

## 3. 状态判定模型

### 3.1 D-024 进度完成卡死判定

命中以下全部条件时触发 `RestoreProgressStalled`：

- `restore.status.phase == InProgress`
- `restore.status.progress.totalItems > 0`
- `restore.status.progress.itemsRestored == totalItems`
- `restore.status.completionTimestamp == nil`
- `now - baseTime > ProgressCompleteGrace`（默认 5 分钟）

`baseTime` 优先使用 `startTimestamp`，否则回退 `creationTimestamp`。

### 3.2 D-025 启动卡死判定

命中以下全部条件时触发 `RestoreStartupStalled`：

- `restore.status.phase == "" || restore.status.phase == New`
- `restore.status.startTimestamp == nil`
- `restore.status.completionTimestamp == nil`
- `now - creationTimestamp > RestoreStartupGrace`（默认 5 分钟）

### 3.3 D-025 重启瞬态失败判定

当 `phase in (PartiallyFailed, Failed)` 且 `failureReason` 包含 `during the server starting`（或等价关键字）时，判定为可重试瞬态失败。

### 3.4 自愈与失败边界

- 第一次命中（任一异常判定）：执行一次自动重试（删除 restore -> 重新创建）。
- 再次命中：进入 `PhaseFailed`，`reason=RestoreStalledAfterRetry`。

## 4. 幂等记录

不新增 CRD 字段，使用 AppRestore label 记录自动重试计数：

- key: `testudo.softcdata.com/app-restore-auto-retry-count`
- `0` 或不存在：允许自动重试
- `>=1`：禁止再次自动重试

`NotFound` 分支据此区分“自动重试后的预期删除”与“异常删除”。

## 5. PVR 检测策略调整

将 PVR 检测从 `restorePVs` 条件中解耦：

- 只要 restore 处于运行态，均执行最佳努力探测。
- 探测失败仅记录日志，不打断主流程。
- 探测到失败或长期 pending 则按既有失败路径终止。

## 6. 兼容性

- 保持 `Completed/Failed/PartiallyFailed` 主流程语义。
- 仅对“重启瞬态失败”增加优先重试逻辑。
- 不影响 API/CRD schema，保留硬超时兜底。

## 7. 验收标准

1. D-024 用例不再无限 `InProgress`，在有界时间进入终态。
2. D-025 启动卡死或重启瞬态失败不再长期悬挂。
3. 自动重试最多一次，且有明确事件与 `reason/message`。
4. `restorePVs=false` 场景仍可探测并收敛 PVR 异常。
