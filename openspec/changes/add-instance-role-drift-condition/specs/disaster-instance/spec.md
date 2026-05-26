## ADDED Requirements

### Requirement: DisasterInstance 必须以 Conditions 持续表达真实主从漂移状态
系统必须 (MUST) 使用 `DisasterInstance.status.conditions` 持续表达当前真实主从关系是否偏离该实例的稳态期望。

#### Scenario: Protected 稳态下识别真实主从漂移
- **Given** 一个 `DisasterInstance` 处于 `Protected` 稳态
- **And** 下游真实副本分布不符合该稳态期望
- **When** operator 执行实例调谐
- **Then** operator 必须写入一个 `type=RoleDrift` 且 `status=True` 的 condition

#### Scenario: Active 稳态下识别真实主从漂移
- **Given** 一个 `DisasterInstance` 处于 `Active` 稳态
- **And** `status.primaryCluster/status.secondaryCluster` 表示当前期望主备关系
- **And** 下游真实副本分布不符合该期望关系
- **When** operator 执行实例调谐
- **Then** operator 必须写入一个 `type=RoleDrift` 且 `status=True` 的 condition

#### Scenario: 操作执行窗口内不持续报漂移
- **Given** 一个 `DisasterInstance` 正在执行 `Failover`、`Reprotect`、`Undo` 或 `Drill`
- **When** operator 执行实例调谐
- **Then** operator 不得将操作中的瞬时状态持续标记为 role drift

### Requirement: 不可安全解释的真实主备漂移必须使实例进入错误态
系统必须 (MUST) 在稳态巡检确认真实主备关系与期望关系发生不可安全解释的不一致时，将 `DisasterInstance` 置为错误态，阻断后续会改变运行期语义的实例操作。

#### Scenario: 稳态漂移时实例进入 Failed
- **Given** 一个 `DisasterInstance` 处于 `Protected` 或 `Active`
- **And** operator 判定 `RoleDrift=True`
- **When** operator 更新实例状态
- **Then** `status.fsmState` 必须为 `Failed`
- **And** `status.reason` 必须为 `RoleDriftDetected`
- **And** `status.message` 必须包含期望主备与真实副本摘要
- **And** `status.availableOperations` 不得包含 `failover`、`reprotect`、`undo`、`synconce`、`syncdata`、`syncresource`

#### Scenario: 双活不得直接返回错误
- **Given** 一个 `DisasterInstance` 处于稳态
- **And** `status.primaryCluster` 对应集群存在非 0 副本 workload
- **And** `status.secondaryCluster` 对应集群也存在非 0 副本 workload
- **When** operator 执行 role drift 巡检
- **Then** operator 不得仅因两侧均存在非 0 副本而将实例置为 `Failed`
- **And** operator 必须保留双活观测信号，reason 为 `BothActiveObserved` 或 `DualActiveAllowed`
- **And** operator 的 message 必须说明两侧均存在非 0 副本，但该事实不能用于自动判断真实主集群

#### Scenario: 显式跳过源缩零的 Failover 完成后允许双活
- **Given** 一个 `DisasterInstance` 的最近一次成功 Failover 使用了显式双活语义，例如跳过源集群缩零
- **And** `status.primaryCluster` 对应集群存在非 0 副本 workload
- **And** `status.secondaryCluster` 对应集群也存在非 0 副本 workload
- **When** operator 执行 role drift 巡检
- **Then** operator 不得将实例置为 `Failed`
- **And** operator 必须保留双活观测或双活允许的 condition 摘要

#### Scenario: 主备反转必须返回错误
- **Given** 一个 `DisasterInstance` 处于稳态
- **And** `status.primaryCluster` 对应集群所有采样 workload 均为 0 副本
- **And** `status.secondaryCluster` 对应集群存在非 0 副本 workload
- **When** operator 执行 role drift 巡检
- **Then** operator 必须写入 `RoleDrift=True` 且 reason 为 `RoleReversed`
- **And** 实例必须进入 `Failed` 且 `reason=RoleDriftDetected`

#### Scenario: 双 standby 必须返回错误
- **Given** 一个 `DisasterInstance` 处于稳态
- **And** `status.primaryCluster` 与 `status.secondaryCluster` 对应集群所有采样 workload 均为 0 副本
- **When** operator 执行 role drift 巡检
- **Then** operator 必须写入 `RoleDrift=True` 且 reason 为 `BothStandby`
- **And** 实例必须进入 `Failed` 且 `reason=RoleDriftDetected`

#### Scenario: 无法可靠判断时不得直接置 Failed
- **Given** 一个 `DisasterInstance` 处于稳态
- **And** operator 无法连接某个下游集群或无法列出采样 workload
- **When** operator 执行 role drift 巡检
- **Then** operator 必须写入 `RoleDrift=Unknown`
- **And** operator 不得仅因本次巡检未知而将实例置为 `Failed`

### Requirement: RoleDrift 错误必须可在真实关系恢复后自动恢复
系统必须 (MUST) 在实例因 `RoleDriftDetected` 进入 `Failed` 后继续复检真实关系，并在真实关系恢复为期望关系时自动清除错误。

#### Scenario: RoleDriftDetected 恢复为 Protected
- **Given** 一个 `DisasterInstance` 当前为 `Failed`
- **And** `status.reason` 为 `RoleDriftDetected`
- **And** 当前 `status.primaryCluster/status.secondaryCluster` 对应基础保护方向
- **And** 下游真实副本分布恢复为主集群非 0、备集群为 0
- **When** operator 执行实例调谐
- **Then** operator 必须将 `status.fsmState` 恢复为 `Protected`
- **And** operator 必须清理 `status.reason/status.message`
- **And** operator 不得改写 `status.primaryCluster/status.secondaryCluster`

#### Scenario: RoleDriftDetected 恢复为 Active
- **Given** 一个 `DisasterInstance` 当前为 `Failed`
- **And** `status.reason` 为 `RoleDriftDetected`
- **And** 当前 `status.primaryCluster/status.secondaryCluster` 对应故障切换后的反向保护方向
- **And** 下游真实副本分布恢复为当前主集群非 0、当前备集群为 0
- **When** operator 执行实例调谐
- **Then** operator 必须将 `status.fsmState` 恢复为 `Active`
- **And** operator 必须清理 `status.reason/status.message`
- **And** operator 不得改写 `status.primaryCluster/status.secondaryCluster`
