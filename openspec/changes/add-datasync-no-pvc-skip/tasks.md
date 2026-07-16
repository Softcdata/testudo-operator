## 1. 规范与设计

- [x] 1.1 审阅并确认无 PVC no-op 的状态语义：成功跳过、非失败。
- [x] 1.2 确认 labelSelector 场景下 PVC 发现规则：直接匹配 PVC 或匹配 Pod 引用 PVC。
- [x] 1.3 确认 `Skipped` 历史记录是否需要前端/API 展示额外适配；本仓库不新增 CRD/API 字段，operator 侧已完成 history/condition/statistics 兼容，前端中文文案可另行适配。

## 2. Operator 实现

- [x] 2.1 在 DataSync 控制器新增源端 PVC 发现函数，按 namespace、labelSelector、Pod PVC 引用计算可恢复 PVC。
- [x] 2.2 在 `executeSync` 中将无 PVC 预检放到 StorageRepository readiness 之前。
- [x] 2.3 无 PVC 时不创建、不更新、不触发 `AppBackup`，不创建 `AppRestore`。
- [x] 2.4 新增 no-op 成功收敛 helper，写入 `Ready`、`lastSyncTime`、condition、history 和 task finished 事件。
- [x] 2.5 调整 `syncStatistics`，将 `Skipped` 视为 completed。
- [x] 2.6 保持有 PVC 场景现有 AppBackup/AppRestore、initial PVC cleanup、hooks 和 modifier 行为不变。

## 3. 测试

- [x] 3.1 单测：无 PVC 首次同步直接 skipped success，且未创建 AppBackup/AppRestore。
- [x] 3.2 单测：无 PVC 手动触发直接 skipped success，且不触发旧 AppBackup action。
- [x] 3.3 单测：存在 PVC 时继续进入现有 AppBackup 创建/触发流程。
- [x] 3.4 单测：labelSelector 匹配 Pod 且 Pod 挂载 PVC 时不跳过。
- [x] 3.5 单测：labelSelector 不匹配 Pod/PVC 时跳过。
- [x] 3.6 单测：源集群 PVC/Pod list 失败时进入 Failed，不写 skipped。
- [x] 3.7 单测：无 PVC 且 StorageRepository 不可用时仍 skipped success。
- [x] 3.8 单测：有 PVC 且 StorageRepository 不可用时保持 StorageUnavailable 失败。
- [x] 3.9 单测：`Skipped` 历史记录在统计中计入 completed，不计入 failed。

## 4. 验证

- [x] 4.1 运行 `go test ./internal/controller/datasync ./internal/controller/restore ./internal/controller`.
- [x] 4.2 运行 `make manifests`，确认无 CRD 字段变更或仅包含预期常量/注释变化。
- [x] 4.3 运行 `openspec validate add-datasync-no-pvc-skip --strict`。
