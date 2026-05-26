# Global Event Reporting Standard (V2)

## Purpose

为了实现跨组件的统一可观测性，我们建立了一套全局事件上报标准。本标准约定了 Kubernetes Event 的结构、Task 命名规范、以及核心资源的事件生命周期。

设计目标：
- **统一格式**：所有 Controller 遵循相同的事件结构。
- **可读性**：Task 名称使用自然语言（中文），便于最终用户理解。
- **完整性**：对于耗时操作，必须成对发射 `Started` 和 `Finished` 事件，并支持中间 `Progress` 事件。
- **持久化**：确保删除操作的完成事件在资源最终清理前成功上报。
## Requirements
### Requirement: 事件结构合规性 (Event Structure Compliance)
控制器必须 (SHALL) 使用定义的 Kubernetes Event 结构发射事件，以确保平台一致性。

#### Scenario: 发射结构化事件
- **WHEN** 控制器发射一个事件
- **THEN** `Reason` 字段必须 (SHALL) 为 `ExecutionStarted`、`ExecutionProgress` 或 `ExecutionFinished`
- **AND** `Type` 必须 (SHALL) 为 `Normal`（开始/进度/成功）或 `Warning`（失败）
- **AND** `Source.Component` 必须 (SHALL) 设置为 "disaster-operator"

### Requirement: 事件消息载荷格式 (Event Message Payload Format)
控制器发射的结构化任务事件消息体必须 (MUST) 使用 JSON 载荷，而不是标签拼接字符串。

#### Scenario: 结构化 JSON 载荷
- **WHEN** 控制器写入 `corev1.Event.message`
- **THEN** 消息体必须 (MUST) 是合法 JSON 字符串
- **AND** 载荷字段必须 (MUST) 包含 `task`、`status`、`message`
- **AND** 载荷字段应 (SHOULD) 包含 `cluster`、`user`、`traceId`、`duration`
- **AND** `duration` 仅在 `ExecutionFinished` 事件中填写，进行中事件可为空

### Requirement: 任务命名规范 (Task Naming Convention)
任务必须 (MUST) 使用严格的 `<Action><Resource> <ResourceName> [SubAction]` 格式，并使用中文动词。

#### Scenario: 命名集群创建任务
- **WHEN** 创建名为 "prod-cluster" 的集群
- **THEN** 任务名称必须 (MUST) 为 "创建集群 prod-cluster"

#### Scenario: 命名备份执行任务
- **WHEN** 为 AppBackup "my-app" 执行备份 "backup-01"
- **THEN** 任务名称必须 (MUST) 为 "应用备份 my-app 执行备份 backup-01"

### Requirement: 事件生命周期管理 (Event Lifecycle Management)
控制器必须 (MUST) 管理事件生命周期，以确保准确追踪持续时间和完成状态。

#### Scenario: 异步操作生命周期
- **WHEN** 开始一个长时间运行的操作
- **THEN** 控制器必须 (MUST) 发射 `ExecutionStarted` 事件
- **AND** 完成时，必须 (MUST) 发射 `ExecutionFinished` 事件 (Success 或 Failed)

#### Scenario: 删除操作生命周期
- **WHEN** 处理资源删除 (DeletionTimestamp != nil)
- **THEN** 控制器必须 (MUST) 在清理前发射 `ExecutionStarted`
- **AND** 必须 (MUST) 在移除 Finalizer *之前* 发射 `ExecutionFinished` 以确保事件被持久化

### Requirement: 全局事件目录合规性 (Global Event Catalog Compliance)
控制器必须 (SHALL) 为核心资源发射目录中定义的特定事件。

#### Scenario: 集群事件
- **WHEN** 协调 (reconciling) Cluster 资源
- **THEN** 必须 (MUST) 根据状态变更发射 "创建集群", "编辑集群", 和 "删除集群" 事件

#### Scenario: 策略事件
- **WHEN** 协调 (reconciling) DisasterPolicy 资源
- **THEN** 必须 (MUST) 根据状态变更发射 "创建策略", "编辑策略", "启用策略", "禁用策略", 和 "删除策略" 事件

