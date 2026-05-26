# 设计：AppBackup 生命周期修复

## 上下文
我们需要解决 AppBackup 状态不明确以及删除时残留 Velero 资源的问题。

## 决策

### 1. 级联删除实现
- **决策**：使用 **Finalizer** 机制。
- **原因**：原计划使用的 `OwnerReference` 不支持跨命名空间引用（AppBackup 在 `disaster-system`，Velero 资源在 `velero`）。
- **实现细节**：
  - 在 `AppBackup` 创建时添加 Finalizer `testudo.softcdata.com/finalizer`。
  - 当 `DeletionTimestamp` 非空时，执行清理逻辑：
    - 根据名称和命名空间查找并删除 Velero `Backup` 或 `Schedule`。
  - 清理成功后移除 Finalizer。

### 2. 状态管理
- **决策**：在 Reconcile 循环早期引入 `InProgress` 状态。
- **实现细节**：
  - 在执行任何耗时操作（如创建 Velero 资源）之前，先检查并更新 Status 为 `InProgress`。
  - 确保 Status 更新后立即返回或继续执行（取决于是否需要重新排队）。

## 替代方案
- **手动 Finalizer**：使用 Finalizer 在删除前手动清理资源。
  - **缺点**：增加了代码复杂性，容易出错（如清理失败导致资源无法删除）。
  - **原因**：OwnerReference 是 Kubernetes 原生支持的，更可靠且代码更简洁。
