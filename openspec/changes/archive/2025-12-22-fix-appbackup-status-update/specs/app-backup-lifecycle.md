# AppBackup Lifecycle Delta Spec

## MODIFIED Requirements

### Requirement: AppBackup 状态管理
AppBackup 必须 (MUST) 准确反映备份任务的当前生命周期状态，包括初始状态的持久化。

#### Scenario: 初始状态持久化
- **Given** 一个新创建的 AppBackup，其 `status.phase` 为空
- **When** 控制器首次执行 Reconcile 循环
- **Then** 无论处理结果如何（成功或因依赖缺失失败），控制器必须 (MUST) 将 `status.phase` 更新为非空值（通常为 `Pending`），确保状态字段在 ETCD 中被持久化。
- **And** 如果发生错误，`status.reason` 和 `status.message` 也必须被更新。

#### Scenario: 依赖缺失时的状态
- **Given** 一个 AppBackup 引用了一个不存在的 Cluster
- **When** 控制器执行 Reconcile 循环
- **Then** `status.phase` 应保持为 `Pending`（等待依赖就绪）。
- **And** `status.reason` 应记录错误原因（如 "ReconcileError"）。
- **And** `status.message` 应包含具体的错误信息（如 "cluster not found"）。
