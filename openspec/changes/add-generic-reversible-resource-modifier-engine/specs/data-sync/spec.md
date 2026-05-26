## ADDED Requirements

### Requirement: DataSync 的 trafficless 内建规则必须与治理规则兼容

系统必须 (MUST) 在保留治理限制的前提下，保证 DataSync trafficless 恢复所需内建规则可执行。

#### Scenario: trafficless ownerReferences 例外生效
- **GIVEN** DataSync 构建 trafficless AppRestore
- **AND** 系统内建规则在 `pods` 资源上执行 `remove /metadata/ownerReferences`
- **WHEN** 编译器执行治理校验
- **THEN** 该规则必须 (MUST) 允许通过

#### Scenario: 用户规则不得借道例外
- **GIVEN** 用户自定义规则在 DataSync 链路命中 `/metadata/ownerReferences`
- **WHEN** 编译器执行治理校验
- **THEN** 必须 (MUST) 返回 `ModifierRuleRejected`
