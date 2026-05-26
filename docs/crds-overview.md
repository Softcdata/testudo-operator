# 灾备系统 CRD 详解

本文件详述当前系统的 9 个自定义资源（CRD）在灾备体系中的职责、结构、生命周期与彼此关系，帮助理解整体工作流并为后续扩展（新增 CRD、Controller 优化、测试设计）提供参考。

## 总览与关系拓扑
```
StorageRepository ──┐
                     │              ┌────────── DisasterBackup ───────────┐
Cluster (源) ──┐     │              │  收集命名空间资源快照                │
               │     │              │  (引用 DisasterConfig)               │
               ├─ DisasterConfig ──┤                                        ├─ DisasterJob → 数据/资源同步与迁移
Cluster (目标) ─┘     │              │                                        │
                     │              └────────────────────────────────────────┘
                     │
AppBackup ──── (针对单集群应用备份: Velero Backup / Schedule)
AppRestore ─── (针对单集群应用恢复: Velero Restore)
BackupPolicy ─ (通用备份策略：时间、TTL，可被其他备份逻辑引用)
BackupRestoreStatistics ─ (备份/恢复统计聚合)
```
- `StorageRepository` 提供持久化备份存储（S3 兼容）；被 `DisasterConfig` 和 `AppBackup` 间接使用。
- `Cluster` 描述外部受管 Kubernetes 集群并负责安装/检测 Velero。
- `DisasterConfig` 绑定两个集群（源→目标）及其同步策略与存储仓库；为后续灾备行为提供配置基线。
- `DisasterBackup` 在指定命名空间执行资源枚举（发现 + 状态记录），形成待同步/迁移的资源集合。
- `DisasterJob` 基于 `DisasterBackup` 和 `DisasterConfig` 驱动实际的数据与资源同步/恢复逻辑，并管理生命周期（正向/反向）。
- `AppBackup` 针对单集群的应用层定时/即时备份（Velero Backup/Schedule），与跨集群灾备逻辑相对独立。
- `AppRestore` 针对单集群的应用层恢复（Velero Restore），支持原地恢复或跨集群恢复。
- `BackupPolicy` 提供通用备份调度策略，可在未来被 `AppBackup` 或其他策略型资源引用（当前控制器为空壳）。
- `BackupRestoreStatistics` 收集并聚合备份/恢复操作的统计信息（成功、失败、进行中等）。

## 各资源详解

### 1. StorageRepository
**作用**：描述一个可用的备份存储位置（当前聚焦 S3 兼容对象存储）。
**关键字段 (spec)**：`storageType`, `bucket`, `region`, `endpoint`, `accessKey`, `secretKey`。
**状态 (status)**：`status`（`Available` / `Unavailable`）。
**控制器行为**：
- 连接 S3 HeadBucket；若桶不存在尝试创建。
- 验证凭据与端点可用性。
- 定期（Requeue）重新校验，提高存储层稳定性监测。
**外部交互**：直接调用 AWS SDK v2（或兼容端点）。
**改进空间**：添加事件（Recorder），支持不可用次数告警、支持自定义端点 TLS 校验。

### 2. Cluster
**作用**：注册并管理一个外部 Kubernetes 集群，使灾备系统能够访问其 API 与 Velero能力。
**关键字段 (spec)**：`kubeConfig`（当前主要使用 kubeconfig，未来可扩展 token）。
**状态 (status)**：`status`（Ready / NotReady / Pending）、`endpoint`、`k8sVersion`、`veleroVersion`。
**控制器行为**：
- 使用 kubeconfig 创建 clientset，探测 API Server 与版本。
- 检测或安装 Velero（若未安装则执行安装逻辑）。
- 创建 `ServerStatusRequest` 查询 Velero 服务端版本。
**外部交互**：与目标集群 API Server、Velero CRD 资源交互。
**改进空间**：
- 增加事件记录（安装成功 / 失败）。
- 失败重试策略细化（指数退避）。
- 支持 Token/OIDC 认证扩展。

### 3. DisasterConfig
**作用**：定义两个集群间的灾备配置（源 + 目标）、存储仓库及资源/数据同步策略。
**关键字段 (spec)**：`sourceCluster`, `targetCluster`, `storageRepository`, `dataSyncType`, `dataSyncInterval`, `resourcesSyncPolicy`, `resourcesSyncInterval`。
**状态 (status)**：`status`（Pending / Ready / NotReady / Error）、`reason`（错误/阻塞原因）。
**控制器行为**：
- 校验源/目标 Cluster 均处于 Ready。
- 默认化 `storageRepository`（若为空置为 `default`）。
- 应用存储仓库（例如向两个集群注册 BSL）。
**外部交互**：使用 Cluster kubeconfig，应用 `StorageRepository` 内容到相关集群（Velero BSL）。
**改进空间**：
- 细化数据同步类型枚举（S3 / NFS / External Trigger）。
- 增加条件（Conditions）替代单一 status + reason。
- 加入对 Interval/Policy 的动态校验（最小/最大值）。

