# Tasks: 修复 AppBackup 删除时因 DeleteBackupRequest 已存在导致的阻塞问题

## 1. 实施方案

- [x] 1.1 修改 `internal/controller/appbackup/appbackup_controller.go`
    - 定位到 `deleteExternalResources` 函数循环创建 `DeleteBackupRequest` 的位置（约 527 行）。
    - 增加 `apierrors.IsAlreadyExists(err)` 的判断。
    - 如果是该错误，则忽略报错，继续执行。

- [x] 1.2 增强循环容错 (可选)
    - 确保单个备份的清理失败不影响整体流程。

## 2. 验证

- [ ] 2.1 静态检查代码逻辑。
- [ ] 2.2 (本地环境模拟) 尝试删除一个已有关联 `DeleteBackupRequest` 的 `AppBackup`。
