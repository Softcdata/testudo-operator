# 设计细节 (Design Details)

### 3.1 API 变更 (AppRestore CRD)
在 `AppRestore` 的 `Spec` 中新增 `ResourceModifierRules` 字段，允许用户直接定义修改规则。结构应与 Velero 的 ResourceModifier 格式保持一致或进行简化封装。

```go
// pkg/apis/disaster/v1/apprestore_types.go

type AppRestoreSpec struct {
    // ... 现有字段 ...

    // ResourceModifierRules 定义了在恢复期间应用于资源的补丁规则。
    // 如果提供，控制器将基于此内容在 Velero 命名空间生成 ConfigMap。
    // +optional
    ResourceModifierRules []ResourceModifierRule `json:"resourceModifierRules,omitempty"`
}

// ResourceModifierRule 对应 Velero 的 ResourceModifier 结构
type ResourceModifierRule struct {
    Conditions Conditions  `json:"conditions"`
    Patches    []JSONPatch `json:"patches"`
}
// ... 定义 Conditions 和 JSONPatch 结构 ...
```

### 3.2 控制器逻辑 (Controller Logic)

#### 写入时机 (When)
在 `AppRestore` 进入 `Restoring` 阶段之前，即在调用 `createVeleroRestore` 方法内部，创建 Velero Restore 资源**之前**。

#### 写入位置 (Where)
ConfigMap 必须写入 **Velero 安装的命名空间** (通常是 `velero`)，因为 Velero 控制器需要读取它。

#### 命名规范 (Naming)
为了避免冲突并易于追踪，ConfigMap 名称应包含 `AppRestore` 的名称和 UID 摘要，例如：
`am-<apprestore-name>-<short-uid>` (am = app modifier)

#### 关联方式 (Association)
1. 控制器根据 `Spec.ResourceModifierRules` 生成 YAML 数据。
2. 在 `velero` 命名空间创建 ConfigMap。
3. 在构造 `velero.Restore` 对象时，将 `Spec.ResourceModifier` 指向该 ConfigMap。

#### 生命周期管理 (Lifecycle)
- **创建**：在创建 Velero Restore 前创建 ConfigMap。
- **更新**：如果 `AppRestore` 更新了规则，且尚未开始恢复，则更新 ConfigMap。
- **删除**：在 `deleteExternalResources` 方法中，除了删除 Velero Restore，还需要删除对应的 ConfigMap。
- **所有权**：由于跨命名空间（AppRestore 可能不在 velero ns），无法使用 OwnerReference 自动垃圾回收。必须在代码中显式删除，或者给 ConfigMap 打上 `AppRestore` 的标签以便通过 LabelSelector 删除。

### 3.3 示例流程 (Workflow)

1. **用户提交 AppRestore**:
   ```yaml
   apiVersion: testudo.softcdata.com/v1
   kind: AppRestore
   metadata:
     name: restore-demo
   spec:
     resourceModifierRules:
     - conditions:
         groupResource: persistentvolumeclaims
       patches:
       - operation: replace
         path: "/spec/storageClassName"
         value: "managed-nfs-storage"
   ```

2. **控制器处理**:
   - 检测到 `resourceModifierRules` 不为空。
   - 生成 ConfigMap `am-restore-demo-xyz123` 到 `velero` 命名空间。
   - 创建 Velero Restore，设置 `spec.resourceModifier.name: am-restore-demo-xyz123`。

3. **清理**:
   - 当 `AppRestore` 被删除时，控制器根据标签 `apprestore.testudo.softcdata.com/uid=<uid>` 查找并删除 `velero` 命名空间下的 ConfigMap。
