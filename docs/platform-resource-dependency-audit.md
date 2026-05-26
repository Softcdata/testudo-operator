# 平台资源依赖与标签查询盘点

## 0. 目标与范围

本盘点用于在继续推进 `unify-deletion-protection` 前，先明确当前平台的“资源依赖关系”和“标签查询现状”，并按模块逐个梳理。

范围：
- `disaster-operator`（控制器侧真实依赖判定）
- `disaster-server`（API 删除入口与查询入口）

规则：
- 先还原“当前真实行为”（as-is），不做理想化推断。
- 每个模块都拆成：
  - 依赖定义
  - 标签写入点
  - 标签查询点
  - 非标签查询点（Spec/Status/全量遍历）
  - 风险与缺口

说明：
- 按当前讨论，`DisasterJob` 不单列为独立盘点模块，相关依赖仅作为其他模块（如 Policy）的子规则出现。

---

## 1. 模块顺序

- [x] M01 Cluster
- [x] M02 StorageRepository
- [x] M03 DisasterPolicy
- [x] M04 DisasterConfig
- [x] M05 DisasterInstance
- [x] M06 AppBackup
- [x] M07 AppRestore
- [x] M08 DisasterGroup

---

## 2. M01 Cluster（已完成）

### 2.1 依赖定义（谁会阻塞 Cluster 删除）

当前 Operator 侧 `Cluster` 删除保护判定如下：

1. 被 `AppBackup` 引用则阻塞。
- 查询条件：`LabelAppBackupCluster = cluster.Name`
- 代码：`cluster_controller.go` `checkDependencies`

2. 被 `AppRestore` 引用则阻塞。
- 查询条件：`LabelAppRestoreCluster = cluster.Name`
- 代码：`cluster_controller.go` `checkDependencies`

3. 被 `DisasterConfig` 引用则阻塞。
- 查询条件：`DisasterConfig.Spec.SourceCluster == cluster.Name` 或 `TargetCluster == cluster.Name`
- 代码：`cluster_controller.go` `checkDependencies`（全量 List DisasterConfig 后内存过滤）

### 2.2 标签写入点（供 Cluster 依赖查询使用）

1. `AppBackup` 标签写入：
- `testudo.softcdata.com/app-backup-cluster = ab.Spec.Cluster`
- 代码：`internal/controller/appbackup/appbackup_controller.go` `syncLabels`

2. `AppRestore` 标签写入：
- `testudo.softcdata.com/app-restore-cluster = appRestore.Spec.Cluster`
- 代码：`internal/controller/apprestore/apprestore_controller.go` `syncLabels`

3. `DisasterConfig -> Cluster` 当前无专用反向标签。
- 依赖查询完全基于 `Spec.SourceCluster/Spec.TargetCluster`。

### 2.3 标签查询点

1. Operator 删除保护：
- `AppBackupList` by `LabelAppBackupCluster`
- `AppRestoreList` by `LabelAppRestoreCluster`
- 代码：`internal/controller/cluster_controller.go`

2. Server 当前删除入口：
- `DELETE /clusters/:name` 不做依赖预检查，直接下发 K8s Delete。
- 代码：`disaster-server/internal/apis/disaster_cluster/v1/handler.go` `deleteCluster`

### 2.4 非标签查询点

1. `DisasterConfig` 对 `Cluster` 的引用：
- 全量 `List DisasterConfig`，按 Spec 字段过滤。
- 查询字段：`spec.sourceCluster` / `spec.targetCluster`

### 2.5 风险与缺口

1. 查询模式混合（Label + 全量 Spec 扫描），行为不统一。
2. `DisasterConfig` 依赖未标签化，规模大时会有全量扫描成本。
3. Server 删除入口无预检查，用户会先收到成功响应，真实是否可删由 Operator Finalizer 阶段决定。
4. 若标签缺失/未同步（AppBackup/AppRestore），会导致 Cluster 依赖漏判风险。

### 2.6 本模块建议（仅记录，不改代码）

1. 明确 `Cluster` 依赖来源优先级：Label 优先 + Spec 兜底，或统一改为图查询。
2. 为 `DisasterConfig` 增加可索引反向关系（标签或投影边），减少全量扫描。
3. 在 Server 侧补删除前检查接口接入前，可先加轻量预检查并返回结构化阻塞信息。

---

## 3. M02 StorageRepository（已完成）

### 3.1 依赖定义（谁会阻塞 StorageRepository 删除）

当前 Operator 侧仅实现了 1 条阻塞规则：

1. 被 `DisasterPolicy` 引用则阻塞。
- 查询条件：`LabelStorageRepositoryName = sr.Name`
- 代码：`internal/controller/storagerepository_controller.go` `handleDelete`

### 3.2 标签写入点（供 StorageRepository 依赖查询使用）

当前在非测试代码中，未发现 `LabelStorageRepositoryName` 的稳定写入路径。

结论：
- 现有删除保护在查这个标签。
- 但生产路径缺少对应标签写入，存在“查询有、写入无”的风险。

### 3.3 标签查询点

1. Operator 删除保护：
- `DisasterPolicyList` by `LabelStorageRepositoryName`
- 代码：`storagerepository_controller.go`

2. Server 当前删除入口：
- `DELETE /storages/:name` 不做依赖预检查，直接下发 Delete。
- 代码：`disaster-server/internal/apis/disaster_storage/v1/handler.go` `deleteStorage`

### 3.4 非标签查询点

当前与 StorageRepository 强相关但未纳入删除保护查询的关系：

1. `DisasterConfig.Spec.StorageRepository`
- 代码：`disasterconfig_controller.go` 中会读取该字段并 Get StorageRepository。

2. `AppRestore.Spec.StorageRepository`
- 代码：`apprestore_state.go` 跨集群恢复时读取并校验。

### 3.5 风险与缺口

1. `StorageRepository <- DisasterPolicy` 规则依赖标签，但标签可能未被实际写入，导致阻塞失效。
2. `StorageRepository <- DisasterConfig` 在当前删除保护中未覆盖（与提案目标不一致）。
3. Server 删除入口无预检查，仍是“先删后由 Operator 决定是否最终删除”。

### 3.6 本模块建议（仅记录，不改代码）

1. 先修复 `LabelStorageRepositoryName` 的生产写入路径，保证现有规则有效。
2. 补充 `StorageRepository <- DisasterConfig` 阻塞规则（可先 Spec 查询，后续再统一图化）。
3. 增加最小回归用例：有 Config 引用时删除 Storage 必须阻塞。

---

## 4. M03 DisasterPolicy（已完成）

### 4.1 依赖定义（谁会阻塞 DisasterPolicy 删除）

当前 Operator 侧 `DisasterPolicy` 删除保护规则：

1. 被 `AppBackup` 引用则阻塞。
- 查询条件：`LabelDisasterPolicyName = policy.Name`
- 代码：`disasterpolicy_controller.go` `handleDelete`

2. 被运行中的 `DisasterJob` 引用则阻塞。
- 查询条件：`LabelDisasterPolicyName = policy.Name`
- 状态条件：`job.Status.Phase in {Backuping, Restoring}`
- 代码：`disasterpolicy_controller.go` `handleDelete`

### 4.2 标签写入点（供 DisasterPolicy 依赖查询使用）

1. `AppBackup` 写入 `LabelDisasterPolicyName`：
- `ab.Labels[LabelDisasterPolicyName] = ab.Spec.DisasterPolicy`
- 代码：`appbackup_controller.go` `syncLabels`

2. `AppBackup` 还会写入 `LabelDisasterPolicyUID`（另一套关联键）：
- 代码：`appbackup_ready.go`

3. `DisasterPolicy` 自身也写 `LabelDisasterPolicyName`，但这不是“被谁引用”的反向索引。
- 代码：`disasterpolicy_controller.go` `syncLabels`

### 4.3 标签查询点

1. Operator 删除保护：按 `LabelDisasterPolicyName` 查询 `AppBackup/DisasterJob`。
2. Server 更新保护（update）：按 `LabelDisasterPolicyUID` 查询 `AppBackup`。
3. Server 删除入口（delete）：不做依赖预检查，直接 Delete。

### 4.4 非标签查询点

1. `DisasterJob` 阻塞还有运行态条件（`Backuping/Restoring`），属于 Status 判定。

### 4.5 风险与缺口

1. 同一资源依赖键不统一：Operator 删除用 `policy-name`，Server 更新用 `policy-uid`。
2. `DisasterJob` 是否稳定写入 `LabelDisasterPolicyName` 需进一步确认；若缺失会导致规则失效。
3. Server 删除没有预检查，可能出现“API 成功返回，但最终删除被阻塞”的体验割裂。

### 4.6 本模块建议（仅记录，不改代码）

1. 统一主关联键（建议 UID 主键 + Name 兜底）。
2. 补齐 `DisasterJob` 标签写入契约及测试。
3. Server 的 update/delete 口径统一到同一依赖判断出口。

---

## 5. M04 DisasterConfig（已完成）

### 5.1 依赖定义（谁会阻塞 DisasterConfig 删除）

当前 Operator 侧 `DisasterConfig` 删除保护规则：

1. 被 `DisasterInstance` 引用则阻塞。
- 查询条件：`inst.Spec.Config == dc.Name`
- 代码：`disasterconfig_controller.go` `checkReferences`

### 5.2 标签写入点（供 DisasterConfig 依赖查询使用）

当前删除保护未使用标签索引。
- 判定完全基于 `DisasterInstance.Spec.Config`。

### 5.3 标签查询点

当前该模块的删除保护无标签查询。

### 5.4 非标签查询点

1. 全量 `List DisasterInstance` 后，按 `Spec.Config` 过滤。
2. 另外 `DisasterConfig` 本身还引用：
- `Spec.SourceCluster`
- `Spec.TargetCluster`
- `Spec.StorageRepository`
- `Spec.DataSyncPolicy`
- `Spec.ResourceSyncPolicy`

### 5.5 风险与缺口

1. 依赖查询为全量扫描，规模上来后成本高。
2. Server 删除入口无预检查，仍依赖 Operator 阶段阻塞。
3. 只比较 `Spec.Config` 名称，当前因 Config 为 cluster-scope 通常可行，但没有显式 UID 级校验。

### 5.6 本模块建议（仅记录，不改代码）

1. 增加 `DisasterInstance -> DisasterConfig` 反向索引（标签或图边）。
2. 后续统一到同一个检查服务，避免 Server/Operator 两套行为。

---

## 6. M05 DisasterInstance（已完成）

### 6.1 依赖定义（谁会阻塞 DisasterInstance 删除）

当前 Operator 侧 `DisasterInstance` 删除保护规则：

1. 运行态阻塞（未强制删除时）：
- `FsmState in {Protected, Active, FailingOver, FailingBack}` 阻塞。
- 代码：`disasterinstance/controller.go` `handleDeletion`

