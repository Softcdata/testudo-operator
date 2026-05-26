# AppRestore Lifecycle Delta Spec

## MODIFIED Requirements

### Requirement: 资源级联删除
删除 AppRestore 时，必须 (MUST) 尝试清理所有关联的外部资源，但不能因目标集群缺失而阻塞删除。

#### Scenario: 目标集群不存在时的删除
- **Given** 一个 AppRestore 引用了一个不存在的 Cluster 资源
- **When** 用户删除该 AppRestore
- **Then** 控制器尝试获取目标集群的客户端失败 (NotFound)
- **And** 控制器必须 (MUST) 记录警告日志，跳过外部资源清理步骤
- **And** 控制器必须 (MUST) 移除 Finalizer

# DisasterJob Lifecycle Delta Spec

## MODIFIED Requirements

### Requirement: 资源级联删除
删除 DisasterJob 时，必须 (MUST) 尝试清理所有关联的外部资源，但不能因源集群或目标集群缺失而阻塞删除。

#### Scenario: 集群不存在时的删除
- **Given** 一个 DisasterJob 引用了一个不存在的 SourceCluster 或 TargetCluster
- **When** 用户删除该 DisasterJob
- **Then** 控制器尝试获取集群的客户端失败 (NotFound)
- **And** 控制器必须 (MUST) 记录警告日志，跳过该集群上的资源清理步骤
- **And** 控制器必须 (MUST) 继续尝试清理另一个集群（如果存在）
- **And** 控制器必须 (MUST) 最终移除 Finalizer
