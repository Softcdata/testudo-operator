## ADDED Requirements

### Requirement: DisasterOperation 核心定义
`DisasterOperation` 必须 (MUST) 作为独立的操作资源，执行容灾相关的关键操作。

#### Scenario: 创建操作请求
- **GIVEN** 用户需要执行 Failover
- **WHEN** 创建 `DisasterOperation` CR
- **THEN** 必须 (MUST) 指定 `spec.instanceName` 引用 DisasterInstance
- **AND** 必须 (MUST) 指定 `spec.operationType` 为支持的操作类型

### Requirement: 支持的操作类型
DisasterOperation 必须 (MUST) 支持以下操作类型。

#### Scenario: Failover 操作
- **GIVEN** operationType 为 `failover`
- **WHEN** Controller 执行操作
- **THEN** 必须 (MUST) 依次执行 PauseSchedules, ScaleDownSource, FinalSync, ScaleUpTarget, SwitchRoles

#### Scenario: Reprotect 操作
- **GIVEN** operationType 为 `reprotect`
- **WHEN** DisasterInstance 处于 `Active` 状态
- **THEN** 必须 (MUST) 确认当前主集群 (Target) 状态，并建立反向同步 (Target -> Source)
- **AND** DisasterInstance 状态必须 (MUST) 恢复为 `Protected` (主集群维持为 Target)

#### Scenario: Undo 操作
- **GIVEN** operationType 为 `undo`
- **WHEN** DisasterInstance 处于 `Active` 状态
- **THEN** 必须 (MUST) 缩容当前主集群 (Target)
- **AND** 必须 (MUST) 扩容原主集群 (Source)
- **AND** 必须 (MUST) 交换主备角色 (Source 变回主)
- **AND** 必须 (MUST) 建立正向同步 (Source -> Target)
- **AND** DisasterInstance 状态必须 (MUST) 恢复为 `Protected`

#### Scenario: Pause 操作
- **GIVEN** operationType 为 `pause`
- **WHEN** Controller 执行操作
- **THEN** 必须 (MUST) 设置 DataSync 和 ResourceSync 的 `paused=true`
- **AND** DisasterInstance 状态必须 (MUST) 变为 `Paused`

#### Scenario: Resume 操作
- **GIVEN** operationType 为 `resume`
- **WHEN** DisasterInstance 处于 `Paused` 状态
- **THEN** 必须 (MUST) 设置 DataSync 和 ResourceSync 的 `paused=false`
- **AND** DisasterInstance 状态必须 (MUST) 恢复为 `Protected`

#### Scenario: SyncOnce 操作
- **GIVEN** operationType 为 `synconce`
- **WHEN** Controller 执行操作
- **THEN** 必须 (MUST) 立即触发一次 DataSync 和 ResourceSync

### Requirement: Failover 步骤执行
DisasterOperation 必须 (MUST) 按顺序执行 Failover 步骤。

#### Scenario: Step 0 - PreCheck
- **GIVEN** Failover 开始
- **WHEN** 执行 PreCheck 步骤
- **THEN** 必须 (MUST) 验证源与目的集群连通性
- **AND** 必须 (MUST) 验证当前无 InProgress 同步任务
- **AND** 如果检查失败，必须 (MUST) 终止操作（除非 force=true）

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

### Requirement: 强制模式
DisasterOperation 必须 (MUST) 支持强制模式以处理源集群不可达的情况。

#### Scenario: 强制 Failover
- **GIVEN** `spec.directives[].force: true`
- **WHEN** 源集群不可达
- **THEN** 必须 (MUST) 跳过 ScaleDownSource 步骤
- **AND** 必须 (MUST) 跳过 FinalSync 步骤
- **AND** 必须 (MUST) 直接执行 ScaleUpTarget

### Requirement: 步骤状态追踪
DisasterOperation 必须 (MUST) 详细记录每个步骤的执行状态。

#### Scenario: 步骤状态
- **GIVEN** 操作正在执行
- **WHEN** 更新 Status
- **THEN** `status.steps[]` 必须 (MUST) 包含每个步骤的状态
- **AND** 每个步骤必须 (MUST) 记录 `name`, `state`, `startTime`, `completionTime`
- **AND** 失败的步骤必须 (MUST) 记录 `message` 说明错误原因

### Requirement: 审计与历史
DisasterOperation 必须 (MUST) 保留操作历史用于审计。

#### Scenario: 操作完成
- **GIVEN** Failover 操作完成
- **WHEN** 操作成功
- **THEN** `status.state` 必须 (MUST) 设置为 `Completed`
- **AND** `status.completionTime` 必须 (MUST) 记录完成时间
- **AND** `status.roleStatus` 必须 (MUST) 记录切换后的角色分配

#### Scenario: 操作失败
- **GIVEN** Failover 某个步骤失败
- **WHEN** 错误发生
- **THEN** `status.state` 必须 (MUST) 设置为 `Failed`
- **AND** 失败的步骤必须 (MUST) 记录错误信息
- **AND** 不得 (MUST NOT) 继续执行后续步骤
