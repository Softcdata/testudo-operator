## MODIFIED Requirements

### Requirement: Velero 完整性检查 (MUST)

在判定集群中的 Velero 是否安装完成并可用于容灾任务时，Operator 必须 (MUST) 同时校验 `velero` Deployment 和 `node-agent` DaemonSet 的存在性与当前运行时可用性。仅当 `velero` Deployment 存在且至少 1 个副本 ready/available，并且 `node-agent` DaemonSet 存在且 ready 数量达到 desired 数量时，才视为 Velero runtime 已就绪。Pod 级诊断只能用于补充当前 active runtime Pod 的异常细节，历史终态 Pod 不得单独阻断 Cluster Ready。

#### Scenario: 检测 node-agent 缺失
- **GIVEN** 目标集群已安装 Velero Deployment
- **BUT** 缺少 `node-agent` DaemonSet
- **WHEN** Operator 执行 Velero 安装检查
- **THEN** 判定为未安装完成
- **AND** 触发 Velero 安装/修复流程

#### Scenario: velero Deployment 未就绪时阻断 Ready
- **GIVEN** 目标集群存在 `velero` Deployment
- **AND** `velero` Deployment 的 `readyReplicas < 1` 或 `availableReplicas < 1`
- **WHEN** `ClusterReconciler` 执行 Velero runtime 诊断
- **THEN** 必须将 `Cluster.status.status` 设置为 `NotReady`
- **AND** 必须将 `Cluster.status.reason` 设置为 `VeleroRuntimeNotReady`
- **AND** `Cluster.status.message` 必须包含 `deployment velero` 的 ready/available/unavailable 诊断详情

#### Scenario: node-agent DaemonSet 未就绪时阻断 Ready
- **GIVEN** 目标集群存在 `node-agent` DaemonSet
- **AND** `node-agent` DaemonSet 的 `desiredNumberScheduled > 0`
- **AND** `numberReady < desiredNumberScheduled`
- **WHEN** `ClusterReconciler` 执行 Velero runtime 诊断
- **THEN** 必须将 `Cluster.status.status` 设置为 `NotReady`
- **AND** 必须将 `Cluster.status.reason` 设置为 `VeleroRuntimeNotReady`
- **AND** `Cluster.status.message` 必须包含 `daemonset node-agent` 的 ready/desired 诊断详情

#### Scenario: 历史终态 Velero Pod 不应阻断 Cluster Ready
- **GIVEN** 目标集群 `velero` Deployment 存在且 `readyReplicas >= 1` 且 `availableReplicas >= 1`
- **AND** 目标集群 `node-agent` DaemonSet 存在且 `numberReady == desiredNumberScheduled`
- **AND** `velero` 命名空间存在历史 runtime Pod，状态为 `Succeeded`、`Failed`、`Evicted`、`ContainerStatusUnknown` 或正在删除
- **WHEN** `ClusterReconciler` 执行 Velero runtime 诊断
- **THEN** 不得仅因为这些历史终态 Pod 将 Cluster 标记为 `NotReady`
- **AND** 不得仅因为这些历史终态 Pod 将 `Cluster.status.reason` 设置为 `VeleroRuntimeNotReady`
- **AND** 阻断性诊断消息不得把这些历史终态 Pod 描述为当前 runtime 未就绪原因

#### Scenario: 当前 active Velero Pod 异常仍阻断 Ready
- **GIVEN** 目标集群存在匹配 Velero runtime 的 active Pod
- **AND** 该 Pod 处于 `Pending` 或 `Running` 但未 ready
- **AND** 该 Pod 的 initContainer 或 container waiting/terminated reason 表示当前运行时异常，例如 `ImagePullBackOff`、`CrashLoopBackOff`、`CreateContainerConfigError`
- **WHEN** `ClusterReconciler` 执行 Velero runtime 诊断
- **THEN** 必须将 `Cluster.status.status` 设置为 `NotReady`
- **AND** 必须将 `Cluster.status.reason` 设置为 `VeleroRuntimeNotReady`
- **AND** `Cluster.status.message` 必须包含该 active Pod 的名称和异常 reason

#### Scenario: 历史终态 Velero Pod 清理失败不阻断 Ready
- **GIVEN** 目标集群 `velero` Deployment 与 `node-agent` DaemonSet 当前均已就绪
- **AND** `velero` 命名空间存在历史终态 runtime Pod
- **WHEN** Operator 尝试 best-effort 清理历史终态 Pod
- **AND** 清理返回 `NotFound`、`Forbidden` 或其他删除错误
- **THEN** 清理失败本身不得将 Cluster 标记为 `NotReady`
- **AND** 清理失败本身不得将 `Cluster.status.reason` 设置为 `VeleroRuntimeNotReady`
