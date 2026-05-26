## ADDED Requirements
### Requirement: 单元测试覆盖率标准
所有核心控制器（Controller）必须 (MUST) 具备单元测试，且核心业务逻辑（Reconcile loop）的语句覆盖率必须达到 80% 以上。

#### Scenario: 验证新控制器的测试覆盖
- **GIVEN** 开发者提交了一个新的控制器实现
- **WHEN** 运行项目标准的测试套件（如 `go test -cover`）
- **THEN** 该控制器的 Reconcile 相关代码覆盖率必须显示为 80% 或更高
- **AND** 测试用例必须涵盖至少一个 Mock 错误处理路径（如 K8s API 报错模拟）

### Requirement: 控制器依赖注入模式
为了提高可测试性，控制器 Reconciler 结构体必须 (MUST) 采用依赖注入模式来访问外部系统或执行命令。

#### Scenario: 重构控制器以支持 Mock
- **GIVEN** 控制器包含直接调用 `os`, `exec` 或底层 `kubeclient` 的硬编码代码
- **WHEN** 进行代码审查或功能开发时
- **THEN** 必须将其抽象为接口（如 `CommandExecutor`, `ClientFactory`）并注入到 Reconciler 中
