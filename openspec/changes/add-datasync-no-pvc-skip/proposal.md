# Change: DataSync 无 PVC 时跳过数据同步恢复

## 背景

当前 `DataSync` 的控制器逻辑会在同步触发后创建或触发 `AppBackup`，备份完成后继续创建 data restore `AppRestore`。`restore-builder` 中 data restore 固定包含 `pods`、`persistentvolumeclaims`、`persistentvolumes`，并通过 trafficless Pod 承接 Velero FSB 数据恢复。

这导致一个实际问题：当受保护命名空间内没有需要同步的 PVC 时，平台仍会执行完整数据同步链路，包含 Velero Backup、Velero Restore、trafficless Pod 镜像拉取和恢复等待。对于没有 PVC 的应用，资源同步已经由 `ResourceSync` 覆盖，`DataSync` 继续执行只会制造空恢复、额外耗时和离线环境镜像拉取风险。

## 目标

- 在启动一次新的 `DataSync` 数据同步前，基于源集群实际资源判断本次保护范围内是否存在可恢复 PVC。
- 当未发现可恢复 PVC 时，将本次 `DataSync` 作为成功 no-op 结束，不创建或触发 `AppBackup`、Velero Backup、`AppRestore`、Velero Restore 和 trafficless Pod。
- 保证 `DisasterInstance` 初始化时，只有资源无 PVC 的场景不会被 DataSync 空恢复阻塞。
- 保留有 PVC 场景的现有 FSB 数据同步行为，包括 initial PVC cleanup、resource modifier、Velero hooks、trafficless restore 和历史状态统计。
- 为用户和上层 API 提供明确状态：本次数据同步因无 PVC 被跳过，而不是失败或卡住。

## 非目标

- 不改变 `ResourceSync` 的资源同步范围。
- 不在本变更中解决 trafficless Pod 镜像私有仓库解析；该问题由 `add-target-registry-trafficless-restore` 覆盖。
- 不改变 Velero FSB 的工作方式。
- 不把“无数据变化”检测扩展为增量数据差异判断。
- 不新增外部 API 字段；优先复用现有 `DataSync.status.conditions` 和 `DataSync.status.history` 表达跳过结果。

## 方案概述

新增 `DataSync` 源端数据负载预检，运行在一次新同步真正创建或触发 `AppBackup` 之前：

1. 读取 `DisasterInstance`、`DisasterConfig` 并解析当前同步方向的源集群。
2. 使用源集群 client 按 `instance.spec.namespaces` 枚举 PVC。
3. 按与 DataSync Backup 一致的保护范围过滤 PVC：
   - 未配置 `instance.spec.labelSelector` 时，命名空间内任意非删除中 PVC 都视为可恢复 PVC。
   - 配置 labelSelector 时，直接匹配该 selector 的 PVC 视为可恢复 PVC。
   - 同时枚举匹配 selector 的 Pod，收集 `spec.volumes[].persistentVolumeClaim.claimName`，被这些 Pod 引用的 PVC 也视为可恢复 PVC，避免业务只给 Pod 打标签、PVC 未打标签时被误跳过。
4. 若结果为空，则写入 no-op 成功状态并结束本次同步。
5. 若结果非空，则继续现有 `AppBackup -> AppRestore` 数据同步链路。

## 行为语义

- 无 PVC 跳过属于成功结果：
  - `status.state` 设置为 `Ready`
  - `status.lastSyncTime` 更新为本次触发时间
  - 清理已有错误状态
  - 写入 `conditions`，reason 使用 `NoPVCFound`
  - 记录结构化任务完成事件，状态为 success
- 历史记录建议新增一条 `Status=Skipped` 的 `SyncHistoryRecord`，`BackupName` 和 `RestoreName` 为空，资源计数为 0。
- `BackupRestoreStatistics` 需要把 `Skipped` 视为成功/完成，而不是失败。
- 预检失败不能静默跳过：
  - 源集群 client 构建失败、源集群 list 失败等依赖问题，应按现有失败路径进入 `Failed`
  - StorageRepository 不可用不应阻断无 PVC 跳过，因为 no-op 不需要访问备份仓库
- 已经进入 Velero Backup 或 Restore 的既有运行不在本提案中强行中断；本提案只保证新的同步运行在触发子任务前完成 no-op 判定。

## 影响范围

- `internal/controller/datasync/controller.go`
  - 新增源端 PVC 发现与 no-op 结束逻辑
  - 调整 `executeSync` 中 dependency、storage readiness、AppBackup 触发顺序
  - 调整同步历史与统计处理
- `pkg/apis/disaster/v1/datasync_types.go`
  - 如需常量化 reason/status，可新增内部常量；不新增 CRD 字段
- `internal/controller/datasync/*_test.go`
  - 新增无 PVC、有 PVC、labelSelector、失败路径等单测
- `openspec/specs/data-sync`
  - 归档后形成 DataSync 行为规范

## 与其他变更的关系

- `add-target-registry-trafficless-restore` 仍然需要：有 PVC 时 data restore 仍会创建 trafficless Pod，离线环境仍需要按目标集群私有仓库解析 busybox 镜像和 pull secret。
- 本变更会减少无 PVC 场景下 trafficless Pod 的创建次数，因此能降低离线镜像问题的暴露面，但不能替代私有仓库镜像方案。
- 本变更应避免修改 `restore-builder` 默认 data restore 语义；restore-builder 仍负责“需要数据恢复时如何构建 AppRestore”，DataSync 控制器负责“本次是否需要数据恢复”。

## 风险

- 若 labelSelector 场景只给 PVC 打了不匹配标签、Pod 也未被 selector 选中，系统会判定无可恢复 PVC；这与当前 Velero Backup 的选择范围一致，应通过文档提示用户 selector 必须覆盖需要保护的数据载体或工作负载。
- 若源集群 API list 权限不足，DataSync 会失败而不是跳过；需要确认 operator 对远端集群具备 `pods` 与 `persistentvolumeclaims` 的 list 权限。
- 若上层统计或前端没有识别 `Skipped`，可能显示为失败；实现时必须同步调整统计聚合和展示兼容。
