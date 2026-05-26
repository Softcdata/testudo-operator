## MODIFIED Requirements

### Requirement: DisasterPolicy 删除保护

当 `DisasterPolicy` 被 `AppBackup` 或 `DisasterConfig` 引用时，删除操作必须 (MUST) 被阻塞，直到所有引用被移除。

#### Scenario: 删除被 AppBackup 引用的策略

- **GIVEN** 存在一个 `DisasterPolicy` 名为 `daily-backup`
- **AND** 存在一个 `AppBackup` 的 `spec.disasterPolicy` 值为 `daily-backup`
- **WHEN** 用户尝试删除该 `DisasterPolicy`
- **THEN** 删除操作必须 (MUST) 被阻塞
- **AND** `Status.Reason` 必须 (MUST) 更新为 `DeletionBlocked`
- **AND** `Status.Message` 必须 (MUST) 包含引用该策略的 `AppBackup` 名称

#### Scenario: 无引用时允许删除策略

- **GIVEN** 存在一个 `DisasterPolicy` 名为 `daily-backup`
- **AND** 没有任何 `AppBackup` 或 `DisasterConfig` 引用该策略
- **WHEN** 用户尝试删除该 `DisasterPolicy`
- **THEN** 删除操作必须 (MUST) 成功完成
- **AND** Finalizer 必须 (MUST) 被移除

## ADDED Requirements

### Requirement: AppBackup 必须携带策略关联标签

当 `AppBackup` 引用了 `DisasterPolicy` 时，必须 (MUST) 在 Labels 中注入 `testudo.softcdata.com/disaster-policy-name` 标签。

#### Scenario: 自动注入策略标签

- **GIVEN** 创建一个 `AppBackup`
- **AND** `spec.disasterPolicy` 设置为 `my-policy`
- **WHEN** Controller 执行 `syncLabels`
- **THEN** `AppBackup` 的 Labels 必须 (MUST) 包含 `testudo.softcdata.com/disaster-policy-name: my-policy`

#### Scenario: 策略字段为空时不添加标签

- **GIVEN** 创建一个 `AppBackup`
- **AND** `spec.disasterPolicy` 为空
- **WHEN** Controller 执行 `syncLabels`
- **THEN** `AppBackup` 的 Labels 不应 (SHOULD NOT) 包含 `testudo.softcdata.com/disaster-policy-name` 键
