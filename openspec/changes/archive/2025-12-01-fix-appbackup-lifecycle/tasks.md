# 任务列表

- [x] 编写 AppBackup 生命周期规范 (`specs/app-backup-lifecycle/spec.md`)
- [x] 实现状态更新逻辑：在 Reconcile 开始时设置 `InProgress` 状态
- [x] 实现级联删除：使用 Finalizer 模式管理跨命名空间 Velero 资源
- [x] 修复错误处理：当创建资源失败时，将状态设置为 `Failed`
- [x] 添加 Event 记录：在关键生命周期节点（Finalizer、资源清理、错误）添加 Kubernetes Event

