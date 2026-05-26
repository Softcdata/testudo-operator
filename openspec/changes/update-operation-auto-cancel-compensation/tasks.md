# Tasks

## 1. Proposal
- [ ] 1.1 产出 failover 失败回滚矩阵并完成评审
- [ ] 1.2 明确 `DirectRollback` / `CancelPath` / `NoAutoCancel` 三类补偿模式
- [ ] 1.3 明确首期只覆盖 failover 自动补偿
- [ ] 1.4 明确自动补偿结果的状态字段、时间戳字段和人工介入信号

## 2. Operator Schema
- [ ] 2.1 在 `DisasterOperationStatus` 中加入 `autoCancelTriggered`、`autoCancelStatus`、`autoCancelMode`
- [ ] 2.2 加入 `autoCancelReason`、`autoCancelTriggerStep`、`manualInterventionRequired`
- [ ] 2.3 加入 `autoCancelCurrentStep`、`autoCancelSteps[]`、`autoCancelTriggeredAt`、`autoCancelCompletionTime`
- [ ] 2.4 更新 CRD 生成产物与序列化兼容测试

## 3. Operator Controller
- [ ] 3.1 将 failover 步骤失败统一收敛到自动补偿分发入口
- [ ] 3.2 为 `PreCheck` 接入 `DirectRollback`
- [ ] 3.3 为 `PauseSchedules`、`FinalSync`、`ScaleDownSource`、`ScaleUpTarget`、`CheckReplicas` 接入 `CancelPath`
- [ ] 3.4 复用现有 cancel helper，在原 failover operation 内推进补偿步骤
- [ ] 3.5 为 `SwitchRoles` / 终态写回边界保持 `NoAutoCancel + manualInterventionRequired=true`
- [ ] 3.6 为自动补偿开始 / 成功 / 失败补结构化事件
- [ ] 3.7 为自动补偿成功 / 失败 / 幂等恢复补 controller tests

## 4. Server / Web Alignment
- [ ] 4.1 与 `add-operation-auto-cancel-result-api` 对齐字段名、枚举值和时间戳语义
- [ ] 4.2 为 detail/list 定义统一 `autoCancelSummary` 投影对象
- [ ] 4.3 为时间线定义“失败 -> 自动补偿触发 -> 自动补偿成功/失败”节点来源
- [ ] 4.4 与 web 对齐自动补偿成功但 failover 本身失败的展示口径

## 5. Verification
- [ ] 5.1 `openspec validate update-operation-auto-cancel-compensation --strict`
- [ ] 5.2 选定至少两个 `CancelPath` 失败步骤验证自动补偿路径
- [ ] 5.3 验证 `PreCheck -> DirectRollback` 与 `SwitchRoles -> NoAutoCancel` 两类边界
