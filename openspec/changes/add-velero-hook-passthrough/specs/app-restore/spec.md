## ADDED Requirements

### Requirement: AppRestore Velero Restore Hook 透传
AppRestore 必须 (MUST) 支持将用户提供的 Velero `RestoreSpec.hooks` 原样写入由其创建的 Velero Restore。

#### Scenario: 恢复任务透传 hooks
- **GIVEN** 一个 AppRestore 的 `spec.template.hooks.resources` 非空
- **WHEN** 控制器创建 Velero `Restore`
- **THEN** 该 Velero `Restore.spec.hooks` 必须等于 AppRestore 的 `spec.template.hooks`
- **AND** ResourceModifier 注入不得覆盖或清空 `Restore.spec.hooks`

#### Scenario: 恢复状态回显 hookStatus
- **GIVEN** 目标集群中的 Velero Restore 状态包含 `status.hookStatus`
- **WHEN** AppRestore 控制器同步恢复状态
- **THEN** `AppRestore.status.restoreStatus.hookStatus` 必须保留 Velero 返回的 attempted/failed 计数

#### Scenario: Velero Restore PartiallyFailed 不得映射为成功
- **GIVEN** 目标集群中的 Velero Restore 最终状态为 `PartiallyFailed`
- **WHEN** AppRestore 控制器同步恢复状态
- **THEN** `AppRestore.status.status` 必须为 `PartiallyFailed`
- **AND** `AppRestore.status.restoreStatus.phase` 必须保留 Velero 的 `PartiallyFailed`
- **AND** 控制器必须保留失败 reason/message，供上层 DataSync、ResourceSync 和 Drill 判定为非成功终态

#### Scenario: 空 hooks 保持现有恢复行为
- **GIVEN** 一个 AppRestore 未配置 `spec.template.hooks`
- **WHEN** 控制器创建 Velero `Restore`
- **THEN** 创建出的 Restore 不应包含额外 Hook 行为
- **AND** 现有 ResourceModifier、namespaceMapping、storageClassMapping 行为不应改变

### Requirement: AppRestore Hook 参数透明传递
AppRestore 的 Hook 参数必须 (MUST) 通过 Velero 原生 restore exec hook command 或 restore init hook 的 initContainer 原生字段传递。

#### Scenario: Restore exec hook 保持 command 参数顺序
- **GIVEN** 一个 AppRestore 的 restore exec hook 配置了 `command: ["/usr/local/bin/dr-hook", "after-restore", "--target", "standby"]`
- **WHEN** 控制器创建 Velero Restore
- **THEN** Velero Restore 中的 command 必须保持完全相同的数组顺序
- **AND** server/operator 不得对 command 执行模板渲染

#### Scenario: Restore init hook 透传 initContainer 参数
- **GIVEN** 一个 AppRestore 的 restore init hook 配置了 initContainer 的 `args`、`env`、`envFrom` 或 `volumeMounts`
- **WHEN** 控制器创建 Velero Restore
- **THEN** Velero Restore 中的 initContainer 配置必须保持原样
- **AND** 平台不得解析或改写 initContainer 内的业务参数

#### Scenario: 敏感参数应通过 Secret 引用传递
- **GIVEN** 一个 AppRestore 的 init hook 需要使用敏感值
- **WHEN** 用户提交 Hook 配置
- **THEN** server 应允许使用 `envFrom.secretRef` 或 `env.valueFrom.secretKeyRef`
- **AND** server 必须拒绝将敏感值直接写入 `args` 或 `command`
- **AND** 错误码必须为 `VeleroHookSensitiveParameter`
- **AND** 错误元数据必须包含命中的字段路径

#### Scenario: Restore execTimeout 超过上限
- **GIVEN** 一个 AppRestore 的 restore exec hook 配置了 `execTimeout: "11m"`
- **WHEN** server 校验请求
- **THEN** 请求必须被拒绝
- **AND** 错误信息必须说明 Restore exec hook execTimeout 最大值为 10m

#### Scenario: Restore waitTimeout 超过上限
- **GIVEN** 一个 AppRestore 的 restore exec hook 配置了 `waitTimeout: "31m"`
- **WHEN** server 校验请求
- **THEN** 请求必须被拒绝
- **AND** 错误信息必须说明 Restore exec hook waitTimeout 最大值为 30m

#### Scenario: Restore init timeout 超过上限
- **GIVEN** 一个 AppRestore 的 restore init hook 配置了 `timeout: "31m"`
- **WHEN** server 校验请求
- **THEN** 请求必须被拒绝
- **AND** 错误信息必须说明 Restore init hook timeout 最大值为 30m
