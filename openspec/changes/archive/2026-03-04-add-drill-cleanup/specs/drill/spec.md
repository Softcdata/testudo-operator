## ADDED Requirements
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