#### Scenario: V2 编排资源事件
- **WHEN** 协调 (reconciling) V2 编排资源（DisasterInstance、DataSync、ResourceSync、DisasterOperation、DisasterGroup、DisasterDrill）
- **THEN** 必须 (MUST) 为关键生命周期动作发射结构化任务事件
- **AND** 每个长耗时动作必须包含 `ExecutionStarted` 与 `ExecutionFinished`
- **AND** 对于多步骤动作（如 Operation/Drill/Sync）应 (SHOULD) 在步骤推进时发射 `ExecutionProgress`

### Requirement: 服务端聚合 (Server-Side Aggregation)
API Server 必须 (SHALL) 基于事件消息中的结构化 JSON 字段进行聚合，并使用可区分批次的复合聚合键。

#### Scenario: 聚合任务
- **WHEN** Server 处理事件列表
- **THEN** 它必须 (MUST) 解析 JSON 载荷中的 `task`、`status`、`traceId` 字段形成任务列表
- **AND** 对于同一任务聚合必须使用复合键：`task + traceId + involvedObject.uid`
- **AND** 当 `traceId` 缺失时，必须使用 `task + involvedObject.uid + startedAtAnchor` 作为兜底键
- **AND** 对于无法解析为 JSON 的事件，必须忽略，不参与任务聚合

### Requirement: 资源历史与资源流 Kind 隔离
Server 必须 (MUST) 对资源历史与资源事件流执行 Kind 级别隔离，禁止同名异 Kind 串流。

#### Scenario: 资源历史接口隔离
- **WHEN** 客户端请求 `GET /apis/v1/:resource/:name/history`
- **THEN** 服务端必须按 `:resource` 映射目标 Kind
- **AND** 仅聚合 `involvedObject.name=:name` 且 `involvedObject.kind=目标Kind` 的事件

#### Scenario: 资源流接口隔离
- **WHEN** 客户端请求 `GET /apis/v1/watch/:resource/:name/events`
- **THEN** 服务端必须按 `:resource` 映射目标 Kind
- **AND** 仅推送 `involvedObject.name=:name` 且 `involvedObject.kind=目标Kind` 的事件

### Requirement: 诊断事件统一限频
Operator 必须 (MUST) 为诊断事件提供统一限频策略，并至少覆盖 `disasteroperation` 与 `cluster` 高频路径。

#### Scenario: 高频诊断事件抑制
- **WHEN** 同一对象在限频窗口内重复发射相同 `eventType + reason + message` 的诊断事件
- **THEN** Operator 必须抑制重复发射
- **AND** 窗口外首次事件必须发射

### Requirement: 前端全局通知窗口聚合
Web 前端必须 (MUST) 对全局事件通知执行窗口聚合，降低重复弹窗噪声。

#### Scenario: 相同任务通知聚合
- **WHEN** 在窗口内收到相同 `taskName + reason + status` 的 `ADDED` 事件
- **THEN** 前端不得创建新的 toast
- **AND** 前端必须刷新或累加同一提示

### Requirement: 策略事件上报 (Policy Event Reporting)
系统必须 (MUST) 在策略的关键生命周期操作时发射结构化 Kubernetes Event，以便用户追踪策略变更历史。

#### Scenario: 创建策略事件
- **Given** DisasterPolicy 资源被创建
- **When** Operator 完成首次 Reconcile（Finalizer 添加且验证通过）
- **Then** 系统必须发射 `ExecutionFinished` 事件
- **And** Task 名称为 `创建策略 <name>`
- **And** Status 为 `Success`

#### Scenario: 编辑策略事件
- **Given** DisasterPolicy 的 Spec 被修改（排除 State 字段）
- **When** Operator 检测到 `Generation > ObservedGeneration`
- **Then** 系统必须发射 `ExecutionFinished` 事件
- **And** Task 名称为 `编辑策略 <name>`

#### Scenario: 启用策略事件
- **Given** DisasterPolicy 的 `Spec.State` 从 Disabled 变为 Enabled
- **When** Operator 完成 Reconcile
- **Then** 系统必须发射 `ExecutionFinished` 事件
- **And** Task 名称为 `启用策略 <name>`

#### Scenario: 禁用策略事件
- **Given** DisasterPolicy 的 `Spec.State` 从 Enabled 变为 Disabled
- **When** Operator 完成 Reconcile
- **Then** 系统必须发射 `ExecutionFinished` 事件
- **And** Task 名称为 `禁用策略 <name>`

