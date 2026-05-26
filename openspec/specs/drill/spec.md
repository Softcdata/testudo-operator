# drill Specification

## Purpose
TBD - created by archiving change design-drill-orchestration. Update Purpose after archive.
## Requirements
### Requirement: DisasterDrill CRD 

系统必须 (MUST) 提供 DisasterDrill CRD，作为用户直接操作的容灾演练资源。DisasterDrill Controller 会自动创建关联的 DisasterOperation (type=drill)。

#### Scenario: 创建 DisasterDrill 触发演练
- **Given** 一个处于 `Protected` 状态的 DisasterInstance
- **And** 存在至少一个成功的备份 (通过 DataSync/ResourceSync)
- **When** 用户创建一个 DisasterDrill 资源，包含 instanceName, targetCluster, namespaceMapping 等配置
- **Then** DisasterDrill Controller 应创建关联的 DisasterOperation (type=drill)
- **And** DisasterOperation 的 ownerReferences 应指向 DisasterDrill
- **And** DisasterDrill.Status 应同步 DisasterOperation 的状态

#### Scenario: 用户通过 DisasterDrill 确认执行
- **Given** 一个处于 `Ready` 状态的 DisasterDrill
- **When** 用户 Patch `spec.confirmed: true`
- **Then** DisasterDrill Controller 应将 confirmed 同步到关联的 DisasterOperation
- **And** DisasterOperation 应进入 `Executing` 状态

#### Scenario: 删除 DisasterDrill 级联删除
- **Given** 一个已完成的 DisasterDrill
- **When** 用户删除 DisasterDrill
- **Then** 系统应级联删除关联的 DisasterOperation

### Requirement: Drill 两阶段编排

演练必须 (MUST) 采用两阶段模式：校验阶段 (自动) 和执行阶段 (用户确认)。

#### Scenario: 成功的 Drill 演练 - 完整流程
- **Given** 一个处于 `Protected` 状态的 DisasterInstance
- **And** 存在至少一个成功的备份 (通过 DataSync/ResourceSync)
- **When** 用户创建一个 DisasterDrill 资源
- **Then** 系统应自动进入校验阶段
- **And** 校验通过后状态变为 `Ready`
- **When** 用户 Patch `spec.confirmed: true`
- **Then** 系统应进入 `Executing` 状态
- **And** 系统应根据恢复模式执行恢复或跳过
- **And** 系统应扩容目标集群工作负载
- **And** 操作状态变为 `Completed` (资源级成功)
- **And** 源集群不受任何影响

#### Scenario: 等待用户确认
- **Given** 一个处于 `Ready` 状态的 DisasterDrill
- **And** `spec.confirmed` 为 false
- **When** Reconcile 执行
- **Then** 系统不应 (MUST NOT) 自动开始执行
- **And** 状态应保持 `Ready`

#### Scenario: 指定目标集群的 Drill 演练
- **Given** 一个处于 `Protected` 状态的 DisasterInstance (secondaryCluster = B)
- **And** 存在一个已注册的测试集群 C
- **When** 用户创建一个 DisasterDrill 且 `targetCluster: C`
- **Then** 系统应创建 AppRestore 在集群 C 中恢复资源和数据
- **And** 集群 B (原备集群) 不受影响

#### Scenario: 使用命名空间映射的 Drill 演练
- **Given** 一个处于 `Protected` 状态的 DisasterInstance (原始命名空间 = app-ns)
- **When** 用户创建一个 DisasterDrill 且 `namespaceMapping: {app-ns: drill-ns}`
- **Then** 系统应创建 AppRestore 恢复资源到 drill-ns 命名空间
- **And** 原始命名空间 app-ns 不受影响

#### Scenario: 使用默认配置的 Drill 演练
- **Given** 一个处于 `Protected` 状态的 DisasterInstance (secondaryCluster = B, 命名空间 = app-ns)
- **When** 用户创建一个 DisasterDrill 且不指定 targetCluster 和 namespaceMapping
- **Then** 系统应在备集群 B 的原命名空间 app-ns 中创建 AppRestore
- **And** 如果备集群上已存在 ResourceSync 同步的资源，AppRestore 会覆盖这些资源

