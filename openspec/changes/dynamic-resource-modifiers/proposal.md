# 提案：用于 Trafficless Sync 的动态资源修改器 (Dynamic Resource Modifiers)

## 摘要

本提案引入了一种机制，根据备份时资源的实际状态动态生成 Velero Resource Modifiers。这解决了 Velero 的 JSON Patch 实现难以替换整个 Map（如 `metadata.labels`）或处理缺失键（missing keys）的局限性。通过记录备份时工作负载上存在的具体 Label 和 Annotation 键名，我们可以在恢复时生成精确的 `remove` Patch，从而确保数据同步 Pod 处于干净的 "trafficless"（无流量）状态。

## 动机

在 V2 容灾编排中，`DataSync` 需要将 Pod 恢复为 "trafficless" 状态，以便在不承接流量的情况下执行数据同步。这要求：
1.  移除所有现有的 Labels（避免被 Service selector 选中）。
2.  移除 OwnerReferences（避免被控制器删除/接管）。
3.  替换 Image 和 Command。

我们在使用 Velero `ResourceModifier` 时遇到了显著挑战：
-   使用新 Map `replace /metadata/labels` 经常失败，或者表现为合并而非替换。
-   如果 Map 不存在，`remove /metadata/labels` 会失败（虽然对工作负载来说很少见）。
-   如果目标 Key 不存在，单条 `replace` 操作会失败。

为了稳健地剥离 Labels，我们需要针对资源上存在的 *每一个具体的 Label Key* 发出明确的 `remove` 操作。由于这些 Key 因应用而异，我们需要在备份期间记录它们。

## 设计

### 1. AppBackup: 记录元数据

`AppBackup` 控制器目前已将副本数记录在 ConfigMap (`am-backup-replicas-<UID>`) 中。我们将扩展此机制（或创建一个新的 `am-backup-metadata-<UID>`），用于记录资源元数据。

**流程：**
1.  当 `AppBackup` 达到 `Ready` 阶段时（或在备份准备期间）。
2.  根据 `AppBackup` 的 LabelSelector 识别目标资源。
3.  对于每个匹配的 `StatefulSet` 和 `Deployment`：
    -   提取 `spec.template.metadata.labels` 的 Keys。
    -   提取 `spec.template.metadata.annotations` 的 Keys。
4.  将此映射关系存储在 ConfigMap 中。

**ConfigMap 结构：**
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: am-backup-metadata-<AppBackup-UID>
  labels:
    appbackup.testudo.softcdata.com/uid: <UID>
data:
  # JSON 序列化的 Map
  resource-metadata.json: |
    {
      "apps/v1/namespaces/default/statefulsets/e2e-nginx": {
        "podLabels": ["app", "statefulset.kubernetes.io/pod-name", "version"],
        "podAnnotations": ["prometheus.io/scrape"]
      }
    }
```

### 2. DataSync: 生成动态修改器

修改 `DataSyncReconciler.makeTrafficlessModifiers` 逻辑：

**流程：**
1.  检索与正在恢复的 Backup 关联的 `am-backup-metadata-<UID>` ConfigMap。
2.  解析 `resource-metadata.json`。
3.  遍历记录的元数据。
4.  生成 `JSONPatch` 操作列表：

```go
patches := []disasterv1.JSONPatch{}

// 1. 为每个记录的 label 生成 Remove patch
for _, key := range recordedLabels {
    patches = append(patches, disasterv1.JSONPatch{
        Operation: "remove",
        Path:      fmt.Sprintf("/metadata/labels/%s", escapeJSONPointer(key)),
    })
}

// 2. 为每个记录的 annotation 生成 Remove patch
for _, key := range recordedAnnotations {
    patches = append(patches, disasterv1.JSONPatch{
        Operation: "remove",
        Path:      fmt.Sprintf("/metadata/annotations/%s", escapeJSONPointer(key)),
    })
}

// 3. 添加 Trafficless Label
patches = append(patches, disasterv1.JSONPatch{
    Operation: "add",
    Path:      "/metadata/labels/trafficless",
    Value:     "true",
})

// 4. 标准 Trafficless Patches (Image, Command, OwnerRef)
// ...
```

### 3. 处理转义 (Escaping)

JSON Pointer 要求将 `/` 转义为 `~1`，将 `~` 转义为 `~0`。由于 K8s labels 经常包含 `/`（例如 `app.kubernetes.io/name`），我们在生成 Path 时必须严格执行此转义逻辑。

## 实施步骤

1.  **修改 `AppBackup` 控制器**：
    -   更新 `recordReplicasToConfigMap`（或重命名/重构），扫描工作负载并记录 label/annotation keys。
    -   以结构化的 JSON 格式持久化到 ConfigMap。

2.  **修改 `DataSync` 控制器**：
    -   在方法逻辑中注入 `Client` 以获取 Metadata ConfigMap。
    -   重构 `makeTrafficlessModifiers` 以接受元数据。
    -   实现动态 Patch 生成循环。
    -   实现 `escapeJSONPointer` 辅助函数。

## 优缺点

**优点：**
-   **稳健性**：明确移除 Key 可以保证操作成功（只要 Key 存在，而我们从备份中知道它存在）。
-   **安全性**：避免了依赖实现细节不明确的 "replace entire map" 行为。
-   **可观测性**：Metadata ConfigMap 提供了应用状态的清晰记录。

**缺点：**
-   **复杂性**：增加了一层状态依赖。DataSync 现在严格依赖于 AppBackup 创建的 Metadata ConfigMap。
-   **过时风险**：如果应用 Labels 在 AppBackup 之后但灾难发生之前发生了变化（对于快照备份不太可能，但在松耦合场景下有可能），Patch 可能会漏掉新 Label。但由于恢复的是备份时的状态，资源状态应与记录的元数据一致。

## 待决问题

-   是否应该记录每个 Pod 的元数据？
    -   *回答*：不需要。记录工作负载（STS/Deployment）级别就足够且更高效。恢复出的 Pod 是由这些模板（STS 管理）生成的，或者是原样恢复的。对于独立 Pod，如果 `IncludedResources` 包含 Pods，我们也可以列出它们并记录 Labels。鉴于我们的 DataSync 侧重于 STS/PVC，工作负载模板元数据是主要目标。如果没有找到所属 Workload，我们可以回退到扫描单个 Pod。
