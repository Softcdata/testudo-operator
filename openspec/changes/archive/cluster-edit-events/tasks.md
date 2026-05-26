# 任务：增加 'Edit Cluster' 事件上报 (Spec + Metadata)

本实施方案引入了集群更新的事件上报机制，涵盖 Spec 变更（通过 `Generation` 检测）和 Metadata 变更（通过哈希计算检测）。

## 任务清单

### 1. 数据结构变更
- [ ] 在 `pkg/apis/disaster/v1/cluster_types.go` 的 `ClusterStatus` 中添加 `ObservedGeneration` (int64)。
- [ ] 在 `pkg/apis/disaster/v1/cluster_types.go` 的 `ClusterStatus` 中添加 `ObservedMetadataHash` (string)。
- [ ] 运行 `make manifests generate` 更新 CRD 和 DeepCopy 方法。

### 2. 代码实现
- [ ] 在 `internal/controller/cluster_controller.go`（或 helper 包）中实现 `calculateMetadataHash` 函数。
    - [ ] 包含所有 Labels，**排除**：`testudo.softcdata.com/cluster-name`、`namespace-count`、`resource-total-count`、`cluster-finalizer`。
    - [ ] 包含 `testudo.softcdata.com/description` Annotation。
    - [ ] 确保哈希计算的确定性（对键进行排序）。
- [ ] 更新 `internal/controller/cluster_controller.go` 中的 `Reconcile` 逻辑：
    - [ ] 计算 `currentMetadataHash`。
    - [ ] 检测变更：`Generation > ObservedGeneration` 或 `currentMetadataHash != ObservedMetadataHash`。
    - [ ] 如果发生变更（且非初始创建），发射 "编辑集群 Started" 事件。
    - [ ] 成功后更新 `ObservedGeneration` 和 `ObservedMetadataHash`。
    - [ ] 成功后发射 "编辑集群 Finished" 事件。

### 3. 验证
- [ ] 验证 `Spec` 变更（例如修改 Token）触发 "Started" 和 "Finished" 事件。
- [ ] 验证 `Metadata` 变更（例如修改描述或添加自定义 Label）触发事件。
- [ ] 验证 Controller 管理的 Label 更新（例如 namespace 数量变化）**不**触发事件。
