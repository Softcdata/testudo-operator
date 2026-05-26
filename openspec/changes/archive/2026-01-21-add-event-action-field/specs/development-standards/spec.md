## MODIFIED Requirements

### Requirement: 结构化任务事件记录 (Structured Task Events)
所有 Operator 控制器在执行用户触发的操作时，必须 (MUST) 记录遵循统一格式的结构化 Kubernetes Event，以便于 Server 端聚合与展示操作历史。

#### Scenario: 任务名称格式规范
- **Given** 控制器需要发射结构化事件
- **When** 构建 Task 名称时
- **Then** 必须 (MUST) 使用 `<动作><资源类型> <资源名称> [附加信息]` 的中文描述格式
- **And** 示例格式如下：
    - 资源创建：`创建集群 my-cluster`、`创建存储 my-storage`、`创建应用备份 my-app`、`创建应用恢复 my-restore`
    - 资源编辑：`编辑集群 my-cluster`、`编辑存储 my-storage`
    - 资源删除：`删除集群 my-cluster`、`删除存储 my-storage`、`删除应用备份 my-app`、`删除应用恢复 my-restore`
    - 任务执行：`应用备份 my-app 执行备份 backup-1`、`应用恢复 my-restore 执行恢复 restore-1`
    - 任务操作：`应用备份 my-app 取消备份 backup-1`、`应用备份 my-app 重试备份 backup-1`

#### Scenario: 确保操作完整性（始终有始有终）
- **Given** 用户触发了一个操作（创建、删除、执行任务等）
- **When** 操作开始执行时
- **Then** 必须 (MUST) 发射一个 `ExecutionStarted` 事件，Status 设为 `InProgress`
- **When** 操作完成时（无论成功或失败）
- **Then** 必须 (MUST) 发射一个 `ExecutionFinished` 事件，包含最终 Status 和 Duration
- **And** 同一操作的开始和结束事件必须使用相同的 Task 名称，以便 Server 端聚合
- **And** 禁止只有开始没有结束的事件，禁止只有结束没有开始的事件

#### Scenario: 事件标识与过滤 (Mandatory Label)
- **Given** 控制器准备发射一个结构化事件
- **When** 构建 Event 对象时
- **Then** 必须 (MUST) 包含 Label: `testudo.softcdata.com/task-event: "true"`
- **And** Server 端将仅采集带有此 Label 的事件作为操作历史

#### Scenario: 记录结构化事件消息
- **Given** 控制器正在执行用户操作
- **When** 发射事件时
- **Then** 必须 (MUST) 使用 `pkg/helper/event_reporter.go` 中的辅助工具
- **And** 消息内容必须包含以下结构化标签：`[Task: %s] [Status: %s] [Duration: %s] [Cluster: %s] [User: %s] [TraceID: %s] <ExtraMessage>`
- **And** 标签含义如下：
    - `Task`: 任务描述，使用中文动作+资源格式
    - `Status`: 当前状态，取值 `InProgress`, `Success`, `Failed`, `Canceled`
    - `Duration`: 执行耗时，进行中显示 `-`，结束显示格式化字符串
    - `Cluster`: 关联集群名称，全局资源填 `-`
    - `User`: 触发用户，未知则为 `system`
    - `TraceID`: 追踪 ID

#### Scenario: 事件防抖 (Event Debouncing)
- **Given** 控制器的 Reconcile 循环被频繁触发
- **When** 资源状态未发生实质性变化时
- **Then** 严禁重复发射相同状态的结构化事件
- **And** 必须在 CRD Status 中维护 `LastEventPhase` 字段来记录上一次发射事件时的状态
- **And** 仅当状态发生变化时才发射新的结构化事件

#### Scenario: 删除操作事件的特殊处理
- **Given** 用户触发了资源删除操作
- **When** 资源进入删除流程（DeletionTimestamp 不为空）
- **Then** 必须 (MUST) 在所有清理工作完成后、移除 Finalizer 之前发射 `ExecutionFinished` 事件
- **And** 这是因为资源被删除后，关联的事件也会被 Kubernetes 清理，导致事件永远停留在 "进行中"
- **And** 正确的事件流程如下：
    1. 检测到 DeletionTimestamp → 发射 `删除xxx [Started]`
    2. 执行清理工作（删除外部资源等）
    3. 清理完成 → 发射 `删除xxx [Finished]`
    4. 移除 Finalizer → 资源被真正删除
- **And** 如果清理工作失败，应发射 `删除xxx [Finished: Failed]` 并保留 Finalizer

## ADDED Requirements

### Requirement: 全局事件目录规范
系统必须 (MUST) 维护一份全局事件目录，记录所有资源支持的操作事件类型。

#### Scenario: Cluster 事件目录
- **Given** 用户操作 Cluster 资源
- **Then** 系统必须支持以下事件类型：
    - `创建集群 {name}`: 资源创建的完整生命周期（包含 Velero 安装等中间过程）
    - `编辑集群 {name}`: 资源配置修改
    - `删除集群 {name}`: 资源删除的完整生命周期（可能包含 Velero 卸载）

#### Scenario: StorageRepository 事件目录
- **Given** 用户操作 StorageRepository 资源
- **Then** 系统必须支持以下事件类型：
    - `创建存储 {name}`: 资源创建的完整生命周期（包含 S3 验证）
    - `编辑存储 {name}`: 资源配置修改
    - `删除存储 {name}`: 资源删除的完整生命周期

#### Scenario: AppBackup 事件目录
- **Given** 用户操作 AppBackup 资源
- **Then** 系统必须支持以下事件类型：
    - `创建应用备份 {name}`: 资源创建的完整生命周期
    - `应用备份 {name} 执行备份 {backupName}`: 备份任务执行
    - `应用备份 {name} 取消备份 {backupName}`: 取消备份操作
    - `应用备份 {name} 重试备份 {backupName}`: 重试备份操作
    - `应用备份 {name} 删除备份 {backupName}`: 删除备份历史记录
    - `删除应用备份 {name}`: 资源删除的完整生命周期

#### Scenario: AppRestore 事件目录
- **Given** 用户操作 AppRestore 资源
- **Then** 系统必须支持以下事件类型：
    - `创建应用恢复 {name}`: 资源创建的完整生命周期
    - `应用恢复 {name} 执行恢复 {restoreName}`: 恢复任务执行
    - `应用恢复 {name} 取消恢复`: 取消恢复操作
    - `应用恢复 {name} 重试恢复`: 重试恢复操作
    - `删除应用恢复 {name}`: 资源删除的完整生命周期
