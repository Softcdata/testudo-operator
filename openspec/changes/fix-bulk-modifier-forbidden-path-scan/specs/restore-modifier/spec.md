## MODIFIED Requirements

### Requirement: 提交期 MUST 执行治理与资源定位校验
提交期校验必须 (MUST) 覆盖规则复杂度、JSON Pointer 路径、资源命中范围和实例命名空间边界。对于由 `bulkModifierActions` 生成的规则快照，系统必须 (MUST) 在快照生成阶段跳过禁止修改的受保护路径，并保留最终规则校验作为兜底。

#### Scenario: 命中零资源时拒绝
- **Given** 一条规则的 `conditions` 最终没有匹配到任何资源
- **When** 用户提交实例创建或更新
- **Then** 系统必须 (MUST) 拒绝该请求

#### Scenario: 禁止修改受保护路径
- **Given** 一条规则试图修改 `/status`、`/metadata/finalizers` 或 `/metadata/ownerReferences`
- **When** 系统执行治理校验
- **Then** 系统必须 (MUST) 拒绝该规则

#### Scenario: bulk replaceExactValue 跳过 status 镜像
- **Given** 一个 `bulkModifierActions.replaceExactValue` 的 `sourceValue` 同时出现在 `Deployment.spec.template.spec.containers[*].image` 与 `Pod.status.containerStatuses[*].image`
- **When** Server 生成 `modifierRuleSnapshot`
- **Then** 系统必须 (MUST) 为 Deployment spec 镜像生成可执行规则
- **And** 系统不得 (MUST NOT) 为 `/status/containerStatuses/*/image` 生成规则
- **And** 用户不得 (MUST NOT) 需要通过 `resourceSelection.excludedResources=["pods"]` 绕过该问题

#### Scenario: bulk 动作只命中受保护路径
- **Given** 一个已启用的 `bulkModifierAction` 只命中 `/status/**`、`/metadata/finalizers/**` 或 `/metadata/ownerReferences/**`
- **When** Server 生成 `modifierRuleSnapshot`
- **Then** 系统不得 (MUST NOT) 生成包含受保护路径的规则
- **And** 系统必须 (MUST) 按无可执行命中失败关闭

#### Scenario: bulk removeKey 跳过受保护对象键
- **Given** 一个 `bulkModifierActions.removeKey` 的 `key` 命中 `/status/**`、`/metadata/finalizers/**` 或 `/metadata/ownerReferences/**` 下的对象键
- **When** Server 生成 `modifierRuleSnapshot`
- **Then** 系统不得 (MUST NOT) 为这些受保护路径生成 remove patch

#### Scenario: metadata 标签与注解值必须保持字符串语义
- **Given** 一条 `reversible` 规则修改 `/metadata/annotations/*`、`/metadata/labels/*` 或嵌套 `metadata` 下的同类路径
- **And** `pair.sourceValue` 或 `pair.targetValue` 看起来像数字、布尔值或 `null`
- **When** 系统编译规则并提交到 Velero
- **Then** 系统必须 (MUST) 保持该值按 string 写入目标字段
- **And** 提交期校验必须 (MUST) 拒绝会在 live 字段类型上造成显式类型错配的 pair 值