2. 被 `DisasterGroup` 引用阻塞（未强制删除时）：
- 查询 `DisasterGroup.Spec.Levels` 是否包含该实例名。
- 代码：`disasterinstance/controller.go` `handleDeletion`

3. 强制删除注解可绕过：
- `testudo.softcdata.com/force-delete=true`

### 6.2 标签写入点（供 DisasterInstance 依赖查询使用）

1. Server 创建实例时写入：
- `testudo.softcdata.com/config = req.Config`
- 代码：`disaster-server/internal/apis/disaster_instance/v1/handler.go`

说明：
- 当前 Operator 删除保护并不读取该标签，而是读 `Spec/Status`。

### 6.3 标签查询点

当前 `DisasterInstance` 删除保护主路径不依赖标签查询。

### 6.4 非标签查询点

1. 状态机字段：`Status.FsmState`。
2. 组关系字段：`DisasterGroup.Spec.Levels`。

### 6.5 风险与缺口

1. 规则强依赖运行态，若状态更新异常可能误阻塞或误放行。
2. 与 `unify-deletion-protection` 提案里“Group 不纳入阻塞规则”的口径可能存在偏差，需决策统一。
3. Server 删除入口无预检查，用户体验仍可能与最终结果不一致。

### 6.6 本模块建议（仅记录，不改代码）

1. 明确 Group 是否属于 Instance 删除阻塞的正式规则（并在文档、代码、验收统一）。
2. 对 `force-delete` 建立明确审计与告警约束。

---

## 7. M06 AppBackup（已完成）

### 7.1 依赖定义（谁会阻塞 AppBackup 删除）

当前 Operator 侧 `AppBackup` 删除阶段是“清理型 Finalizer”：

1. 不存在“被其他业务资源引用则阻塞删除”的显式规则。
- 代码：`internal/controller/appbackup/appbackup_state.go` `DeletingHandler.Handle`

2. 存在 Finalizer 清理外部资源逻辑：
- 删除目标集群中的 Velero `Schedule` / `Backup` 及其关联删除请求。
- 完成清理后移除 Finalizer。
- 代码：`appbackup_state.go`、`appbackup_controller.go` `deleteExternalResources`

### 7.2 标签写入点（AppBackup 自身及反向查询相关）

1. `AppBackup` 主标签写入：
- `LabelAppBackupName = ab.Name`
- `LabelAppBackupCluster = ab.Spec.Cluster`
- `LabelAppBackupType = Schedule|Manual`
- `LabelAppBackupStatus = LatestBackupStatus/Status`
- `LabelAppBackupIncludeNamespace = includedNamespaces`
- 代码：`internal/controller/appbackup/appbackup_controller.go` `syncLabels`

2. 与策略关联标签写入：
- `LabelDisasterPolicyName = ab.Spec.DisasterPolicy`（在 `syncLabels`）
- `LabelDisasterPolicyUID = policy.UID`（在 `ReadyHandler` 处理策略时）
- 代码：`appbackup_controller.go` `syncLabels`、`appbackup_ready.go`

3. Velero 资源关联标签写入：
- 创建 Velero `Backup` / `Schedule` 时写入 `LabelAppBackupUID` 等标签用于反向清理与状态对齐。
- 代码：`appbackup_controller.go` `CreateVeleroBackup` / `CreateVeleroSchedule`

### 7.3 标签查询点

1. Cluster 删除保护会查询 `AppBackup`：
- 查询条件：`LabelAppBackupCluster = cluster.Name`
- 代码：`internal/controller/cluster_controller.go` `checkDependencies`

2. DisasterPolicy 删除保护会查询 `AppBackup`：
- 查询条件：`LabelDisasterPolicyName = policy.Name`
- 代码：`internal/controller/disasterpolicy_controller.go` `handleDelete`

3. Server 列表查询：
- `GET /appbackups` 先按 label selector 粗筛，再在内存做模糊过滤。
- 代码：`disaster-server/internal/apis/app_backup/v1/handler.go` `appBackups`

4. Server 删除入口：
- `DELETE /appbackups/:name` 不做依赖预检查，直接 Delete。
- 代码：`app_backup/v1/handler.go` `deleteAppBackup`

### 7.4 非标签查询点

1. `AppRestore` 创建时会直接读取 `AppBackup`（按名称）：
- 使用 `AppBackup.Spec.Cluster`、`Spec.Template.StorageLocation`、`Status.History` 进行参数补全。
- 代码：`disaster-server/internal/apis/app_restore/v1/handler.go` `createAppRestore`

2. `AppRestore` 运行时会读取 `AppBackup` 获取源类型等信息：
- 代码：`internal/controller/apprestore/apprestore_state.go` `PendingHandler.Handle`

### 7.5 风险与缺口

1. 当前无 `AppRestore <- AppBackup` 删除阻塞规则，删除源 `AppBackup` 后，后续恢复链路可能出现语义不一致。
2. `policy-name` 与 `policy-uid` 两套键并行，易造成跨入口判定不一致。
3. Server 删除入口无预检查，删除是否最终成功仍由 Operator 异步阶段决定。

### 7.6 本模块建议（仅记录，不改代码）

1. 明确 `AppBackup` 是否需要“被 AppRestore 引用时阻塞删除”的正式规则。
2. 统一策略关联主键口径（建议 UID 主键、Name 兜底）。
3. 删除前可补最小预检查：返回“外部清理中/仍有引用”的结构化信息。

---

## 8. M07 AppRestore（已完成）

### 8.1 依赖定义（谁会阻塞 AppRestore 删除）

当前 Operator 侧 `AppRestore` 删除阶段同样为“清理型 Finalizer”：

1. 无“被其他 CR 引用时阻塞删除”的显式规则。
- 代码：`internal/controller/apprestore/apprestore_state.go` `DeletingHandler.Handle`

2. 删除时会清理外部资源后移除 Finalizer：
- 删除 Velero `Restore`（按 `LabelAppRestoreUID`）
- 删除 ResourceModifier ConfigMap（按 `apprestore.testudo.softcdata.com/uid`）
- 代码：`apprestore_controller.go` `deleteExternalResources`、`configmap_manager.go` `DeleteConfigMap`

### 8.2 标签写入点（AppRestore 自身及外部资源关联）

1. `AppRestore` 主标签写入：
- `LabelAppRestoreName`
- `LabelAppRestoreCluster`
- `LabelAppRestoreSource`
- `LabelAppRestoreStatus`
- `LabelAppRestoreSourceType`（从 AppBackup 传播）
- 代码：`internal/controller/apprestore/apprestore_controller.go` `syncLabels`

2. Velero `Restore` 关联标签写入：
- `LabelAppRestoreName`
- `LabelAppRestoreUID`
- 代码：`apprestore_controller.go` `createVeleroRestore`

3. ResourceModifier ConfigMap 关联标签写入：
- `apprestore.testudo.softcdata.com/uid = appRestore.UID`
- 代码：`configmap_manager.go` `EnsureConfigMap`

### 8.3 标签查询点

1. Cluster 删除保护会查询 `AppRestore`：
- 查询条件：`LabelAppRestoreCluster = cluster.Name`
- 代码：`internal/controller/cluster_controller.go` `checkDependencies`

2. AppRestore 删除清理查询：
- `DeleteAllOf Velero Restore` by `LabelAppRestoreUID`
- `DeleteAllOf ConfigMap` by `apprestore.testudo.softcdata.com/uid`
- 代码：`apprestore_controller.go`、`configmap_manager.go`

3. Server 列表查询：
- `GET /apprestores` 使用 label selector + 内存模糊过滤。
- 代码：`disaster-server/internal/apis/app_restore/v1/handler.go` `appRestores`

4. Server 删除入口：
- `DELETE /apprestores/:name` 不做依赖预检查，直接 Delete。
- 代码：`app_restore/v1/handler.go` `deleteAppRestore`

### 8.4 非标签查询点

1. 创建恢复前会按名称读取 `AppBackup`，并回填 `sourceCluster/storageRepository/backupName`。
- 代码：`app_restore/v1/handler.go` `createAppRestore`

2. Pending 阶段会读取 Velero Backup 信息，并按 Spec/Status 计算 `TargetNamespaces`。
- 代码：`internal/controller/apprestore/apprestore_state.go` `PendingHandler.Handle`

### 8.5 风险与缺口

1. 删除路径没有“执行中保护”预检查（例如恢复进行中时是否允许删除）的一致口径。
2. 关键引用关系仍以名称为主（如 `Spec.BackupSource`），缺少 UID 级稳态关联。
3. Server 删除入口无预检查，前后端感知与最终控制器行为可能不一致。

### 8.6 本模块建议（仅记录，不改代码）

1. 明确 AppRestore 删除策略：执行中可删/不可删/需强制开关。
2. 将 `BackupSource` 关联补齐 UID 侧引用或写入稳定反向索引。
3. 补最小回归：恢复进行中删除、删除后外部资源清理完成性。

---

## 9. M08 DisasterGroup（已完成）

### 9.1 依赖定义（谁会阻塞 DisasterGroup 删除）

当前 `DisasterGroup` 删除路径没有实现阻塞检查：

1. Server 删除接口直接 Delete。
- 代码：`disaster-server/internal/apis/disaster_group/v1/handler.go` `deleteGroup`

2. Operator 侧 `DisasterGroup` 控制器仅负责聚合状态，不包含 Finalizer 与删除保护流程。
- 代码：`internal/controller/disastergroup/controller.go`

但反向关系存在：`DisasterGroup` 会阻塞 `DisasterInstance` 删除（已在 M05 记录）。

### 9.2 标签写入点（供 Group 相关查询使用）

1. `DisasterGroup` 本身无统一标签写入契约用于依赖查询。

2. 组操作 `DisasterOperation` 写入组关联标签：
- `testudo.softcdata.com/group = groupName`
- `testudo.softcdata.com/operation = opType`
- 代码：`disaster-server/internal/apis/disaster_group/v1/handler.go` `executeAction`

### 9.3 标签查询点

1. 组历史查询：
- 通过 `LabelSelector: testudo.softcdata.com/group=<name>` 查询 `DisasterOperation`。
- 代码：`disaster_group/v1/handler.go` `getHistory`

2. 组操作 Watch：
- 支持按 `testudo.softcdata.com/group=<name>` 过滤。
- 代码：`disaster_group/v1/handler.go` `watchGroupOperations` / `watchGroupOperation`

### 9.4 非标签查询点

1. `DisasterInstance` 删除保护会全量列举组并扫描 `Spec.Levels`：
- 查询 `group.Spec.Levels` 是否包含实例名。
- 代码：`internal/controller/disasterinstance/controller.go` `handleDeletion`

2. `DisasterGroup` 状态聚合：
- 逐个读取 `Spec.Levels` 中实例并根据 `instance.Status.FsmState` 计算状态。
- 代码：`internal/controller/disastergroup/controller.go` `Reconcile`

