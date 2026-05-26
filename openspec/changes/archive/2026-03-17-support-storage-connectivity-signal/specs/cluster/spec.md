## ADDED Requirements

### Requirement: Cluster ensure-storage 信号支持 sourceCluster 后缀
系统必须 (MUST) 支持通过 `Cluster` 双注解触发跨集群恢复所需的 BSL 创建，命名规则必须对齐 `SourceCluster` 语义。

#### Scenario: 双注解触发 sourceCluster 语义
- **Given** `Cluster` 带有注解 `testudo.softcdata.com/ensure-storage=<storageRepository>`
- **And** `Cluster` 带有注解 `testudo.softcdata.com/ensure-storage-source-cluster=<sourceCluster>`
- **When** `ClusterReconciler` 处理 ensure-storage 信号
- **Then** 必须计算 `bslName=<storageRepository>-<sourceCluster>`
- **And** 必须计算 `prefix=<sourceCluster>`
- **And** 必须调用 `ApplyStorageRepository` 将 BSL 对齐到目标集群

### Requirement: Cluster ensure-storage 信号兼容旧格式
系统必须 (MUST) 兼容仅携带 `ensure-storage` 的历史触发格式，保证已有调用链不回归。

#### Scenario: 缺失 sourceCluster 注解时的回退规则
- **Given** `Cluster` 带有注解 `testudo.softcdata.com/ensure-storage=<storageRepository>`
- **And** `Cluster` 未携带注解 `testudo.softcdata.com/ensure-storage-source-cluster`
- **When** `ClusterReconciler` 处理 ensure-storage 信号
- **Then** 必须计算 `bslName=<storageRepository>-<cluster.Name>`
- **And** 必须计算 `prefix=<cluster.Name>`
- **And** 必须继续执行 BSL 对齐

### Requirement: ensure-storage 信号消费后必须清理触发注解
系统必须 (MUST) 在一次性触发流程结束后清理触发注解，避免重复处理以及无效重试。

#### Scenario: BSL 对齐成功后清理双注解
- **Given** `ClusterReconciler` 成功完成 `ApplyStorageRepository`
- **When** Reconcile 进入信号收尾阶段
- **Then** 必须移除注解 `testudo.softcdata.com/ensure-storage`
- **And** 必须移除注解 `testudo.softcdata.com/ensure-storage-source-cluster`

#### Scenario: StorageRepository 缺失时清理双注解
- **Given** `ClusterReconciler` 根据 `ensure-storage` 查找 `StorageRepository`
- **When** 查询返回 `NotFound`
- **Then** 必须移除注解 `testudo.softcdata.com/ensure-storage`
- **And** 必须移除注解 `testudo.softcdata.com/ensure-storage-source-cluster`
- **And** 必须记录失败事件说明存储名称无效
