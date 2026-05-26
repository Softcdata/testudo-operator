# API 变更规范

## AppBackupStatus

修改 `pkg/apis/disaster/v1/appbackup_types.go` 中的 `AppBackupStatus` 结构体。

```go
type AppBackupStatus struct {
    // ... 现有字段
    
    // TotalBackups records the total number of backups managed by this AppBackup
    TotalBackups int `json:"totalBackups,omitempty"`
    
    // History records the list of recent backups
    History []BackupRecord `json:"history,omitempty"`
}

type BackupRecord struct {
    // Name is the name of the Velero Backup
    Name string `json:"name"`
    
    // Phase is the current phase of the Backup
    Phase string `json:"phase"`
    
    // StartTimestamp is the time when the backup was started
    StartTimestamp *metav1.Time `json:"startTimestamp,omitempty"`
    
    // CompletionTimestamp is the time when the backup completed
    CompletionTimestamp *metav1.Time `json:"completionTimestamp,omitempty"`

    // Errors is the number of errors that occurred during the backup
    Errors int `json:"errors,omitempty"`

    // Warnings is the number of warnings that occurred during the backup
    Warnings int `json:"warnings,omitempty"`

    // Expiration is the time when the backup expires
    Expiration *metav1.Time `json:"expiration,omitempty"`
}
```

## Label 规范

所有由 `AppBackup` 创建的 Velero Backup 必须包含以下 Label：

- Key: `testudo.softcdata.com/app-backup`
- Value: `<AppBackup.Name>`

## 行为变更

### Reconcile 流程

1. **Check Creation**: 尝试创建 Velero Backup。
2. **Optimization (首次创建)**:
   - 如果 `CreateVeleroBackup` 返回 `created=true` **且** `AppBackup.Status.TotalBackups == 0` (或 History 为空)。
   - 直接构造 Status:
     - `TotalBackups` = 1
     - `History` = `[{Name: newBackup.Name, Phase: newBackup.Status.Phase, ...}]`
     - `BackupStatus` = `newBackup.Status`
     - `Status` = `newBackup.Status.Phase`
   - **Return** (跳过后续 List)。
3. **List**: 使用 Label Selector `testudo.softcdata.com/app-backup=<name>` 获取所有 Backup。
4. **Sort**: 按 `CreationTimestamp` 降序排列。
5. **Update Status**:
   - `TotalBackups` = len(list)
   - `History` = 转换前 N 个（例如前 10 个，避免 Status 过大）Backup 信息。
   - `BackupStatus` = list[0].Status (最新的)
   - `Status` = list[0].Status.Phase (最新的)
