## ADDED Requirements

### Requirement: 容灾操作超时机制

系统必须 (MUST) 提供容灾操作的超时保护机制，以防止操作因外部原因（如资源不足、网络中断）而无限期挂起。

#### Scenario: 默认超时继承
- **Given** 一个 DisasterInstance，配置了 `operationTimeoutMinutes: 30`
- **When** 用户创建一个 DisasterOperation，且未指定 `timeoutMinutes`
- **Then** DisasterOperation 应自动继承 `timeoutMinutes: 30`
- **And** 超时计时器应从 Operation 开始执行时启动

#### Scenario: 单次超时覆盖
- **Given** 一个 DisasterInstance，配置了 `operationTimeoutMinutes: 30`
- **When** 用户创建一个 DisasterOperation，并明确指定 `timeoutMinutes: 10`
- **Then** DisasterOperation 应使用 `timeoutMinutes: 10` 进行超时控制

#### Scenario: 步骤执行超时
- **Given** 一个正在执行 Failover 的 DisasterOperation
- **And** 当前步骤 (例如 CheckReplicas) 因条件未满足而持续运行
- **And** 运行时间超过了配置的 `timeoutMinutes`
- **When** Controller 执行下一次 Reconcile
- **Then** 系统必须 (MUST) 终止当前步骤
- **And** 将操作状态标记为 `Failed`
- **And** 记录包含 "TimedOut" 的错误信息和 Event

#### Scenario: 同步操作超时
- **Given** 一个 type=sync 的 DisasterOperation
- **And** 底层 DataSync/ResourceSync 长时间未完成
- **When** 运行时间超过 `timeoutMinutes`
- **Then** 系统必须 (MUST) 将操作标记为 `Failed` (Reason: SyncTimeout)
