## ADDED Requirements

### Requirement: 恢复构建器必须支持通用可逆修改规则编译

系统必须 (MUST) 支持将通用修改规则（DSL）编译为 `AppRestore.spec.resourceModifierRules`，并根据运行时方向（forward/reverse）自动生成对应 patch 结果。

#### Scenario: Forward 方向编译
- **GIVEN** 实例运行方向与配置基线一致（forward）
- **AND** 存在 `map` 类型规则 `sc-main -> sc-dr`
- **WHEN** 构建器生成 AppRestore 规格
- **THEN** 输出的 PVC/PV patch 必须 (MUST) 将 `storageClassName` 设置为 `sc-dr`

#### Scenario: Reverse 方向编译
- **GIVEN** 实例运行方向与配置基线相反（reverse）
- **AND** 同一套规则 `sc-main -> sc-dr`
- **WHEN** 构建器生成 AppRestore 规格
- **THEN** 输出 patch 必须 (MUST) 自动反转为 `sc-dr -> sc-main`

### Requirement: 方向判定失败时必须阻断构建

当运行时主备关系无法映射到配置基线方向时，系统必须 (MUST) 以标准错误码终止编译，不得继续生成不确定 patch。

#### Scenario: 首次构建状态为空时回退 forward
- **GIVEN** `instance.status.primaryCluster` 与 `instance.status.secondaryCluster` 均为空
- **WHEN** 编译器尝试解析方向
- **THEN** 必须 (MUST) 将方向判定为 `forward`
- **AND** 必须 (MUST) 将方向来源标记为 `baselineFallback`

#### Scenario: 方向不可判定
- **GIVEN** baseline 与 runtime 角色关系不一致且不可推导
- **WHEN** 编译器尝试解析方向
- **THEN** 必须 (MUST) 返回 `DirectionResolveFailed`
- **AND** 不得 (MUST NOT) 输出任何资源修改规则

#### Scenario: 状态仅一侧存在时失败关闭
- **GIVEN** `instance.status.primaryCluster` 与 `instance.status.secondaryCluster` 仅一侧为空
- **WHEN** 编译器尝试解析方向
- **THEN** 必须 (MUST) 返回 `DirectionResolveFailed`
- **AND** 不得 (MUST NOT) 输出任何资源修改规则

### Requirement: 不可逆规则必须在编译期被治理

系统必须 (MUST) 对规则执行可逆性与安全校验；违反约束的规则不得进入执行面。

#### Scenario: 规则不可逆
- **GIVEN** 用户提交不可逆且未提供反向策略的规则
- **WHEN** 编译器执行可逆性检查
- **THEN** 必须 (MUST) 返回 `ModifierRuleNotReversible`

#### Scenario: 命中受限路径
- **GIVEN** 规则 patch 路径命中受限字段（如 `status/*`）
- **WHEN** 编译器执行安全治理检查
- **THEN** 必须 (MUST) 返回 `ModifierRuleRejected`

### Requirement: 恢复构建器必须复用 Velero 原生匹配语义

系统必须 (MUST) 支持直接透传 Velero 原生 `conditions + patch` 规则，不得在平台层改变其匹配语义。

#### Scenario: Velero 条件透传
- **GIVEN** 用户规则包含 `groupResource/resourceNameRegex/namespaces/labelSelector/matches`
- **AND** 规则为 `veleroNative` 模式
- **WHEN** 构建器生成 AppRestore 规格
- **THEN** 输出规则必须 (MUST) 保留上述条件语义不变

#### Scenario: Velero patch 透传
- **GIVEN** 用户规则包含 Velero 支持的 patch 结构（如 `patches`）
- **WHEN** 构建器生成 AppRestore 规格
- **THEN** 输出 patch 必须 (MUST) 与输入结构兼容且可被 Velero 直接执行

#### Scenario: ForwardOnly 方向门控
- **GIVEN** `veleroNative` 规则设置 `directionPolicy=ForwardOnly`
- **AND** 当前运行方向为 `reverse`
- **WHEN** 构建器生成 AppRestore 规格
- **THEN** 该规则必须 (MUST) 被跳过

#### Scenario: Auto 默认方向策略
- **GIVEN** 规则未显式设置 `directionPolicy`
- **AND** 该规则为 `veleroNative` 模式
- **WHEN** 构建器生成 AppRestore 规格
- **THEN** 系统必须 (MUST) 按 `Auto` 语义处理该规则

### Requirement: Phase 1 必须限制 veleroNative patch 类型

系统必须 (MUST) 在 Phase 1 仅接受 `veleroNative.patches(JSONPatch)`；对未纳入范围的 patch 类型进行失败关闭。

#### Scenario: 非 Phase 1 patch 类型
- **GIVEN** `veleroNative` 规则包含 `mergePatches` 或 `strategicPatches`
- **WHEN** 构建器执行规则校验
- **THEN** 必须 (MUST) 返回 `ModifierRuleRejected`

### Requirement: Phase 1 必须禁止 reversible 追加路径

系统必须 (MUST) 在 Phase 1 拒绝 `reversible` 规则对 JSON Pointer `/-` 追加路径的写入，以保证幂等与可逆性。