3. Server 列表/详情：
- 全量读取组、实例、配置后在内存聚合与筛选（非标签索引）。
- 代码：`disaster_group/v1/handler.go` `listGroups` / `getGroup`

### 9.5 风险与缺口

1. `DisasterGroup` 删除无预检查，可能在组操作进行中被直接删除，留下“操作对象缺失”的一致性风险。
2. `Instance <- Group` 关系通过全量扫描 `Spec.Levels` 判定，规模增大时成本明显。
3. 组成员关系以实例名为键，缺少 UID 稳态关联。

### 9.6 本模块建议（仅记录，不改代码）

1. 为 `DisasterGroup` 补充最小删除门禁（例如：存在运行中组操作时阻塞）。
2. 为 `Instance <- Group` 增加可索引反向关系（标签或投影边），减少全量扫描。
3. 明确组成员关系键策略（Name/UID 双写或 UID 主键）。

---

## 10. 平台依赖与查询统一矩阵（v0）

| 资源 | 删除阻塞来源（as-is） | 主要查询方式 | 关键字段/标签 | 写入点 | 查询点 | 主要风险 |
|---|---|---|---|---|---|---|
| Cluster | AppBackup/AppRestore/DisasterConfig | Label + 全量 Spec 扫描 | `LabelAppBackupCluster` `LabelAppRestoreCluster` `spec.sourceCluster/targetCluster` | AppBackup/AppRestore `syncLabels` | `cluster_controller.go` | 模式混合、Config 全量扫描 |
| StorageRepository | DisasterPolicy | Label | `LabelStorageRepositoryName` | 生产写入路径不明确 | `storagerepository_controller.go` | 查有写无导致漏判 |
| DisasterPolicy | AppBackup + 运行中 DisasterJob | Label + Status | `LabelDisasterPolicyName` / `LabelDisasterPolicyUID` + `job.Status.Phase` | AppBackup `syncLabels`/`ReadyHandler` | `disasterpolicy_controller.go` + server update | Name/UID 键不一致 |
| DisasterConfig | DisasterInstance | 全量 Spec 扫描 | `inst.Spec.Config` | 无专用索引 | `disasterconfig_controller.go` | 全量扫描成本高 |
| DisasterInstance | 运行态 + DisasterGroup | Status + 全量 Spec 扫描 | `Status.FsmState` `group.Spec.Levels` | Server 创建写 `testudo.softcdata.com/config`（当前删除不读） | `disasterinstance/controller.go` | 状态异常或扫描成本导致误判风险 |
| AppBackup | 当前无“被引用阻塞”规则（仅外部清理 Finalizer） | 删除清理按标签；列表按标签 | `LabelAppBackupUID` `LabelAppBackupCluster` `LabelDisasterPolicyName/UID` | `appbackup_controller.go` `appbackup_ready.go` | `appbackup_state.go`、cluster/policy controller、server list | 缺少 `AppRestore <- AppBackup` 删除门禁 |
| AppRestore | 当前无“被引用阻塞”规则（仅外部清理 Finalizer） | 删除清理按标签；列表按标签 | `LabelAppRestoreUID` `LabelAppRestoreCluster` `LabelAppRestoreSource` | `apprestore_controller.go` `configmap_manager.go` | `apprestore_state.go`、cluster controller、server list | 名称引用为主，删除策略口径不清 |
| DisasterGroup | 当前无删除阻塞规则 | 依赖多为 Spec 扫描；操作历史用标签 | `group.Spec.Levels` `testudo.softcdata.com/group` | group handler 执行操作时写入 operation 标签 | `disasterinstance/controller.go`、group handler | 组删除无门禁、成员关系仅 Name |

---

## 11. 下一步（建议）

1. 基于上述矩阵先做“规则确认会”：
- 逐行确认哪些删除阻塞规则是“必须保留”，哪些要调整（尤其 `Instance <- Group`、`AppRestore <- AppBackup`）。

2. 确认统一关联键策略：
- 每条依赖关系明确主键（建议 UID）与兜底键（Name），并补齐写入契约。

3. 确认 Server 预检查最小闭环：
- 先在删除入口做轻量预检查 + 结构化阻塞信息，减少“API 成功但最终删不掉”的体验割裂。

---

## 12. 规则确认清单（代码扫描定稿）

以下规则为基于当前代码的 as-is 事实，用于后续重构对齐。  
记号说明：`BLOCK` 表示阻塞删除并保留 Finalizer；`ALLOW` 表示允许进入最终删除（移除 Finalizer 或直接 Delete）。

### 12.1 删除阻塞规则（Operator 主路径）

1. `R-CL-DEL-01` `Cluster` 删除：若存在 `AppBackup` 满足 `LabelAppBackupCluster = cluster.Name`，则 `BLOCK`。
- 代码：`internal/controller/cluster_controller.go` `checkDependencies`

2. `R-CL-DEL-02` `Cluster` 删除：若存在 `AppRestore` 满足 `LabelAppRestoreCluster = cluster.Name`，则 `BLOCK`。
- 代码：`cluster_controller.go` `checkDependencies`

3. `R-CL-DEL-03` `Cluster` 删除：若存在 `DisasterConfig` 满足 `Spec.SourceCluster==cluster.Name || Spec.TargetCluster==cluster.Name`，则 `BLOCK`。
- 代码：`cluster_controller.go` `checkDependencies`

4. `R-SR-DEL-01` `StorageRepository` 删除：若存在 `DisasterPolicy` 满足 `LabelStorageRepositoryName = sr.Name`，则 `BLOCK`。
- 代码：`internal/controller/storagerepository_controller.go` `handleDelete`

5. `R-DP-DEL-01` `DisasterPolicy` 删除：若存在 `AppBackup` 满足 `LabelDisasterPolicyName = policy.Name`，则 `BLOCK`。
- 代码：`internal/controller/disasterpolicy_controller.go` `handleDelete`

6. `R-DP-DEL-02` `DisasterPolicy` 删除：若存在 `DisasterJob` 满足 `LabelDisasterPolicyName = policy.Name` 且 `job.Status.Phase in {Backuping, Restoring}`，则 `BLOCK`。
- 代码：`disasterpolicy_controller.go` `handleDelete`

7. `R-DC-DEL-01` `DisasterConfig` 删除：若存在 `DisasterInstance` 满足 `inst.Spec.Config = dc.Name`，则 `BLOCK`。
- 代码：`internal/controller/disasterconfig_controller.go` `checkReferences` + `handleDelete`

8. `R-DI-DEL-01` `DisasterInstance` 删除：若未设置 `testudo.softcdata.com/force-delete=true` 且 `FsmState in {Protected, Active, FailingOver, FailingBack}`，则 `BLOCK`。
- 代码：`internal/controller/disasterinstance/controller.go` `handleDeletion`

9. `R-DI-DEL-02` `DisasterInstance` 删除：若未设置 `force-delete` 且任一 `DisasterGroup.Spec.Levels` 包含该实例名，则 `BLOCK`。
- 代码：`disasterinstance/controller.go` `handleDeletion`

10. `R-DI-DEL-03` `DisasterInstance` 删除：若 `testudo.softcdata.com/force-delete=true`，可绕过 `R-DI-DEL-01/02`，进入 `ALLOW`。
- 代码：`disasterinstance/controller.go` `handleDeletion`

### 12.2 清理型删除规则（非引用阻塞）

1. `R-AB-DEL-01` `AppBackup` 删除：无“被其他 CR 引用即阻塞”的规则；存在 Finalizer 时执行外部清理（Velero Schedule/Backup）后 `ALLOW`。
- 代码：`internal/controller/appbackup/appbackup_state.go` `DeletingHandler.Handle`
- 清理实现：`appbackup_controller.go` `deleteExternalResources`

2. `R-AR-DEL-01` `AppRestore` 删除：无“被其他 CR 引用即阻塞”的规则；存在 Finalizer 时执行外部清理（Velero Restore + ConfigMap）后 `ALLOW`。
- 代码：`internal/controller/apprestore/apprestore_state.go` `DeletingHandler.Handle`
- Finalizer 移除：`apprestore_controller.go` Reconcile 删除分支
- 清理实现：`apprestore_controller.go` `deleteExternalResources` + `configmap_manager.go` `DeleteConfigMap`

3. `R-DG-DEL-01` `DisasterGroup` 删除：当前 Operator 无 Finalizer/删除保护逻辑；由 Server 直接 Delete。
- 代码：`internal/controller/disastergroup/controller.go`、`disaster-server/internal/apis/disaster_group/v1/handler.go` `deleteGroup`

### 12.3 Server 删除入口规则（统一口径）

1. `R-SRV-DEL-01` 以下删除接口均未做依赖预检查，统一为“Best-effort 注解后直接 Delete”：
- `/clusters/:name`
- `/storages/:name`
- `/policies/:name`
- `/configs/:name`
- `/instances/:name`
- `/appbackups/:name`
- `/apprestores/:name`
- `/groups/:name`

结论：Server 侧普遍不承担阻塞判定，真实阻塞主要发生在 Operator Finalizer 阶段（若该资源实现了相关规则）。

### 12.4 规则一致性缺口（本轮确认）

1. `GAP-01` `DisasterPolicy` 关联键不一致：
- Operator 删除阻塞使用 `LabelDisasterPolicyName`
- Server 更新保护使用 `LabelDisasterPolicyUID`
- 风险：不同入口结论可能不一致。

2. `GAP-02` `StorageRepository` 删除阻塞使用 `LabelStorageRepositoryName`，但当前生产代码未确认到稳定写入点（查询有、写入弱/缺）。

3. `GAP-03` `AppBackup`、`AppRestore` 当前均为“清理型删除”，缺少上层引用阻塞契约（是否需要阻塞需产品/架构决策）。

---

## 13. 模块真实调用依赖矩阵（从 DisasterGroup 开始）

本节不是“当前删除保护规则矩阵”，而是 `DisasterGroup` 模块在代码里的真实调用关系（谁调用谁、读写什么、用什么键）。

### 13.1 DisasterGroup 出边矩阵（Group 主动调用谁）