### 4. DisasterBackup
**作用**：在指定命名空间收集当前资源（工作负载等）的快照清单，为后续迁移/同步提供基础数据集。
**关键字段 (spec)**：`disasterConfig`, `namespace`, `labelSelector`, `newNamespace`, `newStorageName`。
**状态 (status)**：`phase`（Pending → Ready → Failed 等）、`resources`（按命名空间分类的资源列表）、`workload`（工作负载分类）、`conditions`、`updateTime`。
**控制器行为**：
- 基于 `DisasterConfig` 的 `sourceCluster` 获取 kubeconfig。
- 动态发现命名空间内的资源（通过 discovery + dynamic client）。
- 将资源名称与类型打包进 status。
**外部交互**：访问源集群 API，枚举资源。
**改进空间**：
- 缓存 discovery 结果减少开销。
- 增加分页或过滤策略（仅选定资源类型）。
- 将资源清单输出为对象存储元数据（保证跨系统追踪）。

### 5. DisasterJob
**作用**：驱动一次或持续的灾备执行（备份/同步/恢复），从 `DisasterBackup` 集合到目标集群或反向操作。
**关键字段 (spec)**：`disasterBackup`, `syncType`（forward / reverse）、`scheduleType`（once / cron）。
**状态 (status)**：`phase`（Pending / Backuping / Restoring / Succeed / Failed / Deleting 等）、`conditions`, `reason`, `startTime`。
**控制器行为**：
- 读取 `DisasterBackup` 与 `DisasterConfig`。
- 初始化 Finalizer（保证删除前执行清理逻辑）。
- 根据阶段执行备份/恢复或资源迁移（可能与 Velero Backup 同步）。
- 删除流程：处理外部资源再移除 Finalizer。
**外部交互**：Velero、目标集群、存储仓库（潜在镜像/存储类调整）。
**改进空间**：
- 统一状态机与 DisasterBackup Phase 语义（当前有交叉命名）。
- 增加并发控制与进度百分比。
- 分离“备份阶段”和“恢复阶段”子资源或子任务。

### 6. AppBackup
**作用**：为单个集群的应用执行定时或即时备份（基于 Velero Backup / Schedule），关注应用层保护，与跨集群灾备分离。
**关键字段 (spec)**：`cluster`, `template`(Velero BackupSpec), `schedule`, `paused`, `skipImmediately`, `useOwnerReferencesInBackup`。
**状态 (status)**：`status`, `scheduleStatus`, `backupStatus`（直接反映 Velero 对象状态）。
**控制器行为**：
- 获取目标集群客户端。
- 应用 `StorageRepository` 配置（确保对应存储位置有效）。
- 根据 `schedule` 创建 Velero Backup 或 Schedule。
- 维护状态与事件（Recorder）。
**外部交互**：Velero API（Backup / Schedule）、StorageRepository（BSL 应用）。
**改进空间**：
- 对 `skipImmediately` 逻辑进行显式条件测试与策略输出。
- 引入失败重试策略（区分瞬时网络失败与永久配置错误）。
- 增加备份结果指标（备份大小、耗时）。

### 7. BackupPolicy
**作用**：定义通用备份策略（定时表达式、TTL、开始时间），为未来与多种备份行为（AppBackup、潜在恢复策略）复用提供集中配置点。
**关键字段 (spec)**：`schedule`, `ttl`, `startTime`。
**状态 (status)**：当前为空（可扩展下次运行时间、已生成备份计数）。
**控制器行为**：目前空实现（无状态维护与下游对象生成）。
**潜在用途**：
- 为 `AppBackup` 或未来 `RestorePolicy` 生成实际备份对象。
- 统一策略变更后自动级联更新相关备份实例。
**改进空间**：
- 增加 Cron 解析与下一次触发时间计算。
- 关联已触发备份的历史记录（status.counters）。
- 提供策略冲突检测（同命名空间多策略相同 schedule）。

### 8. AppRestore
**作用**：定义应用恢复任务，通常基于 `AppBackup` 生成的备份进行恢复。
**关键字段 (spec)**：`backupName`, `restorePVs`, `namespaceMapping`。
**状态 (status)**：`Phase` (Completed, Failed, InProgress), `RestoreName`。
**控制器行为**：
- 创建 Velero Restore 对象。
- 监控 Restore 状态并同步到 `AppRestore` status。
- 维护统计信息 (`BackupRestoreStatistics`)。
**外部交互**：Velero Restore API。

