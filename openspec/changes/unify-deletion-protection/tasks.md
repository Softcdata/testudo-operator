# Tasks: 通用依赖标签驱动的统一删除检查

## 执行状态说明（2026-03-09）
- 本次执行范围：`disaster-operator` + `disaster-server`（标签协议、写入规则、检查接口、单测、OpenSpec 文档一致性）。
- 已完成跨仓库项：`disaster-server` 实现 `POST /api/v1/deletion/check`，并提供 `/apis/v1/deletion/check` 与 `/api/v1/deletion/check` 双入口。
- 非本仓库范围：前端交互接入（前端仓库按检查结果自行决定是否继续删除）。
- 执行环境说明：`go` 安装在 zsh PATH 下，验证命令统一以 `zsh -lic` 方式执行。

## 1. 口径冻结
- [x] 1.1 以 `docs/platform-resource-dependency-audit.md` 的**模块真实调用依赖矩阵**作为 v1 规则唯一基线并冻结模块范围。
- [x] 1.2 输出一份“矩阵 -> 依赖标签写入规则”的映射表，作为实现输入（按模块逐条对应）。
- [x] 1.3 冻结检查判定口径：`can_delete` 由 `upstream` 是否为空派生。
- [x] 1.4 冻结兼容策略：不修改任何现有业务标签与删除接口协议。

## 2. 通用依赖标签协议
- [x] 2.1 新增标签常量：`testudo.softcdata.com/dependency-token`。
- [x] 2.2 定义动态标签 key 规范：`testudo.softcdata.com/dependency-to-<token>`。
- [x] 2.3 定义 `relation-code` 字段规范（短码、长度限制、枚举来源）。
- [x] 2.4 实现 UID -> token 生成器并补充稳定性测试。

## 3. 资源标签写入
- [x] 3.1 在创建路径写入 `dependency-token`。
- [x] 3.2 在创建/更新路径计算并写入 `dependency-to-*`。
- [x] 3.3 采用覆盖式重建策略清理过期依赖边。
- [x] 3.4 保障写入失败可观测（事件/日志/指标至少一项）。

## 4. 存量资源回填
- [x] 4.1 实现一次性回填任务，补齐缺失 token 与下游边。
- [x] 4.2 支持幂等重跑（重复执行结果一致）。
- [x] 4.3 回填期间提供即时补写或兜底检查，避免漏判。

## 5. 统一检查服务与接口
- [x] 5.1 实现 `POST /api/v1/deletion/check`。
- [x] 5.2 响应模型统一返回 `target/upstream/downstream/can_delete`。
- [x] 5.3 上游查询基于 `dependency-to-<target-token>` 统一执行。
- [x] 5.4 下游查询基于目标资源自身 `dependency-to-*` 解析执行。
- [x] 5.5 错误码对齐：不存在 `404`，查询失败 `500`。

## 6. 前端接入策略
- [ ] 6.1 删除交互先调用 `POST /api/v1/deletion/check`。
- [ ] 6.2 `upstream` 非空时提示阻塞详情并由前端决定是否继续删除。
- [ ] 6.3 `upstream` 为空时展示 `downstream` 影响提示后继续删除。
- [x] 6.4 既有 `DELETE` 接口保持现状，不接入强制后端门禁。

## 7. 规则实现（v1）
- [x] 7.1 Cluster 依赖边写入规则（查询规则见 5.x）。
- [x] 7.2 StorageRepository 依赖边写入规则（查询规则见 5.x）。
- [x] 7.3 DisasterPolicy 依赖边写入规则（含 DisasterJob 兼容；查询规则见 5.x）。
- [x] 7.4 DisasterConfig 依赖边写入规则（查询规则见 5.x）。
- [x] 7.5 DisasterInstance 依赖边写入规则（查询规则见 5.x）。
- [x] 7.6 DisasterGroup 依赖边写入规则（查询规则见 5.x）。
- [x] 7.7 AppBackup 依赖边写入规则（查询规则见 5.x）。
- [x] 7.8 AppRestore 依赖边写入规则（查询规则见 5.x）。
- [x] 7.9 DisasterDrill 依赖边写入规则（查询规则见 5.x）。
- [x] 7.10 DisasterBackup 依赖边写入规则（查询规则见 5.x）。

## 8. 测试与回归
- [x] 8.1 单测覆盖 token 生成、动态标签写入、覆盖式重建。
- [x] 8.2 单测覆盖 `upstream/downstream/can_delete` 推导一致性。
- [x] 8.3 集成测试覆盖创建、更新、回填、检查接口联动。
- [x] 8.4 回归验证既有标签不受影响，既有删除调用脚本无需改造。

## 9. 文档与验收
- [x] 9.1 同步 `proposal/design/spec/tasks/acceptance` 术语一致。
- [x] 9.2 输出 P0/P1/P2 验收报告并附测试证据。
- [x] 9.3 在环境具备时执行 `openspec validate unify-deletion-protection --strict`。
