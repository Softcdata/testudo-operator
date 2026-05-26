# Change: 平台状态机一致性治理（Operator/Server/Web）

## Why
当前平台状态机在三端存在“定义分散 + 写入边界不清 + 终态语义不统一”的组合问题，已经在真实实例中暴露为状态互相矛盾：
- `DisasterInstance` 显示为保护中，但资源同步/数据同步为失败。
- 相同业务流程在不同资源上出现“失败事件 + 成功相位”的语义冲突。
- Server/Web 的派生状态与 Operator 原始状态缺少统一契约。

本次变更目标不是修单点，而是建立一套可持续的“状态机治理基线”，避免同类问题反复出现。

## What Changes
- P1：状态字段单写者治理（Operator）
  - 为核心状态字段建立唯一写入者规则（Single Writer Ownership）。
  - 收敛 `DisasterInstance.status.fsmState` 的跨控制器写入竞争。
- P1：终态语义统一（Operator）
  - 统一 `AppBackup/AppRestore` 的失败终态语义与转移规则。
  - 消除“部分失败却返回成功相位”等歧义。
- P1：同步失败后的策略显式化（Operator）
  - 将 DataSync/ResourceSync 失败后是否继续自动同步变为显式策略。
  - 默认保持兼容（不改变现网默认行为）。
- P2：跨层状态语义对齐（Server/Web）
  - 对齐 Server 派生状态与 Operator 原始状态契约。
  - 收敛 Web 操作门禁对混合状态的判定逻辑。
- P2：状态迁移可观测性（Operator + Server）
  - 补齐状态变更事件语义（from/to/reason），并更新全局事件规范。

## 建议（基于当前问题分析）
- 建议 1（先止血）：优先落地“单写者 + 终态语义”两项 P1，先解决“主状态与子状态冲突”的核心可信度问题。
- 建议 2（再控噪）：同步推进全局事件语义收敛，保证状态迁移事件有 `fromState/toState/reason`，为排障提供闭环证据。
- 建议 3（控风险）：同步失败策略默认保持兼容，仅在显式策略开启时切换行为，避免升级后调度行为突变。
- 建议 4（防回归）：建立“状态迁移矩阵 + 并发冲突 + 串流一致性”三类回归门禁，覆盖 Operator/Server/Web。
- 建议 5（分阶段发布）：按 `Operator 源头 -> Server 投影 -> Web 门禁` 顺序灰度，避免下游先改造成语义错配。

## 问题到方案映射
- 状态矛盾（实例显示保护中，但同步失败） -> `P1 单写者 + 终态语义统一`
- 失败后行为不可解释 -> `P1 同步失败策略显式化`
- 三端含义漂移 -> `P2 跨层状态契约`
- 事件可读但不可追溯状态迁移 -> `P2 状态迁移可观测性`

## Impact
- Affected repos
  - `/home/chenxi/YS/disaster-operator`
  - `/home/chenxi/YS/disaster-server`
  - `/home/chenxi/YS/cluster-disaster-web`
- Affected specs
  - `state-machines`（新增）
  - `global-events`（补充状态迁移事件语义）
- Compatibility
  - 不新增对外 API 路径。
  - Phase 1 不要求前端改接口字段。
  - 业务行为会更严格：此前“模糊成功”将被归并为明确失败。

## Non-Goals
- 本次不重写全部控制器，不做大规模架构重构。
- 本次不引入新的事件传输协议（仍沿用现有 Event + Watch 机制）。
- 本次不一次性替换所有历史状态字段命名。

## Success Criteria
- 任一实例在同一时刻不存在“主状态健康 + 子同步失败但未收敛”的长期冲突。
- `AppRestore` 的部分失败语义对外一致（状态、事件、统计一致）。
- DataSync/ResourceSync 在失败后行为可由显式策略解释并验证。
- Server/Web 对同一对象状态展示一致，不再出现跨层含义漂移。