#### Scenario: reversible 命中追加路径
- **GIVEN** `reversible` 规则 `path` 以 `/-` 结尾
- **WHEN** 构建器执行规则校验
- **THEN** 必须 (MUST) 返回 `ModifierRuleNotReversible`

### Requirement: 路径定位校验必须支持 JSON Pointer 语义

系统必须 (MUST) 提供可复用的路径定位校验能力，供提交期服务端校验与执行期 dry-run 共同使用。

#### Scenario: annotation key 包含斜杠转义
- **GIVEN** 规则 `path=/metadata/annotations/testudo.softcdata.com~1source`
- **WHEN** 路径定位器解析 JSON Pointer
- **THEN** 必须 (MUST) 正确将 `~1` 还原为 `/` 并命中目标字段

#### Scenario: 数组路径定位
- **GIVEN** 规则 `path=/spec/ports/0/nodePort`
- **WHEN** 路径定位器在目标资源上执行定位
- **THEN** 必须 (MUST) 支持数组下标访问

#### Scenario: 数组路径越界失败
- **GIVEN** 规则路径下标超出数组范围
- **WHEN** 路径定位器执行定位
- **THEN** 必须 (MUST) 返回 `ModifierRuleRejected`

### Requirement: 系统内建 trafficless 规则必须支持 ownerReferences 例外

系统必须 (MUST) 在治理拒绝 `/metadata/ownerReferences` 的同时，为内建 trafficless 规则保留受限例外。

#### Scenario: 系统内建例外允许
- **GIVEN** 系统内建 trafficless 规则在 `pods` 上执行 `remove /metadata/ownerReferences`
- **WHEN** 构建器执行治理校验
- **THEN** 该规则必须 (MUST) 被允许通过

#### Scenario: 用户规则仍被拒绝
- **GIVEN** 用户自定义规则命中 `/metadata/ownerReferences`
- **WHEN** 构建器执行治理校验
- **THEN** 必须 (MUST) 返回 `ModifierRuleRejected`

### Requirement: 冲突判定必须使用标准冲突键

系统必须 (MUST) 使用 `(normalizedConditions, operation, path)` 作为冲突键；同键不同值必须判定为冲突。

#### Scenario: 同键不同值冲突
- **GIVEN** 两条规则归一化后具有相同 `conditions + operation + path`
- **AND** 目标 `value` 不同
- **WHEN** 构建器合并规则
- **THEN** 必须 (MUST) 返回 `ModifierRuleConflict` 或按配置执行跳过策略

#### Scenario: system-protect 冲突覆盖
- **GIVEN** 系统保护规则与用户规则命中相同冲突键
- **AND** 两者目标 `value` 不同
- **WHEN** 构建器合并规则
- **THEN** 必须 (MUST) 以系统保护规则为最终结果
- **AND** 不得 (MUST NOT) 因该冲突直接失败

### Requirement: 统一方向解析开关必须支持平滑迁移

系统必须 (MUST) 提供 `UseUnifiedDirectionResolver` 迁移开关，并在 Phase 1 默认保持关闭以降低存量风险。

#### Scenario: 开关关闭保持旧行为
- **GIVEN** `UseUnifiedDirectionResolver=false`
- **WHEN** 构建器处理 SC/Ingress 映射
- **THEN** 必须 (MUST) 保持现有 `strictTargetValidation` 失败后反向兜底行为

#### Scenario: 开关关闭时 DSL 显式失败
- **GIVEN** `UseUnifiedDirectionResolver=false`
- **AND** 检测到 `veleroNative` 或 `reversible` 规则
- **WHEN** 构建器执行编译
- **THEN** 必须 (MUST) 返回 `ModifierFeatureDisabled`
- **AND** 不得 (MUST NOT) 静默忽略 DSL 规则

#### Scenario: 开关关闭时 legacy 映射仍可用
- **GIVEN** `UseUnifiedDirectionResolver=false`
- **AND** 仅配置 legacy `storageClassMapping/ingressClassMapping`
- **WHEN** 构建器执行编译
- **THEN** 必须 (MUST NOT) 返回 `ModifierFeatureDisabled`
- **AND** 必须 (MUST) 保持 legacy 映射能力可用

#### Scenario: 开关关闭且运行态不完整时仍走 legacy 路径
- **GIVEN** `UseUnifiedDirectionResolver=false`
- **AND** 仅配置 legacy `storageClassMapping/ingressClassMapping`
- **AND** 运行态主备状态不完整（例如仅一侧存在）
- **WHEN** 构建器执行编译
- **THEN** 不得 (MUST NOT) 因统一方向解析返回 `DirectionResolveFailed`
- **AND** 必须 (MUST) 按 legacy 路径继续处理

#### Scenario: 开关开启时 DSL 全路径生效
- **GIVEN** `UseUnifiedDirectionResolver=true`
- **AND** 存在 `veleroNative` 或 `reversible` 规则
- **WHEN** 构建器执行编译
- **THEN** 必须 (MUST) 按统一方向解析与规则编译流程产出结果
