# Design: 通用可逆资源修改器引擎（Reversible Modifier Engine）

## 当前口径说明

本文保留首轮设计历史，用于说明通用资源修改器引擎的来源与演进背景。

当前正式用户 contract 已由以下现行产物收敛：

1. `refactor-reversible-modifier-pair-only`
2. `openspec/specs/restore-modifier/spec.md`

现行口径如下：

1. `reversible` 只使用 `pair.path`、`pair.sourceValue`、`pair.targetValue`
2. `veleroNative` 保持透传模型不变
3. 旧 `map/template/transform` 仅作为历史设计背景，不再属于当前正式输入

阅读当前实现与校验逻辑时，必须以现行规范为准，而不是以本文中的首轮历史设计分支为准。

## 1. 背景与问题定义

当前恢复修改能力分散在多个入口：

1. `storageClassMapping/ingressClassMapping` 提供类映射，但能力限定在少数字段。
2. `resourceModifierRules` 可以改任意资源路径，但缺少方向语义（正向/反向）与可逆保证。
3. 反向保护（Reprotect）场景下，用户需要维护两套规则或依赖局部兜底逻辑，难以规模化运营。

本设计目标是：**用户只维护一套规则，系统自动在 forward/reverse 两个方向产出正确 patch。**

---

## 2. 设计目标

### 2.1 Goals

1. 统一规则抽象：任意资源字段均可声明式修改。
2. 统一方向语义：在 `DataSync/ResourceSync/Drill` 恢复链路自动判定 `forward|reverse`。
3. 统一可逆保障：规则在 reverse 时自动求逆或显式回放。
4. 统一治理：提供白名单、安全校验、审计与错误码。
5. 统一执行语义：复用 Velero 原生匹配与补丁模型，不重复实现匹配引擎。

### 2.2 Non-Goals

1. 不替换 Velero ResourceModifier 执行面。
2. 不在第一阶段重写 Velero 原生 matcher 语义（以透传复用为主）。
3. 不在第一阶段引入脚本型表达式执行器（如 JS/Lua）。
4. 不要求用户一次性迁移所有旧配置。

---

## 3. 总体架构

```text
DisasterInstance/Operation context
        |
        v
[Direction Resolver] ---> flow=forward|reverse
        |
        v
[Modifier Compiler]
  - load rules (system + instance; profile in phase 2)
  - validate governance
  - compile reversible transforms
  - merge by priority
        |
        v
[Submission Validator]
  - admission-time compile check
  - target resource locate check
  - json pointer existence check
        |
        v
AppRestore.spec.resourceModifierRules
        |
        v
Velero ResourceModifier ConfigMap
```

### 3.1 核心组件

1. Direction Resolver：方向判定器（纯函数）。
2. Modifier Compiler：规则编译器（可逆变换 + 合并）。
3. Governance Validator：安全与复杂度校验。
4. Submission Validator：提交期服务端校验（Admission/Validate API）。
5. Audit Emitter：编译摘要落注解与事件。

### 3.2 与 Velero 的结合原则

1. 匹配语义复用：`groupResource/resourceNameRegex/namespaces/labelSelector/matches` 与 Velero 保持一致。
2. 补丁语义复用：底层输出与 Velero 兼容的补丁结构（Phase 1 仅 `patches(JSONPatch)`）。
3. 平台只做增量能力：方向判定（forward/reverse）、可逆变换编译、治理校验与审计。
4. 结果可回放：编译产物可直接落地为 `AppRestore.spec.resourceModifierRules`，与 Velero 文档/行为对齐。

---

## 4. 数据模型（DSL）

> 说明：以下为逻辑模型。**Phase 1 固定配置入口为 `DisasterInstance.spec.restorePolicy`**；
> `Profile CRD` 复用能力作为 Phase 2 增强项，不纳入本阶段实现。

### 4.1 双层 Rule 模型

