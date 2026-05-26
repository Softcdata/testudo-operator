# Proposal: ResourceSync 主链路承接精细化控制并拆分 cluster/namespaced 恢复

## Why

`add-scoped-resource-selection-filters` 已经完成了 `DisasterInstance.spec.restorePolicy.resourceSelection` 的 scoped 字段模型、恢复侧映射和提交期校验，但 `ResourceSync` 主链路还存在两个缺口：

1. `ResourceSync.buildAppBackupSpec` 仍然使用固定模板，没有承接实例上的 scoped 资源选择，导致 `AppBackup` 可能继续按默认 Velero 相关资源语义把 cluster-scoped 资源带进备份。
2. `ResourceSync` 恢复仍然是单次 `AppRestore`，且统一使用 `ExistingResourcePolicy=update`。当备份中包含用户想要容灾的 cluster-scoped 资源时，这条单 restore 会把 cluster-scoped 资源和 namespaced 资源一起覆盖到目标集群，存在误覆盖从集群 cluster 资源的风险。

用户当前诉求不是重新设计 restorePolicy，而是让“精细化控制”真正闭环到 ResourceSync 主链路，并在 scoped cluster 资源被显式选中时，将 cluster 恢复单独执行，且使用 `none` 而不是 `update`。

## What Changes

### 1. ResourceSync AppBackup 承接 scoped 精细化控制

当 `DisasterInstance.spec.restorePolicy.resourceSelection` 进入 scoped 模式时，`ResourceSync.buildAppBackupSpec` 必须把 scoped 资源选择投影到 `AppBackup.Spec.Template`：

- 继承 scoped namespace 资源过滤。
- 继承 scoped cluster 资源过滤。
- 继续保留 `pods`、`persistentvolumeclaims`、`persistentvolumes` 的 ResourceSync 固定排除语义。
- 当未显式选择 `includedClusterScopedResources` 时，默认不把 cluster-scoped 资源带入 ResourceSync 备份，避免“相关 cluster 资源”被隐式带入。

### 2. ResourceSync 拆分 cluster/namespaced 两阶段恢复

当 scoped 模式下显式选择了 `includedClusterScopedResources` 时，`ResourceSync` 必须将恢复拆成两个顺序阶段：

1. **Cluster phase**
   - 只恢复显式选中的 cluster-scoped kinds
   - `ExistingResourcePolicy=none`
2. **Namespaced phase**
   - 恢复 namespaced 资源
   - `ExistingResourcePolicy=update`
   - 保持现有 skeleton / image rewrite / restore modifier 逻辑

若未显式选择 cluster-scoped kinds，则跳过 cluster phase，只执行 namespaced phase。

### 3. ResourceSync 状态增加分阶段可观测性

`ResourceSyncStatus` 需要补齐 cluster/namespaced phase 的 restore 观测字段，便于区分失败点并支撑上层展示。

## Scope

- `internal/controller/resourcesync/*`
- `pkg/apis/disaster/v1/resourcesync_types.go`
- `config/crd/bases/testudo.softcdata.com_resourcesyncs.yaml`
- `openspec/changes/resourcesync-two-phase-restore/*`

## Non-Goals

- 不修改 DataSync 的执行模型。
- 不把旧模型 `includeClusterResources=true` 改造成分阶段恢复；本次只处理 scoped 精细化控制路径。
- 不引入对象级 cluster 资源精确恢复；仍以 kind 级过滤为主。

## Impact

### 用户可见行为

- 选择 scoped 精细化控制后，ResourceSync 的备份范围会与实例策略保持一致，不再隐式把未显式选择的 cluster-scoped 资源带入主链路。
- 显式选择 cluster-scoped kinds 时，cluster 恢复与 namespaced 恢复将分阶段执行，cluster 侧使用 `none`，避免覆盖已有 cluster 资源。

### 实现影响

- 需要扩展 ResourceSync 的 AppBackup 模板生成逻辑。
- 需要扩展 ResourceSync 的状态字段和恢复状态机。
- 需要补齐单元测试、CRD 产物和 OpenSpec delta。
