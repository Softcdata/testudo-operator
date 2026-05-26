## ADDED Requirements

### Requirement: 配置异常时实例必须进入 ConfigError

系统 MUST 在 `DisasterConfig` 不健康时，将 `DisasterInstance.status.fsmState` 更新为 `ConfigError`。

`DisasterConfig` 不健康的判定条件如下：
- 配置对象不存在。
- `DisasterConfig.status.status == Error`。
- `DisasterConfig.status.status == NotReady`。

#### Scenario: Protected 实例在配置 NotReady 时进入 ConfigError

- **GIVEN** `DisasterInstance.status.fsmState == Protected`
- **AND** `DisasterConfig.status.status == NotReady`
- **WHEN** 控制器执行一次调谐
- **THEN** `DisasterInstance.status.fsmState` 必须变为 `ConfigError`
- **AND** `status.reason` 与 `status.message` 必须写入配置异常语义

#### Scenario: 配置对象不存在时进入 ConfigError

- **GIVEN** 实例引用的 `DisasterConfig` 不存在
- **WHEN** 控制器执行一次调谐
- **THEN** `DisasterInstance.status.fsmState` 必须为 `ConfigError`
- **AND** `status.reason` 必须为 `ConfigNotFound`

### Requirement: 进入 ConfigError 时必须记录进入前状态

系统 MUST 在实例首次进入 `ConfigError` 时写入 `status.lastStableFsmState`，且保持 `ConfigError` 期间不得覆盖该值。

#### Scenario: 首次进入 ConfigError 记录原状态

- **GIVEN** `DisasterInstance.status.fsmState == Paused`
- **AND** 该实例首次命中配置异常
- **WHEN** 控制器执行一次调谐
- **THEN** `status.fsmState` 必须为 `ConfigError`
- **AND** `status.lastStableFsmState` 必须为 `Paused`

#### Scenario: 保持 ConfigError 时不覆盖已记录状态

- **GIVEN** `DisasterInstance.status.fsmState == ConfigError`
- **AND** `status.lastStableFsmState == Active`
- **AND** 配置仍为异常
- **WHEN** 控制器再次调谐
- **THEN** `status.lastStableFsmState` 必须继续为 `Active`

### Requirement: 配置恢复后必须恢复回进入前状态

系统 MUST 在配置恢复为 `Ready` 后，将实例从 `ConfigError` 恢复到 `status.lastStableFsmState` 记录的原状态。

#### Scenario: 从 ConfigError 恢复到 Paused

- **GIVEN** `status.fsmState == ConfigError`
- **AND** `status.lastStableFsmState == Paused`
- **AND** `DisasterConfig.status.status == Ready`
- **WHEN** 控制器执行一次调谐
- **THEN** `status.fsmState` 必须恢复为 `Paused`
- **AND** `status.reason` 与 `status.message` 必须清空
- **AND** `status.lastStableFsmState` 必须清空

#### Scenario: 从 ConfigError 恢复到 Active

- **GIVEN** `status.fsmState == ConfigError`
- **AND** `status.lastStableFsmState == Active`
- **AND** `DisasterConfig.status.status == Ready`
- **WHEN** 控制器执行一次调谐
- **THEN** `status.fsmState` 必须恢复为 `Active`

### Requirement: 缺失状态记忆时禁止猜测恢复目标

系统 MUST NOT 在 `status.lastStableFsmState` 为空时将实例从 `ConfigError` 恢复到任意猜测状态。

#### Scenario: 缺失状态记忆时保持 ConfigError

- **GIVEN** `status.fsmState == ConfigError`
- **AND** `status.lastStableFsmState == ""`
- **AND** `DisasterConfig.status.status == Ready`
- **WHEN** 控制器执行一次调谐
- **THEN** `status.fsmState` 必须继续为 `ConfigError`
- **AND** `status.reason` 必须为 `LastStableStateMissing`

### Requirement: 进行中操作态不受配置守卫接管

系统 MUST NOT 在 `FailingOver`、`FailingBack` 状态中强制切入 `ConfigError`。

#### Scenario: FailingOver 遇到配置异常保持原状态

- **GIVEN** `status.fsmState == FailingOver`
- **AND** `DisasterConfig.status.status == Error`
- **WHEN** 控制器执行一次调谐
- **THEN** `status.fsmState` 必须保持 `FailingOver`
