# Change: 为删除检查补充连带清理计划

## Why

当前 `POST /api/v1/deletion/check` 只能回答两类问题：

- 谁正在引用我（`upstream`）
- 我依赖谁（`downstream`）

但在实际删除确认场景中，这还不够。用户在删除前还需要知道：

- 如果我删除这个资源，**Finalizer 会顺带清理哪些资源**？
- 哪些子资源会通过 **OwnerReference / Kubernetes 级联删除** 一起消失？
- 这些连带删除发生在**本地集群**还是**目标集群**？

目前这些信息散落在各个 Controller 的删除逻辑里，前端无法统一展示，Server 也没有统一的查询协议。例如：

- `AppBackup` 删除时会清理 Velero `Schedule` 与 `Backup`
- `AppRestore` 删除时会清理 Velero `Restore` 与 ResourceModifier `ConfigMap`
- `DisasterInstance` 删除时会通过 `OwnerReference` 级联删除 `DataSync` / `ResourceSync`
- `DisasterDrill` 删除时会通过 `OwnerReference` 级联删除 `DisasterOperation`

因此，需要在统一删除检查能力上继续扩展一层：**删除清理计划（cleanup plan）**。

## Goals

- 为 `POST /api/v1/deletion/check` 增加 `cleanup_plan` 返回结构。
- 在删除前给出“将被连带删除的资源列表”，区分：
  - `finalizer_cleanup`
  - `cascade_cleanup`
- 明确 `DisasterOperation` 等子资源不作为 `upstream` 阻塞，仅在 `cleanup_plan` 中体现（通常为 `cascade_cleanup`）。
- 明确 `DisasterDrill` 作为 `DisasterOperation` 的上游资源，必须出现在 `upstream` 阻塞列表中。
- 定义一套**通用清理标签协议**，让 Finalizer 管理的外部资源也能被统一查询。
- 复用现有 `dependency-token` 作为 owner token，避免引入重复 token 机制。
- 支持跨命名空间、跨集群清理对象的表达与查询。

## Non-Goals

- 不改变现有 Finalizer 的执行行为。
- 不将 `cleanup_plan` 直接作为删除门禁。
- 不要求 v1 一次覆盖所有 Controller 的所有清理副作用。
- 不把运行时的每个底层删除动作（例如 `DeleteBackupRequest` 这种中间请求对象）都暴露给前端。
- 不在本提案中重做 `can_delete` 判定规则；该问题继续由独立规则提案推进。

## What Changes

### 1. 删除检查接口扩展

扩展：`POST /api/v1/deletion/check`

在现有返回体上新增：

- `cleanup_plan.has_cleanup`
- `cleanup_plan.finalizer_cleanup[]`
- `cleanup_plan.cascade_cleanup[]`

语义：

- `finalizer_cleanup[]`：由 Controller 在 Finalizer 中显式清理的资源。
- `cascade_cleanup[]`：依赖 `OwnerReference` 或 Kubernetes GC 自动删除的资源。

### 2. 通用清理标签协议（新增）

为会被 Finalizer 连带删除的资源新增统一标签族：

- `testudo.softcdata.com/cleanup-owner-token=<token>`
- `testudo.softcdata.com/cleanup-relation=<relation-code>`
- `testudo.softcdata.com/cleanup-strategy=<strategy>`
- `testudo.softcdata.com/cleanup-managed-by=disaster-operator`

说明：

- `cleanup-owner-token` 复用 owner 资源的 `dependency-token`。
- `cleanup-relation` 标识清理关系来源，例如：
  - `finalizer.veleroSchedule`
  - `finalizer.veleroBackup`
  - `finalizer.veleroRestore`
  - `finalizer.resourceModifierConfigMap`
- `cleanup-strategy` 描述清理方式，例如：
  - `delete`
  - `delete_request`
  - `owner_reference`

### 3. v1 覆盖范围

第一阶段只覆盖当前已有稳定删除语义的资源：

- `AppBackup`
  - Finalizer 清理：Velero `Schedule`、Velero `Backup`
  - 级联删除：`BackupRestoreStatistics`（OwnerReference）
- `AppRestore`
  - Finalizer 清理：Velero `Restore`、ResourceModifier `ConfigMap`
- `DisasterInstance`
  - 级联删除：`DataSync`、`ResourceSync`
  - 级联清理：`DisasterOperation`（不进入 `upstream`）
- `DisasterGroup`
  - 级联清理：`DisasterOperation`（不进入 `upstream`）
- `DisasterDrill`
  - 级联删除：`DisasterOperation`（不进入 `upstream`）

说明：`DisasterOperation` 作为容灾实例/组/演练的子资源，不应作为 `upstream` 阻塞项，统一归入 `cleanup_plan`。
说明：`DisasterDrill` 作为 `DisasterOperation` 的上游资源，必须保留在 `upstream` 中。

### 4. 查询行为

Server 在构造 `cleanup_plan` 时：

- 对 `finalizer_cleanup`：优先基于通用 cleanup 标签查询。
- 对 `cascade_cleanup`：基于 `OwnerReference` / 明确 owner 关系查询。
- 若目标集群不可达或资源无法枚举，可返回 `resolved=false` 的计划项，并携带 selector / cluster 信息。

## Impact

- Product Impact:
  - 删除确认框可以展示“删除后还会一起删除什么”。
  - 用户能区分“引用阻塞”和“连带清理”两个概念。
- Engineering Impact:
  - 涉及 Operator 写入 cleanup 标签。
  - 涉及 Server 扩展 `deletion/check` 响应模型与查询逻辑。
  - 涉及前端删除确认 UI 的新展示区块。
- Compatibility:
  - `POST /api/v1/deletion/check` 的请求体保持不变。
  - 旧字段 `upstream/downstream/can_delete/message` 保持兼容。

## Risks

- 风险：远端目标集群不可达时，无法准确列出 Finalizer 目标资源。
  - 缓解：允许返回 unresolved 计划项，并附带 selector 与 cluster 信息。
- 风险：部分历史资源还未写入 cleanup 标签，导致短期结果不完整。
  - 缓解：允许查询层保留兼容回退逻辑；后续增加回填。
- 风险：前端误把 `cleanup_plan` 当作“阻塞删除”。
  - 缓解：文档明确 `cleanup_plan` 是影响面展示，不等价于 `can_delete`。

## Open Questions

- `cleanup_plan` 是否需要额外暴露 `blocking_cleanup` 或失败风险信息？本提案 v1 暂不加入。
- `Cluster` 的 `uninstall-velero` 是否应纳入 cleanup plan？本提案 v1 暂不覆盖。
- `AppRestore` 在 Failed/Cancelled 场景中的 Pending workload 清理，是否应作为删除计划一部分？本提案 v1 暂不纳入。
