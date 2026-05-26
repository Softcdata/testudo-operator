# 规范：自定义资源修改器

## Purpose
定义 `DisasterInstance.spec.restorePolicy.modifierRules` 的正式 contract、编译语义、治理限制和提交校验要求。该规范覆盖 `reversible` 与 `veleroNative` 两类模式，其中 `reversible` 仅保留 canonical `pair`。

## Requirements

### Requirement: reversible MUST 使用 pair canonical form
`reversible` 规则必须 (MUST) 使用 `pair.path`、`pair.sourceValue`、`pair.targetValue` 描述同一路径在 baseline source/target 两侧的目标值。

#### Scenario: PVC 存储类映射
- **Given** 用户需要把 `PersistentVolumeClaim.spec.storageClassName` 从 `sc-main` 改为 `sc-dr`
- **When** 编写一条 `reversible` 规则
- **Then** 规则必须 (MUST) 使用 `pair.path=/spec/storageClassName`
- **And** 必须 (MUST) 使用 `pair.sourceValue=sc-main`
- **And** 必须 (MUST) 使用 `pair.targetValue=sc-dr`

#### Scenario: Service NodePort 双向固定值
- **Given** 用户需要让同一个 Service 在两端使用不同 NodePort
- **When** 编写一条 `reversible` 规则
- **Then** 编译器必须 (MUST) 在正向流程选择 `targetValue`
- **And** 在反向流程选择 `sourceValue`

### Requirement: pair 值 MUST 只允许受限 placeholder
`pair.sourceValue` 与 `pair.targetValue` 必须 (MUST) 只允许受限 placeholder，并且不得 (MUST NOT) 暴露通用模板执行能力。

#### Scenario: 允许受限 placeholder
- **Given** 用户需要在环境变量中引用当前 baseline 源/目标集群名
- **When** 在 pair 值中填写 `{{ .SourceCluster }}`、`{{ .TargetCluster }}` 或 `{{ .Flow }}`
- **Then** 系统必须 (MUST) 正确渲染这些值

#### Scenario: 拒绝通用模板表达式
- **Given** 用户在 pair 值中使用 `printf`、`if` 或其他未授权模板语法
- **When** 系统编译该规则
- **Then** 必须 (MUST) 失败关闭
- **And** 错误消息必须 (MUST) 明确说明只允许受限 placeholder

### Requirement: veleroNative MUST 保持透传
`veleroNative` 规则必须 (MUST) 继续使用 `veleroRule.patches` 透传 Velero JSONPatch，不得被本次 pair-only 改造改变语义。

#### Scenario: 透传标签补丁
- **Given** 用户为 `deployments.apps` 配置一条 `veleroNative` 规则
- **When** 规则包含 `add /metadata/labels/...`
- **Then** 编译器必须 (MUST) 产出等价的 `AppRestore.spec.resourceModifierRules`

### Requirement: 提交期 MUST 执行治理与资源定位校验
提交期校验必须 (MUST) 覆盖规则复杂度、JSON Pointer 路径、资源命中范围和实例命名空间边界。

#### Scenario: 命中零资源时拒绝
- **Given** 一条规则的 `conditions` 最终没有匹配到任何资源
- **When** 用户提交实例创建或更新
- **Then** 系统必须 (MUST) 拒绝该请求

#### Scenario: 禁止修改受保护路径
- **Given** 一条规则试图修改 `/status`、`/metadata/finalizers` 或 `/metadata/ownerReferences`
- **When** 系统执行治理校验
- **Then** 系统必须 (MUST) 拒绝该规则

#### Scenario: metadata 标签与注解值必须保持字符串语义
- **Given** 一条 `reversible` 规则修改 `/metadata/annotations/*`、`/metadata/labels/*` 或嵌套 `metadata` 下的同类路径
- **And** `pair.sourceValue` 或 `pair.targetValue` 看起来像数字、布尔值或 `null`
- **When** 系统编译规则并提交到 Velero
- **Then** 系统必须 (MUST) 保持该值按 string 写入目标字段
- **And** 提交期校验必须 (MUST) 拒绝会在 live 字段类型上造成显式类型错配的 pair 值

### Requirement: 旧 transform 输入 MUST 被拒绝
旧 `transform.type=map/template/pair` 输入不得 (MUST NOT) 继续作为正式 contract 被接受。

#### Scenario: 旧 map 输入
- **Given** 用户仍提交旧 `transform.type=map`
- **When** 系统处理该规则
- **Then** 必须 (MUST) 失败关闭
- **And** 错误消息必须 (MUST) 指向 `pair.path/sourceValue/targetValue`

#### Scenario: 旧 template 输入
- **Given** 用户仍提交旧 `transform.type=template`
- **When** 系统处理该规则
- **Then** 必须 (MUST) 失败关闭
- **And** 不得 (MUST NOT) 隐式转换成新的 pair 规则
