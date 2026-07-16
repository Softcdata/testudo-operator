## ADDED Requirements

### Requirement: ResourceSync 长明火待机必须使用显式的 DaemonSet 目标端 placement

系统必须 (MUST) 将处于 ResourceSync 保护范围内的 DaemonSet 纳入长明火待机模型。源端 placement 不得被直接恢复到目标集群；系统必须从可逆 RestorePolicy modifier pair 解析目标端 placement，并在待机期间使用系统隔离 selector。

#### Scenario: 源端和目标端 nodeSelector 不同
- **GIVEN** 源 DaemonSet 的 nodeSelector 为 source-role=edge
- **AND** 该 DaemonSet 的 reversible pair 为 source-role=edge 到 dr.testudo.io/node-pool=agent
- **WHEN** ResourceSync 构建到目标集群的资源恢复
- **THEN** 系统必须 (MUST) 将 dr.testudo.io/node-pool=agent 解析为该 DaemonSet 的目标 placement
- **AND** 必须 (MUST NOT) 将 source-role=edge 作为目标激活值保存或恢复
- **AND** 待机 AppRestore 必须 (MUST) 仍使用系统待机 selector 而不是目标 placement 立即调度业务 Pod

#### Scenario: DaemonSet 缺少目标端 nodeSelector mapping
- **GIVEN** 一个受 DisasterInstance namespaces 和 labelSelector 选择的 DaemonSet
- **AND** 不存在唯一的 daemonsets.apps reversible nodeSelector pair
- **WHEN** ResourceSync 准备长明火资源恢复
- **THEN** ResourceSync 必须 (MUST) 失败并指出缺失或冲突的 DaemonSet placement rule
- **AND** 不得 (MUST NOT) 复制源 nodeSelector、清空 nodeSelector 或默认调度到全部目标节点

#### Scenario: 源端 placement 与 mapping 漂移
- **GIVEN** DaemonSet 已配置 target placement pair
- **AND** 当前源 DaemonSet 的 nodeSelector 或其他非空 placement 字段与对应 sourceValue 不一致
- **WHEN** ResourceSync 执行 placement 预检
- **THEN** 系统必须 (MUST) 失败关闭并报告漂移字段
- **AND** 不得 (MUST NOT) 根据陈旧 mapping 写入 target placement 快照

#### Scenario: 目标端没有候选节点
- **GIVEN** DaemonSet 的 target nodeSelector、required nodeAffinity 和 tolerations 已被解析
- **AND** 目标集群没有任何节点匹配这些静态约束
- **WHEN** ResourceSync 准备 DaemonSet 长明火恢复
- **THEN** ResourceSync 必须 (MUST) 前置失败
- **AND** 不得 (MUST NOT) 将目标 DaemonSet 视为 desiredNumberScheduled=0 的成功状态

#### Scenario: 目标节点被未容忍的调度污点排除
- **GIVEN** 目标节点满足 DaemonSet 的 nodeSelector 和 required nodeAffinity
- **AND** 该节点带有未被目标 tolerations 容忍的 NoSchedule 或 NoExecute taint
- **WHEN** ResourceSync 执行 DaemonSet placement 预检
- **THEN** 系统必须 (MUST) 将该节点排除出静态候选集合
- **AND** 若没有其他候选节点，必须 (MUST) 在恢复前失败关闭

### Requirement: 长明火数据恢复必须安全处理 DaemonSet 所属 Pod

系统必须 (MUST) 继续以 Pod 级 Trafficless 规则处理 DaemonSet 所属的备份 Pod。数据恢复不得恢复 DaemonSet 控制器对象，也不得让临时数据恢复 Pod 被 DaemonSet 接管。

#### Scenario: DaemonSet 所属 Pod 恢复为 Trafficless 临时 Pod
- **GIVEN** DataSync 数据备份包含一个 ownerReferences 指向 DaemonSet 的 Pod
- **WHEN** 系统构建 data restore AppRestore
- **THEN** pods ResourceModifier 必须 (MUST) 清空该 Pod 的 ownerReferences
- **AND** 必须 (MUST) 仅保留 Trafficless 标签并清理源端调度约束
- **AND** 必须 (MUST) 使用当前目标集群解析的 Trafficless 运行时镜像和凭据
- **AND** 不得 (MUST NOT) 将 daemonsets.apps 加入数据恢复的 IncludedResources
