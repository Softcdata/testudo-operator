## 1. 模型
- [x] 1.1 为 `DisasterInstance.spec.restorePolicy` 增加 `bulkModifierActions`
- [x] 1.2 为 `DisasterInstance.spec.restorePolicy` 增加 `modifierRuleSnapshot`
- [x] 1.3 为 `DisasterInstance.spec.restorePolicy` 增加 `modifierRuleSnapshotHash`
- [x] 1.4 为批量动作字段补 CRD schema、默认值说明与 deepcopy

## 2. 构建链路
- [x] 2.1 在 restore builder 中优先消费 `modifierRuleSnapshot`
- [x] 2.2 缺少快照或快照哈希时对“存在已启用 `bulkModifierActions`”的实例失败关闭
- [x] 2.3 snapshot 存在时不得再二次追加 `modifierRules`
- [x] 2.4 在存在已启用批量动作时，于 `AppRestore` 注解或编译摘要中写入固定批量来源信息
- [x] 2.5 透传 `modifier-snapshot-hash`、仅统计已启用动作的 `modifier-bulk-action-count`

## 3. 兼容性
- [x] 3.1 无已启用 `bulkModifierActions` 的实例继续沿用 `modifierRules`
- [x] 3.2 与当前 `restore-modifier` pair-only / veleroNative 编译链保持兼容
- [x] 3.3 与现有 conflict / priority 语义保持一致，不在 operator 内新增新的覆盖规则

## 4. 验证
- [x] 4.1 CRD / 类型单测覆盖新增字段读写
- [x] 4.2 restore builder 单测覆盖 snapshot 优先消费
- [x] 4.3 restore builder 单测覆盖批量来源摘要透传
- [x] 4.4 restore builder 单测覆盖 snapshot 存在时不重复消费手写规则
- [x] 4.5 回归测试覆盖全部 `enabled=false` 时不进入 snapshot 模式
- [x] 4.6 回归验证旧 `modifierRules` 实例行为不受影响
