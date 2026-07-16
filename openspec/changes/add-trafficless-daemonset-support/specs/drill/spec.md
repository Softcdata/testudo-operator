## ADDED Requirements

### Requirement: Drill 必须按目标端 placement 激活并清理长明火 DaemonSet

系统必须 (MUST) 在 Drill 的资源恢复、ScaleUp 和 cleanup 生命周期中正确处理由长明火模型隔离的 DaemonSet。激活只能写入当前方向解析并冻结的目标端 placement，不得还原源端 placement。

#### Scenario: Drill ScaleUp 激活映射命名空间中的 DaemonSet
- **GIVEN** 一个 Drill 使用 namespaceMapping
- **AND** 映射目标命名空间中存在带本实例待机 selector 的 DaemonSet
- **AND** 关联 ResourceSync ConfigMap 包含该源 namespace 和 DaemonSet 的目标 placement 快照
- **WHEN** Drill 执行 ScaleUp
- **THEN** 系统必须 (MUST) 在映射目标命名空间写入快照中的目标 nodeSelector 和其他 placement 字段
- **AND** 必须 (MUST NOT) 使用源集群的 nodeSelector、affinity、tolerations 或 schedulerName 替代目标值

#### Scenario: Drill target placement 缺失时不冒险激活
- **GIVEN** Drill 目标命名空间中的 DaemonSet 带有本系统待机 selector
- **AND** 关联 ResourceSync ConfigMap 缺少该 DaemonSet 的有效目标 placement 快照
- **WHEN** Drill 执行 ScaleUp 或 cleanup
- **THEN** Drill 必须 (MUST) 失败并指出缺失、过期或目标集群不匹配的 placement
- **AND** 不得 (MUST NOT) 以源端值、空 nodeSelector 或默认值替代目标配置

#### Scenario: Drill 指向基线之外的第三集群
- **GIVEN** 一个受保护 DaemonSet 的 reversible pair 只描述 DisasterConfig 的基线 source/target 两端
- **AND** Drill 的 targetCluster 不等于当前方向对应的 placement 快照 targetCluster
- **WHEN** Drill 准备恢复或激活该 DaemonSet
- **THEN** Drill 必须 (MUST) 在修改目标对象前失败并说明缺少该集群的显式 placement mapping
- **AND** 不得 (MUST NOT) 复用基线 targetValue、源端 placement 或空 selector

#### Scenario: 无映射 Drill cleanup 重新隔离 DaemonSet
- **GIVEN** 一个没有 namespaceMapping 的 Drill 已按目标 placement 激活 DaemonSet
- **WHEN** 用户触发 cleanup
- **THEN** 系统必须 (MUST) 将该 DaemonSet 恢复为本实例待机 selector
- **AND** 不得 (MUST NOT) 重新写入源端 placement
- **AND** 不得 (MUST NOT) 让 cleanup 后的 DaemonSet 继续调度业务 Pod

#### Scenario: Failover 就绪检查识别目标 DaemonSet 收敛
- **GIVEN** Failover 已按目标 placement 激活一个 DaemonSet
- **WHEN** Operation 执行 CheckReplicas 且等待就绪
- **THEN** 系统必须 (MUST) 检查 observedGeneration、desiredNumberScheduled、numberAvailable 和 numberUnavailable
- **AND** target placement 没有候选节点或仍存在待机 selector 时必须 (MUST) 失败
- **AND** 不得 (MUST NOT) 用源集群的 DaemonSet 节点数量作为目标集群固定期望副本数
