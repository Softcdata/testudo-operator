# AppRestore Specification

## ADDED Requirements

### Requirement: 取消阶段映射 (MUST)

在计算 `AppRestore` 的 `BackupRestoreStatistics` 时，控制器 (MUST) 必须将 `PhaseCancelled` 状态映射到 `Statistics.Canceled` 计数器。它 (SHALL NOT) 不得将 `PhaseCancelled` 映射到 `Statistics.Failed`。

#### Scenario: AppRestore 已取消
当 `AppRestore` 进入 `Cancelled` 阶段时，统计同步逻辑增加 `Canceled` 计数，并且 (NOT) 不增加 `Failed` 计数。
