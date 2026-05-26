# Change: Add AppBackup Search Labels

## Why
为了方便运维人员和外部系统通过 Kubernetes 标签选择器（Label Selector）快速筛选和统计 AppBackup 资源，特别是在多集群、多命名空间的大规模环境下。目前只能通过 Field Selector 或解析 Status 字段来获取状态，效率较低且不便于使用 `kubectl -l` 进行过滤。

## What Changes
控制器将自动在 `AppBackup` 资源上维护一组反映其核心属性和状态的标签，为了明确标识这些标签属于 AppBackup 资源，Key 中将包含 `app-backup` 前缀：
- `testudo.softcdata.com/app-backup-name`: 记录 AppBackup 的名称。
- `testudo.softcdata.com/app-backup-namespace`: 记录备份目标的命名空间（来源于 `spec.template.includedNamespaces`，若包含 `*` 或为空则标记为 `all`，否则为逗号分隔的字符串）。
- `testudo.softcdata.com/app-backup-cluster`: 记录目标集群 (`spec.Cluster`)。
- `testudo.softcdata.com/app-backup-status`: 记录当前的备份状态 (`status.Status`)。

控制器将在 Reconcile 循环中动态更新这些标签，确保其与资源的实际状态保持一致。

## Impact
- **Affected Specs**: `app-backup-lifecycle`
- **Affected Code**: `internal/controller/appbackup_controller.go`
- **Performance**: 每次状态变更会触发一次 Metadata 更新，需要注意避免死循环（仅在标签值实际变化时更新）。