#### Scenario: 跳过校验的 Drill 演练
- **Given** 一个 DisasterInstance (状态可能非 Protected)
- **When** 用户创建一个 DisasterDrill 且 `skipValidation: true`
- **Then** 系统应跳过 Instance 状态校验
- **And** 继续进入 Ready 状态

#### Scenario: Drill 演练失败 - 无可用备份
- **Given** 一个 DisasterInstance
- **And** DataSync 和 ResourceSync 均无成功的备份记录
- **When** 用户创建一个 DisasterDrill
- **Then** 系统应在校验阶段失败
- **And** 操作状态变为 `Failed`
- **And** Status.Message 提示 "无可用备份，请先执行资源同步"

### Requirement: Drill 与 Failover 的行为差异

系统必须 (MUST) 确保 Drill 与 Failover 的行为清晰区分。

#### Scenario: Drill 不执行源端缩容
- **Given** 一个处于 `Protected` 状态的 DisasterInstance
- **When** 用户创建并完成一个 DisasterDrill
- **Then** 系统不应 (MUST NOT) 执行 ScaleDownSource 步骤
- **And** 系统不应 (MUST NOT) 暂停 DataSync/ResourceSync 调度
- **And** 源集群工作负载继续正常运行

#### Scenario: Drill 不切换主备角色
- **Given** 一个 DisasterInstance (primaryCluster=A, secondaryCluster=B)
- **When** 用户创建并完成一个 DisasterDrill
- **Then** DisasterInstance.Status 中的 primaryCluster 和 secondaryCluster 不应 (MUST NOT) 发生变化

#### Scenario: Drill 使用资源级成功标准
- **Given** 一个处于 `Executing` 状态的 DisasterDrill
- **When** ScaleUpTarget 步骤完成 (Patch 请求成功)
- **Then** 操作状态应变为 `Completed`
- **And** 系统不应 (MUST NOT) 等待 Pod 达到 Ready 状态

### Requirement: Drill 一次性生命周期

演练是一次性操作，必须 (MUST) 遵循一次性生命周期。

#### Scenario: 演练完成后需删除重建
- **Given** 一个处于 `Completed` 状态的 DisasterDrill
- **When** 用户需要再次演练
- **Then** 用户必须 (MUST) 删除当前 DisasterDrill 并创建新的 DisasterDrill
- **And** 系统不应 (MUST NOT) 支持重置或复用已完成的演练

### Requirement: Drill Cleanup Operation
演练资源占用需要能够 (SHALL) 通过显式的清理指令进行释放，且根据演练时的映射情况采用不同的清理机制：无命名空间映射则缩容目标集群，有映射则删除映射下的资源。

#### Scenario: 触发对无命名空间映射的演练进行清理
- **Given** 一个处于 `Completed` 状态的 DisasterDrill，且当时并未提供 `namespaceMapping`
- **When** 用户对该资源的 `spec` 下发 `cleanup: true`
- **Then** 系统应创建 `DisasterOperation (type=drill-cleanup)` 并接管清理动作
- **And** 鉴于复用了目标集群的原有资源（未配置映射），清理动作应当 (MUST) 对目标集群对应容灾实例运行的副本缩容为 0
- **And** 清理结束后，状态应变为 `CleanedUp`

#### Scenario: 触发对有命名空间映射的演练进行清理
- **Given** 一个处于 `Completed` 状态的 DisasterDrill，且当时被提供了 `namespaceMapping: {app-ns: drill-ns}`
- **When** 用户对该资源的 `spec` 下发 `cleanup: true`
- **Then** 系统应创建 `DisasterOperation (type=drill-cleanup)` 并接管清理动作
- **And** 鉴于环境与映射前互相隔离，清理动作应当 (MUST) 直接删除目标集群 `drill-ns` 下该演练恢复所释放的所有 K8s 资源或直接删除 `drill-ns` 命名空间。
- **And** 清理结束后，状态应变为 `CleanedUp`

#### Scenario: 清理结束后删除演练
- **Given** 一个处于 `CleanedUp` 状态的 DisasterDrill
- **When** 用户进行资源删除
- **Then** 系统应自动清理关联的 `DisasterOperation (type=drill-cleanup)`、`DisasterOperation (type=drill)`，释放集群元数据空间。

