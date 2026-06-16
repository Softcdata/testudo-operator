# Change: 修复 bulk modifier 扫描禁止路径导致实例保存失败

## Why
实例级 `bulkModifierActions.replaceExactValue` 会扫描实例保护范围内资源的所有字符串叶子节点，并展开为 `modifierRuleSnapshot`。当用户替换容器镜像完整值时，同一个镜像值可能同时出现在：

- `Deployment.spec.template.spec.containers[*].image`
- `StatefulSet.spec.template.spec.containers[*].image`
- `Pod.spec.containers[*].image`
- `Pod.status.containerStatuses[*].image`

其中 `/status/**` 是资源修改器禁止修改的受保护路径。当前 server 在 bulk 扫描阶段没有过滤这些路径，导致它先生成 `/status/containerStatuses/0/image` 规则，再在规则校验阶段失败：

```text
ModifierRuleRejected: patch path /status/containerStatuses/0/image is forbidden
```

这会让合法的 spec 字段替换也无法保存。临时通过 `resourceSelection.excludedResources=["pods"]` 可以绕开 Pod status，但该字段会影响最终恢复范围，不应作为永久修复。

## What Changes

### 1. bulk 快照生成阶段跳过受保护路径
server 在生成 `modifierRuleSnapshot` 时，必须在扫描阶段跳过现有资源修改器治理禁止的路径：

- `/status`
- `/status/**`
- `/metadata/finalizers`
- `/metadata/finalizers/**`
- `/metadata/ownerReferences`
- `/metadata/ownerReferences/**`

跳过应发生在 match 生成之前，禁止路径不得进入 `modifierRuleSnapshot`。

### 2. replaceExactValue 和 removeKey 使用同一套路径过滤
过滤逻辑必须同时覆盖：

- `replaceExactValue` 字符串叶子节点匹配
- `removeKey` 对象键匹配

这样可以避免任何 bulk 动作生成后续必然被 operator/server 校验拒绝的规则。

### 3. 不要求用户通过 resourceSelection 绕过缺陷
本修复不得要求用户为镜像替换配置：

```json
{"resourceSelection":{"excludedResources":["pods"]}}
```

`resourceSelection` 继续只表达恢复资源范围和 bulk 扫描范围的用户选择，不作为受保护路径治理的替代方案。

### 4. 保留最终规则校验作为防线
现有 `modifierRules` / `modifierRuleSnapshot` 提交校验仍必须拒绝禁止路径。本变更只是让 bulk 生成器提前过滤，不放松任何治理规则。

## Impact
- Affected specs:
  - `restore-modifier`
- Affected code:
  - `/home/chenxi/YS/disaster-server/internal/apis/disaster_instance/v1/bulk_modifier_snapshot.go`
  - `/home/chenxi/YS/disaster-server/internal/apis/disaster_instance/v1/bulk_modifier_snapshot_test.go`
  - 如需复用校验 helper，可影响 `/home/chenxi/YS/disaster-server/internal/apis/disaster_instance/v1/modifier_rule_validation.go`
- Cross-repo impact:
  - 主要实现位于 `disaster-server`
  - `disaster-operator` 仅维护 OpenSpec 提案和现有校验语义，不需要为该缺陷放松 operator 校验

## Non-Goals
- 不新增镜像前缀替换能力。
- 不改变 `replaceExactValue` 的完整值匹配语义。
- 不改变 `resourceSelection` 的恢复范围语义。
- 不默认排除 `pods` 资源。
- 不允许修改 `/status/**`、`finalizers`、`ownerReferences`。
- 不改变 operator 对 `modifierRuleSnapshot` 的优先消费和失败关闭逻辑。

## Risks
- 如果用户的 bulk action 只命中受保护路径，过滤后会变成零可执行命中，仍会失败。
- 如果过滤逻辑与最终校验禁止路径清单漂移，可能再次出现扫描生成后被校验拒绝的问题。

## Mitigation
- 将扫描阶段的禁止路径判断与规则校验语义保持一致，优先复用 helper 或在测试中覆盖同一清单。
- 补充回归测试：同一镜像同时出现在 Deployment spec 与 Pod status 时，只生成 Deployment spec 规则。
- 保留最终校验作为兜底，避免手写规则或异常快照绕过保护。
