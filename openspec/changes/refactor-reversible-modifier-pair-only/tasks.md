## 1. 合同与模型
- [x] 1.1 将 `reversible` 的用户主模型收敛为 canonical `pair`，明确 `path/sourceValue/targetValue` 语义。
- [x] 1.2 明确 `sourceValue/targetValue` 绑定 baseline `source/target`，而不是绑定 `forward/reverse` 字面。
- [x] 1.3 明确 `veleroNative` 完全不变，不进入本次 DSL 收敛范围。

## 2. 非法输入与拒绝口径
- [x] 2.1 定义旧 `map` 为非法输入，不提供 alias、归一化或兼容窗口。
- [x] 2.2 定义旧 `template` 为非法输入，要求改写为 pair + placeholder。
- [x] 2.3 定义提交期、校验期、编译期一致的 fail-closed 错误口径。
- [x] 2.4 明确文档、示例、表单与 API 描述均只暴露 pair-only contract。

## 3. 编译与校验
- [x] 3.1 将 `compileReversibleRule` 收敛为 pair-only canonical branch。
- [x] 3.2 将 placeholder 能力收敛到 pair 值内部，而不是保留独立 `template` mode。
- [x] 3.3 保持方向解析、冲突决议、治理规则、审计输出与现有语义一致。

## 4. 文档与示例
- [x] 4.1 更新 proposal/design/spec 示例，全部只使用 pair。
- [x] 4.2 更新前端/API 文档，移除 `map/template` 作为正式用户引导。
- [x] 4.3 更新或新增对外《自定义规则修改器说明书》，只保留 `pair` 与 `veleroNative` 两类正式模式。
- [x] 4.4 为旧写法补充“非法示例 -> pair 替代写法”的说明。

## 5. 验证
- [x] 5.1 `openspec validate refactor-reversible-modifier-pair-only --strict`
- [x] 5.2 验证等价 pair 规则的编译产物与目标语义一致。
- [x] 5.3 验证旧 `map/template` 在提交期、校验期、编译期都被一致拒绝。
