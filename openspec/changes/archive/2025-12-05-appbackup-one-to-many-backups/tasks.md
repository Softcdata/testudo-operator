# 任务列表

- [x] 修改 `pkg/apis/disaster/v1/appbackup_types.go`:
    - [x] 定义 `BackupRecord` 结构体
    - [x] 在 `AppBackupStatus` 中添加 `TotalBackups` 和 `History` 字段
    - [x] 运行 `make manifests` 和 `make generate` 更新 CRD 和 DeepCopy
- [x] 修改 `internal/controller/appbackup_controller.go`:
    - [x] 修改 `CreateVeleroBackup` 方法，在创建 Backup 时添加 Label `testudo.softcdata.com/app-backup: <name>`
    - [x] 修改 `Reconcile` 方法：
        - [x] 实现优化逻辑：如果是首次创建（`created=true` && `TotalBackups==0`），直接更新 Status 并返回，不进行 List 查询
        - [x] 常规逻辑：使用 `List` 方法配合 Label Selector 查询所有关联的 Backup
        - [x] 实现排序逻辑（按创建时间倒序）
        - [x] 填充 `Status.TotalBackups` 和 `Status.History`
        - [x] 将最新的 Backup 状态同步给 `AppBackup.Status`
- [x] 优化与增强:
    - [x] 丰富 `BackupRecord` 字段 (Errors, Warnings, Expiration)
    - [x] 封装 `ListAppBackups` 和 `SyncBackupStatus` 方法以提高代码可读性
