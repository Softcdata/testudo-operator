## ADDED Requirements

### Requirement: RestorePolicy 必须支持批量修改动作与可执行快照双层模型
系统必须 (MUST) 在 `DisasterInstance.spec.restorePolicy` 中同时支持用户声明的批量修改动作和 server 生成的可执行规则快照。

#### Scenario: 实例保存已启用批量动作与快照
- **Given** 用户为一个 `DisasterInstance` 声明了至少一个 `enabled != false` 的实例级批量修改动作
- **When** Server 将该实例写入集群
- **Then** `restorePolicy` 必须包含 `bulkModifierActions`
- **And** 必须包含与之对应的 `modifierRuleSnapshot`
- **And** 必须包含 `modifierRuleSnapshotHash`
- **And** `modifierRuleSnapshot` 必须已经包含 bulk 生成规则与手写 `modifierRules`

#### Scenario: 全部 bulk 动作禁用时不进入快照契约
- **Given** 一个 `DisasterInstance` 的 `restorePolicy.bulkModifierActions` 全部为 `enabled=false`
- **When** Server 将该实例写入集群
- **Then** 系统必须将该实例视为“没有有效 bulk 动作”
- **And** operator 不得仅因这些已禁用动作而要求 `modifierRuleSnapshot` 或 `modifierRuleSnapshotHash`

### Requirement: 批量修改快照必须符合当前正式资源修改器 contract
系统必须 (MUST) 保证 `modifierRuleSnapshot` 继续只使用当前正式资源修改器 contract。

#### Scenario: replaceExactValue 展开为 pair-only reversible 规则
- **Given** 一个批量动作类型为 `replaceExactValue`
- **When** Server 将它展开为 `modifierRuleSnapshot`
- **Then** 展开的 `reversible` 规则必须 (MUST) 使用 `pair.path`、`pair.sourceValue`、`pair.targetValue`

#### Scenario: removeKey 展开为 veleroNative remove patch
- **Given** 一个批量动作类型为 `removeKey`
- **When** Server 将它展开为 `modifierRuleSnapshot`
- **Then** 展开的规则必须 (MUST) 使用当前正式 `veleroNative` remove patch 结构

#### Scenario: 批量快照不得生成旧 reversible 结构
- **Given** 一个批量动作被展开为 `modifierRuleSnapshot`
- **When** operator 读取该实例
- **Then** 快照不得 (MUST NOT) 包含旧 `transform/map/template` 结构

### Requirement: operator 只在存在已启用 bulkModifierActions 时优先消费 modifierRuleSnapshot
系统必须 (MUST) 只在存在至少一个 `enabled != false` 的 `bulkModifierAction` 时将 `modifierRuleSnapshot` 作为优先规则编译输入。

#### Scenario: snapshot 优先于手写 modifierRules
- **Given** 一个 `DisasterInstance` 的 `restorePolicy` 同时包含至少一个已启用 `bulkModifierAction`、`modifierRuleSnapshot` 和 `modifierRules`
- **When** operator 处理该实例
- **Then** 系统必须优先使用 `modifierRuleSnapshot`
- **And** 不得在运行时重新解释 `bulkModifierActions`

#### Scenario: snapshot 存在时不得重复消费手写规则
- **Given** 一个 `DisasterInstance` 的 `restorePolicy` 同时包含至少一个已启用 `bulkModifierAction`、`modifierRules` 和 `modifierRuleSnapshot`
- **When** operator 处理该实例
- **Then** operator 不得再将 `modifierRules` 追加进统一编译链
- **And** `modifierRuleSnapshot` 应被视为最终执行输入

#### Scenario: 没有已启用 bulk 动作时忽略旧 snapshot
- **Given** 一个 `DisasterInstance` 的 `restorePolicy` 不包含已启用 `bulkModifierActions`
- **And** 由于异常状态仍残留旧 `modifierRuleSnapshot`
- **When** operator 处理该实例
- **Then** operator 不得使用该旧 snapshot 作为优先输入
- **And** 必须回退到 `modifierRules`

### Requirement: 声明已启用批量动作但缺少快照时必须失败关闭
系统必须 (MUST) 在存在至少一个已启用 `bulkModifierAction` 但快照不完整时失败关闭。

#### Scenario: 缺少 modifierRuleSnapshot
- **Given** 一个 `DisasterInstance` 的 `restorePolicy` 包含至少一个已启用 `bulkModifierAction`
- **And** `modifierRuleSnapshot` 为空或缺失
- **When** operator 处理该实例
- **Then** operator 必须失败关闭

#### Scenario: 缺少 modifierRuleSnapshotHash
- **Given** 一个 `DisasterInstance` 的 `restorePolicy` 包含至少一个已启用 `bulkModifierAction`
- **And** 缺少 `modifierRuleSnapshotHash`
- **When** operator 处理该实例
- **Then** operator 必须失败关闭

#### Scenario: 全部 bulk 动作禁用时不因缺少快照失败
- **Given** 一个 `DisasterInstance` 的 `restorePolicy.bulkModifierActions` 全部为 `enabled=false`
- **And** `modifierRuleSnapshot` 与 `modifierRuleSnapshotHash` 为空
- **When** operator 处理该实例
- **Then** operator 不得因为缺少 snapshot / hash 失败关闭
