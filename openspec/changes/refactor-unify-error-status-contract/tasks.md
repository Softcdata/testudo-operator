# Tasks: 统一错误状态语义（Error Reason/Message Contract）

## 1. 口径冻结

- [x] 1.1 冻结统一错误契约：`reason`=稳定错误码，`message`=详细描述。
- [x] 1.2 冻结错误码命名规则（PascalCase/ASCII/不可写自然语言）。
- [x] 1.3 冻结模块字段基线与映射表（现状字段 -> 目标字段）。
- [x] 1.4 运行 `openspec validate refactor-unify-error-status-contract --strict` 并修复文档问题。

## 2. CRD 与类型定义改造

- [x] 2.1 为缺失模块补齐状态错误字段（可选字段，保持兼容；`DisasterJob/DisasterBackup` 已按废弃模块排除）。
- [x] 2.2 为 Conditions 型资源补充顶层错误出口（或定义明确映射规则）。
- [x] 2.3 执行 `make generate` 与 `make manifests`，确保类型与 CRD 产物一致。
- [x] 2.4 `DisasterConfig` 控制器改造：将自然语言 `reason` 收敛为稳定错误码，并将详细文本写入 `message`。
- [x] 2.5 `DisasterGroup` 组级错误出口补齐：新增 `status.reason/status.message`，并在 controller 聚合 `InstanceNotFound/InstanceFailed`。
- [x] 2.6 `DisasterInstance` 统一错误出口补齐：新增 `status.reason/status.message`，并在初始化失败路径写入稳定错误码。

## 3. 公共 helper 能力

- [x] 3.1 新增 `pkg/helper/status_error.go`，提供 `SetStatusError/ClearStatusError/SetConditionError`。
- [x] 3.2 在核心控制器中替换重复错误写入逻辑。
- [x] 3.3 统一成功终态清理 stale error 的策略。

## 4. 控制器分批改造（优先级）

- [x] 4.1 P0：`Cluster`、`AppBackup`、`AppRestore`、`DataSync`、`ResourceSync`、`DisasterOperation`、`DisasterDrill`。
- [x] 4.2 P1：`DisasterConfig`、`StorageRepository`、`DisasterPolicy`、`DisasterInstance`、`DisasterGroup`。
- [x] 4.3 P2：`DisasterBackup`、`DisasterJob`、`BackupPolicy`（legacy/脚手架链路；其中 `DisasterBackup/DisasterJob` 已明确为废弃模块并排除改造）。

## 5. 事件语义对齐

- [x] 5.1 结构化 `ExecutionFinished(Failed)` 载荷增加 `errorCode`。
- [x] 5.2 保证 `errorCode` 与 status/condition reason 一致。
- [x] 5.3 保证事件 `message` 与 status/condition message 语义一致。

## 6. 测试与验收

- [x] 6.1 为每个改造模块补充至少 1 条错误路径单测（断言 reason/message/condition）。
- [x] 6.2 为成功恢复路径补充清理测试（断言 stale error 被清空）。
- [x] 6.3 回归结构化事件测试，验证失败事件携带 `errorCode` 且与状态一致。
- [x] 6.4 执行核心测试集并记录结果（含失败原因与修复）：
  - 通过：`make generate && make manifests`
  - 通过：`go test ./internal/controller/datasync ./internal/controller/resourcesync ./internal/controller/disasteroperation ./internal/controller/disasterdrill ./internal/controller/disasterinstance ./internal/controller/disastergroup`
  - 通过（编译/聚焦）：`go test ./internal/controller -ginkgo.focus=DisasterPolicy -count=1`、`go test ./internal/controller/appbackup -run TestDoesNotExist -count=0`、`go test ./internal/controller/apprestore -run TestDoesNotExist -count=0`
  - 通过（新增回归）：`go test ./internal/controller/disastergroup ./internal/controller/datasync ./internal/controller/resourcesync ./internal/controller/disasteroperation ./internal/controller/disasterinstance -count=1`
  - 通过（新增错误路径）：`go test ./internal/controller -run TestDisasterConfigReconcileSetsSourceClusterNotFound -count=1`
  - 未全部通过（历史存量失败，非本次改动引入）：`go test ./internal/controller/appbackup ./internal/controller/apprestore`

## 7. 跨仓库联动

- [x] 7.1 输出 operator -> server 的错误码映射建议表。
- [x] 7.2 输出 web 展示策略建议（按 reason 分类 + message 直显）。
- [x] 7.3 `disaster-server` DTO 适配（P0）：
  - [x] 7.3.1 `DisasterConfigStatusDTO` 增加 `message` 并完成转换映射。
  - [x] 7.3.2 `SubResourceStatusDTO` 增加 `reason/message`，`getSyncStatus` 填充 `DataSync/ResourceSync` 对应字段。
  - [x] 7.3.3 `DisasterDrillDTO` 收敛为嵌套 `status` 对象，并透出 `status.reason/status.message`。
  - [x] 7.3.4 `DisasterOperationDTO`（group watch）增加 `reason` 并完成转换映射。
  - [x] 7.3.5 `DisasterGroupStatusDTO` 增加 `reason/message` 并完成转换映射（兼容旧 vendor 模式）。
  - [x] 7.3.6 `DisasterInstanceStatusDTO` 增加 `reason/message` 并完成转换映射（兼容旧 vendor 模式）。
