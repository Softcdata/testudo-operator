# Operator 开发最佳实践规范

本文档定义了本项目中 Kubernetes Operator 开发的强制性规范和最佳实践。所有新增或修改的 Controller 代码必须遵循此规范，以确保系统的稳定性、一致性和可维护性。

## 1. 核心原则

*   **Level Triggered (水平触发)**: 控制器应基于当前状态与期望状态的差异进行协调，而不是依赖事件的触发顺序。
*   **最终一致性**: 系统可能在短时间内处于中间状态，但最终必须收敛到期望状态。
*   **无状态设计**: Reconcile 函数应是无状态的，所有必要的状态都应持久化在 CRD 的 Status 中。
*   **防御性编程**: 假设网络会失败、对象会被并发修改、外部资源可能不可用。

## 2. Reconcile 循环模式

### 2.1 分离更新模式 (Split Update Pattern) - **强制标准**

为了彻底消除 `ResourceVersion` 冲突 (Conflict 409)，必须将 Status 更新与 Metadata/Spec 更新分离。

*   **规则**:
    1.  **DeepCopy**: 入口处必须对对象进行 `DeepCopy`，保留原始快照 `original`。
    2.  **优先 Status**: 优先计算并更新 Status。如果 Status 发生变化，执行 `r.Status().Update()` 并**立即返回** (`return ctrl.Result{}, nil`)。
    3.  **后置 Metadata**: 仅当 Status 无变化时，才检查 Metadata (Labels, Finalizers) 的变化并执行 `r.Update()`。
    4.  **禁止混合**: 严禁在一次 Reconcile 中同时调用 `r.Update()` 和 `r.Status().Update()`，除非你非常清楚自己在做什么（如串行回填模式）。

### 2.2 状态机设计

*   **规则**:
    *   使用 `Phase` 字段明确标识资源当前所处的生命周期阶段。
    *   每个 Phase 对应一个独立的 `Handler` 接口实现。
    *   Handler 只负责计算下一个状态或执行副作用，**不负责**直接调用 `r.Update` (由主循环统一处理)。

## 3. 资源删除规范 (Deletion)

### 3.1 安全删除 (Safe Deletion) - **强制标准**

为了防止 `StorageError: invalid object ... UID in object meta: ""` 和并发冲突。

*   **规则**:
    1.  **Re-fetch**: 在执行完耗时的外部资源清理操作（如删除 Velero 资源）后，**必须**重新 `Get` 最新的对象。
    2.  **Patch**: 移除 Finalizer 时，**必须**使用 `client.Patch` 配合 `client.MergeFrom`，严禁使用 `client.Update`。
    3.  **流程**:
        ```go
        // 1. 清理外部资源 (耗时)
        handler.Handle(...) 
        
        // 2. 重新获取最新对象
        r.Get(ctx, key, &obj)
        
        // 3. 移除 Finalizer 并 Patch
        original := obj.DeepCopy()
        controllerutil.RemoveFinalizer(&obj, FinalizerName)
        r.Patch(ctx, &obj, client.MergeFrom(original))
        ```

## 4. 资源创建与初始化

### 4.1 Finalizer 添加

*   **规则**:
    *   利用 Watch 机制。添加 Finalizer 后，执行 Update 并返回 `ctrl.Result{}, nil`。
    *   **不要**返回 `Requeue: true`，这会导致多余的 Reconcile 循环和日志噪音。

### 4.2 默认值设置

*   **规则**:
    *   尽量使用 Mutating Webhook 设置默认值。
    *   如果在 Reconcile 中设置默认值，应视为 Spec 更新，执行 Update 后立即返回。

## 5. 并发与数据一致性

### 5.1 避免隐式 Mutation

*   **规则**:
    *   严禁直接修改 Cache 中的对象 Spec。
    *   如果需要基于 Spec 创建子资源（如构造 Velero Restore），**必须**先 `DeepCopy` Spec 或 Template。
    *   **错误示例**: `template := obj.Spec.Template; template.Field = "new" // 修改了 Cache!`
    *   **正确示例**: `template := obj.Spec.Template.DeepCopy(); template.Field = "new"`

### 5.2 上下文传递

*   **规则**:
    *   使用 `context.Context` 传递 TraceID 和 Logger。
    *   确保 Logger 携带关键元数据（TraceID, ReconcileID, ResourceName）。

## 6. 错误处理

*   **规则**:
    *   **区分错误类型**:
        *   **临时错误** (网络抖动): 返回 `err`，让 Controller Runtime 指数退避重试。
        *   **永久错误** (配置非法): 记录 Event，更新 Status 为 Failed，返回 `nil` (不再重试)。
    *   **Status.Message**: 将用户可读的错误信息写入 `Status.Message`。
    *   **Events**: 关键状态变更和错误必须记录 Kubernetes Event。

## 7. 文档索引

*   详细模式说明: `docs/controller-reconcile-patterns.md`
*   项目架构说明: `openspec/project.md`

## 8. 外部资源依赖检查

### 8.1 CRD 可用性检查

在操作依赖于其他 Operator 的 CRD（如 Velero）之前，必须检查该 CRD 是否存在于集群中。

*   **规则**:
    *   使用 `client.List` 配合 `client.Limit(1)` 检查 CRD 是否可用。
    *   捕获 `meta.IsNoMatchError(err)` 来判断 CRD 是否缺失。
    *   **删除保护**: 在执行清理逻辑（`deleteExternalResources`）时，如果依赖的 CRD 不存在，应记录 Warning Event 并**跳过**清理，允许 Finalizer 移除，防止资源无法删除。
    *   **创建保护**: 在创建子资源前，如果依赖的 CRD 不存在，应更新 Status 为 Failed 或 Pending 并记录 Event，而不是无限重试。
