# 变更：修复 AppBackup 生命周期管理问题

## 为什么
目前的 `AppBackup` 控制器存在两个主要问题：
1.  **状态缺失**：在 Velero 备份创建过程中，`AppBackup` 缺乏明确的“进行中”状态，导致用户无法区分是未开始还是正在处理。
2.  **资源残留**：删除 `AppBackup` 资源时，关联的 Velero `Backup` 资源未被同步删除，导致集群中存在孤儿资源。

## 变更内容
- **状态管理优化**：引入 `InProgress` 状态，在开始创建 Velero 资源前设置该状态。
- **级联删除**：利用 Kubernetes 的 OwnerReference 机制，确保删除 `AppBackup` 时自动删除关联的 Velero `Backup`。

## 影响
- **受影响的规范**：`app-backup-lifecycle` (新增)
- **受影响的代码**：`internal/controller/appbackup_controller.go`
