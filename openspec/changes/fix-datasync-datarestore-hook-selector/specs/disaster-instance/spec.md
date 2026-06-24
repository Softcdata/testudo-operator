## ADDED Requirements

### Requirement: DataSync dataRestore post hook Trafficless selector 兼容
DisasterInstance DataSync 数据恢复必须 (MUST) 在 Trafficless 恢复下保证用户配置的 dataRestore exec post hook 能按原始业务 selector 语义命中恢复 Pod，同时不得恢复业务 labels。

#### Scenario: 业务 label selector 的 exec post hook 在 Trafficless 恢复后执行
- **GIVEN** DisasterInstance 配置了 `spec.veleroHooks.dataRestore.resources[0].labelSelector.matchLabels.app=hook-di`
- **AND** DataSync 备份中的 Pod 带有 `app=hook-di`
- **WHEN** DataSync 创建数据恢复 AppRestore
- **THEN** AppRestore 必须包含一个系统 ResourceModifier，在原始 Pod 匹配 `app=hook-di` 时添加系统 marker label
- **AND** 对应 exec post hook 的 selector 必须重写为该系统 marker label
- **AND** Trafficless labels 覆盖仍必须只保留非业务 labels，不得恢复 `app=hook-di`
- **AND** Trafficless Pod 覆盖启动 command 时必须清空原始 container args，避免恢复 Pod 因命令参数冲突崩溃而导致 hook 执行失败

#### Scenario: Hook 执行参数保持透明
- **GIVEN** DisasterInstance dataRestore exec hook 配置了 `command`、`container`、`onError`、`execTimeout`、`waitTimeout` 或 `waitForReady`
- **WHEN** DataSync 为 Trafficless 兼容重写 hook selector
- **THEN** 这些执行参数必须保持和用户输入一致
- **AND** 平台不得模板渲染或隐式注入 command 参数

#### Scenario: init hook 保持原始 selector
- **GIVEN** DisasterInstance dataRestore hook resource 只包含 `init` restore hook
- **WHEN** DataSync 创建数据恢复 AppRestore
- **THEN** 该 hook resource 的 namespace、resource、labelSelector 必须保持原始值
- **AND** 平台不得为该 init-only hook 注入 marker selector

#### Scenario: 混合 init 与 exec hook 拆分
- **GIVEN** DisasterInstance dataRestore hook resource 同时包含 `init` 和 `exec` restore hook
- **WHEN** DataSync 创建数据恢复 AppRestore
- **THEN** init hook 必须保留在原始 selector 的 hook resource 中
- **AND** exec hook 必须放入使用系统 marker selector 的 hook resource 中
- **AND** 两类 hook 的执行参数必须保持不变

#### Scenario: 多个 hook resource 可同时命中同一恢复 Pod
- **GIVEN** DisasterInstance dataRestore 配置了多个可匹配同一 Pod 的 exec hook resource
- **WHEN** DataSync 创建数据恢复 AppRestore
- **THEN** 每个 hook resource 必须使用独立的系统 marker selector
- **AND** 同一个恢复 Pod 可以同时带有多个 marker labels
- **AND** 所有匹配的 exec hook 都应可由 Velero 分组执行
