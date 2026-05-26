# Change: 为实例恢复策略增加批量资源修改快照契约

## Why
用户当前真正需要的不是“模板目录 + 模板绑定”，而是“对整个容灾实例做批量修改”：

- 把实例保护范围内出现的某个 IP 统一替换为另一个 IP。
- 把实例保护范围内某个 key 统一删除，而不是逐个资源、逐条路径手写规则。

现有 `modifierRules` 仍然适合精确控制单条路径，但它要求用户逐资源、逐字段编写 DSL，心智成本高，不适合这种“整实例批量替换 / 批量删除”的产品诉求。

因此本 proposal 放弃模板方向，改为引入“实例级批量修改动作 + 生成后的规则快照”契约：

- 用户声明高层批量动作。
- server 负责扫描实例保护范围内的资源并展开为具体 `modifierRuleSnapshot`。
- operator 继续只消费规则快照，不在运行时做批量扫描。

## What Changes

### 1. 为 `DisasterInstance.spec.restorePolicy` 增加批量修改动作与快照字段
`restorePolicy` 增加以下正式字段：

- `bulkModifierActions`：用户声明的实例级批量修改动作。
- `modifierRuleSnapshot`：server 生成的可执行规则快照。
- `modifierRuleSnapshotHash`：快照哈希，用于审计与失败关闭。

其中：

- `modifierRules` 继续表示用户显式编写的精确规则。
- `modifierRuleSnapshot` 表示最终给 operator 执行的完整规则集合。
- `enabled` 省略时按 `true` 处理，只有已启用动作才参与执行契约。
- 当存在至少一个已启用 `bulkModifierAction` 时，`modifierRuleSnapshot` 必须已经包含：
  - bulk 动作生成的规则
  - 用户手写 `modifierRules`
- 当 `bulkModifierActions` 全部为 `enabled=false` 时，系统按“没有有效 bulk 动作”处理，不要求 snapshot / hash，也不写 bulk 审计摘要。

### 2. Phase 1 只支持两类批量动作
Phase 1 批量动作只覆盖最直接、最稳妥的两类能力：

- `replaceExactValue`
  - 在实例保护范围内扫描字符串叶子节点。
  - 命中值必须与 `sourceValue` 精确相等。
  - 展开结果必须使用当前正式 `reversible pair-only` 结构。
- `removeKey`
  - 在实例保护范围内扫描对象 / map 成员键。
  - 命中键必须与声明的 `key` 精确相等。
  - 展开结果必须使用当前正式 `veleroNative` remove patch。

Phase 1 额外约束：

- `bulkModifierActions.applyTo` 只允许 `resourceSync`、`drill`
- 省略 `applyTo` 时默认按 `resourceSync` 处理
- 不支持 `dataSync`

### 3. operator 只在存在已启用动作时消费快照，不运行时扫描动作
operator 不负责解释 `bulkModifierActions`，也不在运行时扫描模板或资源。

恢复构建链路只消费：

1. 当至少存在一个已启用 `bulkModifierAction` 时，消费 `modifierRuleSnapshot`。
2. 当没有已启用 `bulkModifierAction` 时，回退到现有 `modifierRules`，保持旧实例兼容。

也就是说：

- `modifierRuleSnapshot` 存在时，operator 不得再把 `modifierRules` 追加进执行链路
- snapshot 已经是 server 计算好的最终执行输入

### 4. 声明了已启用批量动作但缺少快照时必须失败关闭
如果实例存在至少一个已启用 `bulkModifierAction`，但缺少：

- `modifierRuleSnapshot`
- 或 `modifierRuleSnapshotHash`

operator 必须失败关闭，不允许在“动作存在但未展开成快照”的情况下继续恢复构建。

### 5. 恢复构建链路增加批量来源审计摘要
`restore-builder` 在生成 `AppRestore` 时，需要透传批量修改摘要，至少包括：

- `testudo.softcdata.com/modifier-source=bulkActions`
- `testudo.softcdata.com/modifier-bulk-action-count=<已启用动作数>`
- `testudo.softcdata.com/modifier-snapshot-hash=<sha256:...>`

只有在存在已启用 `bulkModifierAction` 时才写入这些摘要；全部禁用时视为无 bulk 来源。

## Non-Goals

- 不再做模板目录、模板 CRUD、模板绑定。
- 不让 operator 在运行时扫描资源并动态生成规则。
- 不在 Phase 1 支持正则替换、子串替换、脚本替换。
- 不在 Phase 1 支持按列表元素名称批量删除。
- 不改变现有 `modifierRules` / `veleroNative` / `pair-only` 的执行语义。

## Impact
- Affected specs:
  - `restore-modifier`
  - `restore-builder`
- Affected code:
  - `pkg/apis/disaster/v1/disasterinstance_types.go`
  - `internal/controller/restore/*`
  - `internal/controller/datasync/*`
  - `internal/controller/resourcesync/*`
  - `internal/controller/disasteroperation/*`
- Cross-repo impact:
  - `disaster-server`：提供 `bulkModifierActions` API、资源扫描与快照生成
  - `cluster-disaster-web`：实例级批量替换 / 删除 UI

## Relationship to Existing Changes
- 建立在当前正式 `restore-modifier` pair-only contract 之上。
- 替代已删除的模板方向 proposal，不再沿用模板绑定模型。
- server 侧需要保证 bulk 生成规则默认优先级低于手写规则默认优先级，从而让精确规则可以稳定覆盖 batch 规则。
