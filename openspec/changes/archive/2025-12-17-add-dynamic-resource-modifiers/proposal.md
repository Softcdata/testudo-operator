# 动态资源修改器 (Dynamic Resource Modifiers)

## 1. 摘要 (Summary)
本提案旨在改进 `AppRestore` 控制器，使其能够根据用户在 CR 中的定义，自动生成并管理 Velero 所需的 `ResourceModifier` ConfigMap。这将消除手动创建 ConfigMap 的步骤，并支持动态的资源修改策略（如 StorageClass 替换、PVC 清理等）。

## 2. 动机 (Motivation)
目前，`AppRestore` 控制器在创建 Velero Restore 时，硬编码引用了一个名为 `clean-pvc-volumename` 的 ConfigMap。
- **局限性**：用户无法为不同的恢复任务指定不同的修改规则（例如不同的 StorageClass 映射）。
- **运维负担**：管理员需要手动在 `velero` 命名空间下预先创建 ConfigMap。
- **自动化缺失**：无法通过 API 动态注入修改规则。

## 3. 目标 (Goals)
- 允许用户在 `AppRestore` CR 中定义资源修改规则。
- 自动管理 Velero `ResourceModifier` ConfigMap 的生命周期（创建、更新、删除）。
- 保持与 Velero `ResourceModifier` 格式的兼容性。

## 4. 非目标 (Non-Goals)
- 重新实现 Velero 的资源修改逻辑（我们只是生成配置供 Velero 使用）。
