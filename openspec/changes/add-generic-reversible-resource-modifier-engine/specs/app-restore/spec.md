## ADDED Requirements

### Requirement: AppRestore 必须承载统一编译产物

系统必须 (MUST) 将规则编译后的结果写入 `AppRestore.spec.resourceModifierRules`，并确保该产物可被 Velero 直接消费。

#### Scenario: 编译产物落地
- **GIVEN** DataSync、ResourceSync 或 Drill 触发恢复构建
- **WHEN** 规则编译成功
- **THEN** `AppRestore.spec.resourceModifierRules` 必须 (MUST) 包含编译后的规则列表

### Requirement: AppRestore 必须暴露方向与编译审计信息

系统必须 (MUST) 在 AppRestore 层提供方向与编译审计信息，供排障与前端展示。

#### Scenario: 方向注解写入
- **GIVEN** 编译器完成方向判定
- **WHEN** 构建 AppRestore
- **THEN** 必须 (MUST) 写入 `testudo.softcdata.com/modifier-flow`
- **AND** 必须 (MUST) 写入 `testudo.softcdata.com/modifier-direction-source`

#### Scenario: 编译摘要写入
- **GIVEN** 编译器完成规则合并
- **WHEN** 构建 AppRestore
- **THEN** 必须 (MUST) 写入 `testudo.softcdata.com/modifier-summary`
- **AND** 摘要至少包含 `appliedRuleCount/skippedRuleCount/rejectedRuleCount/conflictCount`