| 编号 | 调用方 | 被调用资源/模块 | 调用动作 | 关键键/字段 | 用途 | 代码锚点 |
|---|---|---|---|---|---|---|
| DG-OUT-01 | server `GroupHandler` | `DisasterGroup` CR | `Create/Get/List/Update/Delete` | `metadata.name` | 组 CRUD | `disaster-server/internal/apis/disaster_group/v1/handler.go` `createGroup/listGroups/getGroup/updateGroup/deleteGroup` |
| DG-OUT-02 | server `GroupHandler` | `DisasterInstance` CR | `List/Get` | `group.spec.levels[*] -> instanceName` | 组详情、列表聚合、实例选择器 | `handler.go` `preloadInstances/collectInstanceSummaries/instancePicker/listGroupInstances` |
| DG-OUT-03 | server `GroupHandler` | `DisasterConfig` CR | `List/Get` | `instance.spec.config` | 推导 `storageRepository` 展示字段 | `handler.go` `preloadConfigs/collectInstanceSummaries` |
| DG-OUT-04 | server `GroupHandler` | `DisasterOperation` CR | `Create` | `spec.groupName` + labels `testudo.softcdata.com/group` | 发起组级操作编排 | `handler.go` `executeAction` |
| DG-OUT-05 | server `GroupHandler` | `DisasterOperation` CR | `List` | `LabelSelector: testudo.softcdata.com/group=<group>` | 查询组历史 | `handler.go` `getHistory` |
| DG-OUT-06 | server `GroupHandler` | `DisasterOperation` CR | `List+Watch` | `LabelSelector: testudo.softcdata.com/group=<group>` 或 `FieldSelector: metadata.name=<op>` | 组操作实时流 | `handler.go` `watchGroupOperations/watchGroupOperation` |
| DG-OUT-07 | server `GroupHandler` | `DisasterGroup` CR | `Get` | `operationName`（动态探测是否是组名） | 决定 watch 按组还是按单操作 | `handler.go` `watchGroupOperation` |
| DG-OUT-08 | operator `DisasterGroupReconciler` | `DisasterInstance` CR | `Get`（按 levels 遍历） | `group.spec.levels[*]` | 聚合 `readyInstances/totalInstances` | `disaster-operator/internal/controller/disastergroup/controller.go` `Reconcile` |

### 13.2 DisasterGroup 入边矩阵（谁在调用 Group）

| 编号 | 调用方模块 | 被调用方 | 调用动作 | 关键键/字段 | 业务含义 | 代码锚点 |
|---|---|---|---|---|---|---|
| DG-IN-01 | operator `DisasterInstanceReconciler` | `DisasterGroup` CR | `List` + 扫描 `spec.levels` | `instance.name` | 实例删除前检查是否被组引用 | `disaster-operator/internal/controller/disasterinstance/controller.go` `handleDeletion` |
| DG-IN-02 | operator `DisasterOperationReconciler` | `DisasterGroup` CR | `Get` + 读取 `spec.levels` | `operation.spec.groupName` | 组级操作按层编排执行 | `disaster-operator/internal/controller/disasteroperation/controller.go` `handleGroupOperation` |
| DG-IN-03 | operator `DisasterDrillReconciler` | `DisasterGroup` CR | `Get` + 遍历 `spec.levels` | `drill.spec.groupName` | 组演练前置校验 | `disaster-operator/internal/controller/disasterdrill/controller.go` `validateGroupDrill` |
| DG-IN-04 | server `disaster_drill` API | `DisasterGroup` CR | `Get`/`List` | `req.groupName` + `metadata.name` | 创建组演练时校验组存在（含全局兜底查询） | `disaster-server/internal/apis/disaster_drill/v1/handler.go` |

### 13.3 DisasterGroup 真实依赖结论（当前代码）

1. `DisasterGroup` 的直接下游资源是：`DisasterInstance`、`DisasterConfig`、`DisasterOperation`。
2. `DisasterGroup` 的主要上游调用方是：`DisasterInstance` 删除逻辑、`DisasterOperation` 编排逻辑、`DisasterDrill`（server+operator）校验逻辑。
3. 组操作相关唯一稳定标签键是：`testudo.softcdata.com/group`（用于历史查询与 watch 过滤）。
4. `DisasterGroup` 与 `AppBackup/AppRestore/StorageRepository` 没有直接调用边；若出现关联，通常通过 `Instance -> Config -> StorageRepository` 间接形成。

### 13.4 面向“统一检查入口”的最小职责（你定义的两件事）

统一检查入口 `DELETE precheck` 只做两件事：

1. `cleanup_resources`：
- 返回“删除该资源后，系统将主动清理的资源”。
- 来源只看该资源自己的删除流程（finalizer/cleanup handler）。

2. `referenced_by`：
- 返回“其他模块中引用该资源的对象（禁止删除依据）”。
- 只看跨模块引用，不看当前模块内部状态机自检逻辑。

说明：
- `can_delete` 不需要单独做复杂逻辑，可由 `referenced_by` 是否为空直接推导。
- 后续每个模块只要维护两套查询：`cleanup 查询` + `cross-module 引用查询`。

### 13.5 DisasterGroup 按统一检查入口的落地（当前代码）

1. `cleanup_resources`（即将清理）：
- 当前为 `[]`（空）。
- 依据：`DisasterGroup` 当前无 finalizer 和清理逻辑，Server 直接 Delete。

2. `referenced_by`（其他模块引用，禁止删除）：
- `DisasterOperation`：`spec.groupName = <groupName>`（建议阻塞状态：`Pending`、`Running`）。
- `DisasterDrill`：`spec.groupName = <groupName>`（建议阻塞状态：`Pending`、`Ready`、`Executing`、`CleaningUp`）。

3. 返回示例（建议）：
```json
{
  "target": {"kind": "DisasterGroup", "name": "group-a", "namespace": "disaster-system"},
  "cleanup_resources": [],
  "referenced_by": [
    {"kind": "DisasterOperation", "name": "failover-group-a-1741", "namespace": "disaster-system", "field": "spec.groupName", "state": "Running"},
    {"kind": "DisasterDrill", "name": "group-a-drill-001", "namespace": "disaster-system", "field": "spec.groupName", "state": "Executing"}
  ]
}
```

---

## 14. 模块真实调用依赖矩阵（DisasterInstance）

### 14.1 DisasterInstance 出边矩阵（Instance 主动调用谁）

| 编号 | 调用方 | 被调用资源/模块 | 调用动作 | 关键键/字段 | 用途 | 代码锚点 |
|---|---|---|---|---|---|---|
| DI-OUT-01 | operator `DisasterInstanceReconciler` | `DisasterConfig` CR | `Get` | `instance.spec.config` | 初始化主/备集群、策略来源 | `disaster-operator/internal/controller/disasterinstance/controller.go` `handlePending` |
| DI-OUT-02 | operator `DisasterInstanceReconciler` | `DataSync` CR | `CreateOrUpdate` + `SetControllerReference` | `dataSync.spec.instance=instance.name` | 创建实例数据同步子资源 | `controller.go` `ensureDataSync` |
| DI-OUT-03 | operator `DisasterInstanceReconciler` | `ResourceSync` CR | `CreateOrUpdate` + `SetControllerReference` | `resourceSync.spec.instance=instance.name` | 创建实例资源同步子资源 | `controller.go` `ensureResourceSync` |
| DI-OUT-04 | operator `DisasterInstanceReconciler` | `DisasterPolicy` CR | `Get` | `config.spec.dataSyncPolicy/resourceSyncPolicy` | 解析调度表达式并下发到 DS/RS | `controller.go` `resolveScheduleFromPolicy` |
| DI-OUT-05 | operator `DisasterInstanceReconciler` | `DisasterGroup` CR | `List` + 扫描 levels | `group.spec.levels[*]` | 删除前检查是否被组引用 | `controller.go` `handleDeletion` |
| DI-OUT-06 | server `InstanceHandler` | `DisasterOperation` CR | `Create` | `spec.instanceName` + label `testudo.softcdata.com/instance` | 实例动作触发（failover/reprotect 等） | `disaster-server/internal/apis/disaster_instance/v1/handler_action.go` `executeAction` |

### 14.2 DisasterInstance 入边矩阵（谁在调用 Instance）

| 编号 | 调用方模块 | 被调用方 | 调用动作 | 关键键/字段 | 业务含义 | 代码锚点 |
|---|---|---|---|---|---|---|
| DI-IN-01 | `disaster_group` 模块 | `DisasterInstance` CR | `List/Get` | `group.spec.levels[*] -> instanceName` | 组列表/详情/成员聚合 | `disaster-server/internal/apis/disaster_group/v1/handler.go` |
| DI-IN-02 | `disaster_operation` 模块 | `DisasterInstance` CR | `Get` | `operation.spec.instanceName` | 实例级操作执行与状态推进 | `disaster-operator/internal/controller/disasteroperation/controller.go` |
| DI-IN-03 | `disaster_drill` 模块 | `DisasterInstance` CR | `Get/List` | `drill.spec.instanceName` | 实例演练创建校验与执行校验 | `disaster-server/internal/apis/disaster_drill/v1/handler.go`、`disaster-operator/internal/controller/disasterdrill/controller.go` |
| DI-IN-04 | `datasync/resourcesync` 模块 | `DisasterInstance` CR | `Get` | `dataSync.spec.instance` / `resourceSync.spec.instance` | 子资源反查父实例 | `internal/controller/datasync/controller.go`、`internal/controller/resourcesync/controller.go` |

### 14.3 DisasterInstance 按统一检查入口的落地（当前代码）

1. `cleanup_resources`（即将清理）：
- `DataSync`（通常为 `dr-ds-<instance>`，并记录在 `instance.status.dataSyncName`）
- `ResourceSync`（通常为 `dr-rs-<instance>`，并记录在 `instance.status.resourceSyncName`）
- 说明：两者有 `OwnerReference -> DisasterInstance`，实例删除后由 K8s 级联清理，不应作为阻塞引用。

2. `referenced_by`（其他模块引用，禁止删除）：
- `DisasterGroup`：任一 `group.spec.levels` 包含该实例名。
- `DisasterOperation`：`spec.instanceName = <instanceName>` 且状态建议阻塞 `Pending/Running`。
- `DisasterDrill`：`spec.instanceName = <instanceName>` 且状态建议阻塞 `Pending/Ready/Executing/CleaningUp`。

3. 非阻塞引用（建议）：
- `DataSync/ResourceSync` 对 `instance` 的引用属于实例自有子资源链路，归入 `cleanup_resources`，不放入 `referenced_by`。

4. 返回示例（建议）：
```json
{
  "target": {"kind": "DisasterInstance", "name": "ins-a", "namespace": "disaster-system"},
  "cleanup_resources": [
    {"kind": "DataSync", "name": "dr-ds-ins-a", "namespace": "disaster-system", "via": "ownerReference"},
    {"kind": "ResourceSync", "name": "dr-rs-ins-a", "namespace": "disaster-system", "via": "ownerReference"}
  ],
  "referenced_by": [
    {"kind": "DisasterGroup", "name": "group-a", "namespace": "disaster-system", "field": "spec.levels"},
    {"kind": "DisasterOperation", "name": "failover-ins-a-1741", "namespace": "disaster-system", "field": "spec.instanceName", "state": "Running"},
    {"kind": "DisasterDrill", "name": "ins-a-drill-001", "namespace": "disaster-system", "field": "spec.instanceName", "state": "Executing"}
  ]
}
```

---

## 15. 模块真实调用依赖矩阵（DisasterConfig）

### 15.1 DisasterConfig 出边矩阵（Config 主动调用谁）

