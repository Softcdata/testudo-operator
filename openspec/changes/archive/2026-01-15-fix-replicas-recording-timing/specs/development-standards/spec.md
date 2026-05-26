## MODIFIED Requirements

### Requirement: Failover 副本数恢复
系统必须 (MUST) 在 Failover 期间正确恢复目标集群工作负载的副本数。

#### Scenario: 正确记录源集群副本数
Given 一个处于 `Protected` 状态的 `DisasterInstance`
And 源集群有运行中的 Deployment/StatefulSet (副本数 > 0)
When 执行 Failover 操作，进入 `ScaleDownSource` 步骤
Then 系统必须在缩容之前记录所有工作负载的当前副本数到 ConfigMap
And ConfigMap 名称格式为 `replicas-{ResourceSyncName}`
And 记录的副本数必须是缩容前的实际值（非0）。

#### Scenario: 目标集群副本数恢复
Given Failover 的 `ScaleUpTarget` 步骤正在执行
And 存在记录副本数的 ConfigMap
When 系统扩容目标集群的 Deployment/StatefulSet
Then 系统必须从 ConfigMap 读取原始副本数
And 将目标集群工作负载的副本数设置为原始值
And 目标集群的工作负载必须成功启动。

#### Scenario: 保护已记录的副本数不被覆盖
Given Failover 期间源集群副本数已被记录到 ConfigMap
And `FinalSync` 步骤触发了 ResourceSync
When ResourceSync 尝试记录副本数
And 扫描到的副本数为 0 (因为已经缩容)
Then ResourceSync 不得覆盖 ConfigMap 中已有的非零副本数记录。
