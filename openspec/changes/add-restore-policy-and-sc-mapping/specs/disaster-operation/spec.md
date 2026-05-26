## ADDED Requirements

### Requirement: DisasterOperation 必须支持覆盖实例就绪校验默认策略

系统必须 (MUST) 支持通过 `DisasterOperation` 入参覆盖 `DisasterInstance.spec.skipPodReadyCheck` 的默认策略。

#### Scenario: 操作入参覆盖实例默认策略
- **Given** `DisasterInstance.spec.skipPodReadyCheck` 已配置默认值
- **And** 用户在 `DisasterOperation` 入参中提供就绪校验控制参数
- **When** 控制器执行 Failover/Drill 的扩容后检查
- **Then** 系统必须 (MUST) 优先使用操作入参
- **And** 未提供操作覆盖参数时回退到实例默认策略

### Requirement: Group 操作必须透传就绪校验参数到子操作

系统必须 (MUST) 在 Group 操作创建子 `DisasterOperation` 时透传就绪校验参数，保持父子行为一致。

#### Scenario: 父子操作就绪校验参数一致
- **Given** Group 父操作设置了就绪校验控制参数
- **When** 控制器创建对应的子 `DisasterOperation`
- **Then** 子操作必须 (MUST) 继承相同的就绪校验参数
