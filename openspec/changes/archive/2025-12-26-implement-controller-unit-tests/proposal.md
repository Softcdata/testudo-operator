# 提案：实现核心控制器模块单元测试覆盖 (精简版)

## Summary
本项目旨在为 `disaster-operator` 的核心业务控制器（除 DisasterConfig, DisasterJob, DisasterBackup 外）建立完善的单元测试体系，确保代码覆盖率达到 80% 以上。

## Motivation
目前项目已完成 `Cluster` 模块的测试，但 `AppBackup`, `AppRestore` 等关键模块仍需自动化验证。为了集中精力于最核心的业务流转逻辑，我们将排除配置类和扫描类模块，专注提升应用级备份与恢复的稳定性。

## Proposed Changes
- **统一测试标准**: 参考 `Cluster` 模块，使用 `Ginkgo` + `envtest` + `Mock` 模式。
- **模块覆盖范围**:
  - `AppBackup` (应用备份状态机)
  - `AppRestore` (应用恢复与配置映射)
  - `StorageRepository` (存储库管理)
  - `DisasterPolicy` (备份策略应用)
- **依赖注入改造**: 确保相关 Reconciler 支持 `ClientFactory` 和 `CommandExecutor`。

## Testing Strategy
- **Stage-based Testing**: 针对复杂状态机（如 AppBackup Ready 阶段）分步骤验证。
- **Fault Injection**: 通过 Mock 注入跨集群连接失败、Velero 任务冲突等异常。
