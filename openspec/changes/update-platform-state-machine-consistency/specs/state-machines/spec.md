## ADDED Requirements

### Requirement: 状态字段单写者 (Single Writer Ownership)
平台必须 (MUST) 为每个关键状态字段定义且执行唯一写入者规则，避免跨控制器并发覆盖。

#### Scenario: DisasterInstance 状态写入边界
- **WHEN** 系统处理 failover/reprotect 等编排操作
- **THEN** `DisasterInstance.status.fsmState` 必须 (MUST) 由实例状态机统一收敛写入
- **AND** `DisasterOperation` 必须 (MUST) 仅维护自身 `.status.state` 与步骤状态
- **AND** 系统不得 (MUST NOT) 在不同控制器中直接竞争写入同一终态字段

### Requirement: 终态语义一致性 (Terminal Semantic Consistency)
平台必须 (MUST) 保证状态、事件、统计三者对终态含义一致。

#### Scenario: AppRestore 部分失败
- **WHEN** 恢复任务出现部分失败（例如 Velero `PartiallyFailed`）
- **THEN** AppRestore 终态必须 (MUST) 进入失败语义（而非成功语义）
- **AND** 结构化事件与统计计数必须 (MUST) 同步反映失败结果

#### Scenario: AppBackup Failed 粘性
- **WHEN** AppBackup 进入 `Failed` 状态
- **THEN** 在没有新的明确触发（新 Action 或新一轮任务）前必须 (MUST) 保持 `Failed`
- **AND** 系统不得 (MUST NOT) 自动回跳到 `Pending` 造成失败信息丢失

### Requirement: 同步失败策略显式化 (Explicit Sync Failure Policy)
平台必须 (MUST) 将 DataSync/ResourceSync 失败后的调度行为显式化并可配置。

#### Scenario: 失败后继续自动重试
- **WHEN** 同步任务失败且策略配置为继续重试
- **THEN** 系统必须 (MUST) 保持调度并在后续周期继续触发同步
- **AND** 状态或事件中必须 (MUST) 可观测到当前策略决策

#### Scenario: 失败后停止自动重试
- **WHEN** 同步任务失败且策略配置为停止重试
- **THEN** 系统必须 (MUST) 停止后续自动调度
- **AND** 仅允许手动触发或显式恢复后再次执行

### Requirement: 跨层状态语义契约 (Cross-Layer State Contract)
Server 与 Web 必须 (MUST) 以 Operator 状态契约为唯一语义来源，保证展示与门禁一致。

#### Scenario: Server 状态派生一致
- **WHEN** Server 将 Operator 原始状态映射为展示状态
- **THEN** 映射规则必须 (MUST) 与状态契约一致且可复用
- **AND** 对同一对象的列表接口与流式接口必须 (MUST) 输出一致语义

#### Scenario: Web 操作门禁一致
- **WHEN** Web 基于状态决定按钮可用性
- **THEN** 门禁判断必须 (MUST) 依赖统一状态契约
- **AND** 不得 (MUST NOT) 对混合状态自行发明与后端冲突的语义

### Requirement: 状态迁移可观测性 (State Transition Observability)
关键状态迁移必须 (MUST) 产生可追踪的结构化事件。

#### Scenario: 记录状态迁移上下文
- **WHEN** 任一核心资源发生关键状态迁移
- **THEN** 事件消息必须 (MUST) 包含 `fromState`、`toState` 与 `reason`
- **AND** 该事件必须 (MUST) 可被全局事件接口稳定聚合和查询
