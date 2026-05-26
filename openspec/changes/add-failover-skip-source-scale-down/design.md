# 设计文档：Failover 支持跳过源集群缩零

## 1. 现状分析

当前 `DisasterOperation` Failover 流程中，`ScaleDownSource` 是固定步骤。控制器执行路径大致为：
1. `PreCheck`
2. `PauseSchedules`
3. `FinalSync`
4. `ScaleDownSource`
5. `ScaleUpTarget`
6. `CheckReplicas`
7. `SwitchRoles`

已存在的可选行为仅有：
- `force`：源不可达时放宽前置校验。
- `skipFinalSync`：跳过 `FinalSync`。

缺口：无法在“源可达但业务要求保留源副本”的场景下单独跳过 `ScaleDownSource`。

## 2. 设计目标
1. 最小化变更范围，不破坏现有默认 Failover 语义。
2. 通过显式参数控制“跳过源缩零”行为。
3. 保证该行为具备可审计性（Event/Message）。
4. 单实例与组操作保持一致。

## 3. 方案设计

### 3.1 API / CRD 变更
在 `DisasterOperationSpec` 增加字段：

```go
// SkipScaleDownSource 是否跳过故障切换中的源集群缩零步骤
// 仅对 operationType=failover 生效
// +optional
SkipScaleDownSource bool `json:"skipScaleDownSource,omitempty"`
```

字段约束：
- 默认值：`false`
- 生效范围：`operationType=failover`
- 非 failover 操作传入该字段时忽略，不影响其他操作逻辑

### 3.2 Failover 执行逻辑
在 `ScaleDownSource` 执行点增加前置判断：
- 当 `operation.Spec.SkipScaleDownSource == true`：
  - 不执行源集群副本缩零和副本记录逻辑；
  - 记录 `ScaleDownSourceSkipped` 事件；
  - 记录可读消息（包含 `skipScaleDownSource=true`）；
  - 直接进入下一个步骤。
- 当参数缺省或为 `false`：
  - 保持当前缩零逻辑。

### 3.3 Group 子操作透传
`handleGroupOperation` 创建子 `DisasterOperation` 时新增字段透传：
- `SkipScaleDownSource: operation.Spec.SkipScaleDownSource`

保证：
- 用户在组级发起 failover 时，每个子实例与父操作采用一致语义。

### 3.4 与现有参数的关系
- 与 `skipFinalSync` 独立：
  - 可单独使用，也可组合使用。
- 与 `force` 并行：
  - `force` 主要控制源不可达容错；
  - `skipScaleDownSource` 主要控制是否执行缩零动作；
  - 二者互不替代。

## 4. 风险与缓解

### 4.1 风险：双活/脑裂
跳过源缩零会增加源目标同时运行的风险。

### 4.2 缓解策略
1. 参数默认关闭，必须显式开启。
2. 通过 Event + 状态消息做审计留痕。
3. 在 server API 文档与 UI 文案中标记高风险提示。

## 5. 测试设计（BDD）

### 5.1 Operator 场景
1. `skipScaleDownSource=true`：
   - Failover 执行到 `ScaleDownSource` 时应跳过缩零；
   - 后续 `ScaleUpTarget`、`SwitchRoles` 继续执行。
2. `skipScaleDownSource=false` 或未传：
   - 保持现有缩零行为。
3. Group failover：
   - 父操作开启参数后，子 `DisasterOperation` 应携带该字段。
4. 非 failover 操作：
   - 字段不影响原有行为。

### 5.2 Server 场景（联动）
1. 实例 action 接口接收并透传 `skipScaleDownSource`。
2. 组 action 接口接收并透传 `skipScaleDownSource`。
3. 缺省时不写入或写入 `false`，与当前兼容。

## 6. 兼容性说明
- 向后兼容：旧请求不传该字段时行为不变。
- 仅新增可选能力，不改变现有 API 必填项。
