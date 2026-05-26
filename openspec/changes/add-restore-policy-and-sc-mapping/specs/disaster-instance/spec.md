## ADDED Requirements

### Requirement: DisasterInstance 必须提供实例级恢复策略入口

系统必须 (MUST) 支持在 `DisasterInstance.spec.restorePolicy` 配置恢复策略，作为该实例的恢复行为输入。

#### Scenario: 配置实例恢复策略
- **Given** 用户创建或更新 `DisasterInstance`
- **When** 请求中包含 `spec.restorePolicy`
- **Then** 系统必须 (MUST) 持久化该策略并在后续恢复构建中可读取

#### Scenario: 未配置实例恢复策略
- **Given** `DisasterInstance.spec.restorePolicy` 未配置
- **When** 系统构建恢复任务
- **Then** 系统必须 (MUST) 回退到内置默认恢复策略

### Requirement: DisasterInstance 必须支持 Class 映射策略

系统必须 (MUST) 支持在 `DisasterInstance.spec.restorePolicy` 中定义 `StorageClass` 与 `IngressClass` 映射策略。

#### Scenario: 配置 SC/IngressClass 映射规则
- **Given** 用户在 `storageClassMapping.mappings` 与 `ingressClassMapping.mappings` 中配置 `source -> target` 规则
- **When** 配置校验通过
- **Then** 系统必须 (MUST) 将映射作为恢复构建输入

#### Scenario: 配置非法 Class 映射
- **Given** 用户提交重复 source 映射、非法枚举值或冲突规则
- **When** API 接收创建或更新请求
- **Then** 系统必须 (MUST) 拒绝该请求
- **And** 返回可读错误，明确指出冲突字段

### Requirement: DisasterInstance 必须支持就绪校验默认策略

系统必须 (MUST) 支持在 `DisasterInstance.spec.skipPodReadyCheck` 配置容器就绪校验默认策略。

#### Scenario: 配置跳过就绪校验默认策略
- **Given** 用户创建或更新 `DisasterInstance`
- **When** 请求中包含 `spec.skipPodReadyCheck=true`
- **Then** 系统必须 (MUST) 将该实例默认策略持久化
- **And** 后续容灾操作在未被操作入参覆盖时按该默认策略执行
