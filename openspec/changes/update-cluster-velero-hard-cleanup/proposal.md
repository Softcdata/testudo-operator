# Change: 集群删除时彻底清理 Velero 残留

## Why

当前 `Cluster` 删除路径在 `testudo.softcdata.com/uninstall-velero=true` 场景下，仅执行 `helm uninstall velero`。
在实际运行中会遗留以下资源：

- `velero` 命名空间中的历史 Velero CR（`Backup`、`BackupRepository`、`BackupStorageLocation` 等）
- `velero.io` CRD
- Helm Hook 遗留的集群级 RBAC（如 `velero-upgrade-crds`）

这会导致“集群记录已删除但目标集群仍残留 Velero 资源”的结果，不符合“清理 Velero”的用户预期。

## Goals

- 当 `Cluster` 删除且注解 `testudo.softcdata.com/uninstall-velero=true` 时，Operator 必须执行**彻底清理**。
- 清理范围至少包括：
  - Velero Helm release（`helm uninstall velero`）
  - Velero CR 实例（`velero.io` 资源）
  - `velero` 命名空间
  - `*.velero.io` CRD
  - `velero*` 相关集群级 RBAC
- 清理过程必须具备幂等性：重复执行不应报错中断（NotFound/NoMatch 场景可忽略）。

## Non-Goals

- 不改变 `uninstallVelero` 的开关协议（仍由 Cluster 注解控制）。
- 不在本次变更中引入新的 API 字段或 CRD 字段。
- 不覆盖非 Velero 组件的第三方资源清理。

## What Changes

1. 扩展 `ClusterReconciler.uninstallVelero`：
   - 保留 Helm 卸载步骤；
   - 在 Helm 卸载后进入彻底清理阶段。
2. 新增 Velero 残留清理逻辑：
   - 删除 namespaced Velero CR，必要时先移除 finalizer；
   - 删除 `velero` namespace；
   - 删除 `*.velero.io` CRD；
   - 删除名称包含 `velero` 的 `ClusterRole/ClusterRoleBinding`。
3. 增加测试：
   - 覆盖 `release not found` 仍继续清理场景；
   - 覆盖 CR/CRD/RBAC 清理成功场景。

## Impact

- 用户在勾选“清理 Velero”删除集群后，可得到可验证的“彻底清理”结果。
- 清理失败时，`Cluster` 继续保持删除中并重试（维持现有 Finalizer 语义）。
