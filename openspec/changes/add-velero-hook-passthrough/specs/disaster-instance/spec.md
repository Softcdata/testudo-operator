## ADDED Requirements

### Requirement: DisasterInstance DataSync Velero Hook 透传
DisasterInstance 必须 (MUST) 支持实例级 `spec.veleroHooks`，用于将 Velero Hook 透传到 DataSync 自动创建的 AppBackup 和 AppRestore。

#### Scenario: DataSync 备份透传 dataBackup hooks
- **GIVEN** 一个 DisasterInstance 配置了 `spec.veleroHooks.dataBackup.resources`
- **WHEN** DataSync 控制器为该实例创建或更新 AppBackup
- **THEN** 生成的 `AppBackup.spec.template.hooks` 必须等于 `spec.veleroHooks.dataBackup`
- **AND** 该 Hook 只应用于 DataSync 的数据备份链路

#### Scenario: DataSync 恢复透传 dataRestore hooks
- **GIVEN** 一个 DisasterInstance 配置了 `spec.veleroHooks.dataRestore.resources`
- **WHEN** DataSync 控制器为该实例创建 AppRestore
- **THEN** 生成的 `AppRestore.spec.template.hooks` 必须等于 `spec.veleroHooks.dataRestore`
- **AND** 现有 Trafficless ResourceModifier 规则必须继续保留

#### Scenario: ResourceSync 不投影 hooks
- **GIVEN** 一个 DisasterInstance 配置了 `spec.veleroHooks`
- **WHEN** ResourceSync 控制器为该实例创建 AppBackup 或 AppRestore
- **THEN** 控制器不得将 `spec.veleroHooks.dataBackup` 投影到 ResourceSync AppBackup
- **AND** 控制器不得承诺 `spec.veleroHooks.dataRestore` 会在 ResourceSync 资源恢复阶段执行

#### Scenario: 实例 Hook 更新后影响后续同步
- **GIVEN** 一个 DisasterInstance 的 `spec.veleroHooks` 被更新
- **AND** 该实例已存在 DataSync 创建的 `ds-*` AppBackup
- **WHEN** 后续 DataSync 触发新的同步
- **THEN** DataSync 控制器必须在触发备份前对齐既有 AppBackup 的 desired spec/template
- **AND** 既有 `AppBackup.spec.template.hooks` 必须更新为新的 `spec.veleroHooks.dataBackup`
- **AND** 新创建或更新的 AppRestore 必须使用新的 `spec.veleroHooks.dataRestore`
- **AND** 已完成的历史 Velero Backup/Restore 不应被回写修改

### Requirement: DisasterInstance Hook 基础校验
平台接口必须 (MUST) 对 DisasterInstance 的 `spec.veleroHooks` 执行基础合法性校验，避免明显无效的 Hook 配置进入自动容灾链路。

#### Scenario: 拒绝非 Pod 目标 Hook
- **GIVEN** 创建或更新 DisasterInstance 时提供 `veleroHooks.dataBackup.resources`
- **AND** 某个 Hook 显式声明的 `includedResources` 不包含 `pods`
- **WHEN** server 校验请求
- **THEN** 请求必须被拒绝
- **AND** 错误信息必须说明 Velero Hook 仅支持 Pod 目标

#### Scenario: 拒绝实例 Hook 明文敏感参数
- **GIVEN** 创建或更新 DisasterInstance 时提供 `veleroHooks.dataRestore`
- **AND** 某个 hook command 或 initContainer args 包含明显敏感参数，例如 `token=plain-text`
- **WHEN** server 校验请求
- **THEN** 请求必须被拒绝
- **AND** 错误码必须为 `VeleroHookSensitiveParameter`
- **AND** 错误元数据必须包含命中的 `veleroHooks` 字段路径

#### Scenario: 拒绝实例 Hook timeout 超过上限
- **GIVEN** 创建或更新 DisasterInstance 时提供 `veleroHooks.dataRestore`
- **AND** 某个 restore exec hook 配置了 `waitTimeout: "31m"`
- **WHEN** server 校验请求
- **THEN** 请求必须被拒绝
- **AND** 错误信息必须说明 Restore exec hook waitTimeout 最大值为 30m

