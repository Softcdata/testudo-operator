# AppBackup Lifecycle Delta Spec

## MODIFIED Requirements

### Requirement: 资源级联删除 (Cascading Deletion)
删除 AppBackup 时，必须 (MUST) 尝试清理所有关联的外部资源，但不能因目标集群缺失而阻塞删除。

#### Scenario: 目标集群不存在时的删除
- **Given** 一个 AppBackup 引用了一个不存在的 Cluster 资源
- **When** 用户删除该 AppBackup (触发 Finalizer)
- **Then** 控制器尝试获取目标集群的客户端失败 (NotFound)
- **And** 控制器必须 (MUST) 记录警告日志，跳过外部资源清理步骤
- **And** 控制器必须 (MUST) 移除 Finalizer，允许 AppBackup 资源被删除
