# Cleanup Design for DisasterDrill

## 背景

容灾演练 (DisasterDrill) 的目标是在不干扰主集群业务的前提下，在备集群（或特定的演练集群）恢复应用数据与资源，进而进行容灾有效性验证。
当演练执行完毕（`Completed`）后，目前集群内会遗留以下演练环境，持续消耗多余的计算和存储资源：
- 如果没有 `NamespaceMapping`：使用的是备集群原来的命名空间，相当于复用了备集群资源并进行了数据覆盖。此时容灾的副本数在演练期间被拉起至期望大小。
- 如果配置了 `NamespaceMapping`：在特意映射的新命名空间中，创建了新的工作负载等资源。

为让系统在完成演练后不继续占据资源，需要设计一套演练自动清理机制 (Drill Cleanup Operation)。

## 架构决策

**机制选型**：在现有的 `DisasterDrill` CRD 中扩展 `spec.cleanup` (bool) 标志。

### 替代方案评估

| 选型方案 | 流程说明 | 优点 | 缺点 | 最终决策 |
| -------- | ------ | ---- | ---- | -------- |
| 删除 Drill CR 时触发 Finalizer 清理 | 用户删除 `DisasterDrill`，Controller 执行清理并移除 CR。 | 无需新字段 / 操作最少 | 用户无法保留演练历史记录和状态，一旦触发清理连同结果也被删除 | ❌ (不优) |
| 新建独立的 `DrillCleanup` CRD | 用户通过提交一条资源申请清理，类似对旧有 Drill 的引用 | 分离演练与清理周期 | 增加 API 复杂度，对仅一次性配置略显繁琐。 | ❌ |
| **在 Drill CR 中添加 `spec.cleanup: true` 触发** | 对 `DisasterDrill` 执行 Patch，系统响应产生 `DisasterOperation (type=drill-cleanup)` 运行。 | 用户可先保存 Drill 历史供查看；状态清晰 (`CleaningUp` -> `CleanedUp`) | CRD 本身上多了个修改步骤 | ✅ (选中本案) |

## 清理动作分场景设计

1. **若当时演练无 `NamespaceMapping`**： 
   - 意味着在备集群默认命名空间原地发生了拉起和数据恢复 (RestoreModeReuse 或是只做原地恢复)。
   - **清理逻辑**：
     - 不要删除命名空间，因为这是备用集群自带的底层环境（可能存在同步相关的设置且为灾备架构的有机组成）。
     - 将目标集群当前业务相关的 Deployment、StatefulSet 直接 **缩容至 0 副本数** (ScaleDownTo Zero)。

2. **若当时演练有 `NamespaceMapping` (例如 `app-ns -> drill-ns`)**： 
   - 意味着备集群原地不变，恢复资源是在特意指定的映射空间（`drill-ns`）内部署。
   - **清理逻辑**：
     - 若 `drill-ns` 命名空间是演练通过新建才产生的，其内部的关联工作负载全是验证环境专享。
     - 直接删除目标映射关联资源群组（包含：Deployment, Service, PVC, Endpoint 等恢复产生的全级附属）。考虑到更稳妥：也可以直接删除或打理演练被派发的特许新 Namespace (Namespace Deletion)。

## 执行层级

Drill Cleanup 与最初的 Drill 操作一致，由 `DisasterOperationController` 来承载并执行 `OperationTypeDrillCleanup` (或重构为 `CleanupDrill`) 类型任务，确保其底层复用了 `Level/Instances` 这种多级调度树模式。
`DisasterDrill` 资源监听 `spec.cleanup` 被切换后，仅负责创建一个对应的 `DisasterOperation` 下发，自身监控 `Operation` 状态回写到 `CleanedUp`。
