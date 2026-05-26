## ADDED Requirements

### Requirement: reversible 编译 contract 必须收敛为 canonical pair

系统必须 (MUST) 将 `reversible` 的正式用户配置模型收敛为 canonical pair，并以 `path + sourceValue + targetValue` 表达双向编译目标值。

#### Scenario: SC 映射使用 pair
- **GIVEN** 用户需要在 PVC 上表达 `sc-main -> sc-dr`
- **WHEN** 使用 canonical pair 编写 reversible 规则
- **THEN** 规则必须 (MUST) 只声明 `path=/spec/storageClassName`、`sourceValue=sc-main`、`targetValue=sc-dr`

#### Scenario: NodePort 使用 pair
- **GIVEN** 用户需要在 Service NodePort 上表达双向固定值
- **WHEN** 使用 canonical pair 编写 reversible 规则
- **THEN** 规则必须 (MUST) 只声明一组 `sourceValue/targetValue`
- **AND** 不需要再区分 `map` 或 `template` 模式

### Requirement: pair-only 必须覆盖原 template 场景

系统必须 (MUST) 允许 pair 值包含受限 placeholder，以覆盖原 `template` 场景，但不得继续暴露独立 `template` mode。

#### Scenario: pair 值使用 placeholder
- **GIVEN** 用户需要将环境变量分别渲染为 source/target 集群地址
- **WHEN** 在 pair 的 `sourceValue/targetValue` 中使用受限 placeholder
- **THEN** 编译器必须 (MUST) 能在保持 pair contract 不变的前提下产出正确值

#### Scenario: 不再暴露独立 template mode
- **GIVEN** pair-only contract 已启用
- **WHEN** 文档、示例或新配置入口描述 reversible 规则
- **THEN** 不得 (MUST NOT) 再把 `template` 作为正式用户模式并列展示

### Requirement: restore-builder 必须只接受 pair canonical input

系统必须 (MUST) 只接受 canonical pair 进入 restore-builder 编译链路；旧 `map/template` 必须被视为非法输入，而不是兼容 alias。

#### Scenario: legacy map 进入编译链路时被拒绝
- **GIVEN** 输入规则仍使用旧 `map` 写法
- **WHEN** restore-builder 处理 reversible 编译请求
- **THEN** 必须 (MUST) 失败关闭
- **AND** 错误消息必须 (MUST) 指向 pair-only canonical form

#### Scenario: legacy template 进入编译链路时被拒绝
- **GIVEN** 输入规则仍使用旧 `template` 写法
- **WHEN** restore-builder 处理 reversible 编译请求
- **THEN** 必须 (MUST) 失败关闭
- **AND** 不得 (MUST NOT) 隐式导出 `sourceValue/targetValue`
