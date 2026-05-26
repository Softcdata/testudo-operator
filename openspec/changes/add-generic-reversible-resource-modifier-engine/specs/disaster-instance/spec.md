## ADDED Requirements

### Requirement: DisasterInstance 必须提供统一规则入口

系统必须 (MUST) 在 `DisasterInstance.spec.restorePolicy` 提供统一资源修改规则入口，作为 Phase 1 唯一用户配置来源。

#### Scenario: 规则入口可用
- **GIVEN** 用户创建或更新 `DisasterInstance`
- **WHEN** 在 `spec.restorePolicy` 配置资源修改规则
- **THEN** 控制器必须 (MUST) 能在恢复构建链路读取该规则集

### Requirement: DisasterInstance 规则必须满足 Phase 1 校验约束

系统必须 (MUST) 对 `DisasterInstance.spec.restorePolicy` 中的规则执行 Phase 1 约束校验。

#### Scenario: veleroNative patch 类型越界
- **GIVEN** 用户在 `veleroNative` 规则中配置 `mergePatches` 或 `strategicPatches`
- **WHEN** 控制器执行预检或编译校验
- **THEN** 必须 (MUST) 返回 `ModifierRuleRejected`

#### Scenario: reversible 追加路径越界
- **GIVEN** 用户在 `reversible` 规则中配置 `path=.../-`
- **WHEN** 控制器执行预检或编译校验
- **THEN** 必须 (MUST) 返回 `ModifierRuleNotReversible`

### Requirement: DisasterInstance 提交阶段必须执行服务端规则校验

系统必须 (MUST) 在 `DisasterInstance` 创建或更新时执行服务端规则校验，不得将规则定位错误延迟到执行阶段才暴露。

#### Scenario: 提交时 path 不存在
- **GIVEN** 用户提交规则，且 `conditions` 命中目标资源
- **AND** patch `path` 在目标资源上不存在
- **WHEN** 服务端处理创建或更新请求
- **THEN** 必须 (MUST) 拒绝该请求
- **AND** 必须 (MUST) 返回 `ModifierRuleRejected`

#### Scenario: 提交时数组下标越界
- **GIVEN** 用户提交数组路径（例如 `/spec/ports/10/nodePort`）
- **AND** 目标资源数组长度不足
- **WHEN** 服务端处理创建或更新请求
- **THEN** 必须 (MUST) 拒绝该请求
- **AND** 必须 (MUST) 返回 `ModifierRuleRejected`

#### Scenario: 提交时目标资源零命中
- **GIVEN** 用户提交规则且 `conditions` 未命中任何目标资源
- **WHEN** 服务端处理创建或更新请求
- **THEN** 必须 (MUST) 拒绝该请求
- **AND** 返回消息必须 (MUST) 至少包含 `rule id`、`groupResource` 与关键匹配条件
