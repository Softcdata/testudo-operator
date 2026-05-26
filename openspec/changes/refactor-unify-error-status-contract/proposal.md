# Change: 重构并统一全系统错误状态语义（Error Reason/Message Contract）

## Why

当前 `disaster-operator` 在错误信息表达上存在明显不一致，影响排障效率和上层系统（server/web）稳定消费：

- 字段不一致：部分模块使用 `status.reason/status.message`，部分仅有 `status.message`，部分仅在 `conditions[].reason/message` 写入错误。
- 语义不一致：`reason` 在部分模块是稳定枚举（如 `TokenExpired`），在部分模块却写成自然语言句子。
- 清理策略不一致：资源从失败恢复后，历史错误在部分模块会残留，导致 UI/接口误判。
- 事件与状态分离：Warning Event 与 CR Status 的错误语义未形成稳定映射，server 侧聚合时需要做脆弱推断。

本变更旨在建立一个**跨模块统一、可机器消费、可人类排障**的错误状态契约，并推动控制器实现收敛到同一模式。

## Goals

- 定义并落地统一错误契约：`reason`（机器可读错误码）+ `message`（人类可读描述）。
- 明确 Conditions 场景下的错误语义映射，避免“有错误但无统一入口”。
- 将失败事件载荷与状态错误语义对齐，降低 server/web 的解析成本。
- 分阶段完成核心模块改造，保持兼容并可回滚。

## Non-Goals

- 不在本提案中设计完整国际化（i18n）体系。
- 不在本提案中重构全部历史/归档 CRD 行为（以当前活跃控制器与主干 V2 为优先）。
- 不在本提案中定义 server/web 的最终文案策略，仅提供稳定错误码与描述语义。
- 不在本提案中改造已废弃模块：`DisasterJob`、`DisasterBackup`（仅保留废弃标注，不再继续补齐字段）。

## What Changes

### 1) 新增统一错误状态契约（Operator 内部标准）

- `reason`：稳定错误码（PascalCase，ASCII，无空格），用于程序判断与跨服务映射。
- `message`：详细错误描述，包含上下文（资源名、步骤、超时值、外部错误摘要等）。
- 同一失败语义在状态层与条件层必须一致：
  - 若资源有 `status.reason/status.message`，优先写入该对字段。
  - 若资源以 `conditions` 表达失败，`conditions[].reason/message` 必须与状态层语义保持一致（或可映射）。

### 2) 统一字段基线（按模块补齐）

按“可选新增字段、兼容读取旧字段”的原则，补齐缺失的错误字段，目标是核心资源都能稳定输出错误码与错误描述：

- 保持并规范：`Cluster`、`StorageRepository`、`AppBackup`、`AppRestore`、`DisasterPolicy`
- 补齐缺失：`DisasterConfig`（补 `status.message`）、`DisasterOperation`（补 `status.reason`）、`DisasterDrill`（补 `status.reason`）
- Conditions 型资源：`DataSync`、`ResourceSync` 继续保留 `conditions[].reason/message`，并新增顶层错误字段以便统一消费。
- 仅状态机资源：`DisasterInstance`、`DisasterGroup` 按需补充统一错误出口。
- 已废弃模块声明：`DisasterJob`、`DisasterBackup` 归类为 legacy/deprecated，不纳入本提案实施范围。

### 2.1 2026-03-23 当前缺口快照（需落地）

- Operator：
  - `DisasterJob`、`DisasterBackup` 已确认废弃，不作为本提案改造目标（仅文档标注）。
  - active 模块补充：`DisasterPolicy` 已将无效 cron 校验失败收敛为稳定 `reason=InvalidSchedule` + `message`；`AppBackup/AppRestore/StorageRepository` 失败事件已对齐 `errorCode` 与状态错误语义。
- Server（明确 4 个适配缺口）：
  - `DisasterDrillDTO` 已改为嵌套 `status.state/status.reason/status.message`，前端需同步切换读取路径。

### 3) 统一错误码命名与清理规则

- 错误码命名规范：`[Domain][Reason]`，示例：`ConfigNotFound`、`DependencyNotReady`、`BackupFailed`、`TimeoutExceeded`。
- 禁止将自然语言句子写入 `reason`。
- 资源进入成功/就绪终态时，必须清理陈旧错误字段（避免 stale error）。

### 4) 事件载荷与状态语义对齐

在 `ExecutionFinished` 且 `Failed` 的结构化事件中，载荷新增 `errorCode` 字段，并要求：

- `errorCode` 与资源 `status.reason`（或最新失败 condition reason）一致。
- `message` 与资源 `status.message`（或最新失败 condition message）语义一致。

### 5) 引入统一 helper（实现阶段）

新增公共 helper（如 `pkg/helper/status_error.go`），提供统一入口：

- `SetStatusError(...)`
- `ClearStatusError(...)`
- `SetConditionError(...)`

避免各控制器重复拼装、字段遗漏和命名漂移。

## Impact

- Operator
  - 需要更新多个 CRD status 结构体（可选字段），并回归生成代码/CRD manifests。
  - 需要批量调整控制器错误路径写入逻辑和成功路径清理逻辑。
- Server/Web
  - 可基于稳定 `reason` 做错误码映射，不再依赖 message 文案猜测。
  - UI 可直接展示 `message`，并按 `reason` 做差异化动作（重试、引导修复等）。
  - 同步状态页与演练/组 watch 页面不再依赖模糊文本推断失败原因，改为消费 DTO 透出的 `reason/message`。
- 兼容性
  - 采用增量字段与兼容读取策略，不破坏现有对象。
  - 旧字段/旧 message 行为在过渡期仍可读取。

## Risks

- 风险：改造面跨多个控制器，容易出现回归。
  - 缓解：按模块分批、每批带错误路径单测与回归用例。
- 风险：部分资源历史语义复杂，强行统一可能影响现网预期。
  - 缓解：先冻结错误码字典与映射表，再按优先级推进。
- 风险：事件与状态对齐后，server 侧可能出现重复展示。
  - 缓解：在 server 侧按 `traceId/task/errorCode` 做去重策略。

## Open Questions

- 是否要求 `message` 全量英文（当前部分模块为中文）还是允许“英文优先、中文可过渡”？
- 是否在 v1 就引入统一 `details` 结构（结构化上下文），还是先仅落地 `reason/message`？
