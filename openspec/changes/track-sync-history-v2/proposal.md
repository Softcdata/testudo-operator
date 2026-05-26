# 记录同步历史并集成统计

## 概述
为了更准确地计算和展示灾难恢复（Disaster Recovery）中资源同步（ResourceSync）和数据同步（DataSync）的成功率及历史详情，本提案旨在修改 V2 API 资源定义，在 `ResourceSync` 和 `DataSync` 的 Status 中增加详细的历史记录列表（History）。同时，Operator 将负责定期将这些历史记录同步到系统的统计对象（`BackupRestoreStatistics`）中，确保上层应用（如 API Server 和前端）能够通过统一的统计接口获取准确的汇总数据。

## 动机
目前，`ResourceSync` 和 `DataSync` 的 Status 仅记录了最近一次（Last）的同步状态（LastBackupName, LastSyncTime 等）。这导致以下问题：
1. **缺乏历史可追溯性**：用户无法查看过去 N 次同步的详情（如耗时、资源变更数、状态）。
2. **统计数据不准确**：当前的统计依赖于实时查询底层 `AppBackup` CR，或者依赖于 Operator 现有的、可能不完整的统计逻辑（仅统计 AppBackup，未关联到 Sync 任务上下文）。
3. **用户体验受限**：无法在界面上展示“同步历史曲线”或精确的“同步成功率”。

## 目标
1. **扩展 CRD Status**：在 `ResourceSyncStatus` 和 `DataSyncStatus` 中新增 `History` 字段，用于存储最近 N 条（例如 20 条）同步记录。
2. **定义历史记录结构**：包含开始时间、结束时间、耗时、备份/恢复名称、资源数量及最终状态。
3. **实现统计同步**：Operator 在更新 History 时，自动触发或更新关联的 `BackupRestoreStatistics` CR，确保统计数据（Total, Completed, Failed）准确反映这些 History 记录的汇总。

## 范围
- **涉及资源**: `ResourceSync` (V2), `DataSync` (V2).
- **涉及组件**: `disaster-operator` (CRD update, Controller logic), `disaster-server` (DTO update to show history).

## 非目标
- 修改 `AppBackup` 或 `AppRestore` 的内部逻辑（仅引用其名称和状态）。
- 实现无限长度的历史记录（将通过限制可以保存的记录数量来实现，例如保留最近 20 条）。
