## ADDED Requirements

### Requirement: AppBackup 必须以 AutoBackup 策略作为调度配置来源

当 `AppBackup.spec.disasterPolicy` 引用 `DisasterPolicy(type=AutoBackup)` 时，AppBackup 控制器必须 (MUST) 使用策略中的 `schedule`、`ttl` 与 `state` 计算自动备份的实际调度配置。

#### Scenario: AppBackup 继承 AutoBackup 策略定时时间

- **Given** 一个 `DisasterPolicy` 的 `spec.type` 为 `AutoBackup`
- **And** 该策略配置 `spec.schedule=0 2 * * *`
- **And** 一个 `AppBackup` 的 `spec.disasterPolicy` 引用该策略
- **When** AppBackup 控制器处理该 AppBackup
- **Then** 该 AppBackup 的有效调度表达式必须为 `0 2 * * *`
- **And** 其下游 Velero Schedule 的 `spec.schedule` 必须为 `0 2 * * *`

#### Scenario: AppBackup 继承 AutoBackup 策略 TTL

- **Given** 一个 `DisasterPolicy` 的 `spec.type` 为 `AutoBackup`
- **And** 该策略配置 `spec.ttl=72h`
- **And** 一个 `AppBackup` 的 `spec.disasterPolicy` 引用该策略
- **When** AppBackup 控制器处理该 AppBackup
- **Then** 该 AppBackup 的有效 TTL 必须为 `72h`
- **And** 其下游 Velero Schedule 的 `spec.template.ttl` 必须为 `72h`

#### Scenario: AutoBackup 策略配置优先于 AppBackup 表单残留值

- **Given** 一个 `AppBackup` 的 `spec.disasterPolicy` 引用 AutoBackup 策略
- **And** 该策略配置 `spec.schedule=0 2 * * *`
- **And** 该策略配置 `spec.ttl=72h`
- **And** 该 AppBackup 当前保存的 `spec.schedule` 或 `spec.template.ttl` 与策略不一致
- **When** AppBackup 控制器处理该 AppBackup
- **Then** 下游 Velero Schedule 必须使用策略中的 schedule 与 ttl
- **And** 系统不得继续使用 AppBackup 表单中的旧 schedule 或旧 ttl

### Requirement: AppBackup 必须更新既有 Velero Schedule

AppBackup 控制器必须 (MUST) 对既有 Velero `Schedule` 执行配置收敛，而不仅是在 Schedule 不存在时创建。

#### Scenario: 更新既有 Velero Schedule 的定时时间

- **Given** 一个 AppBackup 已经创建了 Velero Schedule
- **And** 该 Velero Schedule 当前 `spec.schedule=0 0 * * *`
- **And** 该 AppBackup 的有效调度表达式已经变为 `0 2 * * *`
- **When** AppBackup 控制器重新处理该 AppBackup
- **Then** 既有 Velero Schedule 的 `spec.schedule` 必须更新为 `0 2 * * *`
- **And** 不得因为 Schedule 已存在而跳过更新

#### Scenario: 更新既有 Velero Schedule 的 TTL

- **Given** 一个 AppBackup 已经创建了 Velero Schedule
- **And** 该 Velero Schedule 当前 `spec.template.ttl=24h`
- **And** 该 AppBackup 的有效 TTL 已经变为 `72h`
- **When** AppBackup 控制器重新处理该 AppBackup
- **Then** 既有 Velero Schedule 的 `spec.template.ttl` 必须更新为 `72h`
- **And** 后续由该 Schedule 创建的 Velero Backup 必须继承新的 TTL

#### Scenario: 禁用策略时暂停既有 Velero Schedule

- **Given** 一个 AppBackup 引用 AutoBackup 策略
- **And** 该策略当前 `spec.state=Disabled`
- **And** 该 AppBackup 已经创建了 Velero Schedule
- **When** AppBackup 控制器重新处理该 AppBackup
- **Then** 既有 Velero Schedule 必须被设置为暂停状态
- **And** 系统不得继续按旧 schedule 创建新的 Velero Backup

#### Scenario: 无法原地更新时安全重建 Velero Schedule

- **Given** 一个 AppBackup 已经创建了 Velero Schedule
- **And** 控制器尝试更新该 Schedule 的有效配置
- **And** Kubernetes 或 Velero 拒绝原地更新
- **When** 控制器处理该错误
- **Then** 控制器必须安全删除并重建该 Velero Schedule
- **And** 控制器不得删除该 Schedule 已经创建的历史 Velero Backup
- **And** 控制器必须记录事件说明 Schedule 已重建
