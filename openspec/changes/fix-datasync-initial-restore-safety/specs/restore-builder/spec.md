## ADDED Requirements

### Requirement: DataSync 专用 Trafficless Pod 必须清理源端节点调度约束
DataSync 控制器在构建自身 data restore AppRestore 时，必须 (MUST) 仅对 DataSync trafficless 临时 Pod 清理源端节点调度约束。该行为不得 (MUST NOT) 改变 ResourceSync、Drill 或业务工作负载模板的恢复语义。

#### Scenario: DataSync 清理临时 Pod nodeName
- **GIVEN** 源集群备份 Pod 包含 `spec.nodeName`
- **WHEN** DataSync 构建 data restore AppRestore
- **THEN** DataSync 生成的 pods ResourceModifier 必须清理 `spec.nodeName`
- **AND** 清理只作用于恢复出来的 trafficless 临时 Pod

#### Scenario: DataSync 清理临时 Pod nodeSelector
- **GIVEN** 源集群备份 Pod 包含 `spec.nodeSelector`
- **WHEN** DataSync 构建 data restore AppRestore
- **THEN** DataSync 生成的 pods ResourceModifier 必须清理 `spec.nodeSelector`
- **AND** 必须保留 trafficless labels、ownerReferences 清空、镜像替换、command/args 覆盖等既有语义

#### Scenario: DataSync 清理临时 Pod affinity
- **GIVEN** 源集群备份 Pod 包含 `spec.affinity.nodeAffinity`
- **WHEN** DataSync 构建 data restore AppRestore
- **THEN** DataSync 生成的 pods ResourceModifier 必须清空 `spec.affinity`
- **AND** 该清理仅作用于 DataSync trafficless 临时 Pod，不得修改业务 workload 模板
- **AND** 系统不得为了精确匹配 `nodeAffinity` 而新增 AppRestore Conditions schema 或备份期动态扫描

#### Scenario: ResourceSync 不受 DataSync 调度清理影响
- **GIVEN** ResourceSync 构建 resource restore AppRestore
- **WHEN** 应用本变更
- **THEN** ResourceSync 的 ResourceModifierRules 不得新增 DataSync trafficless 调度清理规则
- **AND** ResourceSync 的 Scale-to-Zero、资源选择和 restorePolicy 编译语义必须保持不变

#### Scenario: Drill data restore 不受 DataSync 调度清理影响
- **GIVEN** DisasterOperation 执行 Drill data restore
- **WHEN** 应用本变更
- **THEN** shared restore builder 默认 trafficless modifier 不得因本变更改变
- **AND** Drill data restore 的 ResourceModifierRules 不得新增本提案定义的 DataSync 专用调度清理规则

#### Scenario: Failover 扩容不受 DataSync 调度清理影响
- **GIVEN** DisasterOperation 执行 Failover
- **AND** FinalSync 触发了 DataSync
- **WHEN** 后续执行 ScaleUpTarget
- **THEN** ScaleUpTarget 必须继续只更新目标集群业务工作负载的副本数
- **AND** 不得因为 DataSync trafficless 临时 Pod 调度清理而修改业务工作负载模板的调度约束
