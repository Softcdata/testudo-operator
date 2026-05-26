---
description: 增加 'Edit Cluster' 事件上报
---

# 提案：增加 'Edit Cluster' 事件上报

## 1. 背景

目前，`disaster-operator` 上报了 "Create Cluster" 和 "Delete Cluster" 的结构化事件。然而，当用户更新集群规格（例如更改 Token 或 KubeConfig）或修改关键元数据（如描述）时，不会上报相应的 "Edit Cluster"（编辑集群）事件。这导致缺乏对更新操作的可观测性。

全局事件规范（`openspec/specs/global-events/spec.md`）已定义 "编辑集群" 为标准任务，但 Controller 尚未实现。

## 2. 问题分析

`ClusterReconciler` 仅在 Finalizer 不存在时（初始创建）触发 "Create Cluster" 事件。后续对 `Cluster` 资源的更新：
1.  **Spec 变更**：增加了 `metadata.Generation`，但 Controller 目前不跟踪 Generation 变更来触发新事件。
2.  **Metadata 变更**：Label 或 Annotation（如 Description）的更新不影响 `Generation`，因此 Controller 完全忽略它们，不进行事件上报。

## 3. 建议方案

我们将引入 `ObservedGeneration` 来跟踪 `Spec` 变更，并引入 `ObservedMetadataHash` 来跟踪 `Metadata`（Label/Description）变更。

### 3.1 数据结构变更

修改 `pkg/apis/disaster/v1/cluster_types.go`：
```go
type ClusterStatus struct {
    // ...
    // ObservedGeneration 记录上次处理的 Generation
    // +optional
    ObservedGeneration int64 `json:"observedGeneration,omitempty"`

    // ObservedMetadataHash 记录上次处理的元数据（labels/annotations）哈希值
    // +optional
    ObservedMetadataHash string `json:"observedMetadataHash,omitempty"`
}
```

### 3.2 Controller 逻辑变更

修改 `internal/controller/cluster_controller.go`：
1.  在 `Reconcile` 中，计算 `currentMetadataHash`（过滤掉系统产生的 label/annotation）。
2.  比较输入：
    *   `specChanged = cluster.Generation > cluster.Status.ObservedGeneration`
    *   `metadataChanged = currentMetadataHash != cluster.Status.ObservedMetadataHash`
3.  如果 `specChanged || metadataChanged`：
    *   这表示发生了更新。
    *   检查是否为 **更新操作**（即不是初始创建过程）。我们可以通过检查 Finalizer 是否已存在且不再 "Create" 代码块中来区分。
    *   发射 "编辑集群 Started" 事件：`[Task: 编辑集群 <name>] [Status: InProgress]`。
4.  执行协调逻辑（验证，Velero 检查/安装）。
5.  协调成功后：
    *   发射 "编辑集群 Finished" 事件：`[Task: 编辑集群 <name>] [Status: Success]`。
    *   更新 `cluster.Status.ObservedGeneration = cluster.Generation`。
    *   更新 `cluster.Status.ObservedMetadataHash = currentMetadataHash`。

### 3.3 元数据哈希逻辑
为了避免无限循环（因为 Controller 会更新某些 Label），我们必须基于过滤后的元数据集合计算哈希：
*   **包含**：
    *   所有 Label *除了*：`testudo.softcdata.com/cluster-name`，`testudo.softcdata.com/cluster-namespace-count`，`testudo.softcdata.com/cluster-resource-total-count`，`testudo.softcdata.com/cluster-finalizer`。
    *   特定的 Annotation：`testudo.softcdata.com/description`。（或者包含除 `disaster.io/trace-id` 和 `kubectl.kubernetes.io/last-applied-configuration` 之外的所有非系统 Annotation）。

### 3.4 事件详情
*   **Task Name**: `编辑集群 <ClusterName>`
*   **Reason**: `ExecutionStarted`, `ExecutionFinished`
*   **TraceID & User**: 正常从 Annotation 中提取。

## 4. 实施步骤

1.  在 `cluster_types.go` 中更新 `ClusterStatus` 结构体。
2.  运行 `make manifests generate` 更新 CRD 和 DeepCopy 代码。
3.  在 `cluster_controller.go` 中实现元数据哈希计算和事件发射逻辑。
4.  验证编辑集群（Spec 或 Metadata）是否触发正确的事件序列。