| 编号 | 调用方 | 被调用资源/模块 | 调用动作 | 关键键/字段 | 用途 | 代码锚点 |
|---|---|---|---|---|---|---|
| DC-OUT-01 | server `ConfigHandler` | `DisasterConfig` CR | `Create/Get/List/Update/Delete/Watch` | `metadata.name` | 配置 CRUD 与 watch | `disaster-server/internal/apis/disaster_config/v1/handler.go` |
| DC-OUT-02 | server `ConfigHandler` | `DisasterPolicy` CR | `Get` | `config.spec.dataSyncPolicy` / `config.spec.resourceSyncPolicy` | 展示层补全 cron（policy 缺失时回退默认值） | `handler.go` `populatePolicyCrons` |
| DC-OUT-03 | operator `DisasterConfigReconciler` | `Cluster` CR | `Get` | `config.spec.sourceCluster` / `config.spec.targetCluster` | 校验源/目标集群存在与 Ready 状态 | `disaster-operator/internal/controller/disasterconfig_controller.go` `Reconcile/getCluster` |
| DC-OUT-04 | operator `DisasterConfigReconciler` | `StorageRepository` CR | `Get` | `config.spec.storageRepository` | 校验仓库存在并用于 BSL 下发 | `disasterconfig_controller.go` `Reconcile` |
| DC-OUT-05 | operator `DisasterConfigReconciler` | 远端集群 Velero/BSL | `ApplyStorageRepository` | `bslName=<storage>-<sourceCluster>` + `prefix=sourceCluster` | 将同一仓库下发到主/备集群 | `disasterconfig_controller.go` `ApplyStorageRepository` |
| DC-OUT-06 | operator `DisasterConfigReconciler` | `DisasterInstance` CR | `List` + 内存过滤 | `instance.spec.config == config.name` | 删除前引用检查（阻塞删除） | `disasterconfig_controller.go` `checkReferences/handleDelete` |

### 15.2 DisasterConfig 入边矩阵（谁在调用 Config）

| 编号 | 调用方模块 | 被调用方 | 调用动作 | 关键键/字段 | 业务含义 | 代码锚点 |
|---|---|---|---|---|---|---|
| DC-IN-01 | `disaster_instance` operator | `DisasterConfig` CR | `Get` | `instance.spec.config` | 实例初始化时读取主/备集群与同步策略 | `internal/controller/disasterinstance/controller.go` `handlePending` |
| DC-IN-02 | `disaster_operation` operator | `DisasterConfig` CR | 多处 `Get` | `instance.spec.config` | Failover/Drill/Cancel/Undo 读取集群角色与仓库信息 | `internal/controller/disasteroperation/controller.go`、`controller_check.go` |
| DC-IN-03 | `datasync` operator | `DisasterConfig` CR | `Get` | `instance.spec.config` | 生成 `AppBackup/AppRestore` 时读取 source/target/storage | `internal/controller/datasync/controller.go` `executeSync` |
| DC-IN-04 | `resourcesync` operator | `DisasterConfig` CR | `Get` | `instance.spec.config` | 资源同步/恢复时读取 source/target/storage | `internal/controller/resourcesync/controller.go` `executeSync` |
| DC-IN-05 | `disaster_instance` server API | `DisasterConfig` CR | `Get` | `req.config` / `instance.spec.config` | 创建前校验、列表/详情 DTO 聚合 | `disaster-server/internal/apis/disaster_instance/v1/handler.go` |
| DC-IN-06 | `disaster_group` server API | `DisasterConfig` CR | `List/Get` | `instance.spec.config` | 组列表/详情聚合 `storageRepository` 展示字段 | `disaster-server/internal/apis/disaster_group/v1/handler.go` `preloadConfigs/collectInstanceSummaries` |
| DC-IN-07 | `cluster` operator | `DisasterConfig` CR | `List` + 过滤 | `config.spec.sourceCluster/targetCluster` | 集群删除保护（判断集群是否被配置引用） | `internal/controller/cluster_controller.go` `checkDependencies` |
| DC-IN-08 | `disaster_backup` operator | `DisasterConfig` CR | `Get` | `backup.spec.disasterConfig` | 读取源集群并驱动备份流程 | `internal/controller/disasterbackup_controller.go` |

### 15.3 DisasterConfig 按统一检查入口的落地（当前代码）

1. `cleanup_resources`（即将清理）：
- 当前为 `[]`（空）。
- 依据：`DisasterConfig` 删除流程只有“引用检查 + 移除 finalizer”，没有子资源清理动作。

2. `referenced_by`（其他模块引用，禁止删除）：
- 当前已落地阻塞：`DisasterInstance.spec.config = <configName>`（任意状态均应阻塞）。
- 真实调用存在但当前未落地阻塞：`DisasterBackup.spec.disasterConfig = <configName>`（建议至少在非终态 `Pending/Backuping/Restoring/Deleting` 阻塞）。

3. 非阻塞调用（不应直接进入 `referenced_by`）：
- `DisasterOperation` / `DataSync` / `ResourceSync` 对 Config 的读取属于 `Instance -> Config` 派生调用，主阻塞键仍是 `DisasterInstance.spec.config`。
- `disaster_instance`/`disaster_group` server 查询属于展示聚合读取，不构成持久引用关系。

4. 返回示例（建议）：
```json
{
  "target": {"kind": "DisasterConfig", "name": "cfg-a", "namespace": "disaster-system"},
  "cleanup_resources": [],
  "referenced_by": [
    {"kind": "DisasterInstance", "name": "ins-a", "namespace": "disaster-system", "field": "spec.config"},
    {"kind": "DisasterBackup", "name": "backup-a", "namespace": "disaster-system", "field": "spec.disasterConfig", "state": "Backuping"}
  ]
}
```

---

## 16. 模块真实调用依赖矩阵（Cluster）

### 16.1 Cluster 出边矩阵（Cluster 主动调用谁）

| 编号 | 调用方 | 被调用资源/模块 | 调用动作 | 关键键/字段 | 用途 | 代码锚点 |
|---|---|---|---|---|---|---|
| CL-OUT-01 | server `ClusterHandler` | `Cluster` CR | `Create/Get/List/Update/Delete/Watch` | `metadata.name` | 集群 CRUD 与 watch | `disaster-server/internal/apis/disaster_cluster/v1/handler.go` |
| CL-OUT-02 | operator `ClusterReconciler` | `AppBackup` CR | `List` | `label: testudo.softcdata.com/app-backup-cluster=<cluster>` | 集群删除依赖检查 | `internal/controller/cluster_controller.go` `checkDependencies` |
| CL-OUT-03 | operator `ClusterReconciler` | `AppRestore` CR | `List` | `label: testudo.softcdata.com/app-restore-cluster=<cluster>` | 集群删除依赖检查 | `cluster_controller.go` `checkDependencies` |
| CL-OUT-04 | operator `ClusterReconciler` | `DisasterConfig` CR | `List` + 内存过滤 | `spec.sourceCluster/spec.targetCluster` | 集群删除依赖检查 | `cluster_controller.go` `checkDependencies` |
| CL-OUT-05 | operator `ClusterReconciler` | `StorageRepository` + 远端 BSL | `Get` + `ApplyStorageRepository` | `annotation ensure-storage` / `storageName` | 存储连通性信号处理 | `cluster_controller.go` `Reconcile` |
| CL-OUT-06 | operator `ClusterReconciler` | 远端 Velero 组件 | `uninstallVelero` | `annotation uninstall-velero=true` | 删除阶段可选卸载 Velero | `cluster_controller.go` `handleDelete/uninstallVelero` |

### 16.2 Cluster 入边矩阵（谁在调用 Cluster）

| 编号 | 调用方模块 | 被调用方 | 调用动作 | 关键键/字段 | 业务含义 | 代码锚点 |
|---|---|---|---|---|---|---|
| CL-IN-01 | `disaster_config` operator | `Cluster` CR | `Get` | `config.spec.sourceCluster/targetCluster` | Config 就绪校验与 BSL 下发前置 | `internal/controller/disasterconfig_controller.go` |
| CL-IN-02 | `app_backup` server/API | `Cluster` CR | `Get`（校验） | `req.cluster` | 创建应用备份前校验集群 Ready | `internal/common/validator.go`、`internal/apis/app_backup/v1/handler.go` |
| CL-IN-03 | `app_restore` server/API | `Cluster` CR | `Get`（校验） | `req.cluster` | 创建应用恢复前校验目标集群 Ready | `internal/common/validator.go`、`internal/apis/app_restore/v1/handler.go` |
| CL-IN-04 | `DisasterConfig` CR | `Cluster` 语义引用 | Spec 字段引用 | `spec.sourceCluster/spec.targetCluster` | Config 对集群的长期拓扑绑定 | `pkg/apis/disaster/v1/disasterconfig_types.go` |
| CL-IN-05 | `AppBackup/AppRestore` CR | `Cluster` 语义引用 | Spec+Label 引用 | `appbackup.spec.cluster` / `apprestore.spec.cluster` | 备份/恢复执行集群绑定 | `appbackup_types.go`、`apprestore_types.go`、各自 `syncLabels` |
| CL-IN-06 | `disaster_backup` operator | `Cluster` CR | `Get`（经 Config 派生） | `backup.spec.disasterConfig -> config.spec.sourceCluster` | 灾备快照流程读取源集群连接信息 | `internal/controller/disasterbackup_controller.go` |

### 16.3 Cluster 按统一检查入口的落地（当前代码）

1. `cleanup_resources`（即将清理）：
- 条件型清理：当 `annotation(testudo.softcdata.com/uninstall-velero)=true` 时，会尝试卸载目标集群 Velero。
- 非条件型清理：无。

2. `referenced_by`（其他模块引用，禁止删除）：
- `AppBackup`：`spec.cluster=<clusterName>`（当前代码通过 `LabelAppBackupCluster` 查询）。
- `AppRestore`：`spec.cluster=<clusterName>`（当前代码通过 `LabelAppRestoreCluster` 查询）。
- `DisasterConfig`：`spec.sourceCluster/spec.targetCluster` 包含该集群。

3. 真实引用但当前未完整覆盖（建议）：
- `AppRestore.spec.sourceCluster` 也是对 Cluster 的真实引用（跨集群恢复链路），建议纳入统一入口。

4. 返回示例（建议）：
```json
{
  "target": {"kind": "Cluster", "name": "cluster-a", "namespace": ""},
  "cleanup_resources": [
    {"kind": "VeleroInstallation", "name": "velero", "namespace": "velero", "condition": "annotation:testudo.softcdata.com/uninstall-velero=true"}
  ],
  "referenced_by": [
    {"kind": "AppBackup", "name": "ab-a", "namespace": "disaster-system", "field": "spec.cluster"},
    {"kind": "AppRestore", "name": "ar-a", "namespace": "disaster-system", "field": "spec.cluster"},
    {"kind": "DisasterConfig", "name": "cfg-a", "namespace": "disaster-system", "field": "spec.sourceCluster"}
  ]
}
```

