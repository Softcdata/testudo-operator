# 设计：动态镜像重写资源修改器

## 1. 设计目标
1. 解决完整镜像值高频变化导致 `replaceExactValue` DSL 和静态快照失效的问题。
2. 将镜像重写表达为实例级 `restorePolicy` DSL 意图，而不是旧的集群级镜像源映射。
3. 在每次 ResourceSync / Drill / Failover 恢复构建时读取源集群当前真实镜像并动态编译。
4. 保持执行面仍为现有自定义资源修改器 `pair` contract。
5. 严格避免生成 forbidden path，尤其是 `/status/containerStatuses/*/image`。

## 2. 非目标
1. 不恢复 `Cluster.spec.imageSources` + `DisasterConfig.spec.imageRewrite` 作为主要用户配置模型。
2. 不让用户在 DSL 中维护完整镜像 tag/digest。
3. 不在首期支持任意正则替换、脚本、通用模板或 OCI 镜像重命名 DSL。
4. 不在同步时直接修改用户原始 `bulkModifierActions`。
5. 不为了避开 status 镜像而默认排除 `pods` 资源。

## 3. 核心设计选择

### 3.1 DSL 表达稳定意图，不表达完整镜像值
旧写法的问题：

```yaml
bulkModifierActions:
  - id: replace-bkcmdb-synchronizer-image
    action: replaceExactValue
    sourceValue: "10.11.11.1:5000/blueking/bcs-bkcmdb-synchronizer:v1.30.0"
    targetValue: "registry-test.xxx.xxx.com:30088/dr_images/10_11_11_1_5000/blueking/bcs-bkcmdb-synchronizer:v1.30.0"
```

当源集群升级到 `v1.31.0` 后，`sourceValue` 不再命中，DSL 失效。

新写法：

```yaml
bulkModifierActions:
  - id: rewrite-primary-registry-to-dr
    action: rewriteImage
    enabled: true
    applyTo: ["resourceSync", "drill", "failover"]
    directionPolicy: Auto
    imageRewrite:
      sourcePrefix: "10.11.11.1:5000/"
      targetPrefix: "registry-test.xxx.xxx.com:30088/dr_images/10_11_11_1_5000/"
      unmatchedPolicy: Keep
      digestPolicy: Preserve
```

完整镜像值只在运行时读取：

```text
source image = 10.11.11.1:5000/blueking/bcs-bkcmdb-synchronizer:v1.31.0
target image = registry-test.xxx.xxx.com:30088/dr_images/10_11_11_1_5000/blueking/bcs-bkcmdb-synchronizer:v1.31.0
```

### 3.2 用户 DSL 与运行时编译产物分离
本变更采用两层模型：

- 意图层：`restorePolicy.bulkModifierActions[].action=rewriteImage`
- 执行层：本次操作动态生成的 `reversible pair` 规则

系统不得把运行时发现的 tag/digest 回写到意图层。运行时编译产物应只存在于：

- 本次 `AppRestore.spec.resourceModifierRules`
- 或等价的 runtime snapshot / ConfigMap
- 或状态、事件、注解中的审计摘要

这样可以避免：

- 实例 spec 因每次发版而抖动
- 审计中混淆“用户意图”和“本次执行结果”
- 多次同步间复用过期镜像值

### 3.3 与现有 `modifierRuleSnapshot` 的关系
现有 `bulkModifierActions` 设计中，`replaceExactValue/removeKey` 可由 server 在提交期展开为 `modifierRuleSnapshot`，operator 只消费快照。

`rewriteImage` 不适合提交期快照，因为它的核心输入是“同步时源集群当前镜像”。因此：

- `replaceExactValue/removeKey` 可以继续使用提交期快照。
- `rewriteImage` 必须作为 runtime-compiled bulk action。
- 存在 `rewriteImage` 时，restore builder 需要在恢复构建阶段重新编译镜像规则。
- 编译后的规则应与手写规则、其他快照规则合并后再提交给 AppRestore。

如需避免与现有 `modifierRuleSnapshot` 失败关闭语义冲突，实施阶段应引入明确标记：

```yaml
bulkModifierActions:
  - action: rewriteImage
    compilePhase: Runtime
```

