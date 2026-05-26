## Context
当前状态机相关逻辑分布在三层：
- Operator：核心状态写入与调谐推进。
- Server：状态派生、聚合与对外投影。
- Web：状态展示与操作门禁。

现状风险不是“某个 if 写错”，而是治理层缺失：
- 同一状态字段存在多写者；
- 终态定义在不同资源上不一致；
- 同步失败后的行为是隐式策略；
- 跨层映射没有契约化约束。

## Goals / Non-Goals
- Goals
  - 建立状态字段单写者规则，消除跨控制器写入竞争。
  - 统一终态语义，保证状态/事件/统计一致。
  - 将同步失败后的行为显式化并可测试。
  - 建立 Operator -> Server -> Web 的状态语义契约。
- Non-Goals
  - 不进行大规模重构或一次性替换所有历史资源模型。
  - 不引入新传输协议或新对外 API 路径。

## Decisions
- Decision 1: 状态字段单写者（Single Writer Ownership）
  - `DisasterInstance.status.fsmState` 由实例状态机控制器作为唯一终态写入者。
  - `DisasterOperation` 保持编排职责，写自身 `.status.state`，并通过受控信号触发实例状态迁移。
  - 目的：避免并发 Reconcile 造成状态覆盖和抖动。

- Decision 2: 终态语义优先“真实结果”
  - `AppRestore` 发生部分失败时，不再映射为成功终态。
  - `AppBackup` 的 `Failed` 状态改为粘性，需明确触发（新 Action/新一轮任务）才可离开。
  - 目的：消除“事件失败但状态成功”的矛盾。

- Decision 3: 同步失败策略显式化
  - 为 DataSync/ResourceSync 增加失败后调度策略（继续重试/停止重试）。
  - 默认值保持与现网行为一致，避免升级即行为突变。
  - 目的：让行为可解释、可配置、可回归。

- Decision 4: 跨层语义契约化
  - Server 的派生状态映射以 Operator 状态契约为唯一来源。
  - Web 门禁逻辑按契约消费，不自行发明状态含义。
  - 目的：避免三端语义漂移。

- Decision 5: 状态迁移事件规范化
  - 每次关键状态迁移应携带 `fromState/toState/reason` 语义。
  - 统一归入全局事件规范，支持排障与审计。

## Risks / Trade-offs
- 风险：单写者收敛可能改变部分操作路径时序。
  - Mitigation：增加迁移守卫 + 并发回归测试 + 灰度验证。
- 风险：终态语义变严格后，历史“成功率”指标可能下降。
  - Mitigation：在发布说明中明确“统计口径纠偏”。
- 风险：同步失败策略引入后，用户可能误配导致长时间无自动恢复。
  - Mitigation：默认兼容 + 明确告警 + UI 提示策略状态。

## Migration Plan
1. 先落规范与测试矩阵，再做代码改造。
2. 先改 Operator（状态源头），再对齐 Server/Web（消费端）。
3. 灰度验证核心场景：Failover/Reprotect、DataSync/ResourceSync 失败恢复、AppRestore 部分失败。
4. 输出前后对比与回滚方案，确认后全量。

## Open Questions
- DataSync/ResourceSync 的失败后策略字段放在 `spec` 还是由上层策略 CR 统一驱动。
- 容灾组是否在 CRD 层增加显式 `status.state`，还是继续保持 Server 派生。
- 状态迁移事件是否需要单独 Reason（如 `StateTransition`）还是复用现有三类结构化事件。