```yaml
rules:
- id: sc-map
  mode: reversible               # reversible | veleroNative
  enabled: true
  applyTo: ["resourceSync", "dataSync", "drill"]
  priority: 200
  conditions:                    # 与 Velero conditions 对齐
    groupResource: "persistentvolumeclaims"
    namespaces: ["prod"]
    resourceNameRegex: ".*"
  transform:
    type: "map"
    path: "/spec/storageClassName"
    mapping:
      sc-main: sc-dr
      sc-ssd: sc-dr-ssd
  directionPolicy: "Auto"      # Auto|ForwardOnly|ReverseOnly
  onConflict: "Fail"           # Fail|Skip
```

默认值：

1. `enabled=true`
2. `directionPolicy=Auto`
3. `onConflict=Fail`

### 4.2 Velero 原生透传规则（mode=veleroNative）

```yaml
rules:
- id: pvc-node-selector
  mode: veleroNative
  enabled: true
  applyTo: ["resourceSync"]
  priority: 100
  conditions:
    groupResource: "persistentvolumeclaims"
    namespaces: ["prod"]
    labelSelector:
      matchLabels:
        app: "mysql"
  veleroRule:
    patches:
      - operation: add
        path: /metadata/annotations/patched-by
        value: "platform"
```

说明：

1. `conditions` 与 `veleroRule` 均按 Velero 结构透传；
2. 编译器只做治理/冲突检查，不改写其匹配语义；
3. `directionPolicy` 仍可生效（例如 `ForwardOnly` 时 reverse 流程不应用）。
4. **Phase 1 限制**：仅支持 `veleroRule.patches(JSONPatch)`；`mergePatches/strategicPatches` 放入 Phase 2。

### 4.3 可逆变换类型（mode=reversible）

1. `map`（推荐）
   - forward：按 `source -> target` 映射。
   - reverse：自动取逆映射 `target -> source`。

2. `template`
   - 使用方向上下文变量渲染值：
     - `{{ .SourceCluster }}`
     - `{{ .TargetCluster }}`
     - `{{ .Flow }}`
   - 适用于 env/annotation 等字符串字段。

3. `pair`
   - 显式声明 `forwardValue/reverseValue`。
   - 适用于无法用 map/template表达的字段。

4. `patch`（非可逆原子 patch，不推荐第一阶段开放）
   - 默认禁止；
   - 需显式开启且标记 `reverse` 补丁，或由后续 journal 功能支撑。

5. `reversible` + 追加路径（`/-`）
   - Phase 1 默认禁止；
   - 原因：追加语义不可逆且非幂等，重复执行会累积脏数据（如重复 env 条目）。

### 4.4 `directionPolicy` 语义

1. `Auto`（默认）：
   - `reversible`：按 `flow=forward|reverse` 自动生成对应值；
   - `veleroNative`：forward/reverse 均应用同一透传 patch，不改写值。
2. `ForwardOnly`：仅 `flow=forward` 应用规则。
3. `ReverseOnly`：仅 `flow=reverse` 应用规则。

### 4.5 Phase 1 能力矩阵（统一口径）

1. 规则来源：`system rules` + `instance restorePolicy rules`。
2. `reversible` transform：支持 `map/template/pair`，不支持 `patch`。
3. `veleroNative` patch：仅支持 `patches(JSONPatch)`。
4. 冲突决议：`system-protect` wins > 高优先级 wins > 同优先级按 `onConflict`。
5. 灰度开关：`UseUnifiedDirectionResolver` 默认关闭，显式开启后启用统一方向判定。
6. gate 行为：
   - `gate=false`：仅保留旧 SC/Ingress 路径；若检测到新 DSL（`veleroNative/reversible`）则失败关闭并返回 `ModifierFeatureDisabled`；legacy-only 不触发统一方向解析；
   - `gate=true`：统一方向解析 + DSL 编译全路径生效。
