# Design: reversible DSL pair-only canonicalization

## 1. 设计目标

1. 把 `reversible` 的用户心智模型收敛到单一 `pair`。
2. 保持 `veleroNative` 完全不变。
3. 不改变编译结果、审计、dry-run、治理边界。
4. 明确旧 `map/template` 的直接拒绝口径，不引入兼容层。

## 2. 关键观察

### 2.1 `map` 在 Phase 1 已经是 pair

现实现中 `map` 被限制为“恰好一条映射”，并不会提供真正的多值字典语义。因此：

- `mapping: {sc-main: sc-dr}`
- 等价于
  - `sourceValue=sc-main`
  - `targetValue=sc-dr`

### 2.2 `template` 在 Phase 1 也是 pair

`template` 的区别不是“模式不同”，而是“source/target 两侧值不是手写字面量，而是通过受限上下文渲染得到”。

因此对用户来说，核心问题仍然是：

- source 侧这个字段应该是什么值？
- target 侧这个字段应该是什么值？

## 3. Canonical Schema

### 3.1 外层规则保持不变

```yaml
- id: rule-id
  mode: reversible
  enabled: true
  applyTo: ["resourceSync"]
  priority: 200
  conditions:
    groupResource: services
```

### 3.2 `reversible` 内层只保留 pair

```yaml
  pair:
    path: /spec/ports/0/nodePort
    sourceValue: "30080"
    targetValue: "32080"
```

字段定义：

1. `path`
   - JSON Pointer；
   - 继续沿用现有治理与路径校验；
   - 仍禁止 `/-`。
2. `sourceValue`
   - baseline source 侧应写入的值。
3. `targetValue`
   - baseline target 侧应写入的值。

## 4. 编译语义

1. Direction Resolver 语义不变。
2. `flow=forward` 时，编译器选择 `targetValue`。
3. `flow=reverse` 时，编译器选择 `sourceValue`。
4. 选中的值若包含受限 placeholder，则在固定上下文内渲染。
5. 渲染后产出普通 JSONPatch `add path=value`，与现有 reversible 输出保持一致。

## 5. Placeholder 规则

为替代独立 `template` mode，允许 pair 值内联使用受限 placeholder：

- `{{ .SourceCluster }}`
- `{{ .TargetCluster }}`
- `{{ .Flow }}`

约束：

1. 仅允许字符串模板，不引入函数、脚本或任意表达式。
2. placeholder 解析失败必须 fail-closed。
3. placeholder 是 pair 的值能力，不应再暴露为单独模式。

## 6. 旧写法拒绝 contract

### 6.1 `veleroNative`

完全不变，不参与本次 canonicalization。

### 6.2 旧 `map`

旧 `map` 写法不再进入 canonicalization。即使它在 Phase 1 语义上等价于单 entry pair，也不再作为合法输入保留。

约束：

1. 提交期直接拒绝 `map` 字段。
2. 编译期不得再做 `map -> pair` 隐式 normalizer。
3. 错误消息必须明确指向 pair-only canonical form。

### 6.3 旧 `template`

旧 `template` 写法同样直接非法。虽然它表达的仍是 source/target 两侧值，但对外不再保留独立模式。

约束：

1. 提交期直接拒绝 `template` 字段。
2. 编译期不得再做 `template -> pair` 隐式 normalizer。
3. 若用户需要动态字符串，必须改写为 pair 值中的受限 placeholder。

### 6.4 对外口径

1. 新文档、新示例、新配置入口一律只输出 pair。
2. `map/template` 只作为“非法旧写法”被记录，不再作为可接受模式出现。
3. 用户明确说明“没有历史数据”，因此本设计不预留兼容窗口。

## 7. 实施影响

### 7.1 Admission / Validation

1. 用户主路径只校验 pair。
2. 旧 `map/template` 在 schema、Admission 或业务校验阶段应直接失败关闭。
3. 返回错误时应明确指出“仅支持 pair canonical form”。

### 7.2 Compiler

1. `compileReversibleRule` 收敛为单分支：
   - validate pair input -> resolve flow -> select side -> render placeholder -> governance check。
2. 旧 `map/template` 的专用编译分支应被删除或改为直接报错，不再承担 alias normalizer 角色。

### 7.3 Documentation / UX

1. 示例 YAML、前端表单、Apipost/文档均只展示 pair。
2. 若需要展示旧写法，只能作为“非法示例 + 替代写法”出现。
3. 对外《自定义规则修改器说明书》必须成为用户侧单一说明入口，其内容只能保留 `pair` 与 `veleroNative` 两类模式。

## 8. 风险

1. 若 pair 值 placeholder 规则定义过宽，会重新长出“隐式 template mode”。
2. 若提交期、校验期、编译期对旧写法的拒绝口径不一致，会形成新的行为歧义。
3. 若 `sourceValue/targetValue` 没有明确绑定 baseline，而绑定成 forward/reverse，会与运行态角色变化混淆。

## 9. 结论

`veleroNative` 保持原样时，`reversible` 的最小且完整的主模型就是 `pair`。  
`map` 是单 entry pair 的特例，`template` 是 pair 两侧值的受限生成方式。  
因此应该把用户 contract 直接收敛到 `pair`，并把旧 `map/template` 视为非法输入，而不是继续并列成三种正式模式或保留兼容 alias。
