# app-backup-lifecycle Specification

## Purpose
定义 AppBackup 资源的生命周期管理逻辑，包括状态流转、资源关联、手动操作（备份/重试）以及垃圾回收策略。
## Requirements
### Requirement: 资源标识与隔离 (Resource Identification & Isolation)
为了确保控制器能够精确管理属于特定 `AppBackup` 实例的资源，并避免同名资源冲突，必须 (MUST) 采用双标签策略。

#### Scenario: 双标签机制
- **Given** 一个 AppBackup 资源
- **When** 控制器创建关联的 Velero 资源
- **Then** 必须 (MUST) 添加 `testudo.softcdata.com/app-backup` (Name) 和 `testudo.softcdata.com/app-backup-uid` (UID) 标签。

### Requirement: 手动操作 (Manual Actions)
AppBackup 必须 (MUST) 支持通过 `Spec.Action` 字段触发手动操作。

#### Action: Backup (立即备份)
- **Trigger**: 用户设置 `Spec.Action.Type = "Backup"` 且 `RequestAt` 更新。
- **Behavior**: 控制器立即创建一个新的 Velero Backup。
- **Naming**: 备份名称格式为 `app-backup-<Name>-<Timestamp>`。

#### Action: Retry (重试备份)
- **Trigger**: 用户设置 `Spec.Action.Type = "Retry"` 且 `RequestAt` 更新。
- **Behavior**:
    1.  控制器查找最近一次历史记录中的备份名称。
    2.  如果该备份存在，控制器**删除**该 Velero Backup（触发级联删除，清理对象存储中的数据）。
    3.  控制器等待删除完成后，使用**相同的名称**重新创建 Velero Backup。
- **Purpose**: 修复因临时故障导致的失败备份，保持备份名称不变以便于追踪。

### Requirement: 资源级联删除 (Cascading Deletion)
删除 AppBackup 时，必须 (MUST) 清理所有关联的外部资源。

#### Scenario: 删除 AppBackup
- **Given** 一个已存在的 AppBackup 及其关联的 Velero Backup/Schedule
- **When** 用户删除该 AppBackup (触发 Finalizer)
- **Then** 控制器使用 `DeleteAllOf` 方法，配合 `testudo.softcdata.com/app-backup-uid` 标签选择器，删除所有关联的 Velero 资源。
- **Note**: 必须确保只删除匹配 UID 的资源，防止误删同名但属于旧实例的备份。

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

#### Scenario: 备份处理中
- **Given** 一个新的 AppBackup 被创建
- **When** 控制器开始处理该请求但尚未完成 Velero 资源创建
- **Then** AppBackup 的状态应更新为 `InProgress`

#### Scenario: 备份创建失败
- **Given** AppBackup 正在处理中
- **When** 创建 Velero 资源（Backup 或 Schedule）失败
- **Then** AppBackup 的状态应更新为 `Failed`

### Requirement: 历史记录与状态管理 (History & Status Management)
AppBackup 的状态管理必须 (MUST) 采用“合并与即时反馈”策略，通过维护一个持久化的历史记录列表来驱动状态流转。

#### Strategy: 托管状态 (Managed Status)
在 `BackupRecord` 中引入 `ManagedStatus` 字段，用于记录控制器视角的备份状态。
- **Values**: `InProgress`, `Completed`, `Failed`, `Canceled`, `Unknown`
- **Source of Truth**: `AppBackup.Status.LatestBackupStatus` 始终由 `History` 列表中最新一条记录的 `ManagedStatus` 决定。

#### Strategy: 即时反馈 (Immediate Feedback)
为了保证 UI 的即时响应性，控制器在执行耗时操作（如创建/删除 Velero 资源）之前，必须先更新内存中的状态。
- **Action Trigger**: 当用户触发 `Backup` 或 `Retry` 时。
- **Immediate Update**: 控制器**首先**在 `Status.History` 中插入一条 `ManagedStatus=InProgress` 的记录，并更新 `LatestBackupStatus`。
- **Resource Creation**: 随后才执行实际的 Velero Backup 创建请求。
- **Benefit**: 即使 Reconcile 循环尚未完成或 Velero 资源尚未被 Informer 捕获，用户也能立即看到“处理中”的状态。

#### Workflow: 历史记录合并 (History Merge Strategy)
状态同步不再是简单的追加，而是采用“合并策略”来处理历史记录与集群现状的差异。

1.  **Load & Index**: 加载现有的 `Status.History`，构建一个以 `BackupName` 为键的 Map。
2.  **Observe**: 从集群中 List 所有属于该 AppBackup UID 的 Velero Backup。
3.  **Merge**:
    -   **Existing**: 如果 Map 中已存在该备份，使用集群中的最新状态（Phase, StartTime, CompletionTime）更新记录。
    -   **New (Scheduled)**: 如果集群中有新备份（如 Schedule 自动创建）但 Map 中没有，则作为新记录添加到 Map。
    -   **Missing (Data Loss)**: 如果 Map 中有记录但集群中找不到对应的 Velero Backup：
        -   如果记录已被标记为 `Canceled`，保持 `Canceled`。
        -   否则，将其标记为 `Unknown` (表示下游数据丢失)。
