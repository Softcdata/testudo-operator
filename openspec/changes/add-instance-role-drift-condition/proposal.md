# Change: 为容灾实例增加真实主从漂移 condition

## Why
当前实例详情只能看到 `PrimaryCluster/SecondaryCluster/AvailableOperations` 一类摘要信息，但不能持续表达“下游真实主从关系已经偏离当前期望状态”。

条目 23 的核心是在 `status.conditions` 上建立一个正式、可持续收敛的 role drift condition，而不是继续扩 `message` 或零散 reason 字段。

## What Changes
- `DisasterInstance` 引入正式的 role drift condition。
- 该 condition 只在稳态下判定，操作中的豁免窗口必须被显式定义。
- 当稳态巡检发现真实关系与 `status.primaryCluster/status.secondaryCluster` 期望发生不可安全解释的不一致时，实例必须进入错误态，阻断 failover/reprotect/undo/sync 等后续操作。
- 双活是平台支持的显式运行形态（例如 `skipScaleDownSource` 或 Drill 临时双活），不得仅因两侧副本均非 0 将实例置为错误态。
- server/web 通过实例错误状态与 condition 摘要进行展示，不再各自拼装 message。

## Non-Goals
- 不修改 failover `CheckReplicas` 的操作内校验语义。
- 不引入第二套并行状态容器。
- 不根据巡检结果自动改写 `status.primaryCluster/status.secondaryCluster`；真实关系修正必须由用户操作或正式的灾备操作完成。

## Impact
- Affected specs:
  - `disaster-instance`
- Affected code:
  - `internal/controller/disasterinstance/controller.go`
  - `pkg/helper/status_error.go`
- Cross-repo impact:
  - `disaster-server`：condition summary 聚合
  - `cluster-disaster-web`：列表卡片和详情高亮

## Relationship to Existing Changes
- 参考 active change：`add-instance-config-error-fsm-state`
- 本 change 继续使用 `disaster-instance` capability，但聚焦 role drift condition，不重叠 ConfigError 语义。
