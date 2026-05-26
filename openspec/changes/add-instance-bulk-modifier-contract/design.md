# 设计：实例级批量资源修改快照契约

## 1. 设计目标
1. 让用户可以从实例层声明“批量替换值 / 批量删除 key”。
2. 保持 operator 只消费规则快照，不在运行时做批量扫描。
3. 让恢复构建链路可以审计本次规则是否来自批量动作。

## 2. 非目标
1. 不引入模板目录或模板绑定。
2. 不在 operator 中实现资源扫描器。
3. 不在 Phase 1 支持正则替换、子串替换、脚本替换。
4. 不在 Phase 1 支持列表元素级批量删除。
5. 不在 Phase 1 支持 `dataSync` 范围的 bulk action 展开。

## 3. 核心设计选择

### 3.1 采用“动作输入 + 快照执行”双层模型
`restorePolicy` 分成两层：

- 用户输入层：
  - `bulkModifierActions`
  - `modifierRules`
- 执行层：
  - `modifierRuleSnapshot`
  - `modifierRuleSnapshotHash`

语义：

- `bulkModifierActions` 表达实例级批量意图。
- `modifierRules` 表达用户手写的精确规则。
- `modifierRuleSnapshot` 是 server 已解析、已展开、可直接执行的最终规则集合。
- operator 只认执行层，不在运行时重做展开。

推荐字段形状如下：

```yaml
spec:
  restorePolicy:
    bulkModifierActions:
      - id: replace-db-ip
        action: replaceExactValue
        sourceValue: 10.10.0.12
        targetValue: 10.20.0.12
        applyTo: ["resourceSync"]
        directionPolicy: Auto
        enabled: true
      - id: remove-site-role
        action: removeKey
        key: testudo.softcdata.com/site-role
        applyTo: ["resourceSync"]
        directionPolicy: ForwardOnly
        enabled: true
    modifierRules: []
    modifierRuleSnapshot: []
    modifierRuleSnapshotHash: sha256:abcd1234
```

其中：

- `applyTo` 为空时，server 默认按 `["resourceSync"]` 处理
- `enabled` 为空时，server 默认按 `true` 处理
- `replaceExactValue.directionPolicy` 为空时默认 `Auto`
- `removeKey.directionPolicy` 为空时默认 `ForwardOnly`

执行契约中只认“有效批量动作”：

- `effectiveBulkModifierActions = bulkModifierActions` 中 `enabled != false` 的条目
- `enabled=false` 的条目只保留在用户输入层，不参与 snapshot 生成、失败关闭、摘要统计或 operator 的 snapshot 优先判断

### 3.2 operator 对 snapshot 优先消费
推荐执行顺序：

1. 先过滤出 `effectiveBulkModifierActions`。
2. 若 `effectiveBulkModifierActions` 非空且 `modifierRuleSnapshot` 非空，则使用它进入现有规则编译链。
3. 若 `effectiveBulkModifierActions` 为空，则回退使用 `modifierRules`。
4. 若 `effectiveBulkModifierActions` 非空但 snapshot 缺失，则失败关闭。

原因：

- 保持旧实例兼容。
- 避免 operator 同时承担“高层动作解释”和“底层规则编译”两套职责。

重要约束：

- 只有在 `effectiveBulkModifierActions` 非空时，snapshot 才能作为优先输入
- `modifierRuleSnapshot` 存在时，operator 不能把 `modifierRules` 再次 append 进统一编译链
- 否则会导致同一条手写规则执行两次

### 3.3 声明了批量动作但没有快照时必须失败关闭
若实例存在 `effectiveBulkModifierActions`，但：

- `modifierRuleSnapshot` 为空
- 或 `modifierRuleSnapshotHash` 缺失

operator 必须失败关闭。

原因：

- 这说明 server 未完成动作展开，继续执行会让实例语义不确定。

### 3.4 快照仍然必须对齐当前正式资源修改器 contract
`modifierRuleSnapshot` 必须继续只使用当前正式 contract：

- `reversible` 只允许 `pair.path`、`pair.sourceValue`、`pair.targetValue`
- `veleroNative` 继续使用透传 patch
- 禁止旧 `transform/map/template`

因此：

- 实例级批量动作只是“生成规则快照的高层入口”
- 不是新执行引擎

### 3.5 snapshot 必须已经编码好覆盖关系
现有统一编译器不是按“数组顺序”决胜，而是按 `priority`、`onConflict` 和 `ruleID` 决定冲突结果。

因此 operator 约束必须写死：

- bulk 生成规则的默认 `priority` 必须低于手写规则默认值
- 推荐 server 统一写成 `priority=-100`
- 手写规则保留当前语义，默认 `priority=0`

这样才能保证：

- 用户不写优先级时，手写精确规则能稳定覆盖 bulk 规则
- operator 不需要理解“这条规则是不是 batch 生成的”，只按现有引擎执行

## 4. 推荐逻辑模型

```yaml
spec:
  restorePolicy:
    bulkModifierActions:
      - id: replace-db-ip
        action: replaceExactValue
        sourceValue: 10.10.0.12
        targetValue: 10.20.0.12
      - id: remove-site-role
        action: removeKey
        key: testudo.softcdata.com/site-role
        directionPolicy: ForwardOnly
    modifierRules:
      - id: deployment-replicas-override
        ...
    modifierRuleSnapshot:
      - id: bulk-replace-db-ip-001
        ...
    modifierRuleSnapshotHash: sha256:abcd1234
```

说明：

- `bulkModifierActions` 是用户声明的批量动作。
- `modifierRules` 仍保留给精确、手写规则。
- `modifierRuleSnapshot` 是 server 写入的最终执行快照。
- `modifierRuleSnapshot` 中必须已经包含手写规则，不允许 operator 再二次合并 `modifierRules`

## 5. 恢复构建链路要求
`restore-builder` 生成 `AppRestore` 时：

1. 先过滤出 `effectiveBulkModifierActions`
2. 若 `effectiveBulkModifierActions` 非空，读取 `modifierRuleSnapshot`
3. 若 `effectiveBulkModifierActions` 为空，直接读取 `modifierRules`
4. 若存在 `effectiveBulkModifierActions`，同时写入审计摘要，例如：
   - `testudo.softcdata.com/modifier-source=bulkActions`
   - `testudo.softcdata.com/modifier-bulk-action-count=<已启用动作数>`
   - `testudo.softcdata.com/modifier-snapshot-hash`
5. 若 `bulkModifierActions` 全部为 `enabled=false`，不得写入 bulk 摘要，也不得因为残留 snapshot 切换到 snapshot 模式

注解键名应固定，不使用“任意摘要 key”，避免前端和排障脚本再做猜测。

## 6. 风险
1. 若 server 只写动作、不写快照，operator 会失败关闭。
2. 批量动作展开结果可能很多，需通过快照审计与哈希便于排障。
3. 若 server 没有把 bulk 规则降到更低优先级，手写规则不一定能稳定覆盖。
4. 若未来批量动作类型继续增加，仍需保持“动作层”和“执行层”分离，避免 operator 职责膨胀。

## 7. 验证策略
1. 类型 / CRD 单测：新增字段可被读写。
2. restore builder 单测：snapshot 优先于 `modifierRules`。
3. restore builder 单测：存在批量动作时写入来源摘要。
4. restore builder 单测：snapshot 存在时不会重复消费 `modifierRules`。
5. 回归测试：全部 `enabled=false` 时回退到 `modifierRules`，且不写 bulk 摘要。
6. 回归测试：没有 `bulkModifierActions` 的旧实例行为不变。
