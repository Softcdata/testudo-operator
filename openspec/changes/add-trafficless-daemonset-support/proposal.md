# Change: 为长明火恢复链路补齐 DaemonSet 支持

## Why

当前长明火（Trafficless）恢复链路把 Deployment 和 StatefulSet 作为可待机、可激活的工作负载处理：资源恢复阶段把副本数置零，切换或演练时再恢复原副本数。DaemonSet 没有 spec.replicas，现有资源恢复骨架规则、原始运行状态记录、ScaleDown、ScaleUp 和就绪检查均未覆盖它。

更关键的是，源端和目标端的节点并不等价。节点名、角色标签、可用区、污点和 nodeAffinity 可能完全不同，因此把源 DaemonSet 的 nodeSelector 原样恢复到目标集群是错误的：它会造成目标端永久 Pending，或在清空选择器后错误地运行在所有节点上。控制器不能从两个集群的标签中自动推断“等价节点”。

## What Changes

- 为 ResourceSync 的长明火待机模式增加 DaemonSet 隔离：资源恢复后的 DaemonSet 使用受控、不可匹配任何节点的待机 nodeSelector，不能在待机侧承载业务流量。
- 复用现有 DisasterInstance.spec.restorePolicy.modifierRules 的 reversible pair，显式描述每个受保护 DaemonSet 在配置基线两端的 placement。至少覆盖 /spec/template/spec/nodeSelector；源端存在 affinity、tolerations、topologySpreadConstraints 或 schedulerName 时，必须为相应字段显式声明目标值或显式清除值。
- ResourceSync 在同步时校验源端 placement 与 pair.sourceValue 一致，按当前方向解析 pair.targetValue，并在 ConfigMap 中保存版本化的目标 placement 快照。快照保存的是目标端期望值，绝不把源端 selector 当成目标端真值。
- 待机资源恢复中，系统待机 selector 以 system-protect 优先级覆盖用户的目标 nodeSelector；用户的目标 placement 只用于后续激活。Failover、Drill、Undo、Cancel 和 Reprotect 的激活路径只应用目标 placement 快照。
- 未找到唯一的 DaemonSet target placement 规则、源端值与规则漂移、目标 placement 不匹配任何节点，或快照缺失时必须失败关闭。系统不得自动复制源端 selector、清空约束或默认调度到全部目标节点。
- 现有 reversible pair 只表达 DisasterConfig 的基线 source/target 两端。Drill 指向第三个集群时，若其不是当前方向对应的快照 targetCluster，涉及 DaemonSet 的恢复、激活和 cleanup 必须失败关闭；不得把基线 target placement 套用到第三集群。
- 将 DaemonSet 加入源端停机、目标激活、Undo、Cancel、Reprotect、无 namespaceMapping Drill cleanup，以及 Drill ScaleUp 的公共工作负载生命周期。
- 将 DaemonSet 加入 Failover 的就绪判定，按 observedGeneration、desiredNumberScheduled、numberAvailable 和 numberUnavailable 验证；目标 placement 解析为零候选节点时不得把 desiredNumberScheduled=0 误判为成功。
- 明确数据恢复仍只恢复 Pods/PVC/PV：DaemonSet 所属临时 Pod 必须沿用 Trafficless 的去标签、解除 ownerReferences、调度约束清理、运行时镜像和清理语义，不恢复 DaemonSet 控制器对象。
- 与当前动态镜像改写链路联审 DaemonSet 的 containers/initContainers 覆盖，确保目标 placement 激活后不会遗漏目标仓库镜像规则。

## Non-Goals

- 不改变 Deployment 和 StatefulSet 现有的 spec.replicas 置零与恢复逻辑。
- 不根据节点名称、标签名称、可用区名称或节点数量猜测跨集群拓扑映射。
- 不在本变更中引入多目标集群的 DaemonSet placement DSL；第三集群 Drill 的映射需要独立提案。
- 不把 DaemonSet 转换为 Deployment、StatefulSet 或一次性 Job。
- 不修改 DataSync 的备份范围、PVC/PV 恢复语义、Trafficless 镜像解析或 Drill 的两阶段确认模型。
- 不新增 DaemonSet 专用 CRD 字段或另一套拓扑 DSL；复用现有可逆 modifier rules。
- 不自动处理集群级 DaemonSet、kube-system 或不在 DisasterInstance namespaces/labelSelector 范围内的资源。

## Impact

- Affected specs:
  - restore-builder
  - drill
  - restore-modifier
- Affected code at implementation time:
  - internal/controller/restore/builder.go
  - internal/controller/restore/modifier_engine.go
  - internal/controller/resourcesync/controller.go
  - internal/controller/disasteroperation/controller.go
  - internal/controller/disasteroperation/controller_check.go
  - internal/controller/*/*_test.go
- External contract:
  - 不新增 CRD、Server 或 Web API。
  - 已有 restorePolicy.modifierRules 成为 DaemonSet 跨集群 placement 的显式输入；运维侧需要为受保护 DaemonSet 配置双向 pair。
  - ResourceSync 副本记录 ConfigMap 增加向后兼容的 daemonset-placement-v1 数据键。

## Safety Boundary

- 源端 placement 只能作为 pair.sourceValue 的校验输入，绝不能被复制到目标端。
- 目标 placement 必须由当前角色方向解析的 pair.targetValue 产生，并先对目标节点进行候选校验。
- 目标 placement 快照中的 targetCluster 必须与实际激活或 Drill 目标相同；不匹配时不得激活 DaemonSet。
- 系统待机 selector 仅用于压住待机 DaemonSet，必须在 ResourceSync restore 中压过用户 target nodeSelector；激活时只移除系统 marker 并写入已冻结的目标 placement。
- 若用户没有为一个受保护 DaemonSet 显式声明目标 placement，系统必须拒绝该 DaemonSet 的长明火保护，而不是选择隐式的空 selector。
- 命名空间映射 Drill 的资源由命名空间删除清理；没有映射的 Drill cleanup 必须重新写入待机 marker，但不得回写源端 placement。
