# 任务列表

- [x] 修改 `pkg/apis/disaster/v1/appbackup_types.go`:
    - [x] 定义 `BackupAction` 结构体
    - [x] 在 `AppBackupSpec` 中添加 `Action` 字段
    - [x] 在 `AppBackupStatus` 中添加 `LastAction` 字段
    - [x] 运行 `make manifests` 和 `make generate`
- [x] 修改 `internal/controller/appbackup_controller.go`:
    - [x] 修改 `Reconcile` 逻辑:
        - [x] 实现 `Spec.Action` 检查与触发逻辑
        - [x] 实现 `Schedule=""` 时的初始备份逻辑 (`TotalBackups == 0`)
        - [x] 移除原有的无条件创建逻辑
