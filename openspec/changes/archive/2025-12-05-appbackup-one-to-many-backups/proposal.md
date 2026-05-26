# AppBackup 一对多备份关系提案

## 摘要
目前 `AppBackup` 在非定时模式（Schedule 为空）下，与 Velero `Backup` 是一对一的关系。每次 Reconcile 都会检查同一个名称的 Backup。
本提案旨在将此关系扩展为一对多，允许 `AppBackup` 创建并管理多个 Velero `Backup` 实例（基于时间戳命名），并在 Status 中记录备份历史、总数以及最新状态。

## 动机
用户希望 `AppBackup` 能够作为备份历史的管理者，而不仅仅是单个备份任务的映射。
通过支持一对多关系，用户可以：
1. 保留多次手动触发的备份记录。
2. 在 `AppBackup` 状态中直观地看到备份历史和统计信息。

## 设计方案

### 1. 命名与关联
- **命名**: 使用基于时间戳的命名规则（用户已修改 `GenBakcupName`）。
- **关联**: 在创建 Velero `Backup` 时，添加 Label `testudo.softcdata.com/app-backup-uid: <AppBackupUID>`，以便控制器通过 Label Selector 找回所有关联的备份。

### 2. 状态记录 (Status)
`AppBackupStatus` 将新增以下字段：
- `TotalBackups` (int): 管理的备份总数。
- `History` ([]BackupRecord): 备份历史列表（包含名称、状态、创建时间等）。
- `BackupStatus` (Velero BackupStatus): **最新**一个备份的详细状态。
- `Status` (string): **最新**一个备份的 Phase。

### 3. 控制器逻辑
- **Create**: 创建 Backup 时打上关联 Label。
- **Reconcile**:
    1. **优化**: 如果是首次创建（即 `AppBackup` 刚创建了第一个 Velero Backup，且之前记录的 `TotalBackups` 为 0），则跳过 List 查询，直接使用新创建的 Backup 构建 Status。
    2. **常规**: 根据 Label 列出所有属于该 `AppBackup` 的 Velero Backup。
    3. 按创建时间倒序排列。
    4. 更新 `TotalBackups`。
    5. 更新 `History` 列表。
    6. 将最新的 Backup 状态同步到 `AppBackup.Status`。

## 兼容性
- 这是一个向后兼容的变更，旧的 Backup 如果没有 Label 可能需要手动处理或在迁移逻辑中处理（或者仅对新创建的生效）。
