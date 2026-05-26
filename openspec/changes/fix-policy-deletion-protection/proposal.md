# Change: 修复 DisasterPolicy 删除保护机制失效

## Why

当前 `DisasterPolicy` 的删除保护机制存在两个 bug，导致即使存在关联的 `AppBackup` 资源，策略也可以被直接删除：

1. **检查的资源类型错误**：`handleDelete` 检查的是 `DisasterBackupList`，但实际使用策略的是 `AppBackup` 资源。
2. **标签注入缺失**：`AppBackup` 控制器的 `syncLabels` 函数没有添加 `LabelDisasterPolicyName` 标签，导致无法通过标签检索关联关系。

## What Changes

- **修复 `AppBackup` 控制器**：在 `syncLabels` 函数中，当 `Spec.DisasterPolicy` 非空时，添加 `LabelDisasterPolicyName` 标签。
- **修复 `DisasterPolicy` 控制器**：在 `handleDelete` 中，将检查的资源类型从 `DisasterBackupList` 改为 `AppBackupList`。
- **历史数据兼容**：考虑到存量 `AppBackup` 可能缺少该标签，可能需要一次性迁移或在查询时使用双重策略（标签 + spec 字段检查）。

## Impact

- **受影响的规范**: `disaster-policy`（删除保护）
- **受影响的代码**:
  - `internal/controller/appbackup/appbackup_controller.go` → `syncLabels` 函数
  - `internal/controller/disasterpolicy_controller.go` → `handleDelete` 函数
- **风险**: 低。这是一个 bug 修复，不涉及 API 变更，行为符合原始设计意图。
