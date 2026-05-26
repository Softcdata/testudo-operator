## MODIFIED Requirements

### Requirement: DisasterInstance 提交阶段必须执行服务端规则校验

系统必须 (MUST) 在 `DisasterInstance` 创建或更新时执行服务端规则校验，不得将规则定位错误延迟到执行阶段。

#### Scenario: veleroNative 缺少 veleroRule
- **GIVEN** 用户提交 `veleroNative` 规则
- **AND** 规则未携带 `veleroRule`
- **WHEN** 服务端处理创建或更新请求
- **THEN** 必须 (MUST) 拒绝请求（4xx）
- **AND** 不得 (MUST NOT) 将非法规则写入存储

#### Scenario: 提交时 path 非法
- **GIVEN** 用户提交非法 JSON Pointer path
- **WHEN** 服务端处理创建或更新请求
- **THEN** 必须 (MUST) 拒绝请求（4xx）
- **AND** 返回错误必须 (MUST) 指向具体 rule 与 path

#### Scenario: 提交时数组下标越界
- **GIVEN** 用户提交数组路径（例如 `/spec/ports/10/nodePort`）
- **AND** 目标资源数组长度不足
- **WHEN** 服务端处理创建或更新请求
- **THEN** 必须 (MUST) 拒绝请求（4xx）
- **AND** 不得 (MUST NOT) 写入该规则

#### Scenario: 提交时目标资源零命中
- **GIVEN** 用户提交规则且 `conditions` 未命中任何目标资源
- **WHEN** 服务端处理创建或更新请求
- **THEN** 必须 (MUST) 拒绝请求（4xx）
- **AND** 返回消息必须 (MUST) 至少包含 `rule id`、`groupResource` 与关键匹配条件

#### Scenario: 命中治理禁区路径
- **GIVEN** 用户规则命中治理禁区（`status/*`、`/metadata/finalizers`、`/metadata/ownerReferences`）
- **WHEN** 服务端处理创建或更新请求
- **THEN** 必须 (MUST) 拒绝请求（4xx）
- **AND** 不得 (MUST NOT) 写入该规则

#### Scenario: 规则条数或复杂度超限
- **GIVEN** 用户提交规则条数或复杂度超出系统限额
- **WHEN** 服务端处理创建或更新请求
- **THEN** 必须 (MUST) 拒绝请求（4xx）
- **AND** 返回错误需包含触发的限额项

## ADDED Requirements

### Requirement: DisasterInstance 状态口径必须一致

系统必须 (MUST) 对 `status.fsmState` 与外部聚合态保持一致映射，不得产生冲突语义。

#### Scenario: 同一时刻状态语义一致
- **GIVEN** DisasterInstance 处于稳定运行态
- **WHEN** 外部读取实例状态
- **THEN** `currentState` 与 `status.fsmState` 必须 (MUST) 满足预定义映射关系
- **AND** 不得 (MUST NOT) 出现可观测语义冲突
