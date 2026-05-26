# Spec: 集群协调

## MODIFIED Requirements

### Cluster Authentication
Operator 必须支持使用 Token 对受管集群进行认证。

#### Scenario: 使用 Token 协调
Given 一个具有有效 `spec.token` 和 `spec.endpoint` 且没有 `spec.kubeConfig` 的 Cluster CR
When 控制器协调该 Cluster
Then 它必须成功连接到集群
And 它必须将集群状态更新为 Ready（如果满足其他条件）

#### Scenario: 缺少配置
Given 一个既没有 `spec.kubeConfig` 也没有 (`spec.token` + `spec.endpoint`) 的 Cluster CR
When 控制器协调该 Cluster
Then 它必须记录一个 Warning Event
And 它必须将集群状态更新为 NotReady
And 它必须在 Status 中记录错误信息

#### Scenario: DisasterBackup 使用 Token 集群
Given 一个 DisasterBackup CR 引用了一个使用 Token 认证的 Cluster
When DisasterBackup 控制器尝试获取该集群的 Discovery Client
Then 它必须能够成功创建 Client 并获取 API 资源列表
And 它不应因为缺少 KubeConfig 而报错
