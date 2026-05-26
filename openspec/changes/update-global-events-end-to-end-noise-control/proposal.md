# Change: 全局事件端到端降噪与聚合纠偏

## Why
当前“全局事件”链路存在四类并发问题：
- 服务端历史聚合键过粗，存在同名任务跨批次混并。
- 资源历史接口只按 `name` 过滤，存在同名异 Kind 串流。
- Operator 结构化事件已有限频，但诊断事件 `Recorder.Event*` 未统一限频，导致噪音持续。
- 前端对 `ADDED` 事件逐条弹窗，无窗口聚合，放大用户刷屏体感。

这些问题叠加后，造成“审计结果不稳定 + 用户界面高噪声”的双重风险。

## What Changes
- Server（P1）
  - 修复历史聚合键：从 `payload.task` 单键升级为复合键，避免同名任务跨批次混并。
  - 修复 `listResourceEvents`：增加 `resource kind` 过滤，阻断同名异类资源串流。
- Server（P2）
  - 移除 `watch` Header 明文打印。
  - `ConvertToTaskEventDTO` 对非 `ExecutionStarted/ExecutionProgress/ExecutionFinished` 事件早返回。
- Operator（P1/P2）
  - 新增诊断事件统一限频策略，至少覆盖 `disasteroperation` 与 `cluster` 高频路径。
- Web（P2）
  - 全局 toast 增加窗口聚合/去重，按 `task+reason+status` 合并。

## Impact
- 受影响仓库
  - `/home/chenxi/YS/disaster-server`
  - `/home/chenxi/YS/disaster-operator`
  - `/home/chenxi/YS/cluster-disaster-web`
- 受影响能力
  - `global-events`（Operator 规范）
  - `api-standards`（Server 规范，需同步聚合键语义）
- 兼容性
  - 不改变现有接口字段结构。
  - 事件条目数量可能增加（因为修复了错误混并）。
  - 前端通知数量将下降（窗口聚合生效）。

## Non-Goals
- 本次不引入新接口。
- 本次不调整历史 API 返回字段命名。
- 本次不替换既有 WebSocket 传输协议。
