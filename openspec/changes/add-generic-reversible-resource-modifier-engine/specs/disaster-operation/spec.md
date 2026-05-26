## MODIFIED Requirements

### Requirement: Failover 步骤执行
DisasterOperation 必须 (MUST) 按顺序执行 Failover 步骤。

#### Scenario: Step 0 - PreCheck
- **GIVEN** Failover 开始
- **WHEN** 执行 PreCheck 步骤
- **THEN** 必须 (MUST) 验证源与目的集群连通性
- **AND** 必须 (MUST) 验证当前无 InProgress 同步任务
- **AND** 必须 (MUST) 执行 modifier compile dry-run
- **AND** dry-run 失败时必须 (MUST) 终止操作并返回标准错误码（例如 `ModifierFeatureDisabled`、`ModifierRuleRejected`）
- **AND** `force=true` 仅可用于连通性/进行中任务类检查的豁免
- **AND** `force=true` 不得 (MUST NOT) 绕过 modifier compile dry-run 失败

#### Scenario: force 不得绕过 dry-run 失败
- **GIVEN** Failover 开始且 `force=true`
- **AND** modifier compile dry-run 失败
- **WHEN** PreCheck 结束
- **THEN** 操作必须 (MUST) 终止
- **AND** 不得 (MUST NOT) 进入 `ScaleDownSource`

#### Scenario: Step 1 - PauseSchedules
- **GIVEN** Failover 开始
- **WHEN** 执行 PauseSchedules 步骤
- **THEN** 必须 (MUST) 暂停所有同步调度
- **AND** 必须 (MUST) 等待当前运行中的任务完成

#### Scenario: Step 2 - FinalSync
- **GIVEN** PauseSchedules 完成
- **WHEN** 源集群可达
- **THEN** 必须 (MUST) 触发最后一次 DataSync 和 ResourceSync
- **AND** 必须 (MUST) 等待同步完成

#### Scenario: Step 3 - ScaleDownSource
- **GIVEN** FinalSync 完成
- **WHEN** 源集群可达
- **THEN** 必须 (MUST) patch 源集群的 Deployment/StatefulSet replicas=0
- **AND** 必须 (MUST) 等待 Pod 终止

#### Scenario: Step 4 - ScaleUpTarget
- **GIVEN** ScaleDownSource 完成
- **WHEN** 执行 ScaleUpTarget 步骤
- **THEN** 必须 (MUST) 读取注解 `testudo.softcdata.com/original-replica-count`
- **AND** 必须 (MUST) patch 目标集群的 Deployment/StatefulSet 恢复原始副本数
- **AND** 必须 (MUST) 等待 Pod 运行

#### Scenario: Step 5 - SwitchRoles
- **GIVEN** ScaleUpTarget 完成
- **WHEN** 执行 SwitchRoles 步骤
- **THEN** 必须 (MUST) 交换 DisasterInstance 的 primaryCluster 和 secondaryCluster
- **AND** 必须 (MUST) 更新 DisasterInstance 状态为 `Active`

## ADDED Requirements

### Requirement: Reprotect PreCheck 必须执行 modifier dry-run

DisasterOperation 必须 (MUST) 在 Reprotect 进入破坏性阶段前执行 modifier compile dry-run。

#### Scenario: Reprotect 预检失败提前终止
- **GIVEN** Reprotect 开始
- **WHEN** modifier compile dry-run 失败
- **THEN** 必须 (MUST) 在执行破坏性步骤前终止操作
- **AND** 必须 (MUST) 在步骤状态中记录标准错误码
