## ADDED Requirements

### Requirement: 系统必须提供自动备份执行情况统计接口

系统必须 (MUST) 提供一个只读聚合接口，用于统计整个系统 Velero 自动备份在指定时间窗口内的成功与失败数量，并返回前端图表可直接消费的百分比。

#### Scenario: 查询 7 天自动备份执行情况

- **Given** 系统中存在多个自动备份 AppBackup 的历史记录
- **And** 其中部分历史记录在最近 7 天内成功完成
- **And** 其中部分历史记录在最近 7 天内失败
- **When** 用户查询自动备份执行情况统计接口并指定 `period=7d`
- **Then** 响应必须包含最近 7 天内自动备份的 `total`
- **And** 响应必须包含成功数量 `success.count`
- **And** 响应必须包含失败数量 `failed.count`
- **And** 响应必须包含成功百分比 `success.percent`
- **And** 响应必须包含失败百分比 `failed.percent`

#### Scenario: 查询 30 天和 90 天自动备份执行情况

- **Given** 系统中存在跨越多个日期的自动备份历史记录
- **When** 用户分别以 `period=30d` 和 `period=90d` 查询统计接口
- **Then** 系统必须分别按最近 30 天与最近 90 天过滤历史记录
- **And** 每个响应都必须返回对应窗口的 start/end 时间
- **And** 30 天统计不得包含 30 天窗口之前的历史记录
- **And** 90 天统计不得包含 90 天窗口之前的历史记录

#### Scenario: 非法统计窗口被拒绝

- **Given** 用户查询自动备份执行情况统计接口
- **And** 请求参数 `period` 不是 `7d`、`30d` 或 `90d`
- **When** Server 处理该请求
- **Then** Server 必须返回参数错误
- **And** 系统不得执行不受控的大范围历史扫描

### Requirement: 自动备份统计不得混入容灾同步或手动备份

自动备份执行情况统计必须 (MUST) 只统计 Velero 自动备份链路产生的计划备份历史，不得混入容灾同步备份或手动一次性备份。

#### Scenario: 排除 DataSync 和 ResourceSync 产生的备份

- **Given** 系统中存在 `DataSync` 或 `ResourceSync` 创建的 AppBackup 历史记录
- **And** 系统中存在 AutoBackup 策略创建的计划备份历史记录
- **When** 用户查询自动备份执行情况统计接口
- **Then** 统计结果必须只包含 AutoBackup 策略创建的计划备份
- **And** 不得包含 `DataSync` 或 `ResourceSync` 创建的同步备份

#### Scenario: 排除手动一次性备份

- **Given** 系统中存在手动触发或一次性执行的 AppBackup 历史记录
- **And** 系统中存在 AutoBackup 策略创建的计划备份历史记录
- **When** 用户查询自动备份执行情况统计接口
- **Then** 统计结果必须只包含计划自动备份
- **And** 不得包含 `spec.schedule=@manual` 或无 schedule 的手动备份

#### Scenario: 空数据返回零值

- **Given** 指定时间窗口内没有任何自动备份成功或失败历史
- **When** 用户查询自动备份执行情况统计接口
- **Then** 响应中的 `total` 必须为 `0`
- **And** `success.count` 与 `failed.count` 必须为 `0`
- **And** `success.percent` 与 `failed.percent` 必须为 `0`
