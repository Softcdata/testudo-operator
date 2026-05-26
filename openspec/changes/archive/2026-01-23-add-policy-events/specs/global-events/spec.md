## ADDED Requirements

### Requirement: 策略事件上报 (Policy Event Reporting)
系统必须 (MUST) 在策略的关键生命周期操作时发射结构化 Kubernetes Event，以便用户追踪策略变更历史。

#### Scenario: 创建策略事件
- **Given** DisasterPolicy 资源被创建
- **When** Operator 完成首次 Reconcile（Finalizer 添加且验证通过）
- **Then** 系统必须发射 `ExecutionFinished` 事件
- **And** Task 名称为 `创建策略 <name>`
- **And** Status 为 `Success`

#### Scenario: 编辑策略事件
- **Given** DisasterPolicy 的 Spec 被修改（排除 State 字段）
- **When** Operator 检测到 `Generation > ObservedGeneration`
- **Then** 系统必须发射 `ExecutionFinished` 事件
- **And** Task 名称为 `编辑策略 <name>`

#### Scenario: 启用策略事件
- **Given** DisasterPolicy 的 `Spec.State` 从 Disabled 变为 Enabled
- **When** Operator 完成 Reconcile
- **Then** 系统必须发射 `ExecutionFinished` 事件
- **And** Task 名称为 `启用策略 <name>`

#### Scenario: 禁用策略事件
- **Given** DisasterPolicy 的 `Spec.State` 从 Enabled 变为 Disabled
- **When** Operator 完成 Reconcile
- **Then** 系统必须发射 `ExecutionFinished` 事件
- **And** Task 名称为 `禁用策略 <name>`

#### Scenario: 删除策略事件
- **Given** DisasterPolicy 被删除
- **When** Operator 在移除 Finalizer 前
- **Then** 系统必须发射 `ExecutionFinished` 事件
- **And** Task 名称为 `删除策略 <name>`
- **And** Status 为 `Success`

#### Scenario: 跨调用方式一致性
- **Given** 策略变更可能来自 API Server 或 kubectl
- **When** 任意方式触发策略变更
- **Then** Operator 都必须发射对应的事件
