## ADDED Requirements

### Requirement: AppBackup 必须从 AutoBackup 策略继承 TTL

当 `AppBackup.spec.disasterPolicy` 引用 `type=AutoBackup` 的 `DisasterPolicy` 时，AppBackup 控制器必须 (MUST) 将策略 TTL 作为 Velero 自动备份的生效 TTL。

#### Scenario: 策略 TTL 写入 AppBackup 模板

- **Given** 一个 `DisasterPolicy` 的 `spec.type` 为 `AutoBackup`
- **And** 该策略配置 `spec.schedule=0 2 * * *`
- **And** 该策略配置 `spec.ttl=720h`
- **And** 一个 `AppBackup` 引用该策略
- **When** AppBackup 控制器处理该资源
- **Then** `AppBackup.spec.schedule` 必须收敛为 `0 2 * * *`
- **And** `AppBackup.spec.template.ttl` 必须收敛为 `720h`

#### Scenario: Velero Schedule 模板携带策略 TTL

- **Given** 一个引用 AutoBackup 策略的 `AppBackup`
- **And** 策略配置 `spec.ttl=720h`
- **When** AppBackup 控制器创建或更新 Velero `Schedule`
- **Then** `Schedule.spec.template.ttl` 必须为 `720h`
- **And** 由该 Schedule 后续创建的 Velero Backup 必须继承该 TTL

#### Scenario: 策略 TTL 优先于 AppBackup 表单 TTL

- **Given** 一个 `AppBackup` 引用 `AutoBackup` 策略
- **And** 该策略配置 `spec.ttl=720h`
- **And** `AppBackup.spec.template.ttl` 当前为 `24h`
- **When** AppBackup 控制器处理该资源
- **Then** `AppBackup.spec.template.ttl` 必须收敛为 `720h`
- **And** 自动备份详情必须回显 `720h`

#### Scenario: 策略未配置 TTL 时保持既有 AppBackup TTL

- **Given** 一个 `AppBackup` 引用 `AutoBackup` 策略
- **And** 该策略未配置 `spec.ttl`
- **And** `AppBackup.spec.template.ttl` 当前为 `24h`
- **When** AppBackup 控制器处理该资源
- **Then** `AppBackup.spec.template.ttl` 必须保持为 `24h`
- **And** Velero Schedule 模板必须继续使用 `24h`

### Requirement: 自动备份详情必须回显生效 TTL

自动备份详情 API 必须 (MUST) 回显实际用于 Velero Schedule 模板的 TTL。

#### Scenario: 查看策略模式自动备份详情

- **Given** 一个 `AppBackup` 引用 AutoBackup 策略
- **And** 该策略 TTL 已收敛到 `AppBackup.spec.template.ttl=720h`
- **When** 用户查看该自动备份详情
- **Then** 响应中的 `data.spec.ttl` 必须为 `720h`
- **And** 前端详情页必须显示该 TTL

#### Scenario: 查看历史备份过期时间

- **Given** Velero 已基于自动备份 Schedule 创建 Backup
- **And** 该 Backup 状态包含 `expiration`
- **When** AppBackup 控制器同步历史记录
- **Then** `status.history[].expiration` 必须继续回显该 Backup 的实际过期时间
- **And** 系统不得用策略 TTL 字符串替代每条历史记录的实际过期时间
