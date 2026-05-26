# Change: 通用可逆资源修改器引擎（单规则集自动正反向）

Depends on:

1. `add-restore-policy-and-sc-mapping`（提供 `DisasterInstance.spec.restorePolicy` 基础入口）

## 当前口径说明

本文保留首轮通用资源修改器引擎设计历史，用于说明能力来源与演进背景。

当前正式用户 contract 已由以下现行产物收敛：

1. `refactor-reversible-modifier-pair-only`
2. `openspec/specs/restore-modifier/spec.md`

现行口径如下：

1. `reversible` 只使用 `pair.path`、`pair.sourceValue`、`pair.targetValue`
2. `veleroNative` 保持透传模型不变
3. 旧 `map/template/transform` 仅作为历史背景存在，不再属于当前正式输入

阅读和实施当前行为时，必须以现行规范为准，而不是以本文中的首轮历史示例为准。

## Why

当前平台**已经接入 Velero Resource Modifiers**（`AppRestore.spec.resourceModifierRules -> ConfigMap -> Velero Restore`），但在“可逆编排与统一治理”上仍有三个结构性问题：

1. 用户需要按模块理解差异能力（如 `storageClassMapping` 与 `resourceModifierRules` 分散），学习成本高。
2. 反向保护（Reprotect）时，规则反转主要依赖局部逻辑与兜底策略，缺少统一、可预期的机制。
3. 用户若要覆盖更多资源字段（例如 `Deployment env`、`StatefulSet annotations`），只能维护多套规则或手工切换，容易出错。

目标是提供一个**通用且可逆**的增强机制：在保留现有 Velero 接入路径不变的前提下，用户只维护一套规则，系统在正向/反向路径自动编译为正确的恢复修改器。

## Current Baseline

1. 已有能力：
   - 支持通过 `AppRestore.spec.resourceModifierRules` 下发 Velero 资源修改规则；
   - 支持系统内建修改器（如 skeleton/trafficless）与 image rewrite 规则拼装；
   - 支持 SC/Ingress 映射规则生成，并在特定条件下执行反向兜底。
2. 当前缺口：
   - 缺少统一方向解析（forward/reverse）与通用可逆编译入口；
   - 缺少“单规则集自动反向”的通用抽象（非 SC/Ingress 特化）；
   - 缺少跨规则类型的一致治理与审计摘要。

## What Changes

1. 增强“双层规则模型（Velero 原生 + 平台可逆）”：
   - 复用 Velero 原生 `conditions + patches` 能力，支持 `groupResource/resourceNameRegex/namespaces/labelSelector/matches` 等匹配条件；
   - 新增平台可逆变换类型（`map`、`template`、`pair`），用于“单规则集自动正反向”；
   - 编译阶段统一产出 `AppRestore.spec.resourceModifierRules`，最终仍由 Velero 执行。

2. 新增“统一方向解析器（Direction Resolver）”：
   - 基于 `DisasterInstance` 运行态主备关系与配置基线判定 `forward|reverse`；
   - 统一作用于 `DataSync/ResourceSync/Drill` 的 AppRestore 构建链路。

3. 新增“统一规则编译器（Modifier Compiler）”：
   - 将平台可逆规则编译为 Velero 兼容的 `resourceModifierRules`；
   - 对 Velero 原生规则做语义透传（不重复实现匹配引擎）；
   - 与系统内建规则（skeleton/trafficless/class mapping）按优先级合并；
   - 对不可逆/冲突规则在预检阶段给出标准化错误；
   - 在统一方向解析启用后，逐步替代当前 `strictTargetValidation` 场景下的“失败后反向兜底”逻辑（灰度开关控制）。

4. 强化 `DisasterOperation` 预检：
   - Failover/Reprotect 在 PreCheck 必须执行 modifier compile dry-run；
   - dry-run 失败必须在破坏性步骤（如 `ScaleDownSource`）前终止操作并返回标准错误码。

5. 增强“治理与安全约束”：
   - 路径白名单/黑名单；
   - 规则数量与复杂度限额；
   - 审计注解输出（命中规则、方向、编译摘要）。

6. 新增“提交期服务端校验（Admission/Validate API）”：
   - 用户提交/更新 `DisasterInstance.spec.restorePolicy` 时，服务端必须即时执行规则校验；
   - 校验包含：DSL 编译校验、目标资源选择校验、JSON Pointer 路径可定位性校验；
   - 对 `path` 不存在、数组下标越界、目标资源为空命中等输入问题失败关闭并拒绝写入；
   - 执行期 PreCheck 仍保留 compile dry-run 作为二道防线，不作为首个暴露输入错误的阶段。

