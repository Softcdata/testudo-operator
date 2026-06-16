## ADDED Requirements

### Requirement: Drill Data Restore Hook 覆盖
DisasterDrill 必须 (MUST) 支持演练级 `spec.veleroHooks.dataRestore`，用于覆盖 DisasterInstance 的 dataRestore Hook 并透传到演练数据恢复。

#### Scenario: Drill 继承实例 dataRestore hooks
- **GIVEN** 一个 DisasterInstance 配置了 `spec.veleroHooks.dataRestore`
- **AND** DisasterDrill 未配置 `spec.veleroHooks.dataRestore`
- **WHEN** Drill 执行 RestoreData 步骤
- **THEN** 创建的 Data Restore AppRestore 必须使用实例级 dataRestore Hook

#### Scenario: Drill 覆盖 dataRestore hooks
- **GIVEN** 一个 DisasterInstance 配置了 `spec.veleroHooks.dataRestore`
- **AND** DisasterDrill 配置了不同的 `spec.veleroHooks.dataRestore`
- **WHEN** Drill 执行 RestoreData 步骤
- **THEN** 创建的 Data Restore AppRestore 必须使用 Drill 级 dataRestore Hook
- **AND** 实例级 dataRestore Hook 不得合并进本次演练恢复

#### Scenario: Drill 显式空 veleroHooks 清空继承
- **GIVEN** 一个 DisasterInstance 配置了 `spec.veleroHooks.dataRestore`
- **AND** 用户创建 DisasterDrill 时显式传入 `veleroHooks: {}`
- **WHEN** server 创建 DisasterDrill 且 DisasterDrillReconciler 创建关联 DisasterOperation
- **THEN** `drill.spec.veleroHooks` 必须保存为非 nil 空对象
- **AND** `operation.spec.drillConfig.veleroHooks` 必须保存为非 nil 空对象
- **AND** Drill 执行 RestoreData 步骤时不得继承实例级 dataRestore Hook

#### Scenario: Drill 不接受 dataBackup hooks
- **GIVEN** 用户创建或更新 DisasterDrill 时提供 `spec.veleroHooks.dataBackup`
- **WHEN** server 校验请求
- **THEN** 请求必须被拒绝
- **AND** 错误信息必须说明演练不创建数据备份，dataBackup Hook 不生效

#### Scenario: Drill VeleroHooks 复制到 DisasterOperation
- **GIVEN** 用户创建 DisasterDrill 时提供 `spec.veleroHooks.dataRestore`
- **WHEN** DisasterDrillReconciler 创建或更新关联的 DisasterOperation
- **THEN** `operation.spec.drillConfig.veleroHooks.dataRestore` 必须等于 `drill.spec.veleroHooks.dataRestore`
- **AND** 执行端必须从 `operation.spec.drillConfig.veleroHooks` 读取演练级覆盖