---

## 17. 模块真实调用依赖矩阵（StorageRepository）

### 17.1 StorageRepository 出边矩阵（Storage 主动调用谁）

| 编号 | 调用方 | 被调用资源/模块 | 调用动作 | 关键键/字段 | 用途 | 代码锚点 |
|---|---|---|---|---|---|---|
| SR-OUT-01 | server `StorageHandler` | `StorageRepository` CR | `Create/Get/List/Update/Patch/Delete/Watch` | `metadata.name` | 存储仓库 CRUD 与 watch | `disaster-server/internal/apis/disaster_storage/v1/handler.go` |
| SR-OUT-02 | server `StorageHandler` | 目标 `Cluster` + `BSLVerifier` | `GetKubeClient` + `VerifyBSL` | `clusterName/storageName` | 校验 BSL 连通性 | `handler.go` `validateBSLConnectivity` |
| SR-OUT-03 | operator `StorageRepositoryReconciler` | S3 API | `HeadBucket/CreateBucket` 等 | `endpoint/bucket/accessKey/secretKey` | 仓库可用性探测与状态更新 | `internal/controller/storagerepository_controller.go` `ValidateS3Configuration` |
| SR-OUT-04 | operator `StorageRepositoryReconciler` | `DisasterPolicy` CR | `List` | `label: testudo.softcdata.com/storage-repository-name=<sr>` | 删除阶段引用检查（当前实现） | `storagerepository_controller.go` `handleDelete` |

### 17.2 StorageRepository 入边矩阵（谁在调用 Storage）

| 编号 | 调用方模块 | 被调用方 | 调用动作 | 关键键/字段 | 业务含义 | 代码锚点 |
|---|---|---|---|---|---|---|
| SR-IN-01 | `disaster_config` operator | `StorageRepository` CR | `Get` | `config.spec.storageRepository` | Config 就绪与 BSL 下发依赖 | `internal/controller/disasterconfig_controller.go` |
| SR-IN-02 | `app_backup` operator | `StorageRepository` CR | `Get` | `appbackup.spec.template.storageLocation` | 创建/运行备份前校验仓库并构建 BSL 名称 | `internal/controller/appbackup/appbackup_pending.go`、`appbackup_ready.go` |
| SR-IN-03 | `app_restore` operator | `StorageRepository` CR | `Get` | `apprestore.spec.storageRepository` | 跨集群恢复前加载 BSL | `internal/controller/apprestore/apprestore_state.go` |
| SR-IN-04 | `app_backup` server/API | `StorageRepository` CR | `Get`（校验） | `req.storageLocation` | 创建 AppBackup 前校验仓库可用 | `internal/common/validator.go`、`internal/apis/app_backup/v1/handler.go` |
| SR-IN-05 | `DisasterConfig` CR | `StorageRepository` 语义引用 | Spec 字段引用 | `spec.storageRepository` | Config 与仓库的长期绑定 | `pkg/apis/disaster/v1/disasterconfig_types.go` |
| SR-IN-06 | `disaster_operation` operator | `StorageRepository` 间接引用 | 通过 Config 读取 | `instance.spec.config -> config.spec.storageRepository` | Drill 恢复时构造 AppRestoreSpec | `internal/controller/disasteroperation/controller.go` |

### 17.3 StorageRepository 按统一检查入口的落地（当前代码）

1. `cleanup_resources`（即将清理）：
- 当前为 `[]`（空），无子资源清理流程。

2. `referenced_by`（其他模块引用，禁止删除）：
- `DisasterConfig.spec.storageRepository=<srName>`。
- `AppBackup.spec.template.storageLocation=<srName>`。
- `AppRestore.spec.storageRepository=<srName>`（跨集群恢复链路）。

3. 当前实现与真实引用的差异：
- 当前 operator 删除阻塞只查 `DisasterPolicy` 标签（`LabelStorageRepositoryName`），但生产路径未确认稳定写入，且并未覆盖上述真实 Spec 引用。

4. 返回示例（建议）：
```json
{
  "target": {"kind": "StorageRepository", "name": "repo-a", "namespace": "disaster-system"},
  "cleanup_resources": [],
  "referenced_by": [
    {"kind": "DisasterConfig", "name": "cfg-a", "namespace": "disaster-system", "field": "spec.storageRepository"},
    {"kind": "AppBackup", "name": "ab-a", "namespace": "disaster-system", "field": "spec.template.storageLocation"},
    {"kind": "AppRestore", "name": "ar-a", "namespace": "disaster-system", "field": "spec.storageRepository"}
  ]
}
```

---

## 18. 模块真实调用依赖矩阵（DisasterPolicy）

### 18.1 DisasterPolicy 出边矩阵（Policy 主动调用谁）

| 编号 | 调用方 | 被调用资源/模块 | 调用动作 | 关键键/字段 | 用途 | 代码锚点 |
|---|---|---|---|---|---|---|
| DP-OUT-01 | server `PolicyHandler` | `DisasterPolicy` CR | `Create/Get/List/Update/Delete` | `metadata.name` | 策略 CRUD | `disaster-server/internal/apis/disaster_policy/v1/handler.go` |
| DP-OUT-02 | server `PolicyHandler` | `AppBackup` CR | `List` | `label: testudo.softcdata.com/disaster-policy-uid=<policyUID>` | 策略更新前引用检查 | `handler.go` `updatePolicy` |
| DP-OUT-03 | operator `DisasterPolicyReconciler` | `DisasterPolicy` 自身标签 | `syncLabels` | `type/name/state` | 建立策略检索标签 | `internal/controller/disasterpolicy_controller.go` |
| DP-OUT-04 | operator `DisasterPolicyReconciler` | `AppBackup` CR | `List` | `label: testudo.softcdata.com/disaster-policy-name=<policyName>` | 删除阻塞检查 | `disasterpolicy_controller.go` `handleDelete` |
| DP-OUT-05 | operator `DisasterPolicyReconciler` | `DisasterJob` CR | `List` + 状态过滤 | `label: testudo.softcdata.com/disaster-policy-name=<policyName>` + `phase` | 兼容 V1 运行任务阻塞删除 | `disasterpolicy_controller.go` `handleDelete` |

### 18.2 DisasterPolicy 入边矩阵（谁在调用 Policy）

| 编号 | 调用方模块 | 被调用方 | 调用动作 | 关键键/字段 | 业务含义 | 代码锚点 |
|---|---|---|---|---|---|---|
| DP-IN-01 | `app_backup` 模块 | `DisasterPolicy` 语义引用 | Spec + Label 引用 | `appbackup.spec.disasterPolicy` | 自动备份策略绑定 | `pkg/apis/disaster/v1/appbackup_types.go`、`appbackup_controller.go` `syncLabels` |
| DP-IN-02 | `disaster_instance` operator | `DisasterPolicy` CR | `Get` | `config.spec.dataSyncPolicy/resourceSyncPolicy` | DS/RS 调度表达式解析 | `internal/controller/disasterinstance/controller.go` `resolveScheduleFromPolicy` |
| DP-IN-03 | `disaster_config` server API | `DisasterPolicy` CR | `Get` | `config.spec.dataSyncPolicy/resourceSyncPolicy` | 配置详情补全 DataSync/ResourceSync cron 展示 | `internal/apis/disaster_config/v1/handler.go` `populatePolicyCrons` |
| DP-IN-04 | `DisasterConfig` CR | `DisasterPolicy` 语义引用 | Spec 字段引用 | `spec.dataSyncPolicy/spec.resourceSyncPolicy` | 配置与策略绑定（间接影响 Instance 调度） | `pkg/apis/disaster/v1/disasterconfig_types.go` |

### 18.3 DisasterPolicy 按统一检查入口的落地（当前代码）

1. `cleanup_resources`（即将清理）：
- 当前为 `[]`（空）。

2. `referenced_by`（其他模块引用，禁止删除）：
- `AppBackup.spec.disasterPolicy=<policyName>`（强阻塞）。
- `DisasterConfig.spec.dataSyncPolicy/spec.resourceSyncPolicy=<policyName>`（真实引用，建议纳入阻塞）。
- `DisasterJob` 运行态（V1 兼容链路，按既有规则保留）。

3. 返回示例（建议）：
```json
{
  "target": {"kind": "DisasterPolicy", "name": "policy-a", "namespace": "disaster-system"},
  "cleanup_resources": [],
  "referenced_by": [
    {"kind": "AppBackup", "name": "ab-a", "namespace": "disaster-system", "field": "spec.disasterPolicy"},
    {"kind": "DisasterConfig", "name": "cfg-a", "namespace": "disaster-system", "field": "spec.dataSyncPolicy"},
    {"kind": "DisasterJob", "name": "job-a", "namespace": "disaster-system", "field": "label.disaster-policy-name", "state": "Backuping"}
  ]
}
```

---

## 19. 模块真实调用依赖矩阵（AppBackup）

### 19.1 AppBackup 出边矩阵（AppBackup 主动调用谁）

| 编号 | 调用方 | 被调用资源/模块 | 调用动作 | 关键键/字段 | 用途 | 代码锚点 |
|---|---|---|---|---|---|---|
| AB-OUT-01 | server `AppBackupHandler` | `AppBackup` CR | `Create/Get/List/Update/Delete/Watch` | `metadata.name` | 应用备份 CRUD 与 watch | `disaster-server/internal/apis/app_backup/v1/handler.go` |
| AB-OUT-02 | server `AppBackupHandler` | `Cluster`/`StorageRepository` CR | `Get`（校验） | `req.cluster/req.storageLocation` | 创建前依赖校验 | `handler.go` + `internal/common/validator.go` |
| AB-OUT-03 | operator `AppBackupReconciler` | 目标集群 Velero 资源 | `Create/Update/List/Delete` | `LabelAppBackupUID` | 创建/维护 Velero Backup 与 Schedule | `internal/controller/appbackup/appbackup_controller.go`、`appbackup_ready.go` |
| AB-OUT-04 | operator `AppBackupReconciler` | `DisasterPolicy` CR | `Get` | `appbackup.spec.disasterPolicy` | 解析策略并下发 schedule | `appbackup_ready.go` |
| AB-OUT-05 | operator `AppBackupReconciler` | `StorageRepository` CR | `Get` + `ApplyStorageRepository` | `spec.template.storageLocation` | 确保备份存储 BSL 可用 | `appbackup_pending.go`、`appbackup_ready.go` |
| AB-OUT-06 | operator `AppBackupReconciler` | 目标集群 Velero 清理链路 | `DeleteAllOf Schedule` + `Create DeleteBackupRequest` | `label: app-backup-uid` | 删除时清理外部备份资源 | `appbackup_controller.go` `deleteExternalResources` |

### 19.2 AppBackup 入边矩阵（谁在调用 AppBackup）

