## MODIFIED Requirements

### Requirement: AppRestore 必须暴露方向与编译审计信息

系统必须 (MUST) 在 AppRestore 审计信息中同时呈现“编译结果”与“实态核验结果”。

#### Scenario: 审计摘要包含实态核验
- **GIVEN** 规则编译与恢复执行完成
- **WHEN** 写入 `testudo.softcdata.com/modifier-summary`
- **THEN** 摘要必须 (MUST) 包含实态核验结果（至少包含 `effectiveRuleCount` 与 `noEffectRuleCount`）
- **AND** 当 `noEffectRuleCount > 0` 时必须 (MUST) 可关联到具体失败原因
