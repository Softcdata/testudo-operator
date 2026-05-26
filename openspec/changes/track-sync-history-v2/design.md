# 设计文档

## API 变更

### SyncHistoryRecord
在 `pkg/apis/disaster/v1/resourcesync_types.go` (或共享类型文件) 中定义：

```go
type SyncHistoryRecord struct {
    StartTime            *metav1.Time `json:"startTime,omitempty"`
    CompletionTime       *metav1.Time `json:"completionTime,omitempty"`
    Duration             string       `json:"duration,omitempty"`
    BackupName           string       `json:"backupName,omitempty"`
    RestoreName          string       `json:"restoreName,omitempty"`
    BackupResourceCount  int          `json:"backupResourceCount,omitempty"`
    RestoreResourceCount int          `json:"restoreResourceCount,omitempty"`
    Status               string       `json:"status,omitempty"` // "Completed", "Failed", "PartiallyFailed"
}
```

### ResourceSyncStatus & DataSyncStatus
新增字段：
```go
History []SyncHistoryRecord `json:"history,omitempty"`
```

## Controller 逻辑

### 记录生成时机
- **ResourceSync**: 
  - 当检测到关联的 `AppRestore` 完成（Phase=Completed/Failed）时，或者仅备份模式下 `AppBackup` 完成时。
  - 读取 AppBackup/AppRestore 的 Status (StartTime, CompletionTime, ItemsBackedUp, ItemsRestored, Errors)。
  - 生成 Record。
- **DataSync**: 
  - 类似，当 Trafficless Restore 完成或 VolumeSnapshot/Backup 完成时。

### 历史记录维护
- **最大记录数**: 20。
- **策略**: 每次 Append 新记录后，如果 `len(History) > 20`，则移除最早的记录 (`History = History[1:]`)。

### 统计同步 (Statistics Sync)
- **目标对象**: `BackupRestoreStatistics` (ScopeUID = AppBackup UID? No, ScopeUID usually matches Payload UID). 
- **修正**: 对于 `ResourceSync`，统计对象应该是关联到 ResourceSync CR 本身，还是其产生的 AppBackup?
  - 用户要求 "定期去他同步到统计中"。这意味着 Statistics 应该反映 ResourceSync 的历史。
  - 在 `BackupRestoreStatistics` 中，我们可以为 `ResourceSync` 创建一个聚合统计对象（ScopeType=Custom or Resource?）。
  - 或者复用现有的 AppBackup 统计？
  - 现有逻辑：AppBackup controller 更新 AppBackup 统计。
  - **新逻辑**: ResourceSync controller update **ResourceSync Statistics**.
    - ScopeRef: Kind=ResourceSync, Name=..., UID=...
    - Statistics Data: Sum(History.Status == Completed), Sum(History.Status == Failed).

- **实现**:
  - Controller Reconcile Loop 每次更新 History 后，获取（或创建）对应的 `BackupRestoreStatistics` CR。
  - 重新计算 History 中的 Success/Failure 总数。
  - 更新 `BackupRestoreStatistics.Status.Statistics`.

## 兼容性
- 旧的 ResourceSync 对象没有 History，更新后 History 为空，统计从 0 开始或仅统计新增记录。
