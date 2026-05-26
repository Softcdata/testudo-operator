# Change: 支持跨集群恢复存储连通性触发信号

## Why
跨集群 `AppRestore` 的必需 BSL 命名规则是 `<StorageRepository>-<SourceCluster>`。  
当前 `Cluster` 控制器只消费 `testudo.softcdata.com/ensure-storage`，并固定创建 `<StorageRepository>-<ClusterName>`。
当 Server 在恢复前置校验阶段请求目标集群准备 `SourceCluster` 后缀 BSL 时，Operator 现有逻辑无法满足该语义，导致前置校验无法稳定收敛到成功状态。

## What Changes

### 1. 扩展 ensure-storage 信号注解
- 保留现有注解：`testudo.softcdata.com/ensure-storage=<storageRepository>`
- 新增注解：`testudo.softcdata.com/ensure-storage-source-cluster=<sourceCluster>`

### 2. 明确 BSL 命名与前缀计算规则
- 当 `ensure-storage-source-cluster` 存在时：
  - `bslName=<storageRepository>-<sourceCluster>`
  - `prefix=<sourceCluster>`
- 当 `ensure-storage-source-cluster` 不存在时：
  - `bslName=<storageRepository>-<cluster.Name>`
  - `prefix=<cluster.Name>`

### 3. 保持一次性触发器语义
- `ApplyStorageRepository` 执行成功后，必须移除以下注解：
  - `testudo.softcdata.com/ensure-storage`
  - `testudo.softcdata.com/ensure-storage-source-cluster`
- 当 `StorageRepository` 不存在时，必须移除以上注解并记录失败事件，避免对象长期停留在无效触发状态。

### 4. 与 Server 前置校验提案对齐
- 本提案与 `disaster-server` 的 `add-storage-connectivity-check` 联动。
- Server 在跨集群恢复前置校验时写入双注解；Operator 负责消费双注解并创建目标 BSL。

## Impact
- 受影响规范：
  - `openspec/specs/cluster/spec.md`
- 受影响代码：
  - `pkg/metadata/labels.go`
  - `internal/controller/cluster_controller.go`
  - `internal/controller/cluster_controller_test.go`
- 跨仓库影响：
  - `disaster-server` 前置校验接口依赖本能力创建 `<storageRepository>-<sourceCluster>` BSL
