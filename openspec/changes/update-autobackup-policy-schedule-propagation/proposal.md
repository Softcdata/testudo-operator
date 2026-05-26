# Change: AutoBackup 策略更新必须传播到 Velero 自动备份调度

## Why

`DisasterPolicy(type=AutoBackup)` 是 Velero 自动备份的独立策略面，不能按容灾实例的 `DataSync/ResourceSync` 策略引用关系处理。

当前用户修改 AutoBackup 策略的 `spec.schedule` 或 `spec.ttl` 后，`DisasterPolicy` 本身会更新，但既有 `AppBackup` 与远端 Velero `Schedule` 不一定被重新收敛：

- `DisasterPolicyReconciler` 只校验策略、同步标签和状态，不会触发引用该策略的 `AppBackup`。
- `AppBackup` 只在自身 reconcile 时读取 `spec.disasterPolicy`，无法稳定感知策略更新。
- `CreateVeleroSchedule` 对已存在的 Velero `Schedule` 目前只补 cleanup labels，不会比较并更新 `spec.schedule`、`spec.template.ttl` 等调度配置。

这会导致前端看起来策略定时时间已修改，但 Velero 实际自动备份仍按旧 schedule/ttl 运行。

## What Changes

- 明确 `AutoBackup` 与容灾同步策略分离：
  - `DataSync/ResourceSync` 策略由 `DisasterConfig` / `DisasterInstance` 引用。
  - `AutoBackup` 策略由 `AppBackup.spec.disasterPolicy` 或自动备份入口引用。
- `AppBackup` 控制器必须 watch `DisasterPolicy(type=AutoBackup)` 的变更，并重新入队引用该策略的 `AppBackup`。
- 引用 AutoBackup 策略的 `AppBackup` 必须以策略为单一来源，收敛有效：
  - `spec.schedule`
  - `spec.template.ttl`
  - `spec.paused` 或策略禁用后的调度暂停语义
- 已存在的 Velero `Schedule` 必须被更新或在必要时删除重建，确保实际运行配置与策略一致。
- 查询和验收必须以 Velero `Schedule` 的实际 `spec.schedule/spec.template.ttl/spec.paused` 为准，而不是只看 `DisasterPolicy` CR。
- Server 必须对外提供系统级 Velero 自动备份执行统计接口，按 `period=7d|30d|90d` 汇总自动备份成功数、失败数、总数和百分比，用于首页“计划备份执行情况”图表。

## Non-Goals

- 不改变 `DataSync` / `ResourceSync` 策略继承模型。
- 不把 AutoBackup 策略加入 `DisasterConfig` 或 `DisasterInstance` 引用链路。
- 不改变 Velero 原生 Schedule/Backup 的语义。
- 不要求历史 Velero Backup 的 TTL 被 retroactive 修改；策略更新只影响后续由 Schedule 创建的 Backup。
- 不要求一次性手动 `AppBackup` 使用 AutoBackup 策略。
- 自动备份统计接口不统计容灾 `DataSync/ResourceSync` 产生的同步备份，也不统计手动一次性备份。

## Impact

- Affected specs:
  - `disaster-policy`
  - `app-backup`
  - `backup-restore-statistics`
- Affected code:
  - `internal/controller/disasterpolicy_controller.go`
  - `internal/controller/appbackup/appbackup_controller.go`
  - `internal/controller/appbackup/appbackup_ready.go`
  - `pkg/apis/disaster/v1/appbackup_types.go`（仅当需要新增状态/注解字段时）
  - `internal/controller/appbackup/*_test.go`
  - `internal/controller/disasterpolicy_controller_test.go`
- Cross-repo impact:
  - `disaster-server`：策略更新接口无需按容灾实例引用判断 AutoBackup 是否“未使用”；自动备份详情需要展示实际 schedule/ttl；新增系统级自动备份执行统计接口。
  - `cluster-disaster-web`：编辑 AutoBackup 策略后应提示“调度将收敛到 Velero Schedule”，并可刷新展示实际生效值；首页“计划备份执行情况”图表调用新统计接口，支持 7 天、30 天、90 天切换。

## Migration

- 存量 `AutoBackup` 策略不需要数据迁移。
- 存量引用 AutoBackup 策略的 `AppBackup` 在下一次 policy 或自身 reconcile 后收敛。
- 存量 Velero `Schedule` 若存在旧配置，控制器必须更新到当前策略配置。
- 若 Velero `Schedule` 某字段无法原地更新，控制器必须删除并重建 Schedule，并记录事件说明。
