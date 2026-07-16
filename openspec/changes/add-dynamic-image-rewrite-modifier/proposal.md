# Change: 动态镜像重写资源修改器

## Why
实例级 `bulkModifierActions.replaceExactValue` 可以把完整镜像值展开为 `modifierRuleSnapshot`，但完整镜像值本身是高频变化对象。应用每次发布后，源集群中的镜像 tag 或 digest 都可能变化，例如：

```text
10.11.11.1:5000/blueking/bcs-bkcmdb-synchronizer:v1.30.0
10.11.11.1:5000/blueking/bcs-bkcmdb-synchronizer:v1.31.0
10.11.11.1:5000/blueking/bcs-bkcmdb-synchronizer@sha256:...
```

如果用户 DSL 绑定 `sourceValue=完整镜像`，一旦源镜像变化，之前的 DSL 和已展开快照就会失效。用户被迫在每次发布后维护容灾 DSL，这不适合资源同步场景。

本变更将镜像替换从“完整值替换”改为“运行时镜像重写意图”：

- 用户 DSL 只声明稳定意图：哪些源镜像命名空间/仓库前缀应写入哪个目标仓库布局。
- 每次 ResourceSync / Drill / Failover 恢复构建时，系统读取源集群真实资源中的当前镜像值。
- 系统按 DSL 规则推导目标镜像，并动态编译为现有资源修改器可执行的精确 `pair` 规则。

该方案不恢复旧的 `Cluster.spec.imageSources` + `DisasterConfig.spec.imageRewrite` 镜像源映射模型。旧模型是集群级别镜像源别名映射；本变更是实例恢复策略中的资源修改器 DSL 语义，作用域、审计和执行链路都应归入 `restorePolicy`。

## What Changes
### 1. 新增动态镜像重写 bulk action
在 `DisasterInstance.spec.restorePolicy.bulkModifierActions` 中新增动作类型：

```yaml
action: rewriteImage
```

该动作不要求用户填写完整 `sourceValue` / `targetValue`，而是填写稳定的镜像重写规则：

```yaml
restorePolicy:
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

### 2. 运行时动态编译，不修改用户原始 DSL
`rewriteImage` 是运行时编译动作。系统不得在同步时改写用户提交的 `bulkModifierActions`，也不得把当前 tag/digest 回写进用户原始 DSL。

每次恢复构建阶段生成一个本次操作专属的 runtime compiled ruleset：

- 读取当前源集群资源 spec 中的真实镜像。
- 对命中的镜像生成标准 `reversible pair` 规则。
- 将编译结果写入本次 `AppRestore.spec.resourceModifierRules` 或等价运行时快照。
- 通过状态、事件或注解记录编译摘要。

### 3. 保持执行层为现有资源修改器 contract
动态镜像重写不新增底层执行引擎。编译产物必须继续符合现有自定义资源修改器 contract：

- `reversible` 使用 `pair.path/sourceValue/targetValue`
- `veleroNative` 继续透传 Velero JSONPatch
- 不恢复旧 `transform/map/template`
- 不允许生成 `/status/**`、`/metadata/finalizers/**`、`/metadata/ownerReferences/**` 等 forbidden path

### 4. 支持源真实镜像变化
同一条 DSL 必须在不同发布版本下持续有效。例如当前源集群镜像为：

```text
10.11.11.1:5000/blueking/bcs-bkcmdb-synchronizer:v1.31.0
```

系统应在本次 ResourceSync 中动态生成：

```text
registry-test.xxx.xxx.com:30088/dr_images/10_11_11_1_5000/blueking/bcs-bkcmdb-synchronizer:v1.31.0
```

当下一次发布切换为 digest 镜像时，系统应重新读取源集群当前值并重新编译，而不是依赖上一次快照。

### 5. 与旧镜像源映射提案划清边界
本变更不继续推进 `add-image-source-mapping` 的用户模型：

- 不要求维护 `Cluster.spec.imageSources` 别名目录。
- 不要求在 `DisasterConfig.spec.imageRewrite` 中维护源/目标别名映射。
- 不将镜像替换配置下沉为主备关系级全局配置。

平台可以在 UI/API 层提示用户将旧镜像源映射迁移为 `rewriteImage` DSL，但恢复构建链路应以新的 DSL 动态编译模型为准。

### 6. Server / Operator / Web 配套
- Operator：
  - 在 ResourceSync / Drill / Failover 的恢复构建阶段触发动态镜像规则编译。
  - 只扫描源集群资源 spec 中允许修改的镜像字段。
  - 将运行时编译产物交给现有 restore builder / AppRestore ResourceModifier 执行。
- Server：
  - 接受、校验并保存 `rewriteImage` 动作。
  - 提供 preview API 时，应连接源集群读取当前镜像并返回本次预计生成规则。
  - 不在实例持久化时把 `rewriteImage` 展开为长期 `modifierRuleSnapshot`。
- Web：
  - 在自定义资源修改器 DSL 中提供“动态镜像重写”配置入口。
  - 展示 preview 中的当前源镜像、推导目标镜像和跳过路径。
  - 创建实例 Drill 时，若用户未启用 Drill 级资源定制化修改和批量修改，必须省略 `restorePolicy`；只有用户显式配置 Drill 覆盖时才发送该字段。

## Impact
- 受影响规范：
  - `restore-modifier`
  - `restore-builder`
- 受影响代码（实施阶段）：
  - `pkg/apis/disaster/v1/disasterinstance_types.go`
  - `internal/controller/restore/*`
  - `internal/controller/resourcesync/*`
  - `internal/controller/disasteroperation/*`
  - 可能涉及 `internal/controller/disasterinstance/*` 状态摘要
- 跨仓库影响：
  - `disaster-server`：实例 restore policy API、preview API、校验与文档
  - `cluster-disaster-web`：自定义资源修改器配置和 preview 展示
  - `disaster-system-chart`：CRD 发布与文档说明

## Risks
- 运行时扫描源集群会增加恢复构建阶段对源集群可达性的依赖。
- 如果运行时编译产物和用户手写规则同时命中同一路径，必须有确定性冲突处理。
- 多条镜像重写规则可能同时匹配同一镜像，需要定义最长匹配和冲突拒绝。
- 如果将运行时规则错误写回用户 DSL，会造成 spec 抖动和审计污染。
- 如果 Web 无条件提交空 Drill `restorePolicy`，会触发 Drill 覆盖语义并使实例级 `rewriteImage` 在运行时不可见。

## Mitigation
- 用户 DSL 与 runtime compiled ruleset 分离，源镜像完整值只存在于本次操作快照和审计摘要中。
- 默认只支持前缀推导，不在首期支持任意正则替换或脚本模板。
- 多规则匹配采用最长 `sourcePrefix` 优先；无法确定唯一结果时失败关闭。
- 路径扫描复用 forbidden path 治理，禁止生成 status / finalizers / ownerReferences 规则。
- 提供 preview 和事件摘要，展示本次真实源镜像、目标镜像、生成规则数量、跳过路径和冲突明细。
- 将无 Drill 级配置表示为字段缺失，而不是空对象；同时以实际 Drill `AppRestore` 和 Web 请求回归覆盖继承路径。
