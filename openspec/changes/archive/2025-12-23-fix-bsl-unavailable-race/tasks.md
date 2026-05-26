# Tasks: 修复 BSL 竞态条件

- [x] 修改 `internal/controller/BSL.go`:
    - [x] 在 `updateBackupStorageLocation` 中，如果 BSL 状态不是 `Available`，返回错误提示 "BackupStorageLocation is unavailable"。
- [x] 修改 `internal/controller/appbackup/appbackup_pending.go`:
    - [x] 在调用 `ApplyStorageRepository` 后，检查返回的错误。
    - [x] 如果错误包含 "BackupStorageLocation is unavailable"，记录 Warning Event 并返回 `RequeueAfter: 5s`。
