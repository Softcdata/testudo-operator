## MODIFIED Requirements

### Requirement: 全局事件目录合规性 (Global Event Catalog Compliance)
控制器必须 (SHALL) 为核心资源发射目录中定义的结构化任务事件；对于 V2 编排资源，必须进一步满足模块级发射矩阵要求。

#### Scenario: V2 六大模块发射矩阵
- **WHEN** 协调 (reconciling) V2 编排资源（`DisasterInstance`、`DataSync`、`ResourceSync`、`DisasterOperation`、`DisasterGroup`、`DisasterDrill`）
- **THEN** 每个长耗时动作必须 (MUST) 发射 `ExecutionStarted` 与 `ExecutionFinished`
- **AND** 多步骤动作必须 (MUST) 在关键步骤推进时发射 `ExecutionProgress`
- **AND** 事件消息必须 (MUST) 使用结构化 JSON 载荷（至少包含 `task`、`status`、`message`）

#### Scenario: 删除路径事件持久化
- **WHEN** V2 资源进入删除流程 (`DeletionTimestamp != nil`)
- **THEN** 控制器必须 (MUST) 在移除 Finalizer 前发射 `ExecutionFinished`
- **AND** 不得 (MUST NOT) 先移除 Finalizer 再发结束事件

## ADDED Requirements

### Requirement: V2 运行期主备方向语义一致性 (Runtime Direction Consistency)
涉及 source/target 方向语义的 V2 任务事件必须 (MUST) 基于运行期主备角色计算，不得依赖静态初始方向。

#### Scenario: Failover 后反向保护与反向同步
- **GIVEN** `DisasterInstance` 已执行 failover，运行期 primary/secondary 已互换
- **WHEN** 执行 `reprotect` 或后续 `sync` 相关操作
- **THEN** `DisasterOperation` 事件中的 `task` 与 `message` 必须 (MUST) 按运行期方向描述 source/target
- **AND** 不得 (MUST NOT) 使用静态 `sourceCluster/targetCluster` 方向导致语义反转

### Requirement: V2 结构化任务事件优先 (Structured Task Event First)
V2 任务历史必须 (MUST) 由结构化任务事件驱动；传统诊断事件不得替代结构化任务事件。

#### Scenario: 结构化事件与诊断事件共存
- **WHEN** 控制器需要同时输出用户可观测任务历史和调试信息
- **THEN** 必须 (MUST) 发射带 `testudo.softcdata.com/task-event=true` 的结构化任务事件
- **AND** 可额外发射 `Recorder.Eventf` 诊断事件
- **AND** 仅结构化任务事件用于 Server 侧任务聚合与 Web 任务时间线
