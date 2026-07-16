## ADDED Requirements

### Requirement: RestorePolicy MUST 支持动态镜像重写动作
系统必须 (MUST) 在 `DisasterInstance.spec.restorePolicy.bulkModifierActions` 中支持用于镜像重写的运行时动作。该动作必须表达稳定的镜像重写意图，不得要求用户绑定完整镜像 tag 或 digest。

#### Scenario: 使用稳定前缀声明镜像重写意图
- **Given** 用户需要将源集群 `10.11.11.1:5000/` 下的镜像恢复到目标集群 `registry-test.xxx.xxx.com:30088/dr_images/10_11_11_1_5000/` 下
- **When** 用户声明 `bulkModifierActions[].action=rewriteImage`
- **Then** 规则必须 (MUST) 允许用户填写 `imageRewrite.sourcePrefix=10.11.11.1:5000/`
- **And** 必须 (MUST) 允许用户填写 `imageRewrite.targetPrefix=registry-test.xxx.xxx.com:30088/dr_images/10_11_11_1_5000/`
- **And** 不得 (MUST NOT) 要求用户填写完整 `sourceValue` 或 `targetValue`

#### Scenario: 源镜像 tag 变化后 DSL 仍有效
- **Given** 用户已经声明一条 `rewriteImage` 动作
- **And** 上一次同步时源镜像为 `10.11.11.1:5000/blueking/app:v1.30.0`
- **And** 本次同步时源镜像变为 `10.11.11.1:5000/blueking/app:v1.31.0`
- **When** 系统执行本次运行时编译
- **Then** 系统必须 (MUST) 基于本次源集群真实镜像生成目标镜像 `registry-test.xxx.xxx.com:30088/dr_images/10_11_11_1_5000/blueking/app:v1.31.0`
- **And** 不得 (MUST NOT) 依赖上一次完整镜像值

### Requirement: 动态镜像重写 MUST 编译为现有 pair contract
动态镜像重写的运行时编译结果必须 (MUST) 使用现有资源修改器正式 contract，不得引入新的底层执行结构。

#### Scenario: 运行时生成 reversible pair
- **Given** 源集群中一个 Deployment 的 `/spec/template/spec/containers/0/image` 为 `10.11.11.1:5000/blueking/app:v1.31.0`
- **And** `rewriteImage` 动作匹配该镜像
- **When** 系统编译本次恢复规则
- **Then** 必须 (MUST) 生成一条 `reversible` 规则
- **And** 该规则必须 (MUST) 使用 `pair.path=/spec/template/spec/containers/0/image`
- **And** 该规则必须 (MUST) 使用 `pair.sourceValue=10.11.11.1:5000/blueking/app:v1.31.0`
- **And** 该规则必须 (MUST) 使用推导出的 `pair.targetValue`

#### Scenario: 不修改用户原始 DSL
- **Given** 用户声明了一条 `rewriteImage` 动作
- **When** 系统在 ResourceSync 中发现当前源镜像并生成运行时 pair 规则
- **Then** 系统不得 (MUST NOT) 将当前完整镜像值回写到 `bulkModifierActions`
- **And** 不得 (MUST NOT) 因镜像 tag 变化修改用户原始 DSL

### Requirement: 动态镜像重写 MUST 跳过 forbidden path
动态镜像重写扫描源资源时必须 (MUST) 只基于可修改 spec 路径生成规则，并且必须 (MUST) 跳过现有资源修改器治理禁止的路径。

#### Scenario: 工作负载 initContainers 镜像进入运行时规则
- **Given** 一个 Deployment、Job、ReplicaSet 或 ReplicationController 的 PodSpec 包含 `initContainers[].image`
- **And** 该 initContainer 镜像命中 `rewriteImage` 的 `sourcePrefix`
- **When** 系统编译动态镜像重写规则
- **Then** 系统必须 (MUST) 为对应的 `/spec/.../initContainers/<index>/image` 生成规则
- **And** 该规则必须 (MUST) 保留 initContainer 原有数组下标

