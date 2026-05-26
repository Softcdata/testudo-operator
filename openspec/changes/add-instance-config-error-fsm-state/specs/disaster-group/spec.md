## ADDED Requirements

### Requirement: 容灾组聚合必须将 ConfigError 识别为错误成员

系统 MUST 在容灾组状态聚合中，将 `DisasterInstance.status.fsmState == ConfigError` 的实例计入错误成员集合。

#### Scenario: 成员出现 ConfigError 时组进入错误态

- **GIVEN** `DisasterGroup` 包含两个实例
- **AND** 其中一个实例 `status.fsmState == ConfigError`
- **WHEN** `DisasterGroup` 控制器执行一次调谐
- **THEN** 组对象 `status.reason` 必须为非空
- **AND** 组对象 `status.message` 必须包含该实例标识

### Requirement: ConfigError 成员不得计入 ReadyInstances

系统 MUST 仅将 `fsmState == Protected` 的实例计入 `readyInstances`。

#### Scenario: ConfigError 成员不影响 ready 判定规则

- **GIVEN** `DisasterGroup` 有两个成员
- **AND** 第一个成员 `fsmState == Protected`
- **AND** 第二个成员 `fsmState == ConfigError`
- **WHEN** 控制器完成聚合
- **THEN** `status.totalInstances` 必须为 `2`
- **AND** `status.readyInstances` 必须为 `1`
