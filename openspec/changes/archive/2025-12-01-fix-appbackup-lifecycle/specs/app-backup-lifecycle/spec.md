## ADDED Requirements

### Requirement: AppBackup 状态管理
AppBackup 必须 (MUST) 准确反映备份任务的当前生命周期状态。

#### Scenario: 备份处理中
- **Given** 一个新的 AppBackup 被创建
- **When** 控制器开始处理该请求但尚未完成 Velero 资源创建
- **Then** AppBackup 的状态应更新为 `InProgress`

#### Scenario: 备份创建失败
- **Given** AppBackup 正在处理中
- **When** 创建 Velero 资源（Backup 或 Schedule）失败
- **Then** AppBackup 的状态应更新为 `Failed`

### Requirement: 资源级联删除
删除 AppBackup 时，必须 (MUST) 清理所有关联的外部资源。

#### Scenario: 删除 AppBackup
- **Given** 一个已存在的 AppBackup 及其关联的 Velero Backup
- **When** 用户删除该 AppBackup
- **Then** 关联的 Velero Backup 也应被自动删除 (通过 Finalizer 机制)
