## ADDED Requirements

### Requirement: Drill data restore 专用 Trafficless Pod 必须清理源端节点调度约束

DisasterOperation 在构建 Drill data restore AppRestore 时，必须 (MUST) 仅对 Drill data restore 的 Trafficless 临时 Pod 清理源端节点调度约束。该行为不得 (MUST NOT) 改变 ResourceSync、shared restore builder 默认 Trafficless modifier、Failover ScaleUpTarget 或业务工作负载模板的恢复语义。

#### Scenario: Drill 清理临时 Pod nodeName
- **GIVEN** 源集群备份 Pod 包含 `spec.nodeName`
- **WHEN** DisasterOperation 构建 Drill data restore AppRestore
- **THEN** Drill 生成的 pods ResourceModifier 必须清理 `spec.nodeName`
- **AND** 清理只作用于恢复出来的 Trafficless 临时 Pod

#### Scenario: Drill 清理临时 Pod nodeSelector
- **GIVEN** 源集群备份 Pod 包含 `spec.nodeSelector`
- **WHEN** DisasterOperation 构建 Drill data restore AppRestore
- **THEN** Drill 生成的 pods ResourceModifier 必须清理 `spec.nodeSelector`
- **AND** 必须保留 Trafficless labels、ownerReferences 清空、镜像替换、command/args 覆盖和 pull secret 注入语义

#### Scenario: Drill 清理临时 Pod affinity
- **GIVEN** 源集群备份 Pod 包含 `spec.affinity.nodeAffinity`
- **WHEN** DisasterOperation 构建 Drill data restore AppRestore
- **THEN** Drill 生成的 pods ResourceModifier 必须清空 `spec.affinity`
- **AND** 该清理仅作用于 Drill data restore Trafficless 临时 Pod，不得修改业务 workload 模板
- **AND** 系统不得为了精确匹配 `nodeAffinity` 而新增 AppRestore Conditions schema 或备份期动态扫描

#### Scenario: Drill target registry busybox 与调度清理共存
- **GIVEN** 演练目标集群配置了 `veleroInstall.imageRegistry` 和 registry credential
- **WHEN** DisasterOperation 构建 Drill data restore AppRestore
- **THEN** Trafficless Pod 镜像必须继续按演练目标集群解析
- **AND** pods ResourceModifier 必须继续注入 `/spec/imagePullSecrets`
- **AND** 同一个 pods ResourceModifier 必须包含 `nodeName`、`nodeSelector` 和 `affinity` 清理 patch

#### Scenario: Shared builder 不继承 Drill 调度清理
- **GIVEN** 其他调用方直接使用 shared restore builder 构建 data restore AppRestore
- **WHEN** 调用方没有显式传入 Drill data restore 专用 modifier
- **THEN** shared restore builder 默认 Trafficless modifier 不得包含 `nodeName`、`nodeSelector` 或 `affinity` 清理 patch

#### Scenario: ResourceSync 和 Failover 业务扩容不受影响
- **GIVEN** ResourceSync 执行 resource restore 或 DisasterOperation 执行 Failover ScaleUpTarget
- **WHEN** 应用本变更
- **THEN** ResourceSync ResourceModifierRules 不得新增 Drill data restore 调度清理规则
- **AND** Failover ScaleUpTarget 不得因为本规则修改业务工作负载模板的调度约束
