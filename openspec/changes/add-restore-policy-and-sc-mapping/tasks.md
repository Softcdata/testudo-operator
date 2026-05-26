## 1. 规范与模型

- [x] 1.1 在 `DisasterInstance` 增加 `spec.restorePolicy` 规范与类型定义
- [x] 1.2 明确实例级恢复策略优先级（instance restorePolicy > built-in default）
- [x] 1.3 明确 `storageClassMapping` 规则结构、`unmatchedPolicy` 枚举与默认行为
- [x] 1.4 明确 `ingressClassMapping` 规则结构、`unmatchedPolicy` 枚举与默认行为
- [x] 1.5 在 `DisasterInstance` 增加 `spec.skipPodReadyCheck` 默认策略定义
- [x] 1.6 明确 `DisasterOperation` 就绪校验参数覆盖实例默认策略的优先级

## 2. CRD 与校验

- [x] 2.1 更新 `disasterinstances` CRD schema，包含 `restorePolicy` 各子字段
- [x] 2.2 更新 `disasterinstances` CRD schema，包含 `skipPodReadyCheck` 字段
- [x] 2.3 增加字段级校验（空值、重复映射、非法枚举、冲突配置）

## 3. 构建链路改造

- [x] 3.1 在恢复构建器中新增实例策略解析模块（统一入口）
- [x] 3.2 `ResourceSync` 创建 AppRestore 时接入实例策略（`template + modifiers`）
- [x] 3.3 `DataSync` 创建 AppRestore 时接入实例策略（`template + modifiers`）
- [x] 3.4 `DisasterOperation (drill)` 创建 AppRestore 时接入实例策略（`template + modifiers`）
- [x] 3.5 保持 DataSync trafficless 语义不被通用策略破坏
- [x] 3.6 Failover/Drill 就绪校验逻辑支持实例默认策略 + 操作级覆盖
- [x] 3.7 Group 操作创建子 `DisasterOperation` 时透传就绪校验参数

## 4. Class 映射执行与预检

- [x] 4.1 将 `storageClassMapping` 转译为 PVC/PV 维度的 `resourceModifierRules`
- [x] 4.2 将 `ingressClassMapping` 转译为 Ingress 维度的 `resourceModifierRules`
- [x] 4.3 增加目标集群 Class 预检逻辑（严格模式失败）
- [x] 4.4 增加未命中策略逻辑（`Keep` / `Fail`）
- [x] 4.5 输出标准化错误码（如 `StorageClassTargetNotFound`、`IngressClassTargetNotFound`、`ClassMappingInvalid`）

## 5. 可观测性

- [x] 5.1 在 AppRestore 状态或事件中记录策略来源层级（instance/default）
- [x] 5.2 记录 SC 映射命中/未命中统计
- [x] 5.3 记录 IngressClass 映射命中/未命中统计

## 6. 测试与验收

- [x] 6.1 单测：`restorePolicy` 合并逻辑（含默认值回退）
- [x] 6.2 单测：SC/IngressClass 映射规则转译与冲突校验
- [x] 6.3 单测：DataSync/ResourceSync/Drill 三条路径参数下传一致性
- [x] 6.4 集成测试：跨集群恢复场景下 Class 映射生效
- [x] 6.5 回归测试：既有 `DisasterInstance`（未配置 `restorePolicy`）恢复行为保持兼容
- [x] 6.6 单测：Group 父/子 Operation 就绪校验参数透传一致性
- [x] 6.7 回归测试：既有 `DisasterInstance`（未配置 `skipPodReadyCheck`）就绪校验行为保持兼容
- [x] 6.8 `openspec validate add-restore-policy-and-sc-mapping --strict`
