# Tasks: 删除检查补充 cleanup plan

## 1. OpenSpec

- [x] 1.1 确认 `cleanup_plan` 的返回模型与字段命名。
- [x] 1.2 确认通用 cleanup 标签协议与 `dependency-token` 的复用策略。
- [x] 1.3 明确 v1 覆盖范围：`AppBackup`、`AppRestore`、`DisasterInstance`、`DisasterDrill`。

## 2. Operator 标签写入

- [x] 2.1 在 `pkg/metadata` 中新增 cleanup 标签常量与辅助方法。
- [x] 2.2 `AppBackup` 创建/更新 Velero 资源时写入 cleanup 标签。
- [x] 2.3 `AppRestore` 创建/更新 Velero Restore 与 ConfigMap 时写入 cleanup 标签。
- [x] 2.4 为历史资源提供回填策略与兼容查询说明（Operator 渐进式补写 + Server 兼容回退）。

## 3. Server 接口扩展

- [x] 3.1 扩展 `POST /api/v1/deletion/check` 响应模型，新增 `cleanup_plan`。
- [x] 3.2 支持基于 cleanup 标签查询 `finalizer_cleanup`。
- [x] 3.3 支持基于 `OwnerReference` 查询 `cascade_cleanup`。
- [x] 3.4 支持远端资源不可达时返回 unresolved 计划项。
- [x] 3.5 `OwnerReference` 子资源不进入 `upstream`，仅出现在 `cleanup_plan`（例如 `DisasterOperation`）。
- [x] 3.6 `DisasterDrill` 等上游资源必须保留在 `upstream`（例如 `DisasterOperation` 的上游）。
- [x] 3.7 删除 `DisasterInstance` / `DisasterGroup` 时，`cleanup_plan` 必须包含 `DisasterOperation`。

## 4. 测试

- [x] 4.1 为 cleanup 标签辅助函数增加单元测试。
- [x] 4.2 为 `AppBackup` / `AppRestore` 标签写入增加单元测试。
- [x] 4.3 为 `deletion/check` 的 `cleanup_plan` 返回增加单元测试。
- [x] 4.4 覆盖 resolved / unresolved / empty plan 场景。
- [x] 4.5 覆盖 `DisasterOperation` 不出现在 `upstream` 的测试场景。
- [x] 4.6 覆盖 `DisasterOperation` 上游 `DisasterDrill` 保留在 `upstream` 的测试场景。
- [x] 4.7 覆盖删除 `DisasterInstance` / `DisasterGroup` 时 `cleanup_plan` 包含 `DisasterOperation` 的测试场景。

## 5. 文档与联调

- [x] 5.1 更新 Apipost 文档，补充 `cleanup_plan` 返回结构。
- [x] 5.2 为前端删除确认交互补充字段说明（`cleanup_plan` 影响面展示，不等价于阻塞）。
- [x] 5.3 标注 `cleanup_plan` 与 `can_delete` 的语义区别。
