## ADDED Requirements
### Requirement: 备份策略 (DisasterPolicy) 的派生与生效
DisasterPolicy 必须能够被 AppBackup 引用，并将其定义的调度参数（如 Schedule）正确传递给下游的 Velero 资源。

#### Scenario: 自动从策略派生调度任务
- **GIVEN** 一个定义了 `Schedule: "0 0 * * *"` 的 DisasterPolicy
- **WHEN** 创建一个引用该策略的 AppBackup
- **THEN** AppBackup 必须自动继承该调度表达式
- **AND** 在目标集群创建的 Velero Schedule 必须匹配此表达式

### Requirement: 策略类型验证
控制器必须验证策略类型的有效性（如 AutoBackup 类型）。

#### Scenario: 验证非法策略类型
- **WHEN** DisasterPolicy 的类型不符合预期
- **THEN** 引用该策略的资源应当记录错误事件并置为 Failed 状态
