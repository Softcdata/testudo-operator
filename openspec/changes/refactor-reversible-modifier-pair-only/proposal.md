# Change: 将 reversible DSL 收敛为 pair-only

Depends on:

1. `add-generic-reversible-resource-modifier-engine`

## Why

当前 proposal 把 `reversible` 设计为 `map/template/pair` 三种 transform，但 Phase 1 的真实能力边界已经表明它们高度重叠：

1. `map` 在现实现中只接受单条映射，实际语义就是“一对 source/target 值”。
2. `template` 在现实现中也是在 forward/reverse 两个方向产出两侧值，本质仍是“一对 source/target 结果”。
3. `pair` 已经能表达固定双值场景，而用户真正需要理解的核心其实只有一件事：同一路径在 baseline `source/target` 两侧分别是什么值。

继续保留三种写法，会把同一件事拆成三套心智模型，增加文档、校验、前端表单和排障成本。用户已经明确要求“`不用兼容，没有历史数据`”，因此没有必要再为旧写法保留兼容层。

## What Changes

1. 保持 `veleroNative` 透传模型不变：
   - `conditions + patches(JSONPatch)` 语义不变；
   - `directionPolicy`、治理校验、审计与 dry-run 逻辑不变。

2. 将 `reversible` 的**用户主模型**收敛为单一 canonical `pair`：
   - 用户只声明 `path + sourceValue + targetValue`；
   - 编译器根据 `forward/reverse` 选择目标侧值；
   - 不再把 `map/template` 作为正式用户心智模型。

3. 在 pair 值内部支持受限 placeholder：
   - `sourceValue/targetValue` 允许包含受限模板变量；
   - 用于覆盖原 `template` 的字符串场景；
   - 但不再提供独立 `template` mode。

4. 将旧 `map/template` 直接定义为非法输入：
   - Admission、校验与编译阶段必须保持一致拒绝口径；
   - 不提供 alias、归一化、兼容窗口或自动迁移；
   - 新文档、新示例、新配置入口统一只输出 canonical pair。

5. 编译输出与执行面保持不变：
   - 最终仍产出 `AppRestore.spec.resourceModifierRules`；
   - `AppRestore` 注解、`DisasterOperation` PreCheck dry-run、系统规则合并、冲突键与治理规则保持原语义。

6. 对外文档必须同步收敛：
   - 现有或新增的《自定义规则修改器说明书》必须只讲 `pair` 与 `veleroNative`；
   - 旧 `map/template` 只能作为“非法示例 + 替代写法”出现；
   - 不允许对外说明书继续把三种 reversible mode 并列展示。

## Scope Lock

1. 本次 change 只收敛 `reversible` 作者配置面，不改 `veleroNative`。
2. 本次 change 明确不兼容旧 `map/template` 写法；没有历史数据时，不为不存在的存量引入兼容复杂度。
3. 本次 change 不扩大 reversible 的表达能力；目标是**收敛心智，不新增新魔法**。
4. pair 值中的 placeholder 必须受限，禁止演化为新的脚本执行器。
5. pair-only 方案不得改变已有等价规则编译后的 `AppRestore.spec.resourceModifierRules` 结果。

## Rejected Inputs

以下旧写法不再属于合法 contract：

```yaml
rules:
- id: old-map
  mode: reversible
  map:
    path: /spec/storageClassName
    mapping:
      sc-main: sc-dr
```

```yaml
rules:
- id: old-template
  mode: reversible
  template:
    path: /spec/template/spec/containers/0/env/0/value
    forward: "mysql.{{ .TargetCluster }}.svc"
    reverse: "mysql.{{ .SourceCluster }}.svc"
```

拒绝理由：

1. pair-only 是唯一正式 contract。
2. `map/template` 只是旧设计阶段的表达分支，不再对外暴露。
3. 旧写法必须在提交期、校验期、编译期保持一致失败关闭。

## Canonical Model

```yaml
rules:
- id: pvc-sc
  mode: reversible
  applyTo: ["dataSync", "resourceSync"]
  conditions:
    groupResource: persistentvolumeclaims
  pair:
    path: /spec/storageClassName
    sourceValue: sc-main
    targetValue: sc-dr
```

语义：

1. `sourceValue` 指 baseline source 侧的目标值。
2. `targetValue` 指 baseline target 侧的目标值。
3. `flow=forward` 时编译 `targetValue`。
4. `flow=reverse` 时编译 `sourceValue`。

## Examples

### 1) `veleroNative` 不变

```yaml
rules:
- id: add-label-by-selector
  mode: veleroNative
  applyTo: ["resourceSync"]
  directionPolicy: ForwardOnly
  conditions:
    groupResource: deployments.apps
    namespaces: ["prod"]
  veleroRule:
    patches:
      - operation: add
        path: /metadata/labels/disaster-watched
        value: "true"
```

### 2) SC 映射改写为 pair

```yaml
rules:
- id: pvc-sc
  mode: reversible
  applyTo: ["dataSync", "resourceSync"]
  conditions:
    groupResource: persistentvolumeclaims
  pair:
    path: /spec/storageClassName
    sourceValue: sc-main
    targetValue: sc-dr
```

### 3) NodePort 仍是 pair

```yaml
rules:
- id: svc-nodeport
  mode: reversible
  applyTo: ["resourceSync"]
  conditions:
    groupResource: services
    resourceNameRegex: "^core-gateway$"
  pair:
    path: /spec/ports/0/nodePort
    sourceValue: "30080"
    targetValue: "32080"
```

### 4) 原 template 场景改为 pair + placeholder

```yaml
rules:
- id: db-host
  mode: reversible
  applyTo: ["resourceSync"]
  conditions:
    groupResource: deployments.apps
    resourceNameRegex: "^order-api$"
  pair:
    path: /spec/template/spec/containers/0/env/0/value
    sourceValue: "mysql.{{ .SourceCluster }}.svc"
    targetValue: "mysql.{{ .TargetCluster }}.svc"
```

## Non-Goals

1. 不替换 Velero 执行面。
2. 不改变 `veleroNative` 透传 contract。
3. 不扩大 Phase 1 reversible 的可写路径、patch 类型或脚本能力。
4. 不在本 proposal 中交付运行时代码改造；本轮只定义目标 contract 与后续实施任务。
