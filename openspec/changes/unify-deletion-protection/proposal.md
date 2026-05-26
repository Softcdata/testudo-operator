# Change: 基于通用依赖标签的统一删除检查

## Why

当前删除保护主要依赖 Operator Finalizer 内部判定，存在三个问题：

- 规则分散在多个 controller 与 handler，前后端口径不一致。
- 前端缺少统一删除前依赖视图，用户难以在删除前判断风险。
- 依赖关系索引方式不统一（标签、Spec 扫描、状态判断混用），跨模块查询成本高。

结合 `docs/platform-resource-dependency-audit.md` 中的**模块真实调用依赖矩阵**，本变更引入**通用依赖标签协议**：

- 每个资源在自身标签中维护“我依赖哪些下游资源”。
- Server 通过该协议统一查询目标资源的 `upstream` 与 `downstream`。
- 前端根据检查结果自行决定是否继续调用删除接口。

## Goals

- 新增一套通用依赖标签协议，统一描述资源下游关系。
- **不修改现有系统标签语义与键名**，仅新增通用标签。
- 资源在创建与依赖变更时，将下游关系写回到资源自身标签。
- 提供统一删除检查接口，返回 `upstream`、`downstream` 与 `can_delete`。
- 前端可基于 `can_delete` 与 `upstream` 决定是否继续删除。

## Non-Goals

- 不移除或重写 Operator Finalizer 机制。
- 不在本轮引入新的强制删除协议（如 `force=true` 查询参数）。
- 不把统一检查逻辑强制接入所有 `DELETE` 接口门禁。
- 不要求一次性删除所有历史依赖判定代码（允许平滑迁移期兜底）。

## What Changes

### 1. 通用依赖标签协议（新增，不替换旧标签）

定义统一标签族（示意）：

- `testudo.softcdata.com/dependency-token=<token>`
- `testudo.softcdata.com/dependency-to-<token>=<relation-code>`

语义：

- `dependency-token`：资源自身稳定标识（由 UID 派生），用于被其他资源引用。
- `dependency-to-*`：当前资源的下游边集合；每个标签代表一条依赖边。
- `relation-code`：短码，表示依赖来源（如 `spec.config`、`label.policy`）。

约束：

- 不修改已有业务标签（例如 `LabelAppBackupCluster`、`LabelDisasterPolicyName` 等）。
- 统一标签仅用于删除检查与依赖查询，不改变现有业务语义。
- 矩阵到标签写入的逐条映射产物为：`openspec/changes/unify-deletion-protection/dependency-label-mapping.md`。

### 2. 写入时机统一

下游标签写入由资源创建与更新流程触发：

- 资源创建成功后，必须写入 `dependency-token` 与当前下游边。
- 资源依赖字段变化后，必须同步更新 `dependency-to-*`。
- 删除路径中不再增量写边，仅按既有删除流程移除资源。

### 3. 统一删除检查接口

新增：`POST /api/v1/deletion/check`

请求输入：

- `resource_kind`
- `name`
- `namespace`

响应输出：

- `upstream[]`：正在引用目标资源的上游资源列表。
- `downstream[]`：目标资源声明依赖的下游资源列表。
- `can_delete`：建议字段，等价于 `len(upstream) == 0`。

### 4. 删除执行策略（保持现状）

本阶段不把检查逻辑接入删除接口门禁：

- `DELETE` 接口保持现有行为与协议。
- 前端先调用检查接口，再由前端决定是否继续删除。
- 后端最终删除结果仍由现有控制器与 Finalizer 流程保证一致性。

### 5. 覆盖范围（v1）

按 `docs/platform-resource-dependency-audit.md` 的**模块真实调用依赖矩阵**覆盖以下模块：

- `Cluster`
- `StorageRepository`
- `DisasterPolicy`
- `DisasterConfig`
- `DisasterInstance`
- `DisasterGroup`
- `AppBackup`
- `AppRestore`
- `DisasterDrill`
- `DisasterBackup`

说明：

- `DisasterOperation`、`DataSync`、`ResourceSync` 作为内部依赖来源参与关系构建。
- `DisasterJob` 仅作为 `DisasterPolicy` 的兼容阻塞来源，不单列为独立目标模块。

### 6. 规则来源声明（必须遵循）

本提案的依赖定义与覆盖范围，必须以 `docs/platform-resource-dependency-audit.md` 的**模块真实调用依赖矩阵**为唯一基线来源。

约束：

- 新增/调整依赖规则时，需先更新该矩阵，再更新本提案与实现。
- 不允许脱离矩阵新增“推测性依赖”作为默认规则。
- 若矩阵与实现不一致，以矩阵先行修订并在变更说明中标注差异。

## Migration Strategy

1. 新增通用依赖标签写入逻辑，不影响既有标签。
2. 对存量资源执行一次回填任务，补齐 `dependency-token` 与 `dependency-to-*`（operator 启动参数：`--dependency-backfill-on-start`，默认开启）。
3. 上线 `POST /api/v1/deletion/check`，由前端先行接入检查流程。
4. 观察期内保留现有删除路径；后续若需要再评估后端门禁接入。

## Impact

- Backward Compatibility:
  - 现有删除接口 URL/参数/行为保持兼容。
  - 现有业务标签与 Finalizer 语义保持不变。
- Engineering Impact:
  - 涉及标签写入、删除检查服务、前端交互调整与测试体系。
- Product Impact:
  - 删除前可明确展示“谁在引用我（upstream）”和“我依赖谁（downstream）”。

## Risks

- 风险：标签未及时同步导致短时误判。
  - 缓解：创建即写入、更新即重算、定时回填校正。
- 风险：动态标签数量增长。
  - 缓解：限制仅写入审计矩阵内依赖边，边变更时覆盖式重建。
- 风险：前端忽略检查结果仍发起删除。
  - 缓解：前端删除交互强制先检查并显式二次确认。
