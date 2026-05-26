# Tasks

## 1. Proposal
- [x] 1.1 评审 role drift condition 名称、枚举值、双活允许语义、错误态语义和豁免窗口

## 2. Operator
- [x] 2.1 在实例 reconcile 中补稳态 role drift 判定
- [x] 2.2 将结果写入 `status.conditions`
- [x] 2.3 当不可安全解释的 `RoleDrift=True` 时将实例置为 `Failed` 且 `reason=RoleDriftDetected`
- [x] 2.4 在 `Failed(reason=RoleDriftDetected)` 中继续复检，真实关系恢复后自动回到 `Protected` 或 `Active`
- [x] 2.5 为 Protected/Active/双活允许/操作中豁免/漂移失败/恢复六类路径补 tests

## 3. Alignment
- [x] 3.1 与 server 对齐 `Failed + RoleDriftDetected` 和 condition summary
- [x] 3.2 与 web 对齐列表/详情高亮语义和操作按钮禁用语义

## 4. Verification
- [x] 4.1 `openspec validate add-instance-role-drift-condition --strict`
