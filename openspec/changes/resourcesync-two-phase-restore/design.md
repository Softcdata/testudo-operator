# Design: ResourceSync scoped backup projection and phased restore

## 背景

当前 ResourceSync 的事实实现是：

- Backup 侧使用固定 `AppBackup.Template`，只按实例命名空间和标签过滤，未承接 scoped 精细化控制。
- Restore 侧会读取 `DisasterInstance.restorePolicy`，但执行仍是单次 restore。
- `velerov1.RestoreSpec` 本身不支持 backup 侧那套 scoped 字段，因此 cluster/namespaced 恢复必须依赖“显式 included cluster kinds + 分阶段 restore”来落地。

## 设计目标

1. 在 ResourceSync 主链路真正承接 scoped 精细化控制。
2. 保持 ResourceSync 对 `pods/persistentvolumeclaims/persistentvolumes` 的固定排除语义。
3. 只在 scoped 且显式选择 cluster kinds 时引入 cluster phase，避免扩大旧模型行为面。
4. 保持现有 namespaced 恢复的 skeleton、image rewrite、modifier engine 行为。

## Decision 1: 复用既有 `resourcesync-two-phase-restore` change-id

该 change-id 当前只有一份未实施的早期草案，尚未产生代码或 spec 归档结果。本次直接将其升级为当前真实需求，避免引入新的重复 change-id。

## Decision 2: ResourceSync 的 cluster 资源采用“显式 included 才启用”

由于 Velero `RestoreSpec` 没有 scoped restore 字段，若只给出 `excludedClusterScopedResources` 而没有 `includedClusterScopedResources`，系统无法在单份混合 backup 中安全地只恢复 cluster-scoped 资源。

因此本次执行模型采用保守规则：

- scoped 模式下，只有 `includedClusterScopedResources` 非空时，才为 ResourceSync 主链路备份/恢复 cluster-scoped 资源。
- 若 `includedClusterScopedResources` 为空，则 ResourceSync 主链路不备份 cluster-scoped 资源，也不进入 cluster restore phase。

这条规则保证 ResourceSync 的 cluster 资源进入主链路是显式 opt-in，而不是隐式副作用。

## Decision 3: Backup projection 只在 ResourceSync scoped 模式生效

旧模型 `includeClusterResources=true` 继续沿用当前兼容语义，不在本次引入新执行模型。这样可以把行为变化严格限制在“精细化控制”路径。

## 执行模型

### Backup projection

`ResourceSync.buildAppBackupSpec` 从现有固定模板出发，再叠加 scoped 投影：

- `IncludedNamespaces`：若 scoped 选择显式给出 included namespaces，则覆盖默认实例命名空间；否则保持 `instance.spec.namespaces`
- `ExcludedNamespaces`：合并系统固定排除 (`velero`, `kube-system`) 与 scoped excludes
- `LabelSelector`：若 scoped 选择给出，则覆盖默认实例 label selector
- `IncludedNamespaceScopedResources` / `ExcludedNamespaceScopedResources`：承接 scoped namespaced kinds，并始终补入 ResourceSync 固定排除的 `pods/persistentvolumeclaims/persistentvolumes`
- `IncludedClusterScopedResources` / `ExcludedClusterScopedResources`：
  - 当 `includedClusterScopedResources` 非空时，按实例策略透传
  - 否则固定写入 `ExcludedClusterScopedResources=["*"]`

### Restore phases

#### Cluster phase

- 启动条件：scoped 模式且 `includedClusterScopedResources` 非空
- RestoreSpec 关键设置：
  - `IncludedResources = includedClusterScopedResources`
  - `ExcludedResources = excludedClusterScopedResources`
  - `IncludeClusterResources = true`
  - `ExistingResourcePolicy = none`
- 不引入 image rewrite 系统规则

#### Namespaced phase

- scoped 模式：
  - `IncludedResources = includedNamespaceScopedResources`（空表示恢复所有 namespaced resources）
  - `ExcludedResources = excludedNamespaceScopedResources + [pods,persistentvolumeclaims,persistentvolumes]`
  - `IncludeClusterResources = false`
  - `ExistingResourcePolicy = update`
- 非 scoped 模式：
  - 继续沿用现有单 restore 路径
  - 但补齐 ResourceSync 的固定排除语义，避免 restorePolicy 覆盖后重新带入 `pods/persistentvolumeclaims/persistentvolumes`

### Status 扩展

`ResourceSyncStatus` 新增：

- `lastClusterRestoreName`
- `clusterRestoreStatus`
- `lastNamespaceRestoreName`
- `namespaceRestoreStatus`

兼容字段 `lastRestoreName` 保留，并始终写入“最后一次创建或观测到的 restore 名称”。

## 风险与缓解

### 风险 1：scoped exclude-only cluster 配置无法按预期分阶段执行

- 缓解：将 ResourceSync cluster 资源主链路改为显式 opt-in，只对 `includedClusterScopedResources` 生效。

### 风险 2：restorePolicy 覆盖 ResourceSync 固定排除语义

- 缓解：在 namespaced/legacy restore 构建后，统一回补 `pods/persistentvolumeclaims/persistentvolumes` 到 `ExcludedResources`，并从 `IncludedResources` 中剔除。

### 风险 3：状态机分支增多导致历史记录和统计不一致

- 缓解：只在整个同步回合结束时写一条 `SyncHistoryRecord`，并以最终阶段结果作为该回合结果；restore 资源数累计 cluster/namespaced 两阶段已恢复条目。
