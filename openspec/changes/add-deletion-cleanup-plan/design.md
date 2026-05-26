# Design: 删除检查的 cleanup plan 与通用清理标签协议

## 1. 背景

现有删除检查提供的是**依赖关系视图**：

- `upstream`：谁在引用目标资源
- `downstream`：目标资源依赖谁

但真实删除动作还有另一层语义：**删除影响面**。

删除影响面不等于依赖关系。它描述的是：

- 哪些资源会被 Finalizer 显式删除
- 哪些资源会因 OwnerReference 被 K8s 自动级联删除

因此本设计把“依赖关系”和“删除影响面”拆开建模：

- 依赖关系：继续由 `upstream/downstream` 表达
- 删除影响面：由新增的 `cleanup_plan` 表达

## 2. 返回模型设计

建议扩展 `DeletionCheckResponse`：

```json
{
  "target": {},
  "upstream": [],
  "downstream": [],
  "can_delete": true,
  "message": "...",
  "cleanup_plan": {
    "has_cleanup": true,
    "finalizer_cleanup": [],
    "cascade_cleanup": []
  }
}
```

### 2.1 CleanupRef

`finalizer_cleanup[]` 和 `cascade_cleanup[]` 使用统一结构：

- `kind`
- `name`（可选）
- `namespace`（可选）
- `uid`（可选）
- `cluster`（可选，表示目标集群）
- `relation_code`
- `strategy`
- `resolved`
- `selector`（可选，当无法列出具体对象时返回）
- `unresolved_reason`（可选）

### 2.2 设计取舍

- `finalizer_cleanup` 与 `cascade_cleanup` 分开返回，而不是用一个数组加 `type` 字段，原因是前端展示通常就是两个区块。
- `name` 允许为空，因为远端集群不可达时只能提供 selector。
- `selector` 允许存在，用于表达“会清理这一类资源，但当前无法解析成具体对象名”。
- `upstream` 仅用于阻塞引用关系；目标资源的子资源（如 `DisasterOperation`）必须只出现在 `cascade_cleanup`，不进入 `upstream`。
- 对于目标资源的上游（owner/引用方，如 `DisasterDrill` -> `DisasterOperation`），仍必须出现在 `upstream`。

## 3. 通用清理标签协议

### 3.1 标签定义

建议新增：

- `testudo.softcdata.com/cleanup-owner-token`
- `testudo.softcdata.com/cleanup-relation`
- `testudo.softcdata.com/cleanup-strategy`
- `testudo.softcdata.com/cleanup-managed-by`

### 3.2 语义

- `cleanup-owner-token`
  - 值为 owner 资源的 `dependency-token`
  - 用于“根据被删除资源查询会被一起清理的对象”
- `cleanup-relation`
  - 用于表达关系来源
  - 示例：`finalizer.veleroSchedule`
- `cleanup-strategy`
  - 用于表达清理方式
  - 示例：`delete`、`delete_request`
- `cleanup-managed-by`
  - 固定标识当前协议由哪个控制器体系维护

### 3.3 为什么复用 dependency-token

当前项目已经定义：

- `testudo.softcdata.com/dependency-token`

该 token 已满足“稳定、可索引、与资源 UID 绑定”的要求，因此不再新增 `cleanup-token`。这样可以减少：

- 重复元数据
- 回填复杂度
- Server 查询模型复杂度

## 4. 查询模型

### 4.1 Finalizer 清理对象

对于 Finalizer 管理的外部资源：

1. 先获取目标资源的 `dependency-token`
2. 根据资源类型选择对应查询域（本地集群 / 目标集群）
3. 通过 `cleanup-owner-token=<token>` 查询资源
4. 组装为 `finalizer_cleanup[]`

### 4.2 级联删除对象（OwnerReference/子资源关系）

对于 OwnerReference 或明确子资源关系的级联对象：

1. 按资源类型加载子资源列表
2. 通过 `OwnerReference.uid == target.uid` 或关联字段/标签判断归属
3. 组装为 `cascade_cleanup[]`

说明：

- `cascade_cleanup` 不强制要求子资源也写 cleanup 标签。
- v1 先优先基于已有 ownerref 关系输出。
- 若子资源不以 OwnerReference 关联（例如 `DisasterOperation` 通过 `spec.instanceName`/`spec.groupName` 绑定），仍归入 `cascade_cleanup`，且不得进入 `upstream`。
- OwnerReference 子资源不计入 `upstream`，避免与阻塞依赖混淆。

### 4.3 无法解析时的降级

若存在以下情况：

- 目标集群不可达
- 远端 CRD 不可用
- 资源只知道 selector，暂时拿不到具体名字

则返回：

- `resolved=false`
- `selector={...}`
- `unresolved_reason=<reason>`

## 5. v1 资源映射

### 5.1 AppBackup

删除逻辑来源：

- 删除 Velero `Schedule`
- 为 Velero `Backup` 创建 `DeleteBackupRequest`

对外展示时：

- `finalizer_cleanup`
  - `Schedule`（`relation_code=finalizer.veleroSchedule`）
  - `Backup`（`relation_code=finalizer.veleroBackup`，尽管底层通过 `DeleteBackupRequest` 驱动）
- `cascade_cleanup`
  - `BackupRestoreStatistics`（OwnerReference）

### 5.2 AppRestore

删除逻辑来源：

- 删除 Velero `Restore`
- 删除 ResourceModifier `ConfigMap`

对外展示时：

