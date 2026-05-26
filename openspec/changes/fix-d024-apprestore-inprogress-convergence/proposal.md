# Change: 修复 D-024 / D-025 AppRestore 收敛异常

## Why

当前恢复状态机在两类现场问题上都存在长时间不收敛风险：

1. D-024：`restore.phase=InProgress`，且 `itemsRestored==totalItems`（例如 `14/14`）但无 `completionTimestamp`，`AppRestore` 长时间停留在 `Restoring`。
2. D-025：在 Velero 重启窗口或异常窗口，可能出现恢复对象长期“未启动”（phase 空或 `New`，且 `start/completion` 为空）或触发已知的 `during the server starting` 瞬态失败，导致恢复流程不能稳定收敛。

当前控制器主要依赖 1 小时硬超时，无法满足演练和编排链路对分钟级可收敛性的要求。

## What Changes

### 1. 增加 D-024 软收敛判定

在 `Restoring` 状态下新增“进度已完成但相位未终止”的判定：

- 条件：
  - `restore.phase == InProgress`
  - `progress.totalItems > 0`
  - `progress.itemsRestored == progress.totalItems`
  - `completionTimestamp` 为空
  - 持续超过 `ProgressCompleteGrace`（默认 5 分钟）
- 处理：
  - 首次命中：删除并重建同名 Velero Restore（自动自愈）
  - 再次命中：进入 `Failed`，`reason=RestoreStalledAfterRetry`

### 2. 增加 D-025 启动卡死 / 重启瞬态失败收敛

新增两类异常判定并复用同一套“最多一次自动重试”策略：

- 启动卡死：
  - `phase` 为空或 `New`
  - `startTimestamp` 与 `completionTimestamp` 均为空
  - 超过 `RestoreStartupGrace`（默认 5 分钟）
- 重启瞬态失败：
  - `phase in (PartiallyFailed, Failed)`
  - `failureReason` 包含 `during the server starting`（或等价关键字）

处理方式同上：首次自动重试，二次命中失败收敛。

### 3. 调整 NotFound 分支误判逻辑

为避免自动重试后“预期删除”被误判为异常：

- 当检测到已进入自动重试流程时，允许 `Restore NotFound` 分支进入重建流程；
- 其余场景仍保持“异常删除即失败”的保护行为。

### 4. 放开 PVR 异常检查门控（不再绑定 `restorePVs`）

- 只要 Restore 处于运行态，就执行 PVR 异常探测（最佳努力）。
- 无 PVR 不影响主流程；探测失败只记录日志；探测到异常按既有失败路径收敛。

### 5. 可观测性增强

新增/复用事件与错误码：

- `RestoreProgressStalled`
- `RestoreAutoRetryTriggered`
- `RestoreStalledAfterRetry`
- `RestoreStartupStalled`

并在 `status.reason/status.message` 写入可读上下文（持续时长、重试次数、失败原因片段）。

## Non-Goals

- 不修改 Server/Web API 入参模型。
- 不新增 CRD 必填字段。
- 不改变非“重启瞬态失败”场景下 `PartiallyFailed -> AppRestore Succeeded` 的既有兼容语义。

## Compatibility

- 未触发上述异常判定的恢复流程行为保持不变。
- 仍保留原有硬超时兜底（1 小时）。
- 对历史实例与现有接口兼容。

## Impact

### Affected Specs

- `app-restore`（MODIFIED）

### Affected Components

- `internal/controller/apprestore/apprestore_restoring.go`
- `internal/controller/apprestore/*_test.go`

## Risks & Mitigations

1. 风险：软收敛阈值过短导致误判。
- 缓解：默认 5 分钟宽限，且仅在明确异常信号下触发。

2. 风险：自动重试引入副作用。
- 缓解：严格限制最多一次自动重试，重复命中直接失败并暴露原因。

3. 风险：PVR 探测增加控制器负载。
- 缓解：仅在运行态检查，保持最佳努力，不阻塞主流程。

## Rollout Plan

1. 单测覆盖 D-024 / D-025 关键路径与 `restorePVs=false` 探测路径。
2. 落地控制器逻辑与事件码。
3. 完成 e2e 回归，验证两类场景在有界时间内收敛终态。