| 编号 | 调用方模块 | 被调用方 | 调用动作 | 关键键/字段 | 业务含义 | 代码锚点 |
|---|---|---|---|---|---|---|
| AB-IN-01 | `cluster` operator | `AppBackup` CR | `List` | `label: app-backup-cluster=<cluster>` | Cluster 删除阻塞 | `internal/controller/cluster_controller.go` |
| AB-IN-02 | `disaster_policy` operator/server | `AppBackup` CR | `List` | `label: disaster-policy-name/uid` | Policy 删除阻塞与更新限制 | `disasterpolicy_controller.go`、`disaster_policy/v1/handler.go` |
| AB-IN-03 | `app_restore` server/API | `AppBackup` CR | `Get` | `apprestore.spec.backupSource` | 创建恢复时回填 sourceCluster/storageRepository | `internal/apis/app_restore/v1/handler.go` |
| AB-IN-04 | `datasync/resourcesync` operator | `AppBackup` CR | `Create/Get/Update` + `OwnerReference` | `dataSync/resourceSync` 生成固定名称 | 同步子流程备份任务 | `internal/controller/datasync/controller.go`、`resourcesync/controller.go` |

### 19.3 AppBackup 按统一检查入口的落地（当前代码）

1. `cleanup_resources`（即将清理）：
- 目标集群 `Velero Schedule`（按 `LabelAppBackupUID` 删除）。
- 目标集群 `Velero Backup` 及对象存储数据（通过 `DeleteBackupRequest` 触发级联）。

2. `referenced_by`（其他模块引用，禁止删除）：
- `AppRestore.spec.backupSource=<appBackupName>`，建议在恢复非终态时阻塞。
  - 建议阻塞状态：`Pending`、`Initiating`、`Restoring`。

3. 非阻塞引用（建议）：
- `DataSync/ResourceSync` 对 AppBackup 的 OwnerReference 子资源链路属于“可重建执行链”，不作为删除阻塞。

4. 返回示例（建议）：
```json
{
  "target": {"kind": "AppBackup", "name": "ab-a", "namespace": "disaster-system"},
  "cleanup_resources": [
    {"kind": "VeleroSchedule", "name": "sch-ab-a", "namespace": "velero", "via": "label:app-backup-uid"},
    {"kind": "VeleroBackup", "name": "backup-ab-a-*", "namespace": "velero", "via": "DeleteBackupRequest"}
  ],
  "referenced_by": [
    {"kind": "AppRestore", "name": "ar-a", "namespace": "disaster-system", "field": "spec.backupSource", "state": "Restoring"}
  ]
}
```

---

## 20. 模块真实调用依赖矩阵（AppRestore）

### 20.1 AppRestore 出边矩阵（AppRestore 主动调用谁）

| 编号 | 调用方 | 被调用资源/模块 | 调用动作 | 关键键/字段 | 用途 | 代码锚点 |
|---|---|---|---|---|---|---|
| AR-OUT-01 | server `AppRestoreHandler` | `AppRestore` CR | `Create/Get/List/Update/Delete/Watch` | `metadata.name` | 应用恢复 CRUD 与 watch | `disaster-server/internal/apis/app_restore/v1/handler.go` |
| AR-OUT-02 | server `AppRestoreHandler` | `AppBackup` CR | `Get` | `spec.backupSource` | 创建恢复时读取备份来源并回填字段 | `handler.go` `createAppRestore` |
| AR-OUT-03 | server `AppRestoreHandler` | `Cluster` CR | `Get`（校验） | `spec.cluster` | 校验目标集群可用性 | `handler.go` + `internal/common/validator.go` |
| AR-OUT-04 | operator `AppRestoreReconciler` | `StorageRepository` CR | `Get` + `ApplyStorageRepository` | `spec.storageRepository/spec.sourceCluster` | 跨集群恢复时预加载 BSL | `internal/controller/apprestore/apprestore_state.go` |
| AR-OUT-05 | operator `AppRestoreReconciler` | Velero Backup/Restore | `Get/Create/Delete` | `template.backupName` + `LabelAppRestoreUID` | 执行恢复与状态跟踪 | `apprestore_controller.go`、`apprestore_state.go` |
| AR-OUT-06 | operator `AppRestoreReconciler` | 目标集群 ConfigMap | `DeleteAllOf` | `LabelAppRestoreUID` 相关规则 | 删除阶段清理 ResourceModifier ConfigMap | `apprestore_controller.go` `deleteExternalResources` |

### 20.2 AppRestore 入边矩阵（谁在调用 AppRestore）

| 编号 | 调用方模块 | 被调用方 | 调用动作 | 关键键/字段 | 业务含义 | 代码锚点 |
|---|---|---|---|---|---|---|
| AR-IN-01 | `cluster` operator | `AppRestore` CR | `List` | `label: app-restore-cluster=<cluster>` | Cluster 删除阻塞 | `internal/controller/cluster_controller.go` |
| AR-IN-02 | `datasync` operator | `AppRestore` CR | `Create/Get` + `OwnerReference` | `dataSync.status.lastRestoreName` | 数据同步中的恢复步骤 | `internal/controller/datasync/controller.go` |
| AR-IN-03 | `resourcesync` operator | `AppRestore` CR | `Create/Get` + `OwnerReference` | `resourceSync.status.lastRestoreName` | 资源同步中的恢复步骤 | `internal/controller/resourcesync/controller.go` |
| AR-IN-04 | `disaster_operation` operator | `AppRestore` CR | `Create/Get` | `operation.status.resourceRestoreName/dataRestoreName` | Drill 步骤中的资源/数据恢复 | `internal/controller/disasteroperation/controller.go` |

### 20.3 AppRestore 按统一检查入口的落地（当前代码）

1. `cleanup_resources`（即将清理）：
- 目标集群 `Velero Restore`（按 `LabelAppRestoreUID` 批量删除）。
- 目标集群 ResourceModifier `ConfigMap`（删除与该恢复关联配置）。

2. `referenced_by`（其他模块引用，禁止删除）：
- `DisasterOperation`（Drill 链路）：
  - `status.resourceRestoreName=<appRestoreName>` 或 `status.dataRestoreName=<appRestoreName>`。
  - 建议阻塞状态：`Pending`、`Running`。

3. 非阻塞引用（建议）：
- `DataSync/ResourceSync` 的 `LastRestoreName` 引用属于“可重建同步链路”，默认不作为硬阻塞。

4. 返回示例（建议）：
```json
{
  "target": {"kind": "AppRestore", "name": "ar-a", "namespace": "disaster-system"},
  "cleanup_resources": [
    {"kind": "VeleroRestore", "name": "res-ar-a", "namespace": "velero", "via": "label:app-restore-uid"},
    {"kind": "ConfigMap", "name": "restore-modifier-*", "namespace": "velero", "via": "DeleteAllOf"}
  ],
  "referenced_by": [
    {"kind": "DisasterOperation", "name": "drill-op-a", "namespace": "disaster-system", "field": "status.resourceRestoreName", "state": "Running"}
  ]
}
```

---

## 21. 模块真实调用依赖矩阵（DisasterDrill）

### 21.1 DisasterDrill 出边矩阵（Drill 主动调用谁）

| 编号 | 调用方 | 被调用资源/模块 | 调用动作 | 关键键/字段 | 用途 | 代码锚点 |
|---|---|---|---|---|---|---|
| DD-OUT-01 | server `DrillHandler` | `DisasterDrill` CR | `Create/Get/List/Update/Delete/Watch` | `metadata.name` | 演练 CRUD/确认/重跑/清理入口 | `disaster-server/internal/apis/disaster_drill/v1/handler.go` |
| DD-OUT-02 | server `DrillHandler` | `DisasterInstance` / `DisasterGroup` CR | `Get/List` | `req.instanceName/groupName` | 创建演练时对象存在性校验 | `handler.go` `createDrill` |
| DD-OUT-03 | operator `DisasterDrillReconciler` | `DisasterInstance` / `DisasterGroup` / `Cluster` CR | `Get/List` | `spec.instanceName/groupName/targetCluster` | Pending 阶段自检与目标集群安全校验 | `internal/controller/disasterdrill/controller.go` |
| DD-OUT-04 | operator `DisasterDrillReconciler` | `DisasterOperation` CR | `Create/Get/List` + `OwnerReference` | `label: testudo.softcdata.com/drill=<drill>` | 执行演练与清理演练编排 | `controller.go` `handleReady/triggerCleanup/handleExecuting` |
| DD-OUT-05 | operator `DisasterDrillReconciler` | `DisasterDrill` finalizer | `RemoveFinalizer` | `drillFinalizer` | 删除时结束控制权，交由级联删除 | `controller.go` `handleDeletion` |

### 21.2 DisasterDrill 入边矩阵（谁在调用 Drill）

| 编号 | 调用方模块 | 被调用方 | 调用动作 | 关键键/字段 | 业务含义 | 代码锚点 |
|---|---|---|---|---|---|---|
| DD-IN-01 | `DisasterOperation`（drill 子操作） | `DisasterDrill` CR | `OwnerReference` 级联关系 | `ownerRef.kind=DisasterDrill` | 演练主对象删除时清理操作对象 | `disasterdrill/controller.go` `handleReady/triggerCleanup` |
| DD-IN-02 | server `DrillHandler` 自身动作入口 | `DisasterDrill` CR | `confirm/restart/cleanup` 更新 | `spec.confirmed/spec.cleanUp/annotation.restart` | 演练生命周期推进 | `disaster_drill/v1/handler.go` |

### 21.3 DisasterDrill 按统一检查入口的落地（当前代码）

1. `cleanup_resources`（即将清理）：
- `DisasterOperation`（`label: testudo.softcdata.com/drill=<drillName>`，且由 `OwnerReference` 级联清理）。

2. `referenced_by`（其他模块引用，禁止删除）：
- 当前可视为 `[]`（无独立长期引用方）。

3. 返回示例（建议）：
```json
{
  "target": {"kind": "DisasterDrill", "name": "drill-a", "namespace": "disaster-system"},
  "cleanup_resources": [
    {"kind": "DisasterOperation", "name": "drill-a-1738741", "namespace": "disaster-system", "via": "ownerReference"}
  ],
  "referenced_by": []
}
```

---

## 22. 模块真实调用依赖矩阵（DisasterBackup）

### 22.1 DisasterBackup 出边矩阵（Backup 主动调用谁）

