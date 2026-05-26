# 多集群容灾存储隔离与 BSL 命名策略

## 1. 背景与问题

在多集群容灾场景中，上层抽象定义了统一的存储资源（`StorageRepository`），例如 `storage-2`。底层使用 Velero 的 `BackupStorageLocation` (BSL) 来对接实际的对象存储。

原有的实现方式直接使用 `StorageRepository` 的名称作为 Velero BSL 的名称。这在单集群场景下工作正常，但在多集群或跨集群恢复场景下会导致以下严重问题：

1.  **命名冲突**：在同一个管理集群（或恢复目标集群）中，如果需要同时访问“集群A的备份”和“集群B的备份”，由于它们都对应上层的 `storage-2`，系统会尝试创建两个同名为 `storage-2` 但指向不同路径（Prefix）的 BSL，导致 Kubernetes 资源冲突。
2.  **路径混淆**：如果强制覆盖 BSL 配置，会导致正在运行的备份任务写入错误的路径，或者在恢复时无法正确指向源集群的数据目录。

## 2. 解决方案

为了解决上述问题，我们在 Operator 层面引入了 **BSL 名称与存储资源名称解耦** 的策略。

### 核心策略

**BSL 名称 = 存储资源名称 + 集群标识**

*   **StorageRepository**: `storage-2` (用户定义的逻辑名称)
*   **Cluster**: `ip171` (数据所属的集群)
*   **Generated BSL Name**: `storage-2-ip171`
*   **Object Storage Prefix**: `ip171` (或其他指定的子路径)

通过这种方式，在同一个 Kubernetes 集群内，可以共存多个指向同一 Bucket 但不同子路径的 BSL 实例：
*   `storage-2-ip171` -> Bucket: `velero-bucket`, Prefix: `ip171`
*   `storage-2-ip170` -> Bucket: `velero-bucket`, Prefix: `ip170`

## 3. 实施细节

### 3.1 代码重构 (`BSL.go`)

修改了 `BSL` 接口及其默认实现 `DefaultBSL`，增加了 `bslName` 参数，允许调用方显式指定 BSL 的名称，而不是硬编码使用 `sr.Name`。

```go
// 接口定义变更
type BSL interface {
    ApplyStorageRepository(ctx context.Context, cli client.Client, sr *disasterv1.StorageRepository, bslName, prefix string) error
}
```

### 3.2 控制器逻辑变更

#### AppBackup Controller
*   **备份/调度创建**：在创建 Velero `Backup` 或 `Schedule` 资源时，计算出专属的 BSL 名称（如 `storage-2-local-cluster`），并将其填入 `Spec.StorageLocation`。
*   **BSL 维护**：在 `Pending` 阶段，调用 `ApplyStorageRepository` 时传入生成的 `bslName`，确保底层 BSL 资源被正确创建。

#### DisasterConfig Controller
*   **跨集群配置**：在处理容灾配置时，Operator 会在源集群和目标集群分别创建 BSL。
*   **统一命名**：无论是在源集群还是目标集群，BSL 的名称都统一带上**源集群**的后缀。这确保了在目标集群进行恢复时，能够通过 `storage-2-source-cluster` 准确找到源集群的备份数据，而不会与目标集群自身的备份配置（`storage-2-target-cluster`）冲突。

### 3.3 代码复用
移除了 `DisasterConfigReconciler` 和 `AppBackupReconciler` 中重复的 BSL 创建与 Secret 管理代码，统一复用 `internal/controller/BSL.go` 中的逻辑，提高了代码的可维护性。

## 4. 场景示例

假设有两个集群：`cluster-A` 和 `cluster-B`，均使用存储 `storage-minio`。

### 场景 A：Cluster-A 自身备份
1.  用户创建 `AppBackup`，指定使用 `storage-minio`。
2.  Operator 在 Cluster-A 中创建 BSL：
    *   Name: `storage-minio-cluster-A`
    *   Bucket: `my-bucket`
    *   Prefix: `cluster-A`
3.  Velero 执行备份，数据存入 `my-bucket/cluster-A`。

### 场景 B：Cluster-B 恢复 Cluster-A 的数据
1.  用户在 Cluster-B 发起恢复任务（或通过 DisasterConfig 自动同步）。
2.  Operator 在 Cluster-B 中创建 BSL（用于读取 Cluster-A 数据）：
    *   Name: `storage-minio-cluster-A`
    *   Bucket: `my-bucket`
    *   Prefix: `cluster-A`
3.  同时，Cluster-B 可能还有自己的备份配置：
    *   Name: `storage-minio-cluster-B`
    *   Bucket: `my-bucket`
    *   Prefix: `cluster-B`
4.  由于 BSL 名称不同，两者互不干扰，Cluster-B 可以安全地读取 Cluster-A 的数据进行恢复。
