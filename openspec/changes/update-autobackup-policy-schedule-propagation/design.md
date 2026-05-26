## Context

自动备份策略与容灾同步策略是两条不同业务链路：

- 容灾同步策略：`DisasterConfig/DisasterInstance -> DataSync/ResourceSync -> AppBackup/AppRestore`
- Velero 自动备份策略：`DisasterPolicy(type=AutoBackup) -> AppBackup(spec.disasterPolicy) -> Velero Schedule -> Velero Backup`

本变更只处理第二条链路。判断 AutoBackup 策略是否生效、是否可删除、是否已传播，必须以 `AppBackup.spec.disasterPolicy` 和远端 Velero `Schedule` 为依据，不能以 `DisasterConfig.spec.dataSyncPolicy/resourceSyncPolicy` 为依据。

## Goals

- 修改 `DisasterPolicy(type=AutoBackup).spec.schedule/spec.ttl/spec.state` 后，引用它的自动备份调度必须自动收敛。
- 已存在的 Velero `Schedule` 必须被更新或重建，不允许长期保留旧 schedule/ttl。
- 策略禁用必须暂停或移除后续调度触发，不能继续产生新 Velero Backup。
- 对外验收口径必须查询实际 Velero `Schedule`，而不是只查询策略 CR。
- 对外提供系统级自动备份执行统计，支持 7 天、30 天、90 天窗口，服务首页“计划备份执行情况”图表。

## Non-Goals

- 不改变 `DataSync` / `ResourceSync` 策略继承模型。
- 不通过 `DisasterConfig` 或 `DisasterInstance` 管理 AutoBackup。
- 不修改已经创建完成的历史 Velero Backup。
- 不引入新的 `BackupPolicy` 使用链路。
- 不把容灾同步备份、手动一次性备份纳入自动备份执行统计。

## Decisions

### Decision: AppBackup 控制器负责 AutoBackup 策略传播

`AppBackup` 控制器是唯一已经掌握远端集群连接、BSL、Velero Schedule 创建和清理逻辑的控制器，因此策略传播应由 `AppBackupReconciler` 完成。

实现建议：

- 在 `AppBackupReconciler.SetupWithManager` 中 watch `DisasterPolicy`。
- 当 `DisasterPolicy.spec.type=AutoBackup` 变化时，查找同 namespace 下 `AppBackup.spec.disasterPolicy == policy.name` 的对象并入队。
- 入队后的 `AppBackup` 在 Ready 路径读取当前策略并计算 effective schedule/ttl/paused。

备选方案是让 `DisasterPolicyReconciler` 直接修改 AppBackup 或远端 Velero Schedule；该方案会让策略控制器承担远端集群和 Velero 资源职责，边界较差，因此不采用。

### Decision: 策略值是引用模式下的单一来源

当 `AppBackup.spec.disasterPolicy` 指向 AutoBackup 策略时：

- `AppBackup.spec.schedule` 的有效值来自 `policy.spec.schedule`。
- `AppBackup.spec.template.ttl` 的有效值来自 `policy.spec.ttl`；若策略未配置 TTL，则保留 AppBackup 现有模板 TTL。
- `policy.spec.state=Disabled` 必须让 Velero Schedule 进入暂停态，或采用等价的停调度机制。

如果用户同时在 AppBackup 表单里保存 schedule/ttl，策略值优先，避免“选择策略但实际仍用旧表单值”的歧义。

### Decision: Velero Schedule 需要原地收敛，必要时重建

`CreateVeleroSchedule` 需要升级为 ensure 语义：

- Schedule 不存在：创建。
- Schedule 存在但配置一致：不变。
- Schedule 存在但 `spec.schedule/spec.template.ttl/spec.paused/spec.template.storageLocation` 等有效字段不一致：更新。
- 如果 Velero 或 Kubernetes 拒绝某字段原地更新：删除旧 Schedule 并重建。

重建时必须避免删除历史 Backup；只处理 Schedule 本身。

### Decision: 自动备份执行统计由 Server 聚合，Operator 保证历史来源可区分

