## ADDED Requirements
### Requirement: 资源生命周期审计
AppBackup 控制器必须 (MUST) 上报资源的生命周期事件，以便外部系统（如 Server）记录变更历史。

#### Scenario: 资源创建事件
- **Given** 一个新创建的 AppBackup 资源
- **When** 控制器首次处理该资源（Pending 阶段）
- **Then** 控制器必须 (MUST) 发出一个 `Created` 类型的结构化事件
- **And** 任务名称格式应为 `Resource: AppBackup <name> Created`

#### Scenario: 资源更新事件
- **Given** 一个已存在的 AppBackup 资源
- **When** 资源的 Spec 字段（如 Schedule, Action, Template）发生变更
- **Then** 控制器必须 (MUST) 发出一个 `Updated` 类型的结构化事件
- **And** 任务名称格式应为 `Resource: AppBackup <name> Updated`

#### Scenario: 资源删除事件
- **Given** 一个正在被删除的 AppBackup 资源（DeletionTimestamp 非空）
- **When** 控制器开始执行删除逻辑（Deleting Handler）
- **Then** 控制器必须 (MUST) 发出一个 `Deleted` 类型的结构化事件
- **And** 任务名称格式应为 `Resource: AppBackup <name> Deleted`
