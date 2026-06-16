# 变更提案：Velero Hook 透传接入

## Why
平台需要支持 Velero Hook，以便在备份前后或恢复后执行应用自定义动作，例如应用静默、缓存刷新、恢复后初始化等。当前底层 `AppBackup` 和 `AppRestore` 已使用 Velero 原生 `BackupSpec` / `RestoreSpec` 作为 template，但 server API、前端模型以及容灾实例自动编排链路尚未形成明确的 Hook 输入面。

本阶段目标是先完成 Hook 透传，不引入 Hook 模板、模板渲染器或自定义 Hook 执行器。

## What Changes
- 在手工 `AppBackup` 创建/更新/查询接口中透传 Velero `BackupSpec.hooks`。
- 在手工 `AppRestore` 创建/更新/查询接口中透传 Velero `RestoreSpec.hooks`。
- 在 `DisasterInstance` 容灾链路中支持实例级 Hook 透传：
  - `dataBackup` 透传到 DataSync 生成的 `AppBackup.spec.template.hooks`。
  - `dataRestore` 透传到 DataSync 生成的 `AppRestore.spec.template.hooks`。
- DataSync 在实例 `veleroHooks` 更新后必须对齐既有 `ds-*` AppBackup 的 desired template，保证后续同步使用最新 Hook。
- 在 `DisasterDrill` 中支持演练级 `dataRestore` Hook 覆盖，并透传到 drill 创建的 Data Restore。
- 将 Velero `status.hookStatus` 继续向 server/API/UI 回显，用于展示 Hook attempted/failed 结果。
- 在容灾同步历史中增加 `backupHookStatus` / `restoreHookStatus` 汇总字段，作为 Web 展示 Hook 状态的稳定数据来源。
- 对明显敏感参数明文传入执行 server 硬拒绝，避免敏感值进入 CRD、审计日志和 API 响应。
- 明确 Hook timeout 平台最大值，用于 server 校验和测试断言。

## 非目标
- 不新增 HookTemplate CRD。
- 不实现模板参数渲染、模板版本管理或模板审批。
- 不实现平台自有 Hook 执行器；Hook 执行仍完全交给 Velero。
- 不承诺 ResourceSync 资源恢复阶段执行 Velero restore hook，因为该阶段当前排除 Pod，Velero restore hook 只对恢复出的 Pod 生效。
- 不在第一阶段实现细粒度命令白名单或沙箱策略，仅做基础结构合法性校验。

## Impact
- `disaster-operator`
  - CRD Go type / deepcopy / CRD manifest。
  - DataSync / restore builder / DisasterDrill / DisasterOperation 编排。
  - AppBackup、AppRestore 透传路径测试。
- `disaster-server`
  - AppBackup、AppRestore、DisasterInstance、DisasterDrill 请求/响应 DTO。
  - Hook 字段校验、清空语义、OpenAPI/RunAPI 文档同步。
- `cluster-disaster-web`
  - 创建/编辑表单或高级 JSON 输入。
  - 详情页和历史页 Hook 状态展示。

## 风险
- Velero Hook 本质上是对业务 Pod 执行命令，错误命令可能导致应用停机、数据锁未释放或恢复失败。
- restore hook 只作用于 Velero 恢复出的 Pod；对 Deployment/StatefulSet 扩容后新建的 Pod 不会自动执行。
- Hook 命中依赖 namespace、resource、labelSelector 与备份/恢复范围共同匹配，错误配置可能静默无命中。

## 决策
- 第一阶段采用原生 Velero Hook schema 透传，避免平台自定义 DSL 与 Velero 行为偏离。
- 容灾实例使用 `spec.veleroHooks` 表达实例级 DataSync Hook，不借用 `SyncPolicy`，避免 DataSync/ResourceSync 对外统一为 `SyncPolicy` 后造成执行语义歧义。
- 演练只支持 `dataRestore` 覆盖，不支持演练级 `dataBackup`，因为演练不创建新的数据备份。
- `DisasterVeleroHooks.dataBackup` / `dataRestore` 使用指针字段，server DTO 对子字段分别记录 presence，保证 update/clear 语义可判定。
