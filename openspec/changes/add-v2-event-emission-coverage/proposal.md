# Change: 补齐 V2 编排模块结构化事件发射覆盖

## Why

当前 `global-events` 规范已经定义了 V2 资源需要发射结构化事件，但实现层存在三个明显缺口：

1. V2 六大编排模块（`DisasterInstance`、`DataSync`、`ResourceSync`、`DisasterOperation`、`DisasterGroup`、`DisasterDrill`）的发射时机没有形成可执行矩阵，控制器行为容易漂移。  
2. 运行期角色切换（`failover -> reprotect -> 反向 sync`）后，事件语义与静态 source/target 容易出现不一致，影响排障与审计。  
3. Server/Web 已依赖结构化 JSON 事件进行历史与实时展示，若 V2 控制器仅发传统 `Eventf`，任务时间线会不完整。

需要新增一个专门面向 V2 的事件覆盖提案，将“什么时候发、发什么、异常时怎么收敛”固化为标准。

## What Changes

### 1. 规范细化：V2 事件发射矩阵

在 `global-events` 能力中增量定义 V2 六个模块的事件发射矩阵，明确：
- 每类长耗时动作必须有 `ExecutionStarted` + `ExecutionFinished`
- 多步骤流程必须按关键步骤发 `ExecutionProgress`
- 事件消息统一使用 JSON 载荷（`task/status/message` 必填）

### 2. 方向语义：运行期主备角色优先

新增规范约束：涉及主备方向的事件（特别是 `DisasterOperation` 的 failover/reprotect/sync）必须基于运行期角色计算 source/target，不得锚定静态配置方向。

### 3. 协议兼容：结构化事件作为任务历史唯一来源

新增规范约束：V2 任务历史必须由带 `testudo.softcdata.com/task-event=true` 的结构化事件驱动。传统 `Recorder.Eventf` 可保留为诊断事件，但不得替代结构化任务事件。

### 4. 验收强化：测试先行

在提案任务中要求先补 BDD 测试，再落地控制器改造，至少覆盖：
- 主路径：创建、步骤推进、成功终态
- 错误路径：步骤失败、超时、删除清理失败
- 反向路径：failover 后 reprotect + 反向 sync 的事件方向一致性

## Impact

- `disaster-operator`
  - 受影响模块：`internal/controller/disasterinstance/*`、`datasync/*`、`resourcesync/*`、`disasteroperation/*`、`disastergroup/*`、`disasterdrill/*`
  - 受影响公共组件：`pkg/helper/event_reporter.go`（以复用为主）
- `disaster-server`
  - 无接口破坏性变更；需要用现有 `/apis/v1/events` 与 watch 路径做回归验证，确认 V2 事件可聚合
- `cluster-disaster-web`
  - 无接口字段变更；需验证通知与时间线在 V2 操作下可完整展示

## Risks

- 事件量上升导致噪声增加
- 多步骤并发场景下出现重复进度事件
- 删除流程若先移除 Finalizer 再发结束事件，会导致事件丢失

## Mitigation

- 引入“阶段去重”约束（按 `phase/step` 防抖）
- 约束 `ExecutionFinished` 必须在终态写入前/Finalizer 移除前发射
- 通过单元测试和端到端脚本验证“Started/Finished 成对出现”

## Acceptance Criteria

1. `openspec validate add-v2-event-emission-coverage --strict` 通过。  
2. V2 六大模块均有对应的事件发射场景规范，且包含主路径+错误路径。  
3. failover 后执行 reprotect 与反向 sync 时，事件中的方向语义与运行期角色一致。  
4. Server 侧可从结构化事件聚合出完整的 V2 历史与实时流。  