| 编号 | 调用方 | 被调用资源/模块 | 调用动作 | 关键键/字段 | 用途 | 代码锚点 |
|---|---|---|---|---|---|---|
| DB-OUT-01 | server `BackupHandler` | `DisasterBackup` CR | `Create/Get/List/Update/Delete/Watch` | `metadata.name` | 灾备快照 CRUD 与 watch | `disaster-server/internal/apis/disaster_backup/v1/handler.go` |
| DB-OUT-02 | operator `DisasterBackupReconciler` | `DisasterConfig` CR | `Get` | `backup.spec.disasterConfig` | 读取源集群配置 | `internal/controller/disasterbackup_controller.go` |
| DB-OUT-03 | operator `DisasterBackupReconciler` | `Cluster` CR | `Get`（经 Config 派生） | `config.spec.sourceCluster` | 构建源集群 rest config | `disasterbackup_controller.go` `getRestConfigByClusterName` |
| DB-OUT-04 | operator `DisasterBackupReconciler` | 源集群 Discovery/Dynamic API | `ServerPreferredResources/List` | `backup.spec.namespace` | 枚举命名空间资源并写入 status | `disasterbackup_controller.go` |

### 22.2 DisasterBackup 入边矩阵（谁在调用 Backup）

| 编号 | 调用方模块 | 被调用方 | 调用动作 | 关键键/字段 | 业务含义 | 代码锚点 |
|---|---|---|---|---|---|---|
| DB-IN-01 | server `BackupHandler` 动作入口 | `DisasterBackup` CR | `create/update/delete` | `metadata.name` | 用户态灾备快照管理 | `disaster_backup/v1/handler.go` |
| DB-IN-02 | `DisasterConfig` 语义链路 | `DisasterBackup` 依赖字段 | Spec 字段引用 | `backup.spec.disasterConfig` | 通过 Config 间接绑定 Cluster/Storage | `pkg/apis/disaster/v1/disasterbackup_types.go` |

### 22.3 DisasterBackup 按统一检查入口的落地（当前代码）

1. `cleanup_resources`（即将清理）：
- 当前为 `[]`（空），无 finalizer 清理链路。

2. `referenced_by`（其他模块引用，禁止删除）：
- 当前可视为 `[]`（未发现其他模块对 `DisasterBackup` 的稳定反向引用）。

3. 返回示例（建议）：
```json
{
  "target": {"kind": "DisasterBackup", "name": "backup-a", "namespace": "disaster-system"},
  "cleanup_resources": [],
  "referenced_by": []
}
```

---

## 23. 模块真实调用依赖矩阵（DisasterOperation，内部编排子模块）

### 23.1 DisasterOperation 出边矩阵（Operation 主动调用谁）

| 编号 | 调用方 | 被调用资源/模块 | 调用动作 | 关键键/字段 | 用途 | 代码锚点 |
|---|---|---|---|---|---|---|
| DO-OUT-01 | operator `DisasterOperationReconciler` | `DisasterInstance` / `DisasterGroup` CR | `Get` | `spec.instanceName/spec.groupName` | 实例级/组级编排入口 | `internal/controller/disasteroperation/controller.go` |
| DO-OUT-02 | operator `DisasterOperationReconciler` | `DisasterConfig` CR | 多处 `Get` | `instance.spec.config` | 解析源/目标集群与仓库 | `controller.go`、`controller_check.go` |
| DO-OUT-03 | operator `DisasterOperationReconciler` | `DataSync` / `ResourceSync` CR | `Get/Update` | `instance.status.dataSyncName/resourceSyncName` | pause/resume/synconce 操作 | `controller.go` |
| DO-OUT-04 | operator `DisasterOperationReconciler` | `AppRestore` CR | `Create/Get` | `status.resourceRestoreName/dataRestoreName` | Drill 资源/数据恢复步骤 | `controller.go` `executeDrillRestore*` |
| DO-OUT-05 | operator `DisasterOperationReconciler` | 子 `DisasterOperation` CR | `Create/Get` | `owner-operation` label | 组级操作分层并行编排 | `controller.go` `handleGroupOperation` |

### 23.2 DisasterOperation 入边矩阵（谁在调用 Operation）

| 编号 | 调用方模块 | 被调用方 | 调用动作 | 关键键/字段 | 业务含义 | 代码锚点 |
|---|---|---|---|---|---|---|
| DO-IN-01 | `disaster_instance` server action | `DisasterOperation` CR | `Create` | `spec.instanceName` + `label instance` | 实例动作触发 | `internal/apis/disaster_instance/v1/handler_action.go` |
| DO-IN-02 | `disaster_group` server action | `DisasterOperation` CR | `Create/List/Watch` | `spec.groupName` + `label group` | 组动作触发与历史查询 | `internal/apis/disaster_group/v1/handler.go` |
| DO-IN-03 | `disaster_drill` operator | `DisasterOperation` CR | `Create/Get/List` + `OwnerReference` | `label drill` | 演练执行与清理编排 | `internal/controller/disasterdrill/controller.go` |

### 23.3 DisasterOperation 与统一检查入口的关系

1. 当前无独立 server `DELETE /operations` 路由，不是统一删除预检查首批暴露对象。
2. 但它是 `DisasterGroup`、`DisasterInstance`、`AppRestore` 删除阻塞的重要上游引用来源，已在这些模块的 `referenced_by` 中体现。

---

## 24. 模块真实调用依赖矩阵（DataSync，内部同步子模块）

### 24.1 DataSync 出边矩阵（DataSync 主动调用谁）

| 编号 | 调用方 | 被调用资源/模块 | 调用动作 | 关键键/字段 | 用途 | 代码锚点 |
|---|---|---|---|---|---|---|
| DS-OUT-01 | operator `DataSyncReconciler` | `DisasterInstance` / `DisasterConfig` CR | `Get` | `spec.instance -> instance.spec.config` | 同步链路基础信息 | `internal/controller/datasync/controller.go` |
| DS-OUT-02 | operator `DataSyncReconciler` | `AppBackup` CR | `Create/Get/Update` + `OwnerReference` | `name=ds-<datasync>` | 数据备份步骤 | `datasync/controller.go` `executeSync` |
| DS-OUT-03 | operator `DataSyncReconciler` | `AppRestore` CR | `Create/Get` + `OwnerReference` | `name=dsr-<datasync>-*` | 数据恢复步骤 | `datasync/controller.go` `handleRestore` |
| DS-OUT-04 | operator `DataSyncReconciler` | 本地调度器 | `AddOrUpdate/Remove` | `spec.trigger.schedule` | 周期调度触发同步 | `datasync/controller.go` |

### 24.2 DataSync 入边矩阵（谁在调用 DataSync）

| 编号 | 调用方模块 | 被调用方 | 调用动作 | 关键键/字段 | 业务含义 | 代码锚点 |
|---|---|---|---|---|---|---|
| DS-IN-01 | `disaster_instance` operator | `DataSync` CR | `CreateOrUpdate` + `OwnerReference` | `instance.status.dataSyncName` | 实例初始化创建 DS 子资源 | `internal/controller/disasterinstance/controller.go` |
| DS-IN-02 | `disaster_operation` operator | `DataSync` CR | `Get/Update` | `instance.status.dataSyncName` | 操作期间 pause/resume/sync 控制 | `internal/controller/disasteroperation/controller.go` |

### 24.3 DataSync 与统一检查入口的关系

1. 当前无独立 server `DELETE /datasyncs` 路由，不纳入首批统一删除入口。
2. `DisasterInstance` 删除时，DataSync 属于 `cleanup_resources` 链路（OwnerReference 级联）。

---

## 25. 模块真实调用依赖矩阵（ResourceSync，内部同步子模块）

### 25.1 ResourceSync 出边矩阵（ResourceSync 主动调用谁）

| 编号 | 调用方 | 被调用资源/模块 | 调用动作 | 关键键/字段 | 用途 | 代码锚点 |
|---|---|---|---|---|---|---|
| RS-OUT-01 | operator `ResourceSyncReconciler` | `DisasterInstance` / `DisasterConfig` CR | `Get` | `spec.instance -> instance.spec.config` | 资源同步基础信息 | `internal/controller/resourcesync/controller.go` |
| RS-OUT-02 | operator `ResourceSyncReconciler` | `AppBackup` CR | `Create/Get/Update` + `OwnerReference` | `name=rs-<resourcesync>` | 资源备份步骤 | `resourcesync/controller.go` `executeSync` |
| RS-OUT-03 | operator `ResourceSyncReconciler` | `AppRestore` CR | `Create/Get` + `OwnerReference` | `name=rsr-<resourcesync>-*` | 资源恢复步骤 | `resourcesync/controller.go` `handleRestore` |
| RS-OUT-04 | operator `ResourceSyncReconciler` | ConfigMap | `CreateOrUpdate` + `OwnerReference` | `replicas-<resourcesync>` | 记录原始副本数用于回切 | `resourcesync/controller.go` `recordReplicasToConfigMap` |

### 25.2 ResourceSync 入边矩阵（谁在调用 ResourceSync）

| 编号 | 调用方模块 | 被调用方 | 调用动作 | 关键键/字段 | 业务含义 | 代码锚点 |
|---|---|---|---|---|---|---|
| RS-IN-01 | `disaster_instance` operator | `ResourceSync` CR | `CreateOrUpdate` + `OwnerReference` | `instance.status.resourceSyncName` | 实例初始化创建 RS 子资源 | `internal/controller/disasterinstance/controller.go` |
| RS-IN-02 | `disaster_operation` operator | `ResourceSync` CR | `Get/Update` | `instance.status.resourceSyncName` | 操作期间 pause/resume/sync 控制 | `internal/controller/disasteroperation/controller.go` |

### 25.3 ResourceSync 与统一检查入口的关系

1. 当前无独立 server `DELETE /resourcesyncs` 路由，不纳入首批统一删除入口。
2. `DisasterInstance` 删除时，ResourceSync 属于 `cleanup_resources` 链路（OwnerReference 级联）。

---

## 26. 全模块完成说明（本轮）

1. 已完成“真实调用依赖矩阵 + 统一检查入口映射”的模块：
- `DisasterGroup`
- `DisasterInstance`
- `DisasterConfig`
- `Cluster`
- `StorageRepository`
- `DisasterPolicy`
- `AppBackup`
- `AppRestore`
- `DisasterDrill`
- `DisasterBackup`
- `DisasterOperation`（内部编排子模块）
- `DataSync`（内部同步子模块）
- `ResourceSync`（内部同步子模块）

2. 口径声明：
- 按此前约定，`DisasterJob` 不作为独立模块展开，仅在 `DisasterPolicy` 兼容规则中保留其影响。
- 路由扫描可见 `DELETE /jobs/:name`（`disaster-server/internal/apis/disaster_jobs/v1/router.go`），但本轮按当前讨论口径不单列 `DisasterJob` 模块；如后续纳入统一检查入口，可直接复用本文件的两段式结构（`cleanup_resources` / `referenced_by`）补齐。

3. 可直接用于统一检查入口（有 server delete 路由）的资源范围：
- `Cluster`、`StorageRepository`、`DisasterPolicy`、`DisasterConfig`、`DisasterInstance`、`DisasterGroup`、`AppBackup`、`AppRestore`、`DisasterDrill`、`DisasterBackup`。