7. `HasNewDSLRules` 识别边界：
   - 仅识别用户显式 DSL 字段（`veleroNative/reversible`）；
   - 不包含 legacy 字段（`storageClassMapping/ingressClassMapping`）内部转换后的中间规则。

---

## 5. 方向判定机制（通用反转的关键）

### 5.1 输入

1. 配置基线：
   - `baselineSource = config.spec.sourceCluster`
   - `baselineTarget = config.spec.targetCluster`
2. 运行态角色：
   - `runtimeSource = instance.status.primaryCluster`
   - `runtimeTarget = instance.status.secondaryCluster`

### 5.2 判定规则

1. 当 `runtimePrimary/runtimeSecondary` 均为空时，使用 baseline 回退：`flow=forward`，并标记 `directionSource=baselineFallback`。
2. 当 `runtimePrimary/runtimeSecondary` 均非空且 `runtime == baseline`，`flow=forward`，`directionSource=runtimeStatus`。
3. 当 `runtimePrimary/runtimeSecondary` 均非空且 `runtime == reverse(baseline)`，`flow=reverse`，`directionSource=runtimeStatus`。
4. 当仅一侧为空，或 runtime 与 baseline 无法匹配：返回 `DirectionResolveFailed`，阻断恢复链路（Fail-Closed）。

### 5.3 结果

编译器拿到 `flow` 后，对所有支持可逆的 transform 统一求值，不需要模块硬编码“如果 reprotect 则交换”。

---

## 6. 编译算法

### 6.1 伪代码

```text
systemRules = LoadSystemRules()
instanceRules = LoadInstanceRules()  # Phase 1: no profile source

if !UseUnifiedDirectionResolver:
  if HasNewDSLRules(instanceRules):
    return ModifierFeatureDisabled
  # Legacy path: keep existing SC/Ingress behavior, no unified direction resolve.
  legacyRules = CompileLegacyMappingsWithExistingBehavior(instanceRules)
  return MergeAndValidate(systemRules, legacyRules)

flow = ResolveDirection(baseline, runtime)
rules = ValidateAndSort(systemRules + instanceRules, by=priority,id)

for rule in rules:
  if !rule.enabled or !rule.applyTo.contains(currentPath):
    continue
  if !DirectionPolicyAllows(rule.directionPolicy, flow):
    markSkipped(rule, reason="DirectionPolicyFiltered")
    continue
  if !GovernanceAllowed(rule):
    reject(rule, ModifierRuleRejected)
  if rule.mode == "veleroNative":
    candidate = PassThrough(rule.conditions, rule.veleroRule)
  else:
    candidate = CompileReversible(rule, flow)
  decision = ResolveByPrecedence(rule, candidate, result)
  if decision == "reject":
    handle(rule.onConflict)
  if decision == "skip":
    markSkipped(rule, reason="LowerPriorityOrProtectedOverride")
    continue
  append(result, candidate)

return result
```

说明：

1. `HasNewDSLRules` 仅检查用户显式 DSL 配置，不检查 legacy 映射转换产物。
2. `gate=false + legacy-only` 走 legacy path，不触发统一方向解析，避免 `DirectionResolveFailed` 误伤旧能力。

### 6.2 冲突决议顺序

决议顺序（高 -> 低）：

1. `system-protect` 规则永远优先（wins），冲突时覆盖非系统规则并记录 `SystemProtectOverride`。
2. 非系统规则按 `priority` 比较，高优先级 wins（低优先级标记 skip）。
3. 同优先级命中同冲突键且 `value` 不同，按 `onConflict` 处理（默认 `Fail`）。

### 6.3 冲突定义

冲突键定义为：`(normalizedConditions, operation, path)`。

当同一冲突键在同一编译批次被写入不同 `value` 时，视为冲突（跨 `veleroNative/reversible` 模式同样生效）：

1. `onConflict=Fail`：返回 `ModifierRuleConflict`。
2. `onConflict=Skip`：跳过当前规则并记录审计。

