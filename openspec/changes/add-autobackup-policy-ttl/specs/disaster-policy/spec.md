## ADDED Requirements

### Requirement: AutoBackup 策略必须支持 TTL

`DisasterPolicy` 必须 (MUST) 在 `spec.type=AutoBackup` 时支持 `spec.ttl`，用于定义由该策略创建的 Velero Backup 保留时间。

#### Scenario: 创建带 TTL 的 AutoBackup 策略

- **Given** 用户创建 `DisasterPolicy`
- **And** `spec.type` 为 `AutoBackup`
- **And** `spec.ttl` 为有效 duration，例如 `720h`
- **When** Operator 与 Server 接收该策略
- **Then** 策略必须被接受
- **And** `spec.ttl` 必须持久化到 `DisasterPolicy` CR
- **And** 策略列表与详情接口必须回显 `spec.ttl`

#### Scenario: AutoBackup TTL 非法时拒绝或进入稳定错误

- **Given** 用户创建或更新 `DisasterPolicy`
- **And** `spec.type` 为 `AutoBackup`
- **And** `spec.ttl` 不是有效正 duration
- **When** 系统处理该请求
- **Then** Server 提交期应拒绝该请求
- **And** 如果非法值绕过 Server 进入 CR，Operator 必须将策略状态标记为错误
- **And** 错误原因必须为 `InvalidTTL`

#### Scenario: 非 AutoBackup 策略不得使用 TTL

- **Given** 用户创建或更新 `DisasterPolicy`
- **And** `spec.type` 为 `DataSync` 或 `ResourceSync`
- **And** 请求携带 `spec.ttl`
- **When** Server 处理该请求
- **Then** Server 必须拒绝该请求
- **And** Operator 即使看到该字段也不得将 TTL 应用于 DataSync 或 ResourceSync 调度

### Requirement: 策略详情必须回显 TTL

策略查询 API 必须 (MUST) 在 `DisasterPolicy` 列表、详情、创建响应和更新响应中回显 `spec.ttl`。

#### Scenario: 查询 AutoBackup 策略详情回显 TTL

- **Given** 一个 `AutoBackup` 策略已配置 `spec.ttl=720h`
- **When** 用户查询策略详情
- **Then** 响应中的 `data.spec.ttl` 必须为 `720h`
- **And** 不得只在底层 CRD 中存在而在 DTO 中丢失

#### Scenario: 查询策略列表回显 TTL

- **Given** 一个 `AutoBackup` 策略已配置 `spec.ttl=720h`
- **When** 用户查询策略列表
- **Then** 对应列表项的 `spec.ttl` 必须为 `720h`
