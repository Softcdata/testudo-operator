## ADDED Requirements

### Requirement: AutoBackup 策略必须与容灾同步策略分离

`DisasterPolicy(type=AutoBackup)` 必须 (MUST) 作为 Velero 自动备份策略独立处理，不得使用 `DisasterConfig.spec.dataSyncPolicy/resourceSyncPolicy` 或 `DisasterInstance.spec.dataSyncPolicy/resourceSyncPolicy` 判断其是否被使用、是否可删除或是否需要传播。

#### Scenario: AutoBackup 策略使用关系由 AppBackup 决定

- **Given** 一个 `DisasterPolicy` 的 `spec.type` 为 `AutoBackup`
- **And** 没有任何 `DisasterConfig` 或 `DisasterInstance` 引用该策略名
- **And** 存在一个 `AppBackup` 的 `spec.disasterPolicy` 引用该策略名
- **When** 系统判断该策略是否仍在使用
- **Then** 系统必须认为该策略仍被自动备份链路使用
- **And** 系统不得因为容灾实例链路未引用该策略而删除或忽略它

#### Scenario: DataSync 和 ResourceSync 策略仍由容灾链路判断

- **Given** 一个 `DisasterPolicy` 的 `spec.type` 为 `DataSync` 或 `ResourceSync`
- **When** 系统判断该策略是否由容灾实例链路使用
- **Then** 系统必须继续使用 `DisasterConfig` 与 `DisasterInstance` 的同步策略引用关系
- **And** 系统不得把该策略当作 AutoBackup 策略传播到 Velero Schedule

### Requirement: AutoBackup 策略配置变更必须触发引用者收敛

当 `DisasterPolicy(type=AutoBackup)` 的 `spec.schedule`、`spec.ttl` 或 `spec.state` 发生变化时，系统必须 (MUST) 触发所有引用该策略的 `AppBackup` 重新收敛。

#### Scenario: 修改 AutoBackup 策略定时时间触发 AppBackup 收敛

- **Given** 一个 `DisasterPolicy` 的 `spec.type` 为 `AutoBackup`
- **And** 该策略的 `spec.schedule` 从 `0 0 * * *` 修改为 `0 2 * * *`
- **And** 存在一个 `AppBackup` 的 `spec.disasterPolicy` 引用该策略
- **When** Operator 处理策略更新
- **Then** 引用该策略的 `AppBackup` 必须被重新入队或重新收敛
- **And** 其下游 Velero Schedule 必须最终使用新的 `0 2 * * *`

#### Scenario: 修改 AutoBackup 策略 TTL 触发 AppBackup 收敛

- **Given** 一个 `DisasterPolicy` 的 `spec.type` 为 `AutoBackup`
- **And** 该策略的 `spec.ttl` 从 `24h` 修改为 `72h`
- **And** 存在一个 `AppBackup` 的 `spec.disasterPolicy` 引用该策略
- **When** Operator 处理策略更新
- **Then** 引用该策略的 `AppBackup` 必须被重新入队或重新收敛
- **And** 其下游 Velero Schedule 的 `spec.template.ttl` 必须最终使用 `72h`

#### Scenario: 禁用 AutoBackup 策略停止后续调度

- **Given** 一个 `DisasterPolicy` 的 `spec.type` 为 `AutoBackup`
- **And** 该策略的 `spec.state` 从 `Enabled` 修改为 `Disabled`
- **And** 存在一个 `AppBackup` 的 `spec.disasterPolicy` 引用该策略
- **When** Operator 处理策略更新
- **Then** 引用该策略的 `AppBackup` 必须被重新入队或重新收敛
- **And** 其下游 Velero Schedule 必须进入暂停状态或等价的停调度状态
- **And** 系统不得继续按旧 schedule 产生新的 Velero Backup
