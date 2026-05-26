# 验收标准：通用依赖标签驱动的统一删除检查

## 1. 说明

本标准用于验收 `unify-deletion-protection` 在当前阶段的实现，核心要求：

- 使用通用依赖标签表达资源下游关系。
- 统一检查接口返回 `upstream/downstream/can_delete`。
- `can_delete` 仅由 `upstream` 派生。
- 删除检查基于**直接查询 Kubernetes 资源**完成，不依赖关系表落库。
- 既有删除接口保持现状，由前端基于检查结果决定是否继续删除。
- 依赖规则来源明确为 `docs/platform-resource-dependency-audit.md` 的**模块真实调用依赖矩阵**。

验收结论：

- 全部 `P0`、`P1` 通过方可合入。
- `P2` 可作为改进项，不阻塞合入。

## 2. P0（必须通过）

### P0-1 通用依赖标签可用

- 已新增 `testudo.softcdata.com/dependency-token`。
- 已实现 `testudo.softcdata.com/dependency-to-<token>=<relation-code>`。
- 创建路径可写入 token 与下游边。

### P0-2 不改旧标签

- 已有业务标签键名与语义不变。
- 新方案不替换 `LabelAppBackupCluster`、`LabelDisasterPolicyName` 等历史标签。

### P0-3 统一检查接口可用

- 存在接口：`POST /api/v1/deletion/check`。
- 目标存在返回 `200`，包含 `upstream/downstream/can_delete`。
- 目标不存在返回 `404`。

### P0-4 检查判定正确

- `upstream` 非空时，`can_delete=false`，并返回完整上游列表。
- `upstream` 为空时，`can_delete=true`。
- `downstream` 仅用于影响展示，不参与 `can_delete` 计算。

### P0-5 当前阶段不接入后端删除门禁

- 既有 `DELETE` 接口路径、参数、行为保持兼容。
- 删除接口不强制依赖检查接口结果。
- 前端可基于检查结果自行决定是否继续删除。

### P0-6 当前阶段不依赖关系表

- 删除检查主路径不依赖独立关系表。
- 主路径直接通过 K8s 资源查询与通用标签计算 `upstream/downstream`。

### P0-7 存量回填可用

- 可对存量资源补齐 `dependency-token` 与 `dependency-to-*`。
- 回填可幂等重跑，不产生重复或脏边。

## 3. P1（应通过）

### P1-1 模块覆盖完整

至少覆盖以下目标模块：

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

- `DisasterOperation`、`DataSync`、`ResourceSync` 作为内部来源纳入。
- `DisasterJob` 仅作为 `DisasterPolicy` 兼容引用来源。

### P1-2 查询逻辑可解释

- 上游查询规则：按目标 token 生成 `dependency-to-<token>`，对覆盖模块执行 K8s 查询。
- 下游查询规则：读取目标自身 `dependency-to-*` 并解析输出。
- `relation-code` 与实际依赖字段/标签一致。
- 每条查询规则可回溯到“模块真实调用依赖矩阵”的对应条目。

### P1-3 兼容回归通过

- 既有删除接口调用方式无变更。
- 既有自动化脚本无需改造可继续运行。

### P1-4 测试覆盖

- token 生成与动态标签写入测试通过。
- 检查接口 `upstream/downstream/can_delete` 一致性测试通过。
- 回填任务幂等性测试通过。

## 4. P2（建议通过）

### P2-1 前端体验

- 删除前统一先调用检查接口。
- `upstream` 非空展示阻塞列表并提供前端确认策略。
- `upstream` 为空展示 `downstream` 影响提示。

### P2-2 可观测性

- 记录检查耗时、命中上游数量、回填执行结果。
- 错误日志可定位资源键、relation-code、token。

### P2-3 演进兼容

- 文档明确未来可演进到关系表，但不影响当前接口语义。
- 当前返回模型在未来持久化方案下保持兼容。

## 5. 验收执行步骤（推荐）

1. 静态验收：标签协议、接口定义、兼容边界、文档一致性。
2. 单元验收：token、标签重建、判定逻辑、错误处理。
3. 集成验收：创建/更新/回填/检查联动。
4. 回归验收：既有删除接口与脚本兼容性。
5. 输出验收报告：按模板提供结论、证据、阻塞项。

## 6. 验收报告模板

```markdown
# 验收报告：unify-deletion-protection

## 结论
- [ ] 通过
- [ ] 不通过

## P0 检查
- [ ] P0-1 通用依赖标签可用
- [ ] P0-2 不改旧标签
- [ ] P0-3 统一检查接口可用
- [ ] P0-4 检查判定正确
- [ ] P0-5 当前阶段不接入后端删除门禁
- [ ] P0-6 当前阶段不依赖关系表
- [ ] P0-7 存量回填可用

## P1/P2 检查
- [ ] P1-1 模块覆盖完整
- [ ] P1-2 查询逻辑可解释
- [ ] P1-3 兼容回归通过
- [ ] P1-4 测试覆盖
- [ ] P2-1 前端体验
- [ ] P2-2 可观测性
- [ ] P2-3 演进兼容

## 阻塞项
1. <问题描述>（级别：P0/P1/P2）

## 证据
- <PR/Commit/日志/测试报告链接>
```
