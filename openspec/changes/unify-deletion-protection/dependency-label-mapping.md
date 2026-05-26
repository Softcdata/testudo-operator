# 矩阵到依赖标签写入映射（v1）

## 1. 目的

本文件将 `docs/platform-resource-dependency-audit.md` 的“模块真实调用依赖矩阵”转为可执行的标签写入规则，作为 `unify-deletion-protection` 的实现输入。

约定：

- 依赖边定义为 `A -> B`，表示资源 `A` 依赖资源 `B`。
- 写入位置在 `A` 自身标签：`testudo.softcdata.com/dependency-to-<token(B)>=<relation-code>`。
- 每个资源都写 `testudo.softcdata.com/dependency-token=<token(A)>`。
- 旧业务标签（如 `LabelAppBackupCluster`、`LabelDisasterPolicyName`）保持不变，仅新增通用依赖标签。

## 2. 通用协议

- `dependency-token`：`sha256(uid)` 前 16 位十六进制。
- `dependency-to-<token>`：一条下游依赖边。
- `relation-code`：短码（<=63），来源于具体字段或标签语义。
- 同步策略：覆盖式重建（先清理所有旧 `dependency-to-*`，再按当前关系写回）。

## 3. v1 目标模块映射

| 源模块（写入方） | relation-code | 下游目标模块 | 来源字段/标签 | 说明 |
|---|---|---|---|---|
| Cluster | - | - | - | 仅写 `dependency-token`，无固定下游边 |
| StorageRepository | - | - | - | 仅写 `dependency-token`，无固定下游边 |
| DisasterPolicy | `label.storageRepositoryName` | StorageRepository | `labels[testudo.softcdata.com/storage-repository-name]` | 兼容旧链路；仅在标签存在时写边 |
| DisasterConfig | `spec.sourceCluster` | Cluster | `spec.sourceCluster` | Config 指向源集群 |
| DisasterConfig | `spec.targetCluster` | Cluster | `spec.targetCluster` | Config 指向目标集群 |
| DisasterConfig | `spec.storageRepository` | StorageRepository | `spec.storageRepository` | Config 指向存储 |
| DisasterConfig | `spec.dataSyncPolicy` | DisasterPolicy | `spec.dataSyncPolicy` | Config 绑定数据同步策略 |
| DisasterConfig | `spec.resourceSyncPolicy` | DisasterPolicy | `spec.resourceSyncPolicy` | Config 绑定资源同步策略 |
| DisasterInstance | `spec.config` | DisasterConfig | `spec.config` | Instance 依赖 Config |
| DisasterGroup | `spec.levels` | DisasterInstance | `spec.levels[*]` | Group 由实例集合组成 |
| AppBackup | `spec.cluster` | Cluster | `spec.cluster` | Backup 依赖源集群 |
| AppBackup | `spec.disasterPolicy` | DisasterPolicy | `spec.disasterPolicy` | Backup 依赖策略 |
| AppBackup | `spec.template.storageLocation` | StorageRepository | `spec.template.storageLocation` | Backup 依赖存储位置 |
| AppRestore | `spec.cluster` | Cluster | `spec.cluster` | Restore 目标集群 |
| AppRestore | `spec.sourceCluster` | Cluster | `spec.sourceCluster` | Restore 源集群 |
| AppRestore | `spec.backupSource` | AppBackup | `spec.backupSource` | Restore 依赖备份源 |
| AppRestore | `spec.storageRepository` | StorageRepository | `spec.storageRepository` | Restore 依赖存储 |
| DisasterDrill | `spec.instanceName` | DisasterInstance | `spec.instanceName` | 实例演练链路 |
| DisasterDrill | `spec.groupName` | DisasterGroup | `spec.groupName` | 组演练链路 |
| DisasterBackup | `spec.disasterConfig` | DisasterConfig | `spec.disasterConfig` | Backup 依赖 Config |

## 4. 内部来源模块映射（用于 upstream 完整性）

| 源模块（写入方） | relation-code | 下游目标模块 | 来源字段/标签 | 说明 |
|---|---|---|---|---|
| DisasterOperation | `spec.instanceName` | DisasterInstance | `spec.instanceName` | 实例级操作 |
| DisasterOperation | `spec.groupName` | DisasterGroup | `spec.groupName` | 组级操作 |
| DataSync | `spec.instance` | DisasterInstance | `spec.instance` | 实例数据同步子资源 |
| ResourceSync | `spec.instance` | DisasterInstance | `spec.instance` | 实例资源同步子资源 |
| DisasterJob | `spec.disasterBackup` | DisasterBackup | `spec.disasterBackup` | 任务依赖灾备备份 |
| DisasterJob | `label.disasterPolicyName` | DisasterPolicy | `labels[testudo.softcdata.com/disaster-policy-name]` | Policy 兼容依赖来源 |

## 5. 代码落点（当前实现）

- `pkg/metadata/labels.go`：新增协议常量。
- `pkg/metadata/dependency_labels.go`：token 生成、`dependency-to-*` 覆盖式重建。
- 各模块 controller 的 `syncLabels` 或 `syncDependencyLabels`：按本映射写入边。

## 6. 变更治理

- 若“模块真实调用依赖矩阵”新增/变更关系，本文件必须先同步。
- 本文件变更后，再修改控制器写入逻辑与检查接口查询逻辑。
- 不允许跳过矩阵与映射直接添加推测性关系。