- `finalizer_cleanup`
  - `Restore`（`relation_code=finalizer.veleroRestore`）
  - `ConfigMap`（`relation_code=finalizer.resourceModifierConfigMap`）

### 5.3 DisasterInstance / DisasterGroup

删除逻辑来源：

- `DataSync`、`ResourceSync` 通过 OwnerReference 自动级联删除

对外展示时：

- `cascade_cleanup`
  - `DataSync`
  - `ResourceSync`
  - `DisasterOperation`（实例/组操作子资源，不进入 `upstream`）

### 5.4 DisasterDrill

删除逻辑来源：

- `DisasterOperation` 通过 OwnerReference 自动级联删除

对外展示时：

- `cascade_cleanup`
  - `DisasterOperation`（不进入 `upstream`）

说明：当目标资源为 `DisasterOperation` 时，其上游 `DisasterDrill` 应出现在 `upstream`。

## 6. API 兼容策略

- 请求体不变
- 新字段仅追加，不改旧字段
- 若某资源没有连带清理内容，返回：

```json
"cleanup_plan": {
  "has_cleanup": false,
  "finalizer_cleanup": [],
  "cascade_cleanup": []
}
```

## 7. 分阶段落地

### Phase 1

- 定义 `cleanup_plan` 返回模型
- 为 `AppBackup` / `AppRestore` 写入通用 cleanup 标签
- Server 支持构造 `finalizer_cleanup`

### Phase 2

- 为 `DisasterInstance` / `DisasterDrill` 增强 `cascade_cleanup`
- 增加历史资源回填或兼容回退逻辑

### Phase 3

- 前端删除确认框展示 cleanup plan
- 与 `can_delete` 的阻塞说明分区展示

## 8. 历史资源回填与兼容（v1）

本提案新增 cleanup 标签后，会存在一个短期窗口：历史资源未写入 cleanup 标签，导致按 `cleanup-owner-token` 查询不到具体对象。

为避免“删除影响面”在历史场景下失真，v1 明确采用**双路径**策略：

- Operator 渐进式回填：当 Controller 在 reconcile 中遇到已存在的外部资源时，补写 cleanup 标签（等价于“更新路径写入”）。
- Server 兼容回退：当按 cleanup 标签无法命中时，允许使用**存量 UID 标签**匹配目标对象。

### 8.1 Operator 渐进式回填范围

- `AppBackup`
  - 对已存在的 Velero `Schedule`：补写 `finalizer.veleroSchedule` 的 cleanup 标签。
  - 对已存在的 `Schedule.spec.template`：补写 `finalizer.veleroBackup` 的 cleanup 标签，确保 Velero 后续创建的 `Backup` 能继承标签。
  - 对已存在的 Velero `Backup`（包含手动备份复用同名场景）：补写 `finalizer.veleroBackup` 的 cleanup 标签。
- `AppRestore`
  - 对已存在的 Velero `Restore`（含新旧命名规则）：补写 `finalizer.veleroRestore` 的 cleanup 标签。
  - 对 ResourceModifier `ConfigMap`：走 EnsureConfigMap 更新路径，补写 `finalizer.resourceModifierConfigMap` 的 cleanup 标签。

### 8.2 Server 兼容回退的存量标签键（明确冻结）

当 cleanup 标签缺失时，Server 允许回退使用下列“存量 UID 标签”进行匹配：

- `AppBackup` 关联 Velero 资源
  - `Schedule`：`testudo.softcdata.com/app-backup-uid=<AppBackup.uid>`
  - `Backup`：`testudo.softcdata.com/app-backup-uid=<AppBackup.uid>`
- `AppRestore` 关联 Velero/ConfigMap 资源
  - `Restore`：`testudo.softcdata.com/app-restore-uid=<AppRestore.uid>`
  - `ConfigMap`：`apprestore.testudo.softcdata.com/uid=<AppRestore.uid>`

注意：

- cleanup 标签命中优先级高于存量标签。
- 若两种方式均无法列出具体对象，仍必须返回 unresolved 的“骨架计划项”，确保前端能展示影响面并给出解释。

## 9. 前端字段说明（删除确认交互）

`cleanup_plan` 的定位是“删除影响面展示”，它不等价于删除阻塞。

前端建议展示与判定规则如下（不涉及后端门禁）：

- `can_delete`：
  - 仅由 `upstream` 是否为空派生。
  - `upstream` 非空时建议提示“存在引用方”，并展示阻塞列表。
- `cleanup_plan`：
  - 用于提示“删除会连带清理哪些对象”，无论 `can_delete` 是否为 true 都建议展示。
  - `cleanup_plan.has_cleanup=false` 时前端可隐藏影响面区域。
  - `cleanup_plan.has_cleanup=true` 时建议分区展示：
    - `finalizer_cleanup[]`：Finalizer 显式清理（通常在目标集群）。
    - `cascade_cleanup[]`：OwnerReference/子资源关系级联清理（通常在目标集群或本地集群）。

对每条 `CleanupRef` 的展示建议：

- `resolved=true`：
  - 展示 `kind/namespace/name`，并可附带 `relation_code` 与 `strategy` 说明来源。
- `resolved=false`：
  - 展示 `kind`、`cluster`（若有）、`selector`（若有），并把 `unresolved_reason` 作为提示信息展示。

字段语义提示：

- `relation_code`：描述“为什么会被清理”，用于解释与审计（例如 `finalizer.veleroBackup`、`ownerReference.dataSync`、`spec.instanceName`）。
- `strategy`：描述“如何被清理”，用于解释执行方式（例如 `delete`、`delete_request`、`owner_reference`）。
