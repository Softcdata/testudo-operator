## Context
当前全局事件问题不属于单点缺陷，而是 Operator 事件生产、Server 聚合与过滤、Web 通知展示三层协同偏差。修复目标是在不改变现有 API 字段结构的前提下，实现“审计可追踪 + 前端低噪声”的统一效果。

## Goals / Non-Goals
- Goals
  - 消除同名任务跨批次混并。
  - 消除同名异 Kind 资源历史串流。
  - 降低诊断事件重复噪音。
  - 降低前端事件弹窗刷屏。
- Non-Goals
  - 不新增全局事件 API。
  - 不重构 WebSocket 协议与 Envelope 结构。
  - 不改变 TaskEvent 字段命名。

## Decisions
- Decision 1: 历史聚合键改为复合键
  - 采用 `task + traceId + involvedObjectUID` 作为主聚合键。
  - 当 `traceId` 缺失时，使用 `task + involvedObjectUID + startedAtAnchor` 兜底，避免不同批次误合并。
- Decision 2: 资源历史与资源流必须执行 Kind 隔离
  - `/:resource/:name/history` 与 `/watch/:resource/:name/events` 都按 `:resource` 映射的 Kind 过滤。
  - 不允许仅用 `involvedObject.name` 作为资源身份。
- Decision 3: 诊断事件统一限频
  - 在 helper 提供 `Recorder.Event` 包装层，键为 `object + eventType + reason + message`。
  - `Warning` 采用更长窗口，`Normal` 采用较短窗口，窗口内重复事件不再发射。
- Decision 4: 前端 toast 窗口聚合
  - 聚合键固定为 `taskName + reason + status`。
  - 相同键命中窗口时只更新同一提示，不生成新提示。

## Risks / Trade-offs
- 历史条目数会上升：这是从“错误合并”恢复为“正确拆分”，属于行为纠偏。
- Kind 过滤更严格后，错误路径请求会更早暴露。
- 诊断事件减少后，逐条重复日志不可见；可通过结构化事件与调试日志补位。

## Migration Plan
1. 先落 Server A/B/C 并补齐单测，保证 API 行为稳定。
2. 再落 Operator D，验证事件降噪不影响终态可观测性。
3. 最后落 Web E，验证通知层面降噪。
4. 输出专项对比报告：事件数量、混并、串流、前端弹窗数量。

## Verification
- 单测
  - Server：至少覆盖 B/C。
- 回归步骤
  - A：同名任务不同 trace 的聚合拆分验证。
  - D：`disasteroperation/cluster` 诊断事件频次前后对比。
  - E：10 秒窗口内重复 `ADDED` 的 toast 聚合验证。