#### Scenario: 允许清空实例 Hooks
- **GIVEN** 一个 DisasterInstance 已配置 `spec.veleroHooks`
- **WHEN** 用户更新该实例并显式传入空对象或 null
- **THEN** server 必须清空对应 Hook 配置
- **AND** 后续 DataSync 不再投影旧 Hook

#### Scenario: 子字段级 presence 和 clear 可判定
- **GIVEN** 一个 DisasterInstance 已配置 `spec.veleroHooks.dataBackup` 和 `spec.veleroHooks.dataRestore`
- **WHEN** 用户更新该实例时只提交 `veleroHooks.dataRestore`
- **THEN** server 必须保持既有 `dataBackup` 不变
- **AND** 仅替换或清空 `dataRestore`
- **AND** CRD/DTO 模型必须能区分子字段未出现、对象替换、null 清空三种语义

### Requirement: DisasterInstance Hook 参数透明投影
DisasterInstance 的 `spec.veleroHooks` 参数必须 (MUST) 以 Velero 原生结构透明投影到 DataSync 下游资源，平台不得在第一阶段进行模板渲染或隐式参数注入。

#### Scenario: dataBackup command 参数透明投影
- **GIVEN** `spec.veleroHooks.dataBackup` 中的 `exec.command` 包含多个参数
- **WHEN** DataSync 控制器创建或更新 AppBackup
- **THEN** 生成的 `AppBackup.spec.template.hooks` 必须保留完全相同的 command 数组

#### Scenario: dataRestore initContainer 参数透明投影
- **GIVEN** `spec.veleroHooks.dataRestore` 中的 init hook 包含 initContainer `envFrom.secretRef`
- **WHEN** DataSync 控制器创建 AppRestore
- **THEN** 生成的 `AppRestore.spec.template.hooks` 必须保留该 Secret 引用

#### Scenario: 实例 namespace 只作为 Velero 任务范围
- **GIVEN** `spec.veleroHooks.dataBackup.resources[].includedNamespaces` 为空
- **AND** DisasterInstance 配置了 `spec.namespaces`
- **WHEN** DataSync 生成 AppBackup
- **THEN** 平台不得把实例 namespace 渲染进 hook command
- **AND** hook 的实际匹配范围由 Velero Backup 的资源范围和 hook selector 共同决定

### Requirement: DisasterInstance 同步历史 Hook 状态契约
DataSync 同步历史必须 (MUST) 保存并回显本次同步关联 Backup/Restore 的 Hook 汇总状态，作为 server 和 Web 展示的稳定数据来源。

#### Scenario: DataSync 历史记录写入 backupHookStatus
- **GIVEN** 一次 DataSync 同步创建的 AppBackup 历史记录中包含 Velero `hookStatus`
- **WHEN** DataSync 控制器写入同步历史记录
- **THEN** `SyncHistoryRecord.backupHookStatus.hooksAttempted` 必须等于 Velero Backup 的 `hookStatus.hooksAttempted`
- **AND** `SyncHistoryRecord.backupHookStatus.hooksFailed` 必须等于 Velero Backup 的 `hookStatus.hooksFailed`

#### Scenario: DataSync 历史记录写入 restoreHookStatus
- **GIVEN** 一次 DataSync 同步创建的 AppRestore 状态中包含 Velero `hookStatus`
- **WHEN** DataSync 控制器写入同步历史记录
- **THEN** `SyncHistoryRecord.restoreHookStatus.hooksAttempted` 必须等于 Velero Restore 的 `hookStatus.hooksAttempted`
- **AND** `SyncHistoryRecord.restoreHookStatus.hooksFailed` 必须等于 Velero Restore 的 `hookStatus.hooksFailed`

#### Scenario: server 同步历史 DTO 回显 hookStatus
- **GIVEN** DataSync 同步历史记录包含 `backupHookStatus` 或 `restoreHookStatus`
- **WHEN** server 返回容灾实例同步历史
- **THEN** 响应 DTO 必须包含对应 Hook 状态字段
- **AND** Web 不需要通过 BackupName 或 RestoreName 再次关联 AppBackup/AppRestore 状态