#### Scenario: 删除策略事件
- **Given** DisasterPolicy 被删除
- **When** Operator 在移除 Finalizer 前
- **Then** 系统必须发射 `ExecutionFinished` 事件
- **And** Task 名称为 `删除策略 <name>`
- **And** Status 为 `Success`

#### Scenario: 跨调用方式一致性
- **Given** 策略变更可能来自 API Server 或 kubectl
- **When** 任意方式触发策略变更
- **Then** Operator 都必须发射对应的事件

## Implementation Guidelines

### Auxiliary Functions
Use `pkg/helper/event_reporter.go` functions:
- `ReportTaskStartedWithClient(...)`
- `ReportTaskProgressWithClient(...)`
- `ReportTaskFinishedWithClient(...)`

禁止在新代码中使用旧格式事件函数 `ReportTaskStarted(...)` 和 `ReportTaskFinished(...)`。

### User and TraceID
From CR Annotations:
- User: `testudo.softcdata.com/user` (Default: `system`)
- TraceID: `testudo.softcdata.com/trace-id` (Default: auto-generated or empty)

### Global Event Catalog Details

**Cluster Events**
- **创建集群**: 包含 Velero 安装过程。
- **编辑集群**: 包含配置变更或元数据(Label/Description)变更。
- **删除集群**: 包含 Velero 卸载。

**StorageRepository Events**
- **创建存储**: 包含 S3 连接验证。
- **编辑存储**: 包含 S3 配置变更或元数据变更。
- **删除存储**: 确保没被引用。

**AppBackup Events**
- **执行备份**: 对应 Velero Backup。
- **周期备份**: Schedule 触发的备份。

**AppRestore Events**
- **执行恢复**: 对应 Velero Restore。

**DisasterPolicy Events**
- **创建策略**: 策略首次创建。
- **编辑策略**: 规格变更。
- **启用/禁用策略**: 状态变更。
- **删除策略**: 策略移除。

**DisasterInstance Events (V2)**
- **创建容灾实例**: 首次创建实例并完成初始化资源对齐时发射。
- **实例初始化进度**: DataSync/ResourceSync 首次同步推进时发射进度事件。
- **实例进入终态**: 从初始化失败或其他异常进入 Failed 时发射结束事件。
- **删除容灾实例**: 删除流程中在 Finalizer 移除前发射结束事件。

**DataSync Events (V2)**
- **执行数据同步**: 调度或手动触发一次数据同步时发射开始事件。
- **数据同步步骤进度**: 备份创建、恢复创建、等待 Velero 执行等阶段发射进度事件。
- **数据同步完成/失败**: 本次同步进入 Success/Failed 终态时发射结束事件。

**ResourceSync Events (V2)**
- **执行资源同步**: 调度或手动触发一次资源同步时发射开始事件。
- **资源同步步骤进度**: 骨架资源对齐、恢复动作、等待完成等阶段发射进度事件。
- **资源同步完成/失败**: 本次同步进入 Success/Failed 终态时发射结束事件。

**DisasterOperation Events (V2)**
- **执行容灾操作**: 创建并确认操作（如 failover/reprotect/undo/cancel/pause/resume/sync/drill）时发射开始事件。
- **操作步骤进度**: `PreCheck`、`FinalSync`、`ScaleDownSource`、`ScaleUpTarget`、`SwitchRoles` 等步骤推进时发射进度事件。
- **操作完成/失败/取消**: Operation 进入终态（Succeeded/Failed/Canceled）时发射结束事件。

**DisasterGroup Events (V2)**
- **创建容灾组**: 创建并完成组成员合法性校验后发射结束事件。
- **组操作编排进度**: 按 Level 串行推进组内实例操作时发射进度事件。
- **组删除**: 删除容灾组并完成清理时发射结束事件。

**DisasterDrill Events (V2)**
- **创建演练**: Drill 创建并进入 Ready 阶段时发射结束事件。
- **确认执行演练**: `spec.confirmed=true` 后进入执行阶段时发射开始事件。
- **演练执行进度**: 演练步骤推进（实例演练/组演练）过程中发射进度事件。
- **演练完成/失败**: Drill 进入 Completed/Failed 时发射结束事件。
- **演练清理**: `spec.cleanup=true` 触发清理并完成时发射结束事件。
