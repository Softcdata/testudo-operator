# S3 数据同步流程与 BSL 管理机制

本文档详细描述了基于 S3 和 Velero 的跨集群数据同步流程，特别关注 `BackupStorageLocation` (BSL) 的生命周期管理、创建时机及刷新机制。

## 1. 架构概述

Disaster Operator 使用 Velero 的文件系统备份 (FSB/Restic/Kopia) 功能来实现跨集群的数据同步。
*   **控制面**: `DataSync`, `AppBackup`, `AppRestore` 控制器协调同步任务。
*   **数据面**: Velero 将 Pod 卷数据备份到 S3 对象存储，并在目标集群恢复。在
*   **连接桥梁**: `BackupStorageLocation` (BSL) 是 Velero 连接 S3 的关键配置。由于 Velero 是单集群范围的，Operator 必须确保每个参与同步的集群上都正确配置了指向同一 S3 Bucket 的 BSL。

## 2. BSL 命名与映射策略

为了支持多集群共享同一存储后端但又能明确区分（或共用配置），Operator 采用以下映射策略：

*   **全局配置源**: `StorageRepository` CR (位于管理集群)。定义了 S3 的 Endpoint, Bucket, Note 等信息。
*   **目标集群 BSL**: 
    *   命名规则: `<StorageRepository名称>-<目标集群名称>`
    *   例如: 若存储仓库名为 `minio-repo`，目标集群为 `cluster-b`，则在 `cluster-b` 上创建名为 `minio-repo-cluster-b` 的 BSL。

这种命名方式确保了不会与其他集群的配置冲突，同时也明确了该 BSL 是由哪个存储仓库生成的。

## 3. 流程详解：何时创建与刷新 BSL？

BSL 的管理主要由 `AppBackup` 控制器负责。它确保在执行由于 Velero 备份或调度任务之前，目标集群已就绪。

### 3.1. 初始创建阶段 (Pending Phase)
当 `AppBackup` CR 首次创建时（状态为 `Pending`）：
1.  **验证配置**: 检查 `Spec.Cluster` 和 `Spec.Template.StorageLocation`。
2.  **获取存储定义**: 读取管理集群中的 `StorageRepository`。
3.  **应用 BSL (Apply)**: 
    *   连接到目标集群 (`Spec.Cluster`)。
    *   根据命名规则生成 BSL 名称。
    *   创建或更新目标集群上的 Velero `BackupStorageLocation` 资源，并同步 Secret (如 AWS 密钥)。
    *   如果 BSL 状态为 `Unavailable`，控制器会重试。

### 3.2. 运行阶段与动作触发 (Ready Phase)
当 `AppBackup` 处于 `Ready` 状态时，为了支持**反向同步 (Reprotect)** 场景（即 `Spec.Cluster` 发生变更），控制器会在执行任何具体动作前**强制检查并刷新 BSL**。

以下动作触发时会调用 `ensureBSL` 逻辑：

1.  **创建调度 (Schedule)**: 
    *   在创建 Velero Schedule 之前，确保 BSL 存在。
2.  **执行一次性备份 (Manual Backup)**:
    *   当 `DataSync` 触发新的同步（或用户手动触发）时。
3.  **重试备份 (Retry)**:
    *   执行失败重试时。
4.  **首次备份**:
    *   如果是新创建的 AppBackup 且并未立即执行过备份。

### 3.3. 反向同步流程 (Reprotect Workflow)
这是 BSL 刷新机制发挥关键作用的场景：

1.  **故障切换前**: `AppBackup` 指向 Cluster-A (原始主)。BSL `repo-cluster-a` 在 Cluster-A 上创建。
2.  **故障切换后**: 用户执行 Reprotect。
3.  **控制器响应**: 
    *   `DataSync` 控制器检测到方向变化。
    *   更新 `AppBackup` 的 `Spec.Cluster` 为 Cluster-B (新主)。
    *   `AppBackup` 状态保持 `Ready`（或变为 `Pending`，取决于具体实现，当前逻辑保持 Ready）。
4.  **动作执行**:
    *   `AppBackup` 收到新的备份 Action。
    *   `ReadyHandler` 在创建 Velero Backup **之前**，发现需要连接 Cluster-B。
    *   调用 `ensureBSL`。
    *   **自动创建** BSL `repo-cluster-b` 在 Cluster-B 上（若不存在）。
    *   Velero Backup 创建并指向 `repo-cluster-b`。
    *   备份成功执行。

### 3.4. 恢复时的 BSL 管理 (AppRestore)

跨集群恢复时，作为恢复目标的集群也必须配置 BSL 才能读取 S3 上的备份数据。`AppRestore` 控制器在 `Pending` 阶段（`PendingHandler`）会自动处理此逻辑。

**逻辑流程**:
1.  **检测跨集群**: 比较 `Spec.SourceCluster` (备份来源) 和 `Spec.Cluster` (恢复目标)。如果两者不同，则判定为跨集群恢复。
2.  **生成 BSL 名称**:
    *   使用 **源集群名称** 作为后缀。
    *   命名规则: `<StorageRepository名称>-<Spec.SourceCluster>`
    *   例如: 若数据来自 `cluster-a`，要恢复到 `cluster-b`，则在 `cluster-b` 上创建名为 `repo-cluster-a` 的 BSL。
3.  **创建/同步**: 调用 `ensureBSL` 逻辑，在目标集群上创建该 BSL。

**关键点**: 
在恢复场景下，目标集群上通常会存在多个 BSL（自己的 `repo-cluster-b` 用于备份，以及 `repo-cluster-a` 用于读取源集群的数据）。这种设计允许 Velero 清晰地隔离不同来源的备份数据。

## 4. 关键代码逻辑

BSL 的确保逻辑封装在 `AppBackupReconciler` 的辅助方法中（简化版）：

```go
func ensureBSL(cluster, repoName) {
    1. 获取 StorageRepository (repoName)
    2. 计算 bslName = repoName + "-" + cluster
    3. 连接 cluster 的 K8s API
    4. CreateOrUpdate BackupStorageLocation (bslName)
    5. 返回 bslName
}
```

## 5. 总结

*   **创建时机**: AppBackup 初始化时 (`Pending`)，以及每次执行备份/调度前 (`Ready`)。
*   **刷新时机**: 每次执行动作前都会检查，确保配置漂移或集群变更后能自动修复。
*   **核心保障**: 这种**Just-In-Time (JIT)** 的 BSL 检查机制，是支持跨集群双向同步和自动化容灾的基石。
