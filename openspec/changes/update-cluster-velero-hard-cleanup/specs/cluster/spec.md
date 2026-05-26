## ADDED Requirements

### Requirement: Cluster 删除时的 Velero 彻底清理

当 `Cluster` 进入删除流程且显式开启 `uninstall-velero` 注解时，Operator 必须 (MUST) 执行 Velero 的彻底清理，而不仅是 Helm release 卸载。

#### Scenario: 卸载注解开启时执行两阶段清理
- **Given** `Cluster` 资源带有 `testudo.softcdata.com/uninstall-velero=true`
- **And** `Cluster` 正在删除（`deletionTimestamp` 已设置）
- **When** `ClusterReconciler` 执行删除分支
- **Then** 必须先执行 `helm uninstall velero`
- **And** 必须继续执行 Velero 残留资源清理（CR/CRD/RBAC/namespace）

#### Scenario: release not found 仍继续清理残留
- **Given** 目标集群不存在 Helm release `velero`
- **When** Operator 执行 Velero 卸载步骤
- **Then** 不应 (SHOULD NOT) 直接结束流程
- **And** 必须继续执行残留资源清理

#### Scenario: 清理范围包含 CR、CRD、RBAC 与命名空间
- **Given** 目标集群存在历史 Velero 资源残留
- **When** Operator 执行彻底清理
- **Then** 必须删除 `velero.io` 资源实例
- **And** 必须删除 `*.velero.io` CRD
- **And** 必须删除名称匹配 `velero` 的 `ClusterRole/ClusterRoleBinding`
- **And** 必须删除 `velero` 命名空间

#### Scenario: 缺失资源类型时保持幂等
- **Given** 目标集群部分 Velero 资源类型已不存在（例如 CRD 已先被移除）
- **When** Operator 执行彻底清理
- **Then** 对 `NotFound`/`NoMatch` 结果应按幂等处理
- **And** 不应 (SHOULD NOT) 因该类结果中断删除流程
