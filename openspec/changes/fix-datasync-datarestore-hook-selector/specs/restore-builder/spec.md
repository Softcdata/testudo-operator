## ADDED Requirements

### Requirement: Data Restore Hook selector 可由调用方预处理
Restore Builder 必须 (MUST) 支持调用方传入已经过系统兼容处理的 data restore hooks 与 ResourceModifierRules，并保持数据恢复构建逻辑不覆盖这些结果。

#### Scenario: 使用调用方提供的 data restore hooks
- **GIVEN** BuilderConfig 中提供了 `DataRestoreHooks`
- **AND** `RestoreType` 为 `data`
- **WHEN** 调用 `BuildAppRestoreSpec`
- **THEN** 返回的 `AppRestoreSpec.template.hooks` 必须使用调用方提供的 hooks
- **AND** Builder 不得重新计算或覆盖 hooks selector

#### Scenario: 使用调用方提供的 data ResourceModifierRules
- **GIVEN** BuilderConfig 中提供了 `DataResourceModifierRules`
- **AND** 其中包含 Trafficless 规则和 hook marker 规则
- **WHEN** 调用 `BuildAppRestoreSpec`
- **THEN** 返回的 `AppRestoreSpec.resourceModifierRules` 必须保持调用方提供的规则顺序
- **AND** Builder 不得回退到默认 Trafficless 规则
