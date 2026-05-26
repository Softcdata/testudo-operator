# AppBackup 手动触发与重试机制提案

## 摘要
为 `AppBackup` 引入基于 Spec 的手动触发备份和重试机制。通过在 `Spec` 中定义 `Action` 字段来触发立即备份、重试或初始化备份，并在 `Status` 中记录最后一次处理的操作，以实现声明式的操作触发。

## 动机
1. **声明式触发**: 用户希望通过修改 Spec（而非 Annotation）来触发备份或重试，这更符合 Kubernetes 的声明式 API 风格。
2. **统一行为**: 将“手动触发”、“失败重试”和“首次创建即备份”统一为 Action 机制，简化控制器逻辑。
3. **修复缺陷**: 修复 `Schedule=""` 时可能导致的无限备份问题。

## 设计方案

### 1. API 变更
在 `AppBackupSpec` 中增加 `Action` 字段，在 `AppBackupStatus` 中增加 `LastAction` 字段。

```go
type AppBackupSpec struct {
    // ...
    // Action defines the manual action to be performed.
    // +optional
    Action *BackupAction `json:"action,omitempty"`
}

type BackupAction struct {
    // Type is the type of the action.
    // Valid values are: "Backup", "Retry", "Cancel".
    Type string `json:"type"`

    // RequestAt is the time when the action was requested.
    // The controller will process the action if this timestamp is newer than the one in status.
    RequestAt metav1.Time `json:"requestAt"`
}

type AppBackupStatus struct {
    // ...
    // LastAction records the last processed manual action.
    LastAction *BackupAction `json:"lastAction,omitempty"`
}
```

### 2. 触发机制
用户通过更新 `Spec.Action` 来触发操作：
- **Backup**: 用于手动触发一次新的备份（执行）。
- **Retry**: 用于重试失败的备份。
- **Cancel**: 用于取消正在进行的备份。

注意：Velero Schedule 的创建和更新由控制器自动根据 `Spec.Schedule` 字段进行声明式管理，无需 Action 触发。

### 3. 控制器逻辑变更
修改 `Reconcile` 逻辑：

1. **检查 Action**:
   - 获取 `Spec.Action`。
   - 如果 `Spec.Action` 不为空，且 (`Status.LastAction` 为空 或 `Spec.Action.RequestAt` 晚于 `Status.LastAction.RequestAt`)：
     - 如果 Type 为 "Backup" 或 "Retry":
       - 执行 `CreateVeleroBackup`。
     - 更新 `Status.LastAction` 为 `Spec.Action`。
2. **保留隐式逻辑 (Schedule="")**:
   - 当 `Schedule` 为空时，如果 `TotalBackups == 0`，控制器会自动执行一次备份（保留原有的“首次创建即备份”体验）。
3. **Schedule 管理 (Schedule!="")**:
   - 当 `Schedule` 不为空时，控制器会自动创建或更新对应的 Velero Schedule。
   - Velero Schedule 的名称固定为 `app-schedule-<name>`，确保幂等性。

## 交互示例

### 手动触发/重试
```yaml
apiVersion: testudo.softcdata.com/v1
kind: AppBackup
metadata:
  name: my-app
spec:
  # ...
  action:
    type: Backup # 或 Retry
    requestAt: "2023-12-03T12:00:00Z" # 更新时间戳以触发
```