### 6.4 提交期服务端校验（新增）

目标：用户提交规则时即失败关闭，不把输入错误延迟到 Failover/Reprotect 执行阶段。

校验入口：

1. 首选 `ValidatingAdmissionWebhook`（Create/Update `DisasterInstance`）。
2. 可选补充 `Validate API`（用于 UI 即时“校验规则”按钮）。
3. 两种入口必须复用同一校验器实现，避免语义漂移。

校验流程：

1. 规则编译校验：复用编译器阶段校验（gate、方向策略、冲突、治理限制）。
2. 对象定位校验：按 `conditions(groupResource/namespaces/labelSelector/resourceNameRegex)` 在目标集群选择候选对象。
3. 路径定位校验：对每个 patch `path` 执行 JSON Pointer 解析与遍历。
4. 失败关闭：任一规则出现不可定位问题（path 不存在、数组下标越界、目标对象 0 命中）即拒绝提交。

JSON Pointer 约束（Phase 1）：

1. 支持转义：`~1` -> `/`，`~0` -> `~`。
2. 支持数组下标定位（如 `/spec/ports/0/nodePort`）。
3. `reversible` 仍禁止 `/-` 追加路径（保持可逆与幂等约束）。

操作语义：

1. `replace/remove/test`：叶子节点必须存在。
2. `add`：默认要求父节点存在；若启用 strict path mode，可进一步要求叶子存在。

错误输出：

1. 顶层错误码复用 `ModifierRuleRejected`（必要时附带子原因：`PathNotFound`、`IndexOutOfRange`、`TargetNotFound`）。
2. 错误信息必须包含 `ruleID`、`groupResource`、`namespace/name`、`path`，便于用户一次修正。

---

## 7. 与现有能力的兼容策略

### 7.1 `storageClassMapping/ingressClassMapping`

第一阶段保留现有字段，并在编译层转换为 DSL 规则：

1. 老字段继续可用（不破坏 API）。
2. 内部统一进入 Compiler 管线。
3. 方向反转改为 Direction Resolver 主导，不依赖“校验失败后再反转”。

### 7.2 Velero 现有规则兼容

1. 若用户已有 Velero `resourceModifierRules` 模板，允许按 `veleroNative` 模式直接引用/注入。
2. 平台仅做路径安全校验、冲突检查与方向策略过滤，不修改原始匹配字段语义。
3. 平台可逆规则与 Velero 原生规则共存，统一按优先级合并。

### 7.3 系统规则合并顺序

建议顺序（低 -> 高）：

1. 系统基础规则（如 skeleton/trafficless）。
2. 旧字段转换规则（SC/Ingress）。
3. 用户 `veleroNative` 规则。
4. 用户 `reversible` 规则。
5. 系统保护规则（保留最高优先级）。

### 7.4 旧反向兜底逻辑迁移（`strictTargetValidation`）

1. Phase 1 引入灰度开关：`UseUnifiedDirectionResolver`（默认关闭，显式开启后生效）。
2. 开关开启时：SC/Ingress 规则方向由 Direction Resolver 决定，不再依赖“strict 校验失败后反转”。
3. 开关关闭时：保持现有兜底行为，确保存量环境平滑过渡。
4. Phase 2 评估并下线旧兜底分支。

### 7.5 `DisasterOperation` PreCheck 集成

1. Failover/Reprotect 的 PreCheck 必须执行 modifier compile dry-run。
2. dry-run 失败时必须在任何破坏性步骤前终止操作（Fail-Closed）。
3. 标准失败码由编译器返回并透传到 Operation step message（例如 `ModifierFeatureDisabled`、`ModifierRuleRejected`）。
4. 即使已启用提交期服务端校验，PreCheck 仍保留 dry-run 作为运行期防线（输入之外的环境漂移兜底）。

---

## 8. 安全与治理

