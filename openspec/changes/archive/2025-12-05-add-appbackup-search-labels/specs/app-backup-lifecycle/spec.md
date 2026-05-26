## ADDED Requirements

### Requirement: 搜索标签同步 (Search Labels Synchronization)
控制器必须 (MUST) 在 `AppBackup` 资源上自动维护一组用于搜索和过滤的标签。

#### Scenario: 标签初始化与更新
- **Given** 一个 AppBackup 资源被创建或更新
- **When** 控制器执行 Reconcile 循环
- **Then** 控制器必须 (MUST) 检查并确保以下标签存在且值正确：
    - `testudo.softcdata.com/app-backup-name`: 等于 `metadata.name`
    - `testudo.softcdata.com/app-backup-namespace`: 来源于 `spec.template.includedNamespaces`。若列表为空，则值为空字符串；否则为列表元素的逗号分隔字符串。
    - `testudo.softcdata.com/app-backup-cluster`: 等于 `spec.Cluster`
    - `testudo.softcdata.com/app-backup-type`: 若 `spec.schedule` 不为空，则为 "Schedule"，否则为 "Manual"。
    - `testudo.softcdata.com/app-backup-status`: 优先取 `status.latestBackupStatus`，若为空则取 `status.status`，若仍为空则设为 "Pending"。

#### Scenario: 状态变更同步
- **Given** AppBackup 的备份状态发生变化 (例如从 `InProgress` 变为 `Completed`)
- **When** 控制器更新 `status.Status` 字段
- **Then** 控制器必须 (MUST) 同步更新 `testudo.softcdata.com/app-backup-status` 标签以反映最新状态