## Phase 1 Scope Lock

1. 配置入口固定为单一来源：`DisasterInstance.spec.restorePolicy`（实例级）。
2. `Profile CRD` 复用能力明确放入 Phase 2，不纳入本次实现范围。
3. `veleroNative` 在 Phase 1 仅支持 `patches(JSONPatch)`；`mergePatches/strategicPatches` 放入 Phase 2。
4. `directionPolicy` 默认值为 `Auto`：
   - `veleroNative + Auto`：forward/reverse 都应用同一条 patch（不反转值）；
   - `veleroNative + ForwardOnly/ReverseOnly`：按方向门控应用；
   - `reversible + Auto`：按方向自动正反向编译。
5. 方向判定边界：
   - `instance.status.primary/secondary` 均为空：按 baseline 作为 `forward`，并记录 `directionSource=baselineFallback`；
   - 两者均非空：与 baseline 比较，得到 `forward` 或 `reverse`；
   - 仅一侧为空或无法匹配 baseline：返回 `DirectionResolveFailed`。
6. `reversible` 规则在 Phase 1 禁止写入 JSON Pointer 追加路径（`/-`），避免不可逆与非幂等。
7. 治理默认拒绝 `/metadata/ownerReferences`，但允许**系统内建 trafficless 规则**仅在 `pods` 资源上执行 `remove /metadata/ownerReferences`（例外白名单）。
8. Phase 1 默认开启“提交期服务端校验”；规则不应等到 Failover/Reprotect 执行阶段才暴露路径定位错误。

## Phase 1 Capability Matrix

1. 规则来源：
   - `system rules`（内建）
   - `instance restorePolicy rules`（用户）
2. transform 类型（`reversible`）：
   - 支持：`map`、`template`、`pair`
   - 不支持：`patch`（留到后续）
3. velero patch 类型（`veleroNative`）：
   - 支持：`patches(JSONPatch)`
   - 不支持：`mergePatches`、`strategicPatches`
4. 冲突决议：
   - `system-protect` 永远优先（wins）
   - 其余规则按 `priority` 决议，高优先级 wins
   - 同优先级且同冲突键不同值，按 `onConflict`（默认 `Fail`）
5. 灰度开关：
   - `UseUnifiedDirectionResolver` Phase 1 默认 **关闭**（显式开启）
   - gate=false 时仅允许旧 SC/Ingress 路径；若检测到新 DSL 规则，必须显式失败并返回 `ModifierFeatureDisabled`
   - gate=false + legacy-only 时走旧路径，不触发统一方向解析
   - gate=true 时统一方向解析 + DSL 编译全路径生效

## Failover Order Premise

1. 本提案不改变 Failover 既有步骤顺序。
2. 本提案以前提顺序 `FinalSync -> ScaleDownSource` 为准（与当前控制器实现和 `openspec/project.md` 一致）。
3. 历史文档中的 `ScaleDownSource -> FinalSync` 描述视为过时内容，按本提案统一口径修订。

## Examples

### 1) `veleroNative` 透传匹配 + patch

```yaml
rules:
- id: add-label-by-selector
  mode: veleroNative
  applyTo: ["resourceSync"]
  directionPolicy: ForwardOnly
  conditions:
    groupResource: deployments.apps
    namespaces: ["prod"]
    labelSelector:
      matchLabels:
        app: web
  veleroRule:
    patches:
      - operation: add
        path: /metadata/labels/disaster-watched
        value: "true"
```

说明：

1. 方向由 Direction Resolver 统一计算（baseline `source/target` 对比 runtime `primary/secondary`），不是由 patch 内容猜测。
2. 该例 `directionPolicy=ForwardOnly`，仅在 `flow=forward` 命中；`flow=reverse` 自动跳过。
3. `veleroNative` 规则不做值反转，只做透传；若需双向不同值，使用 `reversible`（`map/template/pair`）或两条 `ForwardOnly/ReverseOnly` 规则。

### 2) SC 映射（`map` 自动反向）

```yaml
rules:
- id: sc-map-pvc
  mode: reversible
  applyTo: ["dataSync", "resourceSync"]
  conditions:
    groupResource: persistentvolumeclaims
  transform:
    type: map
    path: /spec/storageClassName
    mapping:
      sc-main: sc-dr
- id: sc-map-pv
  mode: reversible
  applyTo: ["dataSync", "resourceSync"]
  conditions:
    groupResource: persistentvolumes
  transform:
    type: map
    path: /spec/storageClassName
    mapping:
      sc-main: sc-dr
```

