# Kubernetes Controller Reconcile 状态更新模式指南

在开发 Kubernetes Operator 时，正确处理资源的状态（Status）和元数据（Metadata/Spec）更新至关重要。不当的更新逻辑会导致 `ResourceVersion` 冲突（Conflict 409 错误）、无限循环 Reconcile 或状态抖动。

本文档介绍了两种经过验证的、健壮的状态更新模式。

## 模式一：分离更新模式（推荐）

这是最标准、最健壮的模式，符合 Kubernetes 控制器的设计哲学。核心思想是将 Status 更新与 Metadata/Spec 更新完全分离，一次 Reconcile 循环只推进一种状态。

### 适用场景
- 逻辑复杂，状态流转多的控制器。
- 需要严格避免并发冲突的场景。
- 追求代码清晰度和可维护性。

### 实现逻辑
1. **保存快照**：在 Reconcile 开始时，使用 `DeepCopy()` 保存原始对象的副本。
2. **优先处理 Status**：
   - 执行业务逻辑，计算新的 Status。
   - 比较新旧 Status（使用 `reflect.DeepEqual`）。
   - 如果 Status 发生变化：调用 `Status().Update()`，然后**立即返回**（`return result, nil`）。
   - 这会触发一次新的 Reconcile，下一次循环中 Status 已是最新，代码将跳过此步。
3. **后处理 Metadata**：
   - 仅当 Status 未变化时，才检查 Metadata（Labels, Finalizers, Annotations）。
   - 比较新旧 Metadata。
   - 如果 Metadata 发生变化：调用 `Update()`。

### 代码示例
```go
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    // 1. 获取资源
    var obj myv1.MyResource
    if err := r.Get(ctx, req.NamespacedName, &obj); err != nil {
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }

    // 2. 保存原始快照
    original := obj.DeepCopy()

    // 3. 执行业务逻辑 (可能会修改 obj.Status 或 obj.Labels)
    // ... business logic ...

    // 4. 优先更新 Status
    if !reflect.DeepEqual(original.Status, obj.Status) {
        if err := r.Status().Update(ctx, &obj); err != nil {
            return ctrl.Result{}, err
        }
        // 关键：状态更新后立即返回，等待下一次 Reconcile 处理 Metadata
        return ctrl.Result{}, nil
    }

    // 5. 更新 Metadata (仅当 Status 无变化时执行)
    if !reflect.DeepEqual(original.ObjectMeta.Labels, obj.ObjectMeta.Labels) ||
       !reflect.DeepEqual(original.ObjectMeta.Finalizers, obj.ObjectMeta.Finalizers) {
        if err := r.Update(ctx, &obj); err != nil {
            return ctrl.Result{}, err
        }
    }

    return ctrl.Result{}, nil
}
```

---

## 模式二：串行回填模式（变通方案）

这种模式允许在一次 Reconcile 循环中连续执行 Metadata 更新和 Status 更新。它通过利用 `Update` 操作返回的最新对象版本，来避免版本冲突。

### 适用场景
- 逻辑相对简单，希望在一个循环内完成所有更新。
- 遗留代码重构成本较高时。

### 实现逻辑
1. **暂存 Status**：在执行主资源更新前，将内存中计算好的最新 Status 暂存到一个变量中。
2. **更新主资源**：调用 `r.Update(ctx, obj)` 更新 Metadata/Spec。
   - **关键点**：`r.Update` 成功后，`obj` 会被重置为 API Server 返回的最新状态（包含最新的 `ResourceVersion`），但通常包含的是**旧的 Status**（因为 `Update` 不更新 Status 子资源）。
3. **回填 Status**：将暂存的最新 Status 重新赋值给更新后的 `obj`。
   - 此时 `obj` 拥有：最新的 Metadata + 最新的 ResourceVersion + 最新的 Status。
4. **更新 Status**：调用 `r.Status().Update(ctx, obj)`。由于使用了最新的 `ResourceVersion`，更新会成功。

