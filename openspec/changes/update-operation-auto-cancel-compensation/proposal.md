# Change: 在现有 cancel 语义之上补齐失败自动补偿

## Why
当前 operator 已经存在手工 `Cancel` 操作，也存在部分失败步骤自动把实例状态收敛回 `Protected` 的逻辑。但这些行为还没有被收敛成正式的“失败后自动补偿”能力。

条目 21 的核心不是重新发明一个 cancel 系统，而是先把现有 rollback / cancel 语义矩阵化，再定义自动补偿入口、终态和可观测性。

## What Changes

### 1. 正式引入失败回滚矩阵作为状态机前置契约
- proposal 必须先定义：
  - 哪些失败步骤进入直接回滚 (`DirectRollback`)
  - 哪些失败步骤复用现有 `Cancel` 路径 (`CancelPath`)
  - 哪些失败步骤首期仍停在 `Failed`
  - `Cancel` 当前允许在哪些状态和步骤执行

首期矩阵拟定为：

| Failover 失败步骤 | 现状副作用边界 | 首期补偿模式 | 目标终态 | 是否需人工介入 |
| --- | --- | --- | --- | --- |
| `PreCheck` | 尚未进入破坏性步骤 | `DirectRollback` | Instance=`Protected`，Operation=`Failed + autoCancelStatus=Succeeded` | 否 |
| `PauseSchedules` | 可能已有同步被暂停 | `CancelPath` | 同上 | 仅补偿失败时需要 |
| `FinalSync` | 可能已触发最终同步 | `CancelPath` | 同上 | 仅补偿失败时需要 |
| `ScaleDownSource` | 已修改源集群副本数 | `CancelPath` | 同上 | 仅补偿失败时需要 |
| `ScaleUpTarget` | 目标侧可能已被拉起 | `CancelPath` | 同上 | 仅补偿失败时需要 |
| `CheckReplicas` | 目标侧已被拉起，等待就绪 | `CancelPath` | 同上 | 仅补偿失败时需要 |
| `SwitchRoles` / 终态写回 | 已接近角色切换边界，语义可能歧义 | `NoAutoCancel` | Operation=`Failed` | 是 |

### 2. 自动补偿能力建立在现有 cancel / rollback 语义之上
- 自动补偿不是从零开始的新状态机。
- 它只能：
  - 复用现有 rollback-to-Protected 语义 (`DirectRollback`)，或
  - 在明确的失败步骤上触发一个系统自发的 `CancelPath`
- 首期不创建新的 `DisasterOperation(cancel)` 子 CR，而是在原 `DisasterOperation(failover)` 内持久化补偿状态并推进补偿步骤。

### 3. 首期只覆盖 failover 失败自动补偿
- 首期只在 `Failover` 失败场景启用自动补偿。
- `Reprotect`、`Undo`、`Drill` 暂不进入首期范围。

### 4. 自动补偿结果必须成为正式状态与事件契约
- operator 需要记录：
  - 是否触发自动补偿
  - 触发步骤
  - 采用的是直接回滚还是 cancel 路径
  - 触发原因
  - 最终成功 / 失败
  - 仍需人工介入的剩余点
- 上层 API 和时间线必须能稳定暴露这些结果。

首期字段拟定为：
- `status.autoCancelTriggered: bool`
- `status.autoCancelStatus: NotTriggered|Running|Succeeded|Failed`
- `status.autoCancelMode: DirectRollback|CancelPath|NoAutoCancel`
- `status.autoCancelReason: string`
- `status.autoCancelTriggerStep: string`
- `status.autoCancelCurrentStep: string`
- `status.autoCancelSteps[]: StepStatus`
- `status.autoCancelTriggeredAt: timestamp`
- `status.autoCancelCompletionTime: timestamp`
- `status.manualInterventionRequired: bool`

## Non-Goals
- 不修改现有手工 cancel 的基本语义。
- 不在首期覆盖所有 operation type。
- 不在 proposal 第一层引入新的用户可见操作按钮。
- 不新增新的“Recovered”之类终态枚举；自动补偿成功后仍沿用现有 `Failed` 终态，并由新增字段表达“失败后已补偿恢复”的结果语义。

## Impact
- Affected specs:
  - `disaster-operation`
- Affected code:
  - `internal/controller/disasteroperation/controller.go`
  - `internal/controller/disasteroperation/controller_retry.go`
  - `internal/controller/disasteroperation/*_test.go`
- Cross-repo impact:
  - `disaster-server`：detail/list/action 结果暴露
  - `cluster-disaster-web`：时间线与详情页展示

## Relationship to Existing Changes
- 现有 active change：`introduce-cancel-operation`
- 本 change 不替代手工 cancel。
- 本 change 以手工 cancel 作为现状输入，新增“失败自动补偿”的契约和结果暴露。
- 本 change 也依赖当前 failover 失败后隐式收敛 `Protected/Failed` 的既有实现，需要将这些隐式分支显式写成矩阵。

## Risks
- 若不先锁定失败回滚矩阵，就直接扩自动补偿，会继续造成重复补偿和终态覆盖冲突。
- 若自动补偿结果不成为稳定状态契约，上层只能靠事件猜测，仍然不可运维。
- 若把 `SwitchRoles` 之后的歧义区间也纳入首期自动补偿，会把“角色是否真正切换”问题进一步放大。
