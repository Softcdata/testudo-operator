# Change: 修复 DataSync/ResourceSync 调度时间忽略 DisasterPolicy 配置

## Why

用户在 `DisasterConfig` 中配置了 `DataSyncPolicy` 和 `ResourceSyncPolicy`（关联到具体的 `DisasterPolicy` CR，其中包含 Cron 调度表达式），但 DataSync 和 ResourceSync 的实际同步触发时间始终不符合策略设置。

**根因**：`DisasterInstanceController` 的 `ensureDataSync` 和 `ensureResourceSync` 函数中存在未完成的 `TODO` 逻辑。代码仅检查了 `DisasterConfig.Spec.DataSyncPolicy` 是否非空，但**未查询对应的 `DisasterPolicy` CR**，而是直接用硬编码的默认值（`*/15 * * * *` / `0 2 * * *`）覆盖调度表达式，导致策略完全失效。

**缺陷代码位置**：`internal/controller/disasterinstance/controller.go`
- `ensureDataSync` 函数，约第 398–401 行
- `ensureResourceSync` 函数，约第 438–441 行

```go
// BUG: 硬编码，忽略实际 DisasterPolicy 内容
if config.Spec.DataSyncPolicy != "" {
    // TODO: 查找 DisasterPolicy 并获取 schedule
    dataSync.Spec.Trigger.Schedule = "*/15 * * * *" // ← 硬编码
}
```

## What Changes

- **修复** `ensureDataSync`：从 `DisasterConfig.Spec.DataSyncPolicy` 中读取策略名称，查询对应的 `DisasterPolicy` CR，取其 `spec.schedule` 赋值给 `DataSync.Spec.Trigger.Schedule`
- **修复** `ensureResourceSync`：从 `DisasterConfig.Spec.ResourceSyncPolicy` 中读取策略名称，查询对应的 `DisasterPolicy` CR，取其 `spec.schedule` 赋值给 `ResourceSync.Spec.Trigger.Schedule`
- **防御处理**：若策略未配置（字段为空）或策略处于 `Disabled` 状态，则不设置 Schedule（与暂停行为一致）
- **防御处理**：若查询 `DisasterPolicy` 失败（不存在或无权限），应记录事件并以 `RequeueAfter` 重试，而不是静默使用硬编码值
- **更新 RBAC**：`DisasterInstanceController` 需要新增 `disasterpolicies` 的 `get;list;watch` 权限

## Impact

- Affected specs: 无现有规范（此为 Bug 修复，补充行为至符合预期）
- Affected code:
  - `internal/controller/disasterinstance/controller.go`（核心修复）
  - `internal/controller/disasterinstance/controller_test.go`（补充测试用例）