### 代码示例
```go
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    // ... 获取资源 & 执行业务逻辑 ...

    // 1. 暂存计算好的最新 Status
    calculatedStatus := obj.Status

    // 2. 更新主资源 (Metadata/Spec)
    // 注意：obj 会被更新为 Server 端最新版本
    if err := r.Update(ctx, &obj); err != nil {
        return ctrl.Result{}, err
    }

    // 3. 回填 Status
    // 因为 r.Update 返回的对象里 Status 是旧的，必须覆盖回去
    obj.Status = calculatedStatus

    // 4. 更新 Status
    // 此时 obj.ResourceVersion 是最新的，Status Update 不会冲突
    if err := r.Status().Update(ctx, &obj); err != nil {
        return ctrl.Result{}, err
    }

    return ctrl.Result{}, nil
}
```

## 最佳实践：安全删除 (Safe Deletion)

在处理资源删除（移除 Finalizer）时，为了避免 `StorageError: invalid object ... UID in object meta: ""` 错误，必须遵循以下规范。

### 问题背景
当 Controller 尝试移除 Finalizer 并执行 `Update` 时，如果内存中的对象状态（特别是 Metadata）不完整或已被修改，全量 Update 可能会导致向 API Server 发送一个缺少 UID 的对象，从而被拒绝。

### ❌ 错误做法 (使用 Update)
```go
// 危险：如果 obj 在内存中被修改过或不完整，全量 Update 会导致 UID 丢失报错
controllerutil.RemoveFinalizer(&obj, FinalizerName)
if err := r.Update(ctx, &obj); err != nil {
    return ctrl.Result{}, err
}
```

### ✅ 正确做法 (使用 Patch)
使用 `client.Patch` 配合 `client.MergeFrom`。这种方式只会向 API Server 发送变更的字段（即 Finalizers 列表），而不会发送整个对象，因此非常安全且高效。

```go
// 1. 保存原始状态 (在修改 Finalizer 之前)
original := obj.DeepCopy()

// 2. 执行删除逻辑 (清理外部资源)
// ...

// 3. 移除 Finalizer
controllerutil.RemoveFinalizer(&obj, FinalizerName)

// 4. 使用 Patch 提交差异
// client.MergeFrom 会计算差异，只发送 "移除 Finalizer" 的指令
if err := r.Patch(ctx, &obj, client.MergeFrom(original)); err != nil {
    return ctrl.Result{}, err
}
```

## 场景对比：添加 Finalizer 流程

在资源初始创建时，Controller 通常需要第一时间添加 Finalizer。两种模式对此的处理流程不同。

### 1. 分离更新模式 (AppRestore)
利用 Kubernetes 的 Watch 机制，减少不必要的 Requeue。

*   **流程**：
    1.  `Handler` 检测到无 Finalizer，执行 `controllerutil.AddFinalizer`。
    2.  `Handler` 返回 `ctrl.Result{}, nil` (不要求 Requeue)。
    3.  `Reconcile` 主逻辑在尾部检测到 Metadata 变化（Finalizer 增加了）。
    4.  执行 `r.Update(ctx, obj)`。
    5.  **关键**：Update 成功后，API Server 会推送一个新的事件。Controller 收到事件后触发下一次 Reconcile。
*   **结果**：日志清晰，没有重复的 "FinalizerAdded" 事件。

### 2. 串行更新模式 (AppBackup)
利用强制 Requeue 确保状态落库，属于防御性编程。

*   **流程**：
    1.  `Handler` 检测到无 Finalizer，执行 `controllerutil.AddFinalizer`。
    2.  `Handler` 返回 `ctrl.Result{Requeue: true}, nil` (强制 Requeue)。
    3.  `Reconcile` 主逻辑在尾部执行 `r.Update(ctx, obj)` 保存 Finalizer。
    4.  `Reconcile` 返回 Handler 的结果（即 `Requeue: true`）。
    5.  Controller 立即将该 Key 重新加入队列。
*   **结果**：虽然逻辑闭环，但会导致多一次 Reconcile 循环，可能会在日志中看到多次 "FinalizerAdded" 或相关处理日志。