### Requirement: Drill 未提供覆盖策略时 MUST 继承实例动态镜像重写
当 Drill 没有配置 Drill 级资源修改器或批量修改器时，系统必须 (MUST) 保留实例级 `restorePolicy`，不得通过一个无业务含义的空 `restorePolicy` 覆盖实例的 `rewriteImage` 动作。

#### Scenario: 未配置 Drill 级修改器的演练重写多个 initContainers
- **Given** `DisasterInstance.spec.restorePolicy.bulkModifierActions` 包含 `applyTo=["drill"]` 的 `rewriteImage` 动作
- **And** 源集群的 Deployment 包含两个命中前缀的 `initContainers[].image`
- **And** 用户未配置 Drill 级资源定制化修改或批量修改
- **When** 系统创建本次 Drill 的资源 `AppRestore`
- **Then** Drill 请求不得 (MUST NOT) 携带空的 `restorePolicy` 覆盖
- **And** `AppRestore.spec.resourceModifierRules` 必须 (MUST) 包含两个对应的 `/spec/template/spec/initContainers/<index>/image` 运行时规则

#### Scenario: Pod status image 不进入运行时规则
- **Given** 一个 Pod 同时包含 `/spec/containers/0/image` 和 `/status/containerStatuses/0/image`
- **And** 两个字段的镜像值均匹配 `rewriteImage` 的 `sourcePrefix`
- **When** 系统编译动态镜像重写规则
- **Then** 系统可以 (MAY) 为 `/spec/containers/0/image` 生成规则
- **And** 不得 (MUST NOT) 为 `/status/containerStatuses/0/image` 生成规则

#### Scenario: forbidden metadata path 不进入运行时规则
- **Given** 一个资源的 forbidden metadata 路径中存在看起来像镜像的字符串
- **When** 系统编译动态镜像重写规则
- **Then** 不得 (MUST NOT) 为 `/metadata/finalizers/**` 或 `/metadata/ownerReferences/**` 生成规则

### Requirement: 动态镜像重写 MUST 提供确定性匹配与冲突处理
动态镜像重写必须 (MUST) 在多规则匹配和规则冲突时提供确定性行为。

#### Scenario: 多个前缀命中时使用最长前缀
- **Given** 两条 `rewriteImage` 动作分别声明 `sourcePrefix=10.11.11.1:5000/` 和 `sourcePrefix=10.11.11.1:5000/blueking/`
- **And** 源镜像为 `10.11.11.1:5000/blueking/app:v1.31.0`
- **When** 系统编译动态镜像重写规则
- **Then** 系统必须 (MUST) 使用 `10.11.11.1:5000/blueking/` 对应的规则

#### Scenario: 同一最长前缀产生冲突目标时失败
- **Given** 两条 `rewriteImage` 动作对同一镜像具有相同长度的最长匹配前缀
- **And** 两条动作会生成不同的目标镜像
- **When** 系统编译动态镜像重写规则
- **Then** 系统必须 (MUST) 失败关闭
- **And** 错误必须 (MUST) 包含冲突资源、路径和动作 ID

### Requirement: 动态镜像重写 MUST 支持未匹配策略
动态镜像重写必须 (MUST) 支持未匹配镜像策略，用于控制保护范围内镜像未命中任何规则时的行为。

#### Scenario: 默认 Keep 未匹配镜像
- **Given** 一个源集群资源包含未命中任何 `rewriteImage` 规则的镜像
- **And** 用户未显式设置 `unmatchedPolicy`
- **When** 系统编译动态镜像重写规则
- **Then** 系统必须 (MUST) 保持该镜像不变
- **And** 必须 (MUST) 在编译摘要中记录未匹配镜像

#### Scenario: Fail 策略拒绝未匹配镜像
- **Given** 一个源集群资源包含未命中任何 `rewriteImage` 规则的镜像
- **And** 用户设置 `imageRewrite.unmatchedPolicy=Fail`
- **When** 系统编译动态镜像重写规则
- **Then** 系统必须 (MUST) 失败关闭
- **And** 错误必须 (MUST) 包含未匹配镜像明细
