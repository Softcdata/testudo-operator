## ADDED Requirements

### Requirement: DataSync 核心定义
`DataSync` 必须 (MUST) 采用 **Trafficless Restore (无流量恢复)** 方案实现 PVC 数据的跨集群复制。

#### Scenario: Trafficless 策略
- **GIVEN** DataSync 由 DisasterInstance 创建
- **WHEN** 执行数据同步
- **THEN** 必须 (MUST) 分两阶段完成：元数据备份 + 数据恢复
- **AND** 数据恢复时必须 (MUST) 创建"隐形 Pod"完成 FSB 数据注入

---

### Requirement: 数据备份 (源端)
DataSync 必须 (MUST) 在源端执行带 FSB 的完整备份。

#### Scenario: FSB 备份配置
- **GIVEN** DataSync 触发同步
- **WHEN** 创建 AppBackup
- **THEN** 必须 (MUST) 配置 `DefaultVolumesToFsBackup: true`
- **AND** 必须 (MUST) 配置 `SnapshotVolumes: false`
- **AND** 备份必须 (MUST) 包含 Pod 和 PVC 资源

---

### Requirement: Trafficless Restore (目标端)
DataSync 必须 (MUST) 使用 Trafficless Restore 在目标端恢复数据。

#### Scenario: 创建隐形 Pod
- **GIVEN** ResourceSync 已在目标端恢复资源骨架 (Replicas=0, PVC 存在但为空)
- **WHEN** DataSync 执行数据恢复
- **THEN** 必须 (MUST) 创建 AppRestore 只恢复 `pods` 资源
- **AND** 必须 (MUST) 应用 ResourceModifier 将 Pod "绝育"

#### Scenario: Trafficless Modifier 配置
- **GIVEN** 执行 Trafficless Restore
- **WHEN** 应用 ResourceModifier
- **THEN** 必须 (MUST) 删除 `/metadata/name` 并设置 `/metadata/generateName: "trafficless-sync-"` (避免 StatefulSet 控制器识别)
- **AND** 必须 (MUST) 替换 Pod 的 `/metadata/labels` 为 `{"trafficless": "true"}` (确保 Service/Deployment 无法选中)
- **AND** 必须 (MUST) 替换 Pod 的 `/metadata/annotations` 为 `{}` (清除所有注释)
- **AND** 必须 (MUST) 移除 Pod 的 `/metadata/ownerReferences` (确保不被 GC 删除)
- **AND** 必须 (MUST) 替换容器镜像为 `busybox:1.36` 或配置的 trafficlessImage
- **AND** 必须 (MUST) 替换容器命令为 `["sleep", "infinity"]` (确保无业务逻辑)

> **⚠️ 重要说明: StatefulSet vs Deployment 管理方式差异**
> 
> | 控制器类型 | Pod 识别方式 | 移除 Labels 是否有效 |
> |:---|:---|:---|
> | Deployment / ReplicaSet | Label Selector | ✅ 有效 |
> | StatefulSet | Pod 名称模式 (`{name}-{ordinal}`) | ❌ 无效 |
> 
> StatefulSet 通过 Pod 命名模式（而非 Labels）管理 Pod。当 ResourceSync 在目标端恢复了 `replicas: 0` 的 StatefulSet，
> 若 DataSync 恢复一个名为 `e2e-nginx-0` 的 Pod，即使移除了所有 Labels，StatefulSet 控制器仍会识别并删除该 Pod。
> 因此**必须使用 `generateName` 重命名 Pod**，使其脱离 StatefulSet 管理范围。

#### Scenario: FSB 数据注入
- **GIVEN** 隐形 Pod 在目标集群启动
- **WHEN** Pod 挂载已存在的 PVC
- **THEN** Velero Node Agent 必须 (MUST) 检测到 Pod 并执行 `fs-restore`
- **AND** 备份的数据必须 (MUST) 被写入 PVC

#### Scenario: 隐形 Pod 清理
- **GIVEN** 数据注入完成
- **WHEN** DataSync 更新状态
- **THEN** status 应当 (SHOULD) 记录隐形 Pod 名称
- **AND** Failover 时必须 (MUST) 删除隐形 Pod

---

### Requirement: 定时触发
DataSync 必须 (MUST) 支持基于 Cron 表达式的定时同步。

#### Scenario: Cron 调度
- **GIVEN** DataSync 配置了 `spec.trigger.schedule: "*/15 * * * *"`
- **WHEN** 调度时间到达
- **THEN** Controller 必须 (MUST) 触发一次数据同步

#### Scenario: 手动触发
- **GIVEN** DataSync 配置了 `spec.trigger.manual` 时间戳
- **WHEN** 时间戳大于上次同步时间
- **THEN** Controller 必须 (MUST) 立即触发一次数据同步

---

### Requirement: 暂停功能
DataSync 必须 (MUST) 支持暂停以配合 Failover 操作。

#### Scenario: 暂停同步
- **GIVEN** DisasterOperation 设置 `DataSync.spec.paused=true`
- **WHEN** 调度时间到达
- **THEN** Controller 不得 (MUST NOT) 触发新的同步任务
- **AND** 应当 (SHOULD) 等待当前运行中的任务完成

---

### Requirement: 状态报告
DataSync 必须 (MUST) 报告详细的同步状态。

#### Scenario: 同步状态
- **GIVEN** 同步完成
- **WHEN** 更新 Status
- **THEN** `status.state` 必须 (MUST) 为 `Ready` 或 `InProgress` 或 `Failed`
- **AND** `status.lastSyncTime` 必须 (MUST) 记录最后同步时间
- **AND** `status.lastBackupName` 必须 (MUST) 记录最近的 AppBackup 名称
- **AND** `status.lastRestoreName` 必须 (MUST) 记录最近的 AppRestore 名称

#### Scenario: 隐形 Pod 状态
- **GIVEN** Trafficless Restore 完成
- **WHEN** 更新 Status
- **THEN** `status.trafficlessPods` 应当 (SHOULD) 记录当前存在的隐形 Pod 列表
- **AND** 每个条目包含 `name`, `namespace`, `pvcName`