或在内部根据 action 类型固定判定为 runtime action。operator 不应因为 `rewriteImage` 没有长期 `modifierRuleSnapshot` 而失败关闭。

### 3.4 动态编译流程
每次 ResourceSync / Drill / Failover 恢复构建阶段执行：

1. 读取实例 `restorePolicy`，筛选 `enabled != false` 且 `applyTo` 命中当前操作的 `rewriteImage` 动作。
2. 连接当前源集群，按实例保护范围列出资源。
3. 扫描允许的 spec 镜像字段。
4. 对每个镜像值按 `sourcePrefix` 做匹配。
5. 选择最长匹配规则。
6. 生成目标镜像。
7. 生成标准 `reversible pair` 规则。
8. 与其他可执行修改器规则合并。
9. 写入本次 AppRestore ResourceModifier。
10. 记录编译摘要。

Drill 在第 1 步前必须先计算有效恢复策略：未提供 `DrillConfig.restorePolicy` 时继承实例 `restorePolicy`；提供非空覆盖时保留现有“Drill 修改器/bulk 输入替换实例输入”的语义。因此，Web 在两个 Drill 级修改开关均关闭时不得提交空 `restorePolicy`，否则会把实例的 `rewriteImage` 动作替换为空列表，导致后续扫描和编译都不会执行。

### 3.5 允许扫描和生成的路径
允许生成规则的路径包括：

```text
/spec/containers/*/image
/spec/initContainers/*/image
/spec/ephemeralContainers/*/image
/spec/template/spec/containers/*/image
/spec/template/spec/initContainers/*/image
/spec/template/spec/ephemeralContainers/*/image
/spec/jobTemplate/spec/template/spec/containers/*/image
/spec/jobTemplate/spec/template/spec/initContainers/*/image
```

禁止生成规则的路径包括：

```text
/status
/status/**
/metadata/finalizers
/metadata/finalizers/**
/metadata/ownerReferences
/metadata/ownerReferences/**
```

Pod 资源本身不应被默认排除。Pod spec 中的镜像字段可以修改；Pod status 中的镜像字段必须跳过。

### 3.6 镜像重写算法
首期只支持前缀重写：

```text
targetImage = targetPrefix + strings.TrimPrefix(sourceImage, sourcePrefix)
```

示例：

```text
sourcePrefix = 10.11.11.1:5000/
targetPrefix = registry-test.xxx.xxx.com:30088/dr_images/10_11_11_1_5000/
sourceImage  = 10.11.11.1:5000/blueking/bcs-bkcmdb-synchronizer:v1.31.0
targetImage  = registry-test.xxx.xxx.com:30088/dr_images/10_11_11_1_5000/blueking/bcs-bkcmdb-synchronizer:v1.31.0
```

digest 镜像按相同规则保留 suffix：

```text
sourceImage = 10.11.11.1:5000/blueking/app@sha256:abc
targetImage = registry-test.xxx.xxx.com:30088/dr_images/10_11_11_1_5000/blueking/app@sha256:abc
```

### 3.7 多规则匹配与冲突
规则：

1. 多条 `sourcePrefix` 命中同一镜像时，最长前缀优先。
2. 如果最长前缀存在多个规则且目标不一致，编译失败。
3. 如果生成后的同一资源、同一路径已有手写规则或其他 bulk 规则修改为不同值，按现有 `priority/onConflict` 处理；无法确定时失败关闭。
4. 运行时生成规则默认 `priority=-100`，让用户手写精确规则默认覆盖动态规则。

### 3.8 方向策略
`directionPolicy=Auto` 时，系统根据当前主备方向选择 rewrite 方向：

- 正向：`sourcePrefix -> targetPrefix`
- 反向：`targetPrefix -> sourcePrefix`

`ForwardOnly` 只允许正向。首期可以不提供 `ReverseOnly`，除非现有枚举已经包含该值。

### 3.9 未匹配策略
`unmatchedPolicy`：

- `Keep`：未匹配镜像保持原值，默认值。
- `Fail`：保护范围内可扫描镜像未命中任何 `rewriteImage` 规则时，本次编译失败。

