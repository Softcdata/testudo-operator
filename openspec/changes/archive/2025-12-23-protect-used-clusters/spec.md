# Spec: 集群保护

## Requirements

### Deletion Protection
系统必须阻止删除仍被其他资源引用的集群。

#### Scenario: 删除被 AppBackup 引用的集群
Given 一个名为 "cluster-a" 的 Cluster
And 一个引用 "cluster-a" 的 AppBackup 资源
When 用户尝试删除 "cluster-a"
Then 集群不应被从 Kubernetes 中移除
And 集群状态应包含 "DeletionBlocked" 的相关信息
And 应产生一个 Warning Event 说明存在关联的 AppBackup

#### Scenario: 删除被 DisasterConfig 引用的集群
Given 一个名为 "cluster-b" 的 Cluster
And 一个将 "cluster-b" 作为 SourceCluster 的 DisasterConfig
When 用户尝试删除 "cluster-b"
Then 集群不应被从 Kubernetes 中移除
And 应产生一个 Warning Event 说明存在关联的 DisasterConfig

#### Scenario: 删除无引用的集群
Given 一个名为 "cluster-c" 的 Cluster
And 没有任何资源引用 "cluster-c"
When 用户尝试删除 "cluster-c"
Then 集群应被成功移除

### Modification Protection (Server-Side)
系统应限制通过 API 修改关键集群配置。

#### Scenario: 修改集群名称（如果适用）
Given 一个已存在的集群
When 用户尝试修改其名称
Then 操作应被拒绝（通常 K8s 自身也禁止修改 Name）

#### Scenario: 修改连接信息
Given 一个已存在的集群
When 用户尝试更新 Token 或 KubeConfig
Then 操作应被允许（支持凭证轮换）
