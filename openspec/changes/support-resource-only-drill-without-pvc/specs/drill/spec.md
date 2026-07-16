## ADDED Requirements

### Requirement: Drill 必须支持显式无数据卷的纯资源恢复

系统必须 (MUST) 仅在 DataSync 明确且一致地声明当前保护范围没有可恢复 PVC 时，将该实例的 Drill 分类为 `ResourceOnly`。`ResourceOnly` 必须恢复 ResourceSync 资源备份并执行 ScaleUp，但不得进入任何数据恢复链路。

#### Scenario: 明确无 PVC 的实例进入 ResourceOnly Ready
- **GIVEN** DisasterInstance 关联的 ResourceSync 为 `Ready` 且存在非空 `status.lastBackupName`
- **AND** DataSync 为 `Ready`
- **AND** DataSync condition 为 `Type=NoDataVolumes`、`Status=True`、`Reason=NoPVCFound`
- **AND** DataSync 最新一条 history 的 `status=Skipped`
- **WHEN** DisasterDrill 执行 Pending 前置校验
- **THEN** DisasterDrill 必须 (MUST) 进入 `Ready`
- **AND** `status.validationResults.backupAvailable` 必须 (MUST) 为 true
- **AND** `status.restoreMode` 必须 (MUST) 为 `ResourceOnly`

#### Scenario: ResourceOnly 执行纯资源演练
- **GIVEN** 一个已经确认的 `ResourceOnly` DisasterDrill
- **WHEN** 关联 DisasterOperation 执行演练
- **THEN** 步骤必须 (MUST) 为 `RestoreResource -> ScaleUp`
- **AND** 不得 (MUST NOT) 创建 data AppRestore 或 Velero data Restore
- **AND** 不得 (MUST NOT) 创建 trafficless Pod 或 PodVolumeRestore
- **AND** 资源恢复与 ScaleUp 成功后 Operation 必须 (MUST) 完成

#### Scenario: 明确 no-data 状态优先于陈旧数据备份名
- **GIVEN** DataSync 同时保留一个历史 `status.lastBackupName`
- **AND** DataSync 当前满足 `Ready + NoDataVolumes=True/NoPVCFound + latest history Skipped`
- **WHEN** Drill 分类恢复模式
- **THEN** 必须 (MUST) 分类为 `ResourceOnly`
- **AND** 不得 (MUST NOT) 因陈旧 `lastBackupName` 执行 RestoreData

#### Scenario: no-data 证据不完整不得跳过数据恢复
- **GIVEN** DataSync 缺少 `NoDataVolumes=True/NoPVCFound` 或最新 history 不是 `Skipped`
- **AND** DataSync 没有可用的 `status.lastBackupName`
- **WHEN** DisasterDrill 执行 Pending 前置校验
- **THEN** 不得 (MUST NOT) 分类为 `ResourceOnly`
- **AND** DisasterDrill 必须 (MUST) 前置失败

### Requirement: Drill 备份预检必须失败关闭

系统必须 (MUST) 在 DisasterDrill 进入 `Ready` 之前校验每个实例的 ResourceSync 资源备份以及 DataSync 数据备份或显式 no-data 状态。缺失、未就绪或不一致状态不得推迟到 Operation 恢复步骤才失败。

#### Scenario: 普通 DataSync 备份缺失
- **GIVEN** DataSync 不满足显式 no-data 条件
- **AND** DataSync 的 `status.lastBackupName` 为空
- **WHEN** DisasterDrill 执行 Pending 前置校验
- **THEN** `status.validationResults.backupAvailable` 必须 (MUST) 为 false
- **AND** DisasterDrill 必须 (MUST) 进入 `Failed`
- **AND** 不得 (MUST NOT) 进入 `Ready`
- **AND** 不得 (MUST NOT) 创建 DisasterOperation

#### Scenario: ResourceSync 备份缺失
- **GIVEN** ResourceSync 不存在、未处于 `Ready` 或 `status.lastBackupName` 为空
- **WHEN** DisasterDrill 执行 Pending 前置校验
- **THEN** FullRestore 和 ResourceOnly 都必须 (MUST) 前置失败
- **AND** 不得 (MUST NOT) 创建 DisasterOperation

#### Scenario: skipValidation 不得绕过备份安全检查
- **GIVEN** DisasterDrill 设置 `spec.skipValidation=true`
- **AND** 实例没有有效数据备份且不满足显式 no-data 条件
- **WHEN** DisasterDrill 执行 Pending 前置校验
- **THEN** DisasterDrill 仍必须 (MUST) 进入 `Failed`
- **AND** 不得 (MUST NOT) 因 skipValidation 进入 `Ready`

#### Scenario: FullRestore 行为保持不变
- **GIVEN** ResourceSync 和 DataSync 均为 `Ready` 且各自存在有效 `status.lastBackupName`
- **AND** DataSync 不满足显式 no-data 条件
- **WHEN** DisasterDrill 校验并执行
- **THEN** `status.restoreMode` 必须 (MUST) 为 `FullRestore`
- **AND** Operation 步骤必须 (MUST) 保持 `RestoreResource -> RestoreData -> ScaleUp`

### Requirement: Drill 模式必须按实例冻结并在确认时复核

系统必须 (MUST) 在 Ready 阶段保存逐实例恢复模式，并在用户确认时复核当前同步状态。容灾组可以包含 FullRestore 与 ResourceOnly 实例，但每个子 Operation 必须使用自己的模式。

#### Scenario: Ready 到 Confirm 期间模式发生变化
- **GIVEN** DisasterDrill 已在 Ready 阶段保存逐实例恢复模式
- **AND** 用户确认前该实例从 ResourceOnly 变为 FullRestore 或反向变化
- **WHEN** 用户设置 `spec.confirmed=true`
- **THEN** 系统必须 (MUST) 拒绝按旧模式创建或执行 Operation
- **AND** DisasterDrill 必须 (MUST) 进入 Failed 并提示备份状态已变化

#### Scenario: 混合容灾组逐实例执行
- **GIVEN** 一个 DisasterGroup 同时包含有效 FullRestore 实例和 ResourceOnly 实例
- **WHEN** Group DisasterDrill 完成 Pending 校验
- **THEN** 父 Drill 的聚合 `status.restoreMode` 必须 (MUST) 为 `Mixed`
- **AND** 必须 (MUST) 保存每个实例的恢复模式
- **WHEN** 父 Operation 创建各实例子 Operation
- **THEN** 每个子 Operation 必须 (MUST) 获得独立 DrillConfig
- **AND** FullRestore 子 Operation 必须包含 RestoreData
- **AND** ResourceOnly 子 Operation 不得包含 RestoreData

#### Scenario: 组内任一实例备份无效
- **GIVEN** DisasterGroup 中至少一个实例没有有效 ResourceSync 备份，或既没有有效 DataSync 备份也不满足显式 no-data 条件
- **WHEN** Group DisasterDrill 执行 Pending 校验
- **THEN** 整个 DisasterDrill 必须 (MUST) 进入 Failed
- **AND** 不得 (MUST NOT) 进入 Ready 或创建父 DisasterOperation