默认使用 `Keep`，避免公共镜像、sidecar 镜像或特殊镜像导致同步不可用。需要强一致镜像治理的用户可显式设置 `Fail`。

### 3.10 Preview 与审计
建议提供 preview 能力，输入当前操作类型，返回运行时预计编译结果：

```json
{
  "operation": "resourceSync",
  "generatedRuleCount": 1,
  "matchedImages": [
    {
      "resource": "deployments.apps",
      "namespace": "bkcmdb",
      "name": "bcs-bkcmdb-synchronizer",
      "path": "/spec/template/spec/containers/0/image",
      "sourceImage": "10.11.11.1:5000/blueking/bcs-bkcmdb-synchronizer:v1.31.0",
      "targetImage": "registry-test.xxx.xxx.com:30088/dr_images/10_11_11_1_5000/blueking/bcs-bkcmdb-synchronizer:v1.31.0",
      "bulkActionID": "rewrite-primary-registry-to-dr"
    }
  ],
  "skippedPaths": [
    {
      "resource": "pods",
      "namespace": "bkcmdb",
      "name": "bcs-bkcmdb-synchronizer-xxx",
      "path": "/status/containerStatuses/0/image",
      "reason": "forbiddenPath"
    }
  ],
  "unmatchedImages": []
}
```

运行时应记录：

- `generatedRuleCount`
- `matchedImageCount`
- `unmatchedImageCount`
- `skippedForbiddenPathCount`
- `compileHash`
- `bulkActionIDs`

## 4. 示例编译结果
用户 DSL：

```yaml
bulkModifierActions:
  - id: rewrite-primary-registry-to-dr
    action: rewriteImage
    enabled: true
    applyTo: ["resourceSync"]
    directionPolicy: Auto
    imageRewrite:
      sourcePrefix: "10.11.11.1:5000/"
      targetPrefix: "registry-test.xxx.xxx.com:30088/dr_images/10_11_11_1_5000/"
```

源集群当前 Deployment：

```yaml
spec:
  template:
    spec:
      containers:
        - name: synchronizer
          image: 10.11.11.1:5000/blueking/bcs-bkcmdb-synchronizer:v1.31.0
```

运行时生成规则：

```yaml
- id: runtime-image-rewrite-0001
  mode: reversible
  applyTo: ["resourceSync"]
  priority: -100
  conditions:
    groupResource: deployments.apps
    namespaces: ["bkcmdb"]
    resourceNameRegex: "^bcs-bkcmdb-synchronizer$"
  pair:
    path: /spec/template/spec/containers/0/image
    sourceValue: "10.11.11.1:5000/blueking/bcs-bkcmdb-synchronizer:v1.31.0"
    targetValue: "registry-test.xxx.xxx.com:30088/dr_images/10_11_11_1_5000/blueking/bcs-bkcmdb-synchronizer:v1.31.0"
  directionPolicy: Auto
```

## 5. 验证策略
1. CRD/type 测试：`rewriteImage` 字段可读写，且不要求完整 `sourceValue/targetValue`。
2. 编译器单测：tag 变化后同一 DSL 生成新的 pair 规则。
3. 编译器单测：digest 镜像保留 digest suffix。
4. 编译器单测：多条前缀命中时最长前缀优先。
5. 编译器单测：同一最长前缀目标冲突时失败。
6. forbidden path 回归：Pod status image 不进入运行时规则。
7. restore builder 单测：`rewriteImage` 不要求长期 `modifierRuleSnapshot`，但本次 AppRestore 包含运行时生成规则。
8. E2E：源集群镜像从 `v1.30.0` 更新到 `v1.31.0` 后，不修改 DSL，再次 ResourceSync 仍生成新目标镜像。
9. E2E：`unmatchedPolicy=Fail` 时，未命中镜像导致恢复构建失败并返回明细。
10. Drill 回归：未提供 Drill 级 `restorePolicy` 时，实例 `rewriteImage` 仍会在生成的 AppRestore 中重写多个 `initContainers`。
11. Web E2E：两个 Drill 级修改开关关闭时，创建请求不携带 `restorePolicy`，开启任一开关后才携带显式覆盖。
