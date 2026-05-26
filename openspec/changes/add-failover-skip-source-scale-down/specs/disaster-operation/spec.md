## ADDED Requirements

### Requirement: Failover 支持显式跳过源集群缩零
`DisasterOperation` 在执行 `operationType=failover` 时必须 (MUST) 支持可选字段 `spec.skipScaleDownSource`。该字段用于显式控制是否跳过 `ScaleDownSource` 步骤，默认值为 `false`。

#### Scenario: 显式跳过源缩零并继续流程
- **Given** 一个 `operationType=failover` 的 `DisasterOperation`
- **And** `spec.skipScaleDownSource=true`
- **When** Failover 执行到 `ScaleDownSource` 步骤
- **Then** 控制器必须 (MUST) 跳过源集群缩零动作
- **And** 必须 (MUST) 继续执行后续步骤（如 `ScaleUpTarget`、`SwitchRoles`）
- **And** 必须 (MUST) 记录可审计的跳过信号（Event 或状态消息）说明是由 `skipScaleDownSource=true` 触发

#### Scenario: 默认行为保持兼容
- **Given** 一个 `operationType=failover` 的 `DisasterOperation`
- **And** `spec.skipScaleDownSource` 未设置或为 `false`
- **When** Failover 执行到 `ScaleDownSource` 步骤
- **Then** 控制器必须 (MUST) 执行现有源集群缩零逻辑

#### Scenario: 非 failover 操作忽略该参数
- **Given** 一个 `operationType` 不为 `failover` 的 `DisasterOperation`
- **And** `spec.skipScaleDownSource=true`
- **When** 控制器执行该操作
- **Then** 系统必须 (MUST) 忽略该字段，不得改变该操作类型的既有语义

### Requirement: Group Failover 必须透传跳过缩零参数
当 `DisasterGroup` 触发 failover 并创建子 `DisasterOperation` 时，父操作中的 `spec.skipScaleDownSource` 必须 (MUST) 原样透传到子操作，确保组内实例行为一致。

#### Scenario: 组操作创建子操作时透传参数
- **Given** 一个以 Group 方式发起的 failover 操作
- **And** 父 `DisasterOperation` 配置了 `spec.skipScaleDownSource=true`
- **When** 控制器为组内实例创建子 `DisasterOperation`
- **Then** 子操作的 `spec.skipScaleDownSource` 必须 (MUST) 为 `true`
