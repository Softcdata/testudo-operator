# Backup Restore Statistics Specification

## Purpose
`BackupRestoreStatistics` 是一个新的 Kubernetes CRD，用于提供备份和恢复操作的聚合统计信息。
## Requirements
### Requirement: 统计数据模型
系统必须 (MUST) 提供 `BackupRestoreStatistics` 资源来存储备份和恢复操作的统计信息。

#### Scenario: 统计计数器
- **Given** 一个 `BackupRestoreStatistics` 对象
- **Then** 它必须包含以下计数器：
    - `Total`: 总操作数
    - `InProgress`: 进行中的操作数
    - `Completed`: 成功完成的操作数
    - `Failed`: 失败的操作数
    - `Canceled`: 被取消的操作数
    - `Unknown`: 状态未知的操作数

### Requirement: 状态流转管理
系统必须 (MUST) 支持原子性的状态流转更新，以保证统计数据的准确性。

#### Scenario: 状态变更
- **Given** 一个操作从状态 A 变为状态 B (例如 `InProgress` -> `Completed`)
- **When** 调用 `TransitionState` 方法
- **Then** 状态 A 的计数器减 1，状态 B 的计数器加 1。
- **And** `Total` 计数器保持不变（除非是新操作）。

### Requirement: 自动维护
控制器必须 (MUST) 在管理 `AppBackup` 和 `AppRestore` 生命周期时，自动维护相关的统计数据。

#### Scenario: 创建新备份
- **Given** 用户创建了一个新的 `AppBackup`
- **When** 控制器开始处理
- **Then** 对应的统计对象中 `Total` 加 1，`InProgress` 加 1。

#### Scenario: 备份完成
- **Given** 一个正在进行的备份任务完成
- **When** 控制器检测到完成状态
- **Then** 对应的统计对象中 `InProgress` 减 1，`Completed` 加 1。

