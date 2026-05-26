# Design: Instance Role Drift Condition

## 关键决策
- 主状态载体固定为 `status.conditions`
- 只在稳态下评估 role drift
- `Failover/Reprotect/Undo/Drill` 执行窗口内不持续报 drift
- 若稳态评估确认真实主备关系与期望发生不可安全解释的不一致，`DisasterInstance` 必须进入 `Failed`，并写入 `reason=RoleDriftDetected`
- 双活是平台支持的显式运行形态；仅观测到两侧副本均非 0 时不得将实例置为 `Failed`
- role drift 巡检只负责报错和阻断，不自动修正 `status.primaryCluster/status.secondaryCluster`

## 期望关系来源
- `expectedPrimary = DisasterInstance.status.primaryCluster`
- `expectedSecondary = DisasterInstance.status.secondaryCluster`
- 禁止仅以 `DisasterConfig.spec.sourceCluster/targetCluster` 推导期望关系；Failover/Reprotect 后的期望关系必须以实例 status 中的当前角色为准。

## 真实关系采样
- 首版仅采样 `deployments.apps` 与 `statefulsets.apps`，与现有 standby scale-to-zero 执行面保持一致。
- 采样范围必须受 `DisasterInstance.spec.namespaces` 与 `DisasterInstance.spec.labelSelector` 限制。
- 以 workload `spec.replicas` 判定真实角色；`spec.replicas` 为空时按 Kubernetes 默认值 1 处理。
- 每个集群计算：
  - `workloadCount`
  - `nonZeroReplicaWorkloadCount`
  - `zeroReplicaWorkloadCount`
  - `desiredReplicasTotal`

## 真实角色分类
- `Active`：存在至少一个被纳入采样的 Deployment/StatefulSet，且其期望副本数大于 0。
- `Standby`：存在被纳入采样的 Deployment/StatefulSet，且所有期望副本数均为 0。
- `Unknown`：无可采样 workload，或远端集群不可达，或 list 失败。

## 判定矩阵
| expectedPrimary 真实角色 | expectedSecondary 真实角色 | RoleDrift condition | 实例状态 | reason | 语义 |
| --- | --- | --- | --- | --- | --- |
| Active | Standby | False | 保持当前稳态 | ExpectedRoleMatched | 真实主备与期望一致 |
| Standby | Active | True | Failed | RoleReversed | 真实主备与 CRD 期望相反 |
| Active | Active | False 或 Unknown | 保持当前稳态 | BothActiveObserved/DualActiveAllowed | 两边都未缩 0，但双活可能是显式允许形态，不作为硬错误 |
| Standby | Standby | True | Failed | BothStandby | 两边都缩 0，没有活跃业务面 |
| Unknown | 任意 | Unknown | 保持当前稳态 | CheckFailed/NoWorkloadObserved | 无法可靠判断，不因观测失败直接置 Failed |
| 任意 | Unknown | Unknown | 保持当前稳态 | CheckFailed/NoWorkloadObserved | 无法可靠判断，不因观测失败直接置 Failed |

## 实例错误语义
- 在 `Protected` 或 `Active` 中发现不可安全解释的 `RoleDrift=True` 时：
  - `status.fsmState = Failed`
  - `status.reason = RoleDriftDetected`
  - `status.message` 必须包含判定 reason、期望主备、真实副本摘要。
  - `status.availableOperations` 必须移除 failover/reprotect/undo/synconce/syncdata/syncresource 等会改变运行期语义的操作。
  - `status.conditions[type=RoleDrift]` 必须保留具体漂移 reason，例如 `BothStandby`、`RoleReversed`。
- `RoleDrift=Unknown` 不得直接置 Failed，避免集群短暂不可达导致误停；但必须写入 condition 供 server/web 展示。
- `BothActiveObserved` 不得直接置 Failed。它必须作为可观测信号保留，供 server/web 展示“双活观测”或“显式双活运行中”，但不阻断实例状态机。

## 双活语义
- Failover 若通过显式参数跳过源集群缩零，完成后可能出现当前主与当前备都存在非 0 副本的合法状态。
- Drill 执行期间会在演练目标侧短暂拉起资源，也不得污染实例级 role drift 错误。
- 因此采样器不能使用“两侧非 0”反推真实主备，也不能将其作为错误；只能记录 `BothActiveObserved`，并在 message 中说明该观测无法证明真实主是谁。
- 若后续需要区分“显式允许双活”和“未知原因双活”，应引入可审计的双活许可来源，例如最近一次完成的 failover `skipScaleDownSource=true`、实例策略字段或专门的运行时标记。

## 恢复语义
- 当实例因 `reason=RoleDriftDetected` 进入 `Failed` 后，控制器必须继续按受限频率复检真实关系。
- 若复检结果恢复为 `RoleDrift=False`，实例可以自动清除错误并回到由当前 `status.primaryCluster/status.secondaryCluster` 推导出的稳态：
  - 当前主等于基础配置 target 且当前备等于基础配置 source -> `Active`
  - 其他情况 -> `Protected`
- 恢复时只清理 `reason/message` 与可用操作，禁止改写 `primaryCluster/secondaryCluster`。
- 若复检仍为 `RoleDrift=True`，保持 `Failed` 并刷新错误 message。

## 性能边界
- 若判定需要扫描 workload，必须限定扫描范围并控制重算频率
- 不允许在每次 reconcile 都无界遍历整个集群
- 建议默认巡检间隔为 5 分钟；状态刚进入 `Protected/Active`、实例 generation 变化、主备字段变化时允许立即巡检一次。