### 9. BackupRestoreStatistics
**作用**：存储备份或恢复操作的统计数据（成功、失败、进行中数量）。
**关键字段 (spec)**：`scopeType` (app, namespace), `scopeRef`。
**状态 (status)**：`statistics` (Total, Completed, Failed, InProgress, Canceled, Unknown)。
**控制器行为**：
- 无独立控制器逻辑（被动更新）。
- 由 `AppBackup` 和 `AppRestore` 控制器计算快照并同步更新。
**设计模式**：Level-Triggered Sync (Snapshot & Patch)。
**聚合方式**：客户端聚合（通过 Label Selector `testudo.softcdata.com/owner-kind`）。

## 典型端到端灾备流程示例
1. 创建 `StorageRepository`（验证 S3 存储可用）。
2. 创建两个 `Cluster`（源与目标），控制器安装/检测 Velero，状态进入 Ready。
3. 创建 `DisasterConfig`：指定源/目标集群与仓库、同步间隔与策略；控制器验证依赖进入 Ready。
4. 创建一次 `DisasterBackup`：控制器枚举源命名空间资源并在 status 中列出。
5. 创建 `DisasterJob`：
   - 读取已准备好的 `DisasterBackup`。
   - 如果是 forward 同步：执行资源/数据复制或触发 Velero 恢复到目标集群。
   - 更新状态到 Succeed 或 Failed。
6. 后续周期性同步：根据 `DisasterConfig` 策略或新的 `DisasterJob` 实例执行增量。
7. 需要应用级保护时：创建 `AppBackup`（即时或基于 schedule），Velero 生成备份快照。
8. （可选）引入 `BackupPolicy` 统一多备份策略，未来自动生成对应 `AppBackup` 实例。

## 常见扩展与优化点
- 事件标准化：统一使用 Recorder 输出阶段性事件（创建、失败、重试）。
- 指标/监控：为每个控制器暴露进度、耗时、错误计数（Prometheus）。
- 重试与退避：抽象通用重试工具避免硬编码 `RequeueAfter`。
- Finalizer 覆盖率提升：对会创建外部资源的 CRD（AppBackup, DisasterBackup）添加清理逻辑。
- 资源过滤策略：允许在 DisasterBackup 中配置排除/包含的 API 组/Kind。
- 多租户隔离：通过命名空间或标签隔离不同租户灾备配置与作业。

## 交互矩阵简表
| CRD | 依赖 | 外部系统 | 控制器核心行为 | 状态驱动 | 清理需求 |
|-----|------|----------|----------------|----------|----------|
| StorageRepository | 无 | S3 对象存储 | 校验/创建桶 | Available/Unavailable | 中等（可能需删除桶，当前未实现） |
| Cluster | kubeconfig | K8s API / Velero | 安装/检测 Velero | Ready/NotReady | 低（可能撤销安装） |
| DisasterConfig | Cluster / StorageRepository | 源/目标集群 | 验证依赖/应用仓库 | Pending/Ready/Error | 中等（撤销 BSL） |
| DisasterBackup | DisasterConfig | 源集群 API | 资源枚举快照 | Phase 机 | 低（无外部副作用） |
| DisasterJob | DisasterBackup / DisasterConfig | Velero / 源/目标集群 | 同步/迁移/恢复 | Phase 机 | 高（Finalizer 已实现） |
| AppBackup | Cluster / StorageRepository | Velero | 创建 Backup/Schedule | Backup/ScheduleStatus | 中等（清理 Velero 对象） |
| AppRestore | Cluster / StorageRepository | Velero | 创建 Restore | RestoreStatus | 中等（清理 Velero 对象） |
| BackupPolicy | 无 (未来被引用) | 无 | （待实现）策略定义 | （待扩展） | 低 |
| BackupRestoreStatistics | AppBackup / AppRestore | 无 | 被动更新 | StatisticsData | 低 |

## 后续建议路线
1. 实现 `BackupPolicy` 控制器核心逻辑（生成 AppBackup 或调度触发）。
2. 对 `AppBackup` / `DisasterBackup` 增加 Finalizer 及清理 Velero 资源（避免孤儿对象）。
3. 引入统一 `Condition` 集合替换部分简单 status 字段（提高可观测性）。
4. 增加灾备数据对比/一致性校验（源与目标资源哈希比对）。
5. 建立公共库：重试、事件封装、Velero 操作抽象，减少重复代码。

---
若需将本概览集成到主 README，可告知，我可以自动合并相应片段。
