# 任务清单：修复 DisasterPolicy 删除保护机制

## 1. AppBackup 标签注入

- [x] 1.1 在 `internal/controller/appbackup/appbackup_controller.go` 的 `syncLabels` 函数中，添加 `LabelDisasterPolicyName` 标签注入逻辑
  - 当 `ab.Spec.DisasterPolicy != ""` 时，设置 `ab.Labels[LabelDisasterPolicyName] = ab.Spec.DisasterPolicy`
  - 当策略字段为空时，确保移除该标签（避免残留）

## 2. DisasterPolicy handleDelete 修复

- [x] 2.1 修改 `internal/controller/disasterpolicy_controller.go` 的 `handleDelete` 函数
  - 将 `DisasterBackupList` 替换为 `AppBackupList`（修正类型）
  - 保留对 `DisasterJobList` 的检查（如果仍需要）

## 3. 历史数据兼容性

- [x] 3.1 评估是否需要对存量 `AppBackup` 进行标签补全
  - **结论**: 采用方案 A（重启 Operator）。重启后 AppBackup Controller 会重新 Reconcile 所有资源，自动执行 `syncLabels` 补全标签。

## 4. 测试验证

- [x] 4.1 编写/更新 `disasterpolicy_controller_test.go` 的删除保护测试用例
  - 场景：创建一个 AppBackup 引用策略 → 尝试删除策略 → 验证被阻塞 ✓
  - 场景：删除 AppBackup 后 → 删除策略 → 验证成功 ✓
  - 测试结果: 2 Passed | 0 Failed
- [x] 4.2 本地手动验证
  - 需要重启运行中的 Operator 以应用修复

## 5. 增强：添加 Phase 状态字段

- [x] 5.1 在 `DisasterPolicyStatus` 中添加 `Phase` 字段 (`Active`/`Deleting`)
- [x] 5.2 更新 Controller 在正常 Reconcile 时设置 `Phase = Active`
- [x] 5.3 更新 Controller 在删除被阻塞时设置 `Phase = Deleting`
- [x] 5.4 添加 `Phase` 的 printcolumn 用于 kubectl 显示

## 6. Server 端：策略名称列表排除删除中策略

- [x] 6.1 修改 `disaster-server` 的 `/policies/names` 接口
  - 在 `policyNames` 函数中添加过滤逻辑
  - 排除 `Status.Phase == PolicyPhaseDeleting` 的策略
- [x] 6.2 更新 `DisasterPolicyStatusDTO`
  - 添加 `Phase` 字段
  - 更新类型转换函数 `ConvertStatusToDTO`

## 7. 文档更新

- [x] 7.1 更新 Apipost 接口文档
  - 更新 `/apis/policies.testudo.softcdata.com/v1/policies` (列表) 响应示例，增加 `phase` 字段
  - 更新 `/apis/policies.testudo.softcdata.com/v1/policies/:name` (详情) 响应示例，增加 `phase` 字段
  - 在接口详细说明 (`description`) 中添加 `Phase` 字段的状态定义 (`Active`/`Deleting`)
  - 在接口详细说明中补充 `reason` 和 `message` 字段的含义及示例

