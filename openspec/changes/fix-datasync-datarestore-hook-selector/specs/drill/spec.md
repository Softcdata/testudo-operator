## ADDED Requirements

### Requirement: Drill dataRestore hook Trafficless selector 兼容
DisasterDrill 数据恢复在继承或覆盖 DisasterInstance dataRestore hooks 时，必须 (MUST) 复用 DataSync 的 Trafficless selector 兼容规则，使 exec post hook 能匹配演练恢复后的 Pod。

#### Scenario: Drill dataRestore exec hook 使用 marker selector
- **GIVEN** DisasterDrill 继承实例级 `spec.veleroHooks.dataRestore` 或配置了演练级 `spec.veleroHooks.dataRestore`
- **AND** dataRestore hook 使用业务 label selector 匹配原始 Pod
- **WHEN** Drill 执行 RestoreData 步骤并创建 data AppRestore
- **THEN** data AppRestore 必须包含对应 marker ResourceModifier
- **AND** exec post hook selector 必须重写为 marker selector
- **AND** hook command、container、timeout、onError 和 wait 设置必须保持不变

#### Scenario: Drill namespaceMapping 下 marker rule 和 exec hook 使用不同命名空间阶段
- **GIVEN** DisasterDrill 配置了 `namespaceMapping: {app-ns: drill-ns}`
- **AND** dataRestore hook resource 的 `includedNamespaces` 包含 `app-ns`
- **WHEN** Drill 创建 data AppRestore
- **THEN** marker ResourceModifier 的 namespace 条件必须使用备份对象的原始命名空间 `app-ns`
- **AND** 被重写的 exec hook resource namespace 条件必须使用恢复后的目标命名空间 `drill-ns`
- **AND** init hook 的 selector 不得被 marker 重写
