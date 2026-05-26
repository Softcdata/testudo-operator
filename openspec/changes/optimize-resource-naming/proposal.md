# 优化资源命名策略提案

## 提案背景
当前灾备操作生成的资源名称存在严重的 redundancy (冗余) 和长度超标问题。
例如，一个 Drill DataSync 任务会生成如下名称链：
1. **Velero Backup**: `app-backup-ds-dr-ds-pppp1-dr-instance-test1-1770705513` (长度 60+)
2. **AppRestore CR**: `restore-app-backup-ds-dr-ds-pppp1-dr-instance-test1-1770705513` (长度 70+)
   - 包含多层 `restore-` 和 `app-backup-` 前缀。
3. **Velero Restore**: `app-restore-restore-app-backup-ds-dr-ds-pppp1-dr-instance-test1-1770705513` (长度 80+)

这不仅使得资源名称难以阅读，而且很容易超过 Kubernetes 资源名称的 63 字符限制 (`DNS-1123 subdomain`)，导致创建失败。

## 目标
1. **缩短名称长度**：确保所有生成的资源名称严格控制在 63 字符以内。
2. **消除冗余前缀**：避免 `app-restore-restore-...` 这种多层嵌套。
3. **保持唯一性与可读性**：使用确定性哈希 (Hash) 和简短的时间戳/后缀。

## 详细设计

### 1. 通用命名规则
采用短前缀 + 资源标识(截断) + 唯一后缀(Hash/Time) 的格式。

`{prefix}-{resource[:20]}-{hash/time}`

### 2. AppBackup (CR) -> Velero Backup
**当前逻辑**: `GenBackupName` -> `app-backup-{name}-{timestamp}`
**优化逻辑**:
- 前缀改为 `bak-`。
- 如果 `name` 超过 40 字符，截断并在末尾添加 Hash。
- `timestamp` 使用 Hex 格式 (8字符) 代替十进制 (10字符)。

**示例**: `bak-ds-dr-instance-1-6989ab29`

### 3. DataSync -> AppRestore (CR)
**当前逻辑**: `handleRestore` -> `restore-{BackupName}`
**优化逻辑**:
- 增加类型标识，避免同名 DataSync/ResourceSync 冲突。
- 使用 `Hash(BackupName)` (前6位) 作为后缀，确保针对同一备份的恢复操作名是幂等的。
- 格式: `rec-ds-{DataSyncName[:20]}-{BackupNameHash}`。

**示例**: `rec-ds-dr-instance-1-7f8a9b` (BackupNameHash derived from backup name)

### 3.1 ResourceSync -> AppRestore (CR)
**当前逻辑**: `handleRestore` -> `restore-{BackupName}`
**优化逻辑**:
- 格式: `rec-rs-{ResourceSyncName[:20]}-{BackupNameHash}`。

**示例**: `rec-rs-dr-instance-1-7f8a9b`

### 4. AppRestore (CR) -> Velero Restore
**当前逻辑**: `GenRestoreName` -> `app-restore-{AppRestoreName}`
**优化逻辑**:
- 检查 `AppRestoreName` 是否以标准前缀 (如 `rec-`, `drr-`, `ddr-`) 开头。
- 统一加短前缀 `res-` (Restore)。

**最终示例**:
- 数据恢复: `res-rec-ds-dr-instance-1-6989ab29`
- 资源恢复: `res-rec-rs-dr-instance-1-6989ab29`

### 5. DisasterOperation -> AppRestore (CR)
**当前逻辑**: `ddr-{opName[:10]}-{hash[:6]}`
**优化逻辑**:
- 保持现状，已经足够短且唯一。
- `ddr-` (Drill Data Restore)
- `drr-` (Drill Resource Restore)

## 实施步骤
1. 修改 `internal/controller/appbackup/controller.go`: 优化 `GenBackupName`。
2. 修改 `internal/controller/datasync/controller.go`: 优化 `handleRestore` 中的名称生成。
3. 修改 `internal/controller/apprestore/controller.go`: 优化 `GenRestoreName`。
4. 提供简单的迁移/兼容逻辑（新命名只应用于新创建的资源，旧资源不受影响）。

## 兼容性考虑
- 这是一个非破坏性变更 (Non-breaking change)。
- 仅影响新创建的资源名称。
- Operator 在查找旧资源时（如果通过 Label 查找）不受影响；如果通过 Name 查找，需确保逻辑中没有硬编码的旧格式假设 (代码审查显示主要通过 Label 或 Status 引用 Name，因此安全)。
