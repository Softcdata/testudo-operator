# Change: Add Drill Cleanup Operation

## Why
在真实的容灾演练 (Drill) 场景中，底层自动恢复任务完成后（即状态进入 `Completed` 时），演练流程**并不会立即结束**。用户需要在接下来的时间内，自行登录或连入这个生成的演练环境进行功能验收、数据完整性测试和系统验证。
当用户彻底验证完成后，我们不能强行替用户中断环境；相反，需提供一个由用户主管控制的交互：“清理资源”，作为真正演练生命周期的收尾动作，以免其始终停留在“已完成”进而持续占用被分配到的计算和存储资源。如果底层完全没有映射关系（即使用原备用集群的骨架进行重写），缩容为 0 即可达到资源释放。如果有映射到特定命名空间生成的一套资源，就应自动地将新空间内资源予以释放。

## What Changes
- 修改 `v1.DisasterDrillSpec`，新增 `CleanUp bool` 字段，作为承接外部 API 下达（用户点击"清理"按钮）触发清理演练的标识。
- 修改 `v1.DisasterDrillStatus`，在 `DrillState` 中新增 `CleaningUp` 和 `CleanedUp` 两种状态代表验证后的释放阶段。
- 修改 `v1.OperationType`，新增 `OperationTypeDrillCleanup ("drill-cleanup")`。
- 修改 `DisasterOperation` 侧来支持对于 `drill-cleanup` 类型操作逻辑进行处理。
  - **无 NamespaceMapping (命名空间映射) 的场景**：通过缩容目标集群中与容灾实例关联的工作负载（缩为 0）以撤出计算资源。由于这复用了原备集群的主工作命名空间，不允许有跨资源的直接 DELETE 毁灭性删除操作。
  - **有 NamespaceMapping (命名空间映射) 的场景**：删除受控目标集群中被映射出（仅为本次演练派发创建）的关联资源/命名空间，直接回收垃圾。
- **BREAKING**: 无破坏性变更。

## Impact
- Affected specs: `disaster-operator/openspec/specs/drill/spec.md`
- Affected code:
  - `pkg/apis/disaster/v1/disasterdrill_types.go`
  - `pkg/apis/disaster/v1/disasteroperation_types.go`
  - `internal/controller/disaster/disasterdrill_controller.go`
  - `internal/controller/disaster/disasteroperation_controller.go` (新增 Drill Cleanup)
