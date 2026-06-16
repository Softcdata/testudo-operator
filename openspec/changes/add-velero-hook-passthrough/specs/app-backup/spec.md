## ADDED Requirements

### Requirement: AppBackup Velero Backup Hook 透传
AppBackup 必须 (MUST) 支持将用户提供的 Velero `BackupSpec.hooks` 原样写入由其创建的 Velero Backup 或 Schedule Template。

#### Scenario: 一次性备份透传 hooks
- **GIVEN** 一个 AppBackup 的 `spec.template.hooks.resources` 非空
- **AND** AppBackup 以一次性备份方式执行
- **WHEN** 控制器创建 Velero `Backup`
- **THEN** 该 Velero `Backup.spec.hooks` 必须等于 AppBackup 的 `spec.template.hooks`
- **AND** 控制器不得修改 hook 的 command、container、onError 或 timeout 字段

#### Scenario: 调度备份透传 hooks
- **GIVEN** 一个 AppBackup 的 `spec.schedule` 非空
- **AND** `spec.template.hooks.resources` 非空
- **WHEN** 控制器创建或更新 Velero `Schedule`
- **THEN** 该 Velero `Schedule.spec.template.hooks` 必须等于 AppBackup 的 `spec.template.hooks`

#### Scenario: 备份历史回显 hookStatus
- **GIVEN** 目标集群中的 Velero Backup 状态包含 `status.hookStatus`
- **WHEN** AppBackup 控制器同步备份历史
- **THEN** `AppBackup.status.history[].veleroStatus.hookStatus` 必须保留 Velero 返回的 attempted/failed 计数

### Requirement: AppBackup Hook 参数透明传递
AppBackup 的 Hook 参数必须 (MUST) 通过 Velero 原生 `exec.command` 数组或业务容器已有环境传递，平台不得在第一阶段执行模板渲染或隐式参数注入。

#### Scenario: 保持 command 参数顺序
- **GIVEN** 一个 AppBackup 的 backup hook 配置了 `exec.command: ["/usr/local/bin/dr-hook", "pre-backup", "--mode", "quiesce"]`
- **WHEN** 控制器创建 Velero Backup 或 Schedule
- **THEN** Velero 对象中的 `exec.command` 必须保持完全相同的数组顺序
- **AND** server/operator 不得拼接、拆分、重排或转义这些参数

#### Scenario: 不渲染平台占位符
- **GIVEN** 一个 AppBackup 的 hook command 包含 `${testudo.namespace}` 或 `{{ namespace }}`
- **WHEN** server 校验请求
- **THEN** 请求必须被拒绝
- **AND** 错误信息必须说明第一阶段不支持平台占位符渲染

#### Scenario: 敏感参数不得明文传入
- **GIVEN** 一个 AppBackup 的 hook command 包含明显敏感参数，例如 `password=plain-text`
- **WHEN** server 校验请求
- **THEN** 请求必须被拒绝
- **AND** 错误码必须为 `VeleroHookSensitiveParameter`
- **AND** 错误元数据必须包含命中的字段路径
- **AND** 错误信息应建议通过业务容器已有 Secret env 或挂载文件传递敏感值

#### Scenario: Backup exec timeout 超过上限
- **GIVEN** 一个 AppBackup 的 backup hook 配置了 `exec.timeout: "11m"`
- **WHEN** server 校验请求
- **THEN** 请求必须被拒绝
- **AND** 错误信息必须说明 Backup exec hook timeout 最大值为 10m