首页“计划备份执行情况”需要的是系统级聚合结果，而不是单个 `BackupRestoreStatistics` CR 的全量状态。建议由 `disaster-server` 提供只读聚合接口，Operator 侧继续保证 `AppBackup.status.history` 中的自动备份历史可被识别。

统计口径：

- 只统计自动备份链路：
  - `AppBackup.spec.disasterPolicy` 引用 `DisasterPolicy(type=AutoBackup)` 的 AppBackup。
  - 或历史记录/Velero Backup 带有自动备份类型标签。
- 不统计：
  - `DataSync` / `ResourceSync` 产生的 `AppBackup`。
  - `spec.schedule=@manual` 或无 schedule 的一次性手动备份。
- 时间窗口使用历史记录的完成时间优先；若无完成时间，可使用开始时间作为 fallback。
- 成功计数映射 `ManagedStatus=Completed` 或 Velero `Phase=Completed`。
- 失败计数映射 `ManagedStatus=Failed` 或 Velero `Failed/PartiallyFailed/FailedValidation`。
- 取消、进行中、未知不计入成功/失败，但可在响应中作为扩展字段返回。

接口建议：

```http
GET /apis/backuprestorestatistics.testudo.softcdata.com/v1/autobackups/execution-summary?period=7d
```

为符合现有统计接口参数风格，时间窗口参数使用 `period`。`range` 可作为短期兼容别名，但 API 规范和前端新接入应使用 `period`。

响应建议：

```json
{
  "period": "7d",
  "range": "7d",
  "total": 54,
  "success": {
    "count": 27,
    "percent": 50
  },
  "failed": {
    "count": 27,
    "percent": 50
  },
  "window": {
    "start": "2026-04-23T00:00:00+08:00",
    "end": "2026-04-30T00:00:00+08:00"
  }
}
```

`period` 必须限制为 `7d`、`30d`、`90d`，避免任意窗口导致大范围扫描不可控。百分比建议由 Server 计算，前端只渲染。

## Risks / Trade-offs

- **策略更新导致多个 AppBackup 同时重排队**：按 namespace list 并逐个 enqueue，规模可控；后续可加 field index 优化。
- **Schedule 删除重建期间错过一次触发**：优先原地 update；只有 update 被拒绝时才重建，并记录 Event。
- **禁用语义选择**：优先使用 `Schedule.spec.paused=true`，避免删除 Schedule 后丢失状态和审计线索。
- **存量 AppBackup spec 未持久化策略继承值**：验收以 Velero Schedule 为准；如需要详情回显，可在 AppBackup spec/status 中持久化 effective 值。
- **统计数据源不一致**：优先使用 `AppBackup.status.history`，并用自动备份标签/策略引用过滤；后续若引入专门统计 CR，可保持 API 响应结构不变。
- **跨时区窗口边界**：Server 使用请求时区或系统默认时区计算窗口，响应返回 start/end，避免前端与后端理解不一致。

## Verification Plan

- 单测：
  - AutoBackup policy schedule 更新会 enqueue 引用它的 AppBackup。
  - AppBackup 引用 policy 时，effective schedule/ttl 优先来自 policy。
  - Velero Schedule 已存在且 schedule/ttl 不一致时会 update。
  - policy Disabled 时 Velero Schedule paused。
  - DataSync/ResourceSync 策略变化不会触发 AutoBackup 传播。
  - 自动备份统计接口只统计 AutoBackup 历史，不混入容灾同步备份或手动备份。
  - 7d/30d/90d 窗口过滤和百分比计算正确。
- E2E：
  - 创建 AutoBackup policy。
  - 创建引用该 policy 的 AppBackup。
  - 验证 Velero Schedule 初始 schedule/ttl。
  - 修改 policy schedule/ttl。
  - 验证 Velero Schedule 更新为新值。
  - 禁用 policy。
  - 验证 Velero Schedule paused 且不再产生新 Backup。
  - 构造自动备份成功/失败历史，验证统计接口返回成功/失败数量和百分比，并驱动前端图表。
