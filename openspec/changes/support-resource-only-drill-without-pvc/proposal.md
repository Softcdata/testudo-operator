# Change: 支持无 PVC 的纯资源容灾演练

## 背景

`add-datasync-no-pvc-skip` 已定义并实现 DataSync 在保护范围内没有可恢复 PVC 时的成功 no-op：`DataSync.status.state=Ready`、`NoDataVolumes=True`、`Reason=NoPVCFound`，最新同步历史为 `Skipped`，且不创建数据备份。

当前 Drill 没有消费这组状态语义。`DisasterDrill` 在 Pending 预检中把 `BackupAvailable` 固定写成 `true`，并把恢复模式固定为 `FullRestore`；确认后，`DisasterOperation` 固定生成 `RestoreResource -> RestoreData -> ScaleUp`。因此，无 PVC 实例会在执行到 `RestoreData` 时因 `DataSync.status.lastBackupName` 为空而失败，无法完成只恢复 Kubernetes 资源的演练。普通的数据备份缺失也被推迟到执行阶段才暴露，导致 Drill 错误进入 `Ready`。

## 目标

- 仅在 DataSync 明确声明“当前保护范围没有数据卷”时，允许 Drill 以 `ResourceOnly` 模式进入 `Ready`。
- `ResourceOnly` 演练只执行 `RestoreResource -> ScaleUp`，不创建 data `AppRestore`、Velero data Restore、PodVolumeRestore 或 trafficless Pod。
- 普通 DataSync 备份缺失、状态不一致或 ResourceSync 备份缺失必须在 Pending 预检失败，不能进入 `Ready`，也不能创建 `DisasterOperation`。
- 实例演练和容灾组演练使用同一套逐实例分类规则；混合组中的每个子 Operation 使用自己的恢复模式。
- 在用户确认时复核 Ready 阶段的逐实例模式快照，避免 Ready 期间 PVC/备份状态变化后继续按过期模式执行。

## 非目标

- 不修改 DataSync 的 PVC 发现、备份、恢复、history 或 statistics 逻辑。
- 不修改 ResourceSync 的资源选择、备份、恢复或副本记录逻辑。
- 不修改 Failover、Reprotect、Undo、Cancel、ResourceSync 主链路或业务 workload 模板。
- 不把任意空 `lastBackupName` 解释为无 PVC；只有完整且一致的 no-data 状态可以进入 `ResourceOnly`。
- 不改变 Drill cleanup 的缩容或 namespace 删除语义。
- 不在本仓库修改 server/web DTO；server 当前按字符串透传 `status.restoreMode`，其 OpenAPI 枚举与展示文案作为跨仓适配项单独同步。

## 方案概述

新增共享的 Drill 备份分类逻辑，对每个 DisasterInstance 同时检查 ResourceSync 和 DataSync：

1. ResourceSync 必须为 `Ready`，且 `status.lastBackupName` 非空；否则任何模式都失败。
2. DataSync 满足以下全部条件时分类为 `ResourceOnly`：
   - `status.state=Ready`
   - `conditions` 中 `Type=NoDataVolumes`、`Status=True`、`Reason=NoPVCFound`
   - 最新一条 `status.history[].status=Skipped`
3. 不满足 no-data 条件，但 DataSync 为 `Ready` 且 `status.lastBackupName` 非空时，分类为 `FullRestore`。
4. 其他情况全部失败关闭，不允许进入 `Ready`。

`NoDataVolumes/NoPVCFound + latest Skipped` 的当前状态优先于可能残留的历史 `lastBackupName`，避免新一轮已确认无 PVC 后误用旧数据备份。

## API 与状态边界

- 扩展 `RestoreMode`：
  - `ResourceOnly`：资源恢复后直接 ScaleUp。
  - `Mixed`：仅用于容灾组 Drill 的聚合状态回显，不得作为单实例执行模式。
- `DisasterDrill.status.instanceRestoreModes` 保存 Ready 阶段逐实例模式快照；实例 Drill 也保存单元素快照，用于区分新状态和升级前的历史 Ready 对象。
- `DisasterOperation.spec.drillConfig.restoreMode` 保存单实例或子 Operation 的已选执行模式。
- `DisasterOperation.spec.drillConfig.instanceRestoreModes` 仅供组父 Operation 向各子 Operation 传递逐实例模式；创建子 Operation 时必须 deep-copy 配置并收敛为单一 `restoreMode`。
- 历史未携带新字段的 Operation 保持 `FullRestore` 默认行为，避免升级后擅自跳过数据恢复。

## 安全语义

- `skipValidation=true` 仅保持现有的实例状态和目标 Cluster readiness 跳过能力，不得绕过备份存在性与 no-data 一致性检查。
- Drill 从 `Ready` 确认执行前必须重新分类；若模式快照缺失，则补齐后重新调谐；若模式与快照不同或备份变为不可用，则失败并要求重新创建/重置演练。
- 组 Drill 必须在 Pending 阶段验证所有成员；任一成员无有效模式时，整个 Drill 不得进入 `Ready`。
- 混合组父状态为 `Mixed`，但每个子 Operation 只能收到 `FullRestore` 或 `ResourceOnly`，不得共享或原地修改同一个 `DrillConfig` 指针。

## 影响范围

- `pkg/apis/disaster/v1/disasterdrill_types.go`
- `pkg/apis/disaster/v1/disasteroperation_types.go`
- `internal/controller/disasterdrill/`
- `internal/controller/disasteroperation/`
- `config/crd/bases/testudo.softcdata.com_disasterdrills.yaml`
- `config/crd/bases/testudo.softcdata.com_disasteroperations.yaml`
- Drill Controller 读取 DataSync/ResourceSync 所需 RBAC 产物
- 相关 generated deepcopy/client 产物和单元测试

## 风险

- 这是 CRD/API 枚举与字段扩展，部署 Operator 前必须先升级 CRD；旧 CRD 会裁剪新字段或拒绝新枚举值。
- server OpenAPI 当前把 restoreMode 枚举限定为 `Reuse/FullRestore`，Operator 变更合入后需要同步增加 `ResourceOnly/Mixed`，否则文档与真实响应不一致。
- 过度宽松的 no-data 判断会造成数据遗漏，因此本提案采用三重证据并失败关闭；不兼容手工伪造、部分状态写入或历史状态不一致的 DataSync。
