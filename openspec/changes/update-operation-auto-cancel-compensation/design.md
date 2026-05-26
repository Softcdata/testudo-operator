# Design: Operation Auto Cancel Compensation

## 背景
当前系统已有两类相近但未统一的行为：
- 某些 failover 失败步骤会自动把实例状态收敛回 `Protected`
- 用户可手工发起 `Cancel` 操作

本设计不新增第三套并行逻辑，而是把它们统一成“失败后的补偿语义矩阵”。

## 关键决策

### D1. 首期仅处理 Failover
- 自动补偿只覆盖 `Failover`。
- 原因：
  - 当前 cancel/rollback 语义主要围绕 failover 已经存在
  - 其余 operation type 语义尚未收敛

### D2. 先产出失败回滚矩阵，再决定自动补偿入口
- 失败回滚矩阵是 proposal 的一部分，不是实施阶段临时补图。
- 需要明确每个步骤失败后的默认终态、补偿模式和人工介入边界。

### D3. 自动补偿优先复用现有 cancel 路径，不创建新的 Cancel 子 CR
- 若现有 cancel 语义能覆盖，就优先复用其步骤和 helper。
- 但补偿仍在原 failover `DisasterOperation` 中持久化，不额外创建新 CR。
- 原因：避免额外 CR 噪音和时间线聚合歧义。

### D4. 自动补偿结果必须持久化到 Operation 状态，并以 Instance 终态配合体现
- 不能只通过 Event 展示。
- 上层需要稳定查询：是否触发、是否成功、失败原因、是否仍需人工介入。

## 失败回滚矩阵

| Failover 失败步骤 | 语义边界 | 首期补偿模式 | Operation 终态 | Instance 终态 |
| --- | --- | --- | --- | --- |
| `PreCheck` | 尚未进入破坏性步骤 | `DirectRollback` | `Failed + autoCancelStatus=Succeeded` | `Protected` |
| `PauseSchedules` | 同步可能已被暂停 | `CancelPath` | 同上；补偿失败则 `Failed` | 成功时 `Protected`，失败时 `Failed` |
| `FinalSync` | 最终同步可能已被触发 | `CancelPath` | 同上；补偿失败则 `Failed` | 同上 |
| `ScaleDownSource` | 源侧副本数已可能被修改 | `CancelPath` | 同上；补偿失败则 `Failed` | 同上 |
| `ScaleUpTarget` | 目标侧已可能被拉起 | `CancelPath` | 同上；补偿失败则 `Failed` | 同上 |
| `CheckReplicas` | 目标侧已被拉起，等待 ready | `CancelPath` | 同上；补偿失败则 `Failed` | 同上 |
| `SwitchRoles` / 终态写回 | 已接近角色切换歧义边界 | `NoAutoCancel` | `Failed` | 保持现场，人工介入 |

说明：
- `DirectRollback` 只用于“尚未进入破坏性步骤”的场景，不执行额外 cleanup step。
- `CancelPath` 复用现有 cancel helper，并在同一 Operation 上持久化补偿步骤。
- `NoAutoCancel` 明确属于首期排除范围。

## 数据模型

### Operator Status 扩展
在 `DisasterOperationStatus` 中新增：
- `autoCancelTriggered: bool`
- `autoCancelStatus: NotTriggered|Running|Succeeded|Failed`
- `autoCancelMode: DirectRollback|CancelPath|NoAutoCancel`
- `autoCancelReason: string`
- `autoCancelTriggerStep: string`
- `autoCancelCurrentStep: string`
- `autoCancelSteps[]: StepStatus`
- `autoCancelTriggeredAt: metav1.Time`
- `autoCancelCompletionTime: metav1.Time`
- `manualInterventionRequired: bool`

设计意图：
- `autoCancelTriggered/status/mode/reason` 提供 server 可直接消费的稳定摘要。
- `autoCancelCurrentStep/autoCancelSteps[]` 用于补偿过程幂等恢复，不污染原始 failover `steps[]`。
- `manualInterventionRequired` 将“补偿失败”与“首期排除”统一成上层可直接展示的布尔信号。

## Reconcile 流程

### 1. Failover 失败进入统一分发
当 failover 任一步骤失败或超时时：
1. 读取失败步骤
2. 查询失败回滚矩阵
3. 写入 `autoCancelTrigger*` 和 `autoCancelMode`
4. 根据模式分支

### 2. DirectRollback
适用于 `PreCheck`：
1. 将 Instance 直接收敛为 `Protected`
2. 清理错误状态
3. 写入：
   - `autoCancelTriggered=true`
   - `autoCancelStatus=Succeeded`
   - `manualInterventionRequired=false`
4. Operation 以 `Failed` 终态结束，并在 `message` 中明确“故障切换失败后已自动补偿”

### 3. CancelPath
适用于 `PauseSchedules` 到 `CheckReplicas`：
1. 写入 `autoCancelStatus=Running`
2. 初始化 `autoCancelSteps[]`
3. 复用现有 `Cancel` 的 helper：
   - `ScaleDownTarget`
   - `ScaleUpSource`
   - `ResumeSchedules`
4. 成功时：
   - Instance 收敛到 `Protected`
   - Operation 终态为 `Failed`
   - `autoCancelStatus=Succeeded`
5. 失败时：
   - Operation 终态为 `Failed`
   - `autoCancelStatus=Failed`
   - `manualInterventionRequired=true`

### 4. NoAutoCancel
适用于 `SwitchRoles` / 终态写回边界：
1. 不进入补偿路径
2. 直接写 `autoCancelTriggered=false`
3. `manualInterventionRequired=true`
4. Operation 保持 `Failed`

## 事件与时间线
- operator 必须继续发射结构化任务事件。
- 进入补偿路径时至少发射一条稳定语义进度事件：
  - `FailoverFailedAutoCancelStarted`
  - `FailoverFailedAutoCancelSucceeded`
  - `FailoverFailedAutoCancelFailed`
- 这些事件是观测辅助，不替代 status 字段。

## 幂等与重试语义
- 补偿过程不得创建新的 `DisasterOperation(cancel)`。
- 每次 Reconcile 必须先检查 `autoCancelStatus` 与 `autoCancelSteps[]`，从中断点继续推进。
- 已完成的补偿步骤不得重复执行破坏性动作。
- `SwitchRoles` 之后不做自动补偿，避免角色是否已翻转的歧义扩大。

## 备选方案

### 方案 A：所有失败都自动创建一个新的 Cancel Operation
- 放弃原因：会引入额外 CR 噪音，并让状态时间线变得更难聚合。

### 方案 B：所有失败都只保持现状，由人工触发 Cancel
- 放弃原因：无法满足条目 21 的产品目标，也不能消除当前部分步骤已经自动回滚的语义裂缝。

### 方案 C：只保留 Event，不增加稳定状态字段
- 放弃原因：server 仍然只能靠事件文本猜测补偿结果，无法形成稳定 API 契约。