4.  **Sort**: 将合并后的记录列表按 `StartTime` 倒序排列（最新的在最前）。
5.  **Prune**: (可选) 根据策略移除过期的历史记录，但目前设计倾向于保留所有记录直至手动清理。

#### Scenario: 取消备份
- **Given** 一个正在进行的备份
- **When** 用户触发 `Cancel` 操作
- **Then** 控制器立即在 `History` 中将该记录标记为 `Canceled` (即时反馈)。
- **And** 控制器删除对应的 Velero Backup。
- **And** 在后续的 Sync 循环中，即使资源已消失，该记录仍保留为 `Canceled` 状态。

### Requirement: 搜索标签同步 (Search Labels Synchronization)
控制器必须 (MUST) 在 `AppBackup` 资源上自动维护一组用于搜索和过滤的标签。

#### Scenario: 标签初始化与更新
- **Given** 一个 AppBackup 资源被创建或更新
- **When** 控制器执行 Reconcile 循环
- **Then** 控制器必须 (MUST) 检查并确保以下标签存在且值正确：
    - `testudo.softcdata.com/app-backup-name`: 等于 `metadata.name`
    - `testudo.softcdata.com/app-backup-namespace`: 来源于 `spec.template.includedNamespaces`。若列表为空，则值为空字符串；否则为列表元素的逗号分隔字符串。
    - `testudo.softcdata.com/app-backup-cluster`: 等于 `spec.Cluster`
    - `testudo.softcdata.com/app-backup-type`: 若 `spec.schedule` 不为空，则为 "Schedule"，否则为 "Manual"。
    - `testudo.softcdata.com/app-backup-status`: 优先取 `status.latestBackupStatus`，若为空则取 `status.status`，若仍为空则设为 "Pending"。

#### Scenario: 状态变更同步
- **Given** AppBackup 的备份状态发生变化 (例如从 `InProgress` 变为 `Completed`)
- **When** 控制器更新 `status.Status` 字段
- **Then** 控制器必须 (MUST) 同步更新 `testudo.softcdata.com/app-backup-status` 标签以反映最新状态

### Requirement: 内部架构 (Internal Architecture)
为了保证代码的可维护性和扩展性，控制器必须 (MUST) 采用模块化的设计。

#### Scenario: 状态机模式
- **Given** AppBackup 控制器的复杂业务逻辑
- **When** 执行 Reconcile 循环
- **Then** 必须 (MUST) 通过状态机（State Machine）模式按顺序执行各个阶段（Init, Finalizer, Storage, Action, Schedule/OneOff, Status）。

### Requirement: Trace ID 传播 (Trace ID Propagation)
为了支持全链路追踪，控制器必须 (MUST) 将 `AppBackup` 上的 `trace_id` Annotation 传播到所有创建的子资源中。

#### Scenario: 传播到 Velero Backup
- **Given** 一个带有 `testudo.softcdata.com/trace-id` Annotation 的 `AppBackup`
- **When** 控制器创建关联的 `Velero Backup`
- **Then** 该 `Velero Backup` 的 Annotations 中必须 (MUST) 包含相同的 `trace_id`

#### Scenario: 传播到 Velero Schedule
- **Given** 一个带有 `testudo.softcdata.com/trace-id` Annotation 的 `AppBackup`
- **When** 控制器创建关联的 `Velero Schedule`
- **Then** 该 `Velero Schedule` 的 Annotations 中必须 (MUST) 包含相同的 `trace_id`
- **And** 该 `Velero Schedule` 的 `Spec.Template.Metadata.Annotations` 中也必须 (MUST) 包含相同的 `trace_id` (以确保由 Schedule 创建的 Backup 也继承该 ID)


### Requirement: 外部资源清理的幂等性 (Idempotent External Cleanup)
控制器在执行外部资源（如 Velero）清理时，必须 (MUST) 具备幂等性，能够处理资源已存在或已删除的情况，确保清理过程不被阻塞。

#### Scenario: 忽略已存在的 DeleteBackupRequest
- **GIVEN** AppBackup 正在被删除
- **AND** 目标集群中已经存在针对某个备份的 `DeleteBackupRequest`
- **WHEN** 控制器执行 `deleteExternalResources` 清理外部资源
- **THEN** 控制器在创建 `DeleteBackupRequest` 遇到 `AlreadyExists` 错误时应忽略该错误
- **AND** 继续执行后续清理流程，最终移除 Finalizer