### 8.1 必须限制的路径

默认拒绝：

1. `/status/*`
2. `/metadata/finalizers`
3. `/metadata/ownerReferences`
4. 可能破坏对象归属和删除语义的敏感路径

例外（仅系统内建规则）：

1. `trafficless` 内建规则允许 `remove /metadata/ownerReferences`。
2. 例外仅允许在 `pods` 资源生效，且仅限 `remove` 操作。
3. 用户自定义规则仍不得命中 `/metadata/ownerReferences`。

### 8.2 复杂度限额

1. 单实例最大规则数（例如 500）。
2. 单规则最大 patch 数（例如 50）。
3. 正则长度与复杂度限制（防 ReDoS）。

### 8.3 错误码

1. `DirectionResolveFailed`
2. `ModifierRuleRejected`
3. `ModifierRuleNotReversible`
4. `ModifierRuleConflict`
5. `ModifierCompileFailed`
6. `ModifierFeatureDisabled`

---

## 9. 可观测性与审计

在 AppRestore 注解写入：

1. `testudo.softcdata.com/modifier-flow=forward|reverse`
2. `testudo.softcdata.com/modifier-direction-source=runtimeStatus|baselineFallback`
3. `testudo.softcdata.com/modifier-summary=<json>`
   - `appliedRuleCount`
   - `skippedRuleCount`
   - `rejectedRuleCount`
   - `conflictCount`

并发射结构化事件（ExecutionProgress/Finished）包含：

1. 方向
2. 命中规则列表（可截断）
3. 首个失败规则与错误码
4. `directionSource`（用于判定是否触发 baseline 回退）

---

## 10. 示例

### 10.1 单规则自动正反向（SC）

用户只维护：

```yaml
transform:
  type: map
  path: /spec/storageClassName
  mapping:
    sc-main: sc-dr
```

1. forward（A -> B）：`sc-main => sc-dr`
2. reverse（B -> A）：自动求逆，`sc-dr => sc-main`

### 10.2 Env 示例（模板）

```yaml
transform:
  type: template
  path: /spec/template/spec/containers/0/env/0/value
  valueTemplate: 'mysql.{{ .TargetCluster }}.svc'
```

1. forward：`TargetCluster=dr`，写入 `mysql.dr.svc`
2. reverse：`TargetCluster=main`，写入 `mysql.main.svc`

---

## 11. 测试策略

1. 单测：方向判定函数（forward/reverse/异常）。
2. 单测：`map/template/pair` 在 reverse 时编译结果正确。
3. 单测：`reversible` 命中 `/-` 追加路径时拒绝。
4. 单测：trafficless 系统规则可合法移除 `pods.ownerReferences`，用户同路径规则被拒绝。
5. 单测：`system-protect` 覆盖低优先级冲突规则。
6. 集成：DataSync/ResourceSync/Drill 三链路编译结果一致。
7. 回归：`UseUnifiedDirectionResolver=false` 时旧兜底行为不变。

---

## 12. 分阶段上线

### Phase 1（最小可用）

1. Direction Resolver。
2. `map/template/pair` 编译。
3. `veleroNative`（仅 `patches(JSONPatch)`）透传。
4. 旧字段兼容转换 + 编译。
5. 审计注解与基础错误码。
6. `UseUnifiedDirectionResolver` 灰度开关。

### Phase 2（增强）

1. Profile 复用（跨实例共享规则）。
2. `veleroNative` 扩展支持 `mergePatches/strategicPatches`。
3. 更丰富的规则治理能力（规则分组、命中统计、冲突可解释性增强）。
4. 冲突可视化与模拟预检 API（dry-run）。
5. 下线旧 `strictTargetValidation` 反向兜底分支。

### Phase 3（高级可逆）

1. 可选 journal 支持（覆盖更复杂的不可逆 patch 回滚）。
2. 更细粒度策略治理与租户隔离能力。
