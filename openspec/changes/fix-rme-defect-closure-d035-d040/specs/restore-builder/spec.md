## ADDED Requirements

### Requirement: 规则执行结果必须通过实态核验

系统必须 (MUST) 对关键修改项执行“编译结果 + 资源实态”双重校验，避免 no-op 伪成功。

#### Scenario: SC 映射实态未变化
- **GIVEN** 编译结果已包含 PVC `storageClassName` 修改 patch
- **WHEN** 恢复流程执行完成
- **AND** 目标 PVC 的 `spec.storageClassName` 未按预期变化
- **THEN** 任务必须 (MUST) 标记为失败
- **AND** 失败信息必须 (MUST) 包含资源名、path、期望值、实际值

#### Scenario: forward/reverse 双向核验
- **GIVEN** 同一规则在 forward 与 reverse 场景均被执行
- **WHEN** 流程结束
- **THEN** 两个方向都必须 (MUST) 满足实态变化断言
- **AND** 任一方向不满足时必须 (MUST) 失败关闭

### Requirement: PVC 路径匹配错误必须前移暴露

系统必须 (MUST) 将 PVC 相关路径匹配错误前移到构建期/预检期，避免在执行深水区首次失败。

#### Scenario: 初始化出现零匹配路径
- **GIVEN** 带 PVC 的初始化恢复场景
- **AND** 规则路径无法匹配目标结构
- **WHEN** 构建器执行路径定位或预检
- **THEN** 必须 (MUST) 直接失败并返回可定位错误
- **AND** 不得 (MUST NOT) 等到恢复执行阶段才暴露 `expected one matching path ... got 0`
