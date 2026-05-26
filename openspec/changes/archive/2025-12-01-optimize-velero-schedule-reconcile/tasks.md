# 任务列表

- [x] 修改 `CreateVeleroSchedule` 方法，使其能区分"新创建"和"已存在"两种情况 (例如返回 `created bool`)
- [x] 在 `Reconcile` 逻辑中，如果是新创建的 Schedule，更新 Status 后直接返回
- [x] 实现 Spec 差异比对逻辑 (比较 `Schedule`, `Template`, `Paused` 等字段)
- [x] 修改更新逻辑，仅在检测到差异时执行 `Update`