说明：PVC 与 PV 都使用明确路径 `/spec/storageClassName`；forward 时 `sc-main -> sc-dr`，reverse 时自动 `sc-dr -> sc-main`。

### 3) Service NodePort（`pair` 固定双值）

```yaml
rules:
- id: svc-nodeport
  mode: reversible
  applyTo: ["resourceSync"]
  conditions:
    groupResource: services
    resourceNameRegex: "^core-gateway$"
  transform:
    type: pair
    path: /spec/ports/0/nodePort
    forwardValue: 32080
    reverseValue: 30080
```

说明：同一套规则在主备反转后自动切回原端口，无需维护两套配置。

### 4) Deployment 环境变量（`template`）

```yaml
rules:
- id: db-host-template
  mode: reversible
  applyTo: ["resourceSync"]
  conditions:
    groupResource: deployments.apps
    resourceNameRegex: "^order-api$"
  transform:
    type: template
    path: /spec/template/spec/containers/0/env/0/value
    valueTemplate: 'mysql.{{ .TargetCluster }}.svc'
```

说明：forward 自动渲染主 -> 备地址；reverse 自动渲染备 -> 主地址。Phase 1 中 `reversible` 不允许使用 `/-` 追加路径。

## Concrete Paths

以下为提案第一阶段推荐的“具体路径”基线（JSON Pointer）：

1. StorageClass（PVC）：`groupResource=persistentvolumeclaims`，`path=/spec/storageClassName`
2. StorageClass（PV）：`groupResource=persistentvolumes`，`path=/spec/storageClassName`
3. IngressClass：`groupResource=ingresses.networking.k8s.io`，`path=/spec/ingressClassName`
4. Service NodePort：`groupResource=services`，`path=/spec/ports/0/nodePort`（多端口场景用具体下标路径）
5. Deployment 容器镜像：`groupResource=deployments.apps`，`path=/spec/template/spec/containers/0/image`
6. Deployment Env 覆盖：`groupResource=deployments.apps`，`path=/spec/template/spec/containers/0/env/0/value`
7. StatefulSet Env 覆盖：`groupResource=statefulsets.apps`，`path=/spec/template/spec/containers/0/env/0/value`
8. Annotation 写入：`groupResource=deployments.apps`，`path=/metadata/annotations/testudo.softcdata.com~1source`（`/` 需转义为 `~1`）

说明：Phase 1 中 `reversible` 数组字段必须使用确定下标，不允许 `/-`。若资源模板不稳定，建议先用更严格的 `conditions`（如 `resourceNameRegex`、`labelSelector`）缩小命中范围，再下发路径 patch。

## Non-Goals

1. 本变更不替换 Velero 执行面，最终仍通过 ResourceModifier ConfigMap 下发。
2. 本变更不在第一阶段重写 Velero 的匹配语义引擎（以透传复用为主）。
3. 本变更不移除现有 `resourceModifierRules` 入口，也不破坏已有规则语义。
4. 本变更不强制迁移已有 `storageClassMapping/ingressClassMapping` 配置，先保持兼容。
5. 本变更第一阶段不交付 Profile CRD 规则复用能力。
6. 本变更第一阶段不交付 `mergePatches/strategicPatches` 编译透传能力。
7. 本变更第一阶段不要求额外交付新的规则 CRD，规则仍收敛在 `DisasterInstance.spec.restorePolicy`。

## Impact

1. 用户层：
   - 从“多套规则手工切换”升级为“单规则集自动反向”。
2. 控制器层：
   - `restore/builder` 增加统一编译入口；
   - `admission/validation` 增加提交期服务端校验入口（可 webhook 或校验接口）；
   - `datasync/resourcesync/disasteroperation` 复用同一方向判定与编译逻辑。
3. 可观测性：
   - AppRestore 注解与事件新增方向与规则摘要，便于排障。
4. 生态兼容：
   - 用户已有 Velero Resource Modifiers 规则可低成本复用，并逐步叠加平台可逆能力。

## Risks

1. 规则过于自由可能触发越权修改：
   - 通过路径白名单和安全校验限制。
2. 旧规则迁移期间行为差异：
   - 先兼容旧字段并提供 dry-run 预检。
3. 方向判定错误导致反向不生效：
   - 引入显式方向计算日志与可测试的判定函数。
4. 提交期校验引入 API 时延与依赖风险：
   - 通过样本上限、超时控制、结构化错误码与二道 PreCheck 防线降低影响。
