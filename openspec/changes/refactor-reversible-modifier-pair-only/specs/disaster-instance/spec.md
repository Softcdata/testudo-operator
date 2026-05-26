## ADDED Requirements

### Requirement: DisasterInstance 的 reversible 用户配置面必须只讲 pair

系统必须 (MUST) 在 `DisasterInstance.spec.restorePolicy.modifierRules` 中，将 `reversible` 的正式用户配置面收敛为 pair-only；`veleroNative` 保持原模型不变。

#### Scenario: 用户提交新的 reversible 规则
- **GIVEN** 用户创建或更新 `DisasterInstance`
- **WHEN** 提交新的 reversible 规则
- **THEN** 正式文档与推荐写法必须 (MUST) 只展示 pair
- **AND** 不得 (MUST NOT) 继续要求用户在 `map/template/pair` 三者之间做模式判断

#### Scenario: veleroNative 不受影响
- **GIVEN** 用户提交 `veleroNative` 规则
- **WHEN** 系统处理该请求
- **THEN** 必须 (MUST) 保持现有 `veleroRule` 透传 contract 不变

### Requirement: 提交期必须直接拒绝旧 map/template

系统必须 (MUST) 在提交期直接拒绝旧 `map/template` 写法；pair-only 是唯一正式 reversible contract。

#### Scenario: legacy map 在提交期被拒绝
- **GIVEN** 用户提交旧 `map` 规则
- **WHEN** 系统执行提交期校验
- **THEN** 必须 (MUST) 拒绝该请求
- **AND** 返回消息必须 (MUST) 指向 pair-only canonical form

#### Scenario: legacy template 在提交期被拒绝
- **GIVEN** 用户提交旧 `template` 规则
- **WHEN** 系统执行提交期校验
- **THEN** 必须 (MUST) 拒绝该请求
- **AND** 返回消息必须 (MUST) 指向 pair-only canonical form
