## 1. CRD updates
- [x] 1.1 在 `disasterdrill_types.go` 中新增 `DrillStateCleaningUp` 和 `DrillStateCleanedUp` 状态，及 `CleanUp bool` 字段。
- [x] 1.2 在 `disasteroperation_types.go` 中新增 `OperationTypeDrillCleanup` (值为 `"drill-cleanup"`)。

## 2. Controllers Update
- [x] 2.1 在 `DisasterDrill` controller 中监听 `CleanUp=true` 事件，并生成关联的 `DisasterOperation (type=drill-cleanup)`。
- [x] 2.2 在 `DisasterOperation` 中增加 `drill-cleanup` 执行逻辑前置检查，解析 `NamespaceMapping`。
- [x] 2.3 实现 **无 NamespaceMapping** 场景下的清理逻辑（利用已有的 `ScaleDownTarget` 动作将特定集群的对应容灾实例副本数缩为 0）。
- [x] 2.4 实现 **有 NamespaceMapping** 场景下的清理逻辑（读取目标集群 Namespace，删除恢复资源或整个 Namespace）。
- [x] 2.5 `DisasterOperation` 成功后将其状态同步回 `DisasterDrill`（进入 `CleanedUp`）。

## 3. Testing
- [x] 3.1 编写单元测试验证 `DisasterDrill` 转入清理阶段状态。
- [x] 3.2 编写 BDD 测试验证基于 `RestoreModeReuse` (无 NS 映射) 场景被正确地通过缩容回 0 完成清理。
- [x] 3.3 编写 BDD 测试验证 `NamespaceMapping` 分配的新 NS 资源能被完全删除（或 NS 被删除）。
