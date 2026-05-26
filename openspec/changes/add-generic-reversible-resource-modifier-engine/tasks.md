## 0. 前置依赖
- [x] 0.1 确认 `add-restore-policy-and-sc-mapping` 已合入并可用（字段与行为基线稳定）。
- [x] 0.2 对齐 `openspec/project.md` 与 V2 operation spec 的 Failover 顺序文档描述差异。

## 1. 设计与模型
- [x] 1.1 定义双层规则 DSL（`veleroNative` 透传 + `reversible` 变换），统一 `conditions/directionPolicy/priority`。
- [x] 1.2 固定 Phase 1 配置入口：`DisasterInstance.spec.restorePolicy`（不引入 Profile 规则源）。
- [x] 1.3 定义方向上下文模型（baseline source/target、runtime source/target、flow=forward|reverse、directionSource）。
- [x] 1.4 定义规则编译产物与审计摘要模型（编译命中、跳过、拒绝原因）。

## 2. 规则编译器与方向引擎
- [x] 2.1 在 `restore` 模块新增 Direction Resolver（纯函数 + 单测）。
- [x] 2.2 明确 Direction Resolver 边界：status 双空回退 forward（baselineFallback），单侧为空直接失败。
- [x] 2.3 实现可逆变换编译器（`map`、`template`、`pair`）。
- [x] 2.4 增加不可逆规则检测与错误码（`ModifierRuleNotReversible`、`ModifierRuleConflict`）。
- [x] 2.5 支持 `veleroNative` 规则透传（不重写 matcher），并执行治理/冲突检查（Phase 1 仅 `patches`）。
- [x] 2.6 实现 `directionPolicy` 默认值/门控语义（`Auto`、`ForwardOnly`、`ReverseOnly`）。
- [x] 2.7 支持系统规则与用户规则合并（`system-protect` wins，高优先级 wins，冲突可解释）。
- [x] 2.8 实现冲突键：`(normalizedConditions, operation, path)`。
- [x] 2.9 禁止 `reversible` 规则写入 `/-` 追加路径（失败关闭）。

## 3. 控制器接入
- [x] 3.1 `DataSync` 构建 AppRestore 时接入统一编译入口。
- [x] 3.2 `ResourceSync` 构建 AppRestore 时接入统一编译入口。
- [x] 3.3 `DisasterOperation (drill/reprotect 相关恢复路径)` 接入统一编译入口。
- [x] 3.4 Failover/Reprotect 的 PreCheck 阶段执行 modifier compile dry-run，失败提前阻断。
- [x] 3.5 增加灰度开关 `UseUnifiedDirectionResolver` 并兼容旧逻辑回滚。
- [x] 3.6 定义 gate=false 时 DSL 规则失败关闭（`ModifierFeatureDisabled`），避免静默不生效。
- [x] 3.7 增加提交期服务端校验入口（优先 Admission Webhook，可选 Validate API），在 `DisasterInstance` create/update 时执行规则校验。
- [x] 3.8 保持执行期 PreCheck dry-run 作为二道防线，提交期通过不等于运行期可跳过。

## 4. 安全治理
- [x] 4.1 增加 patch 路径白名单与黑名单校验（拒绝 `status/*`、`finalizers`、`ownerReferences` 等）。
- [x] 4.2 增加系统内建 trafficless 例外：允许 `pods` 上 `remove /metadata/ownerReferences`。
- [x] 4.3 增加规则条数/路径深度/正则复杂度限额。
- [x] 4.4 增加审计输出（AppRestore 注解 + 结构化事件，包含 `modifier-flow` 与 `modifier-direction-source`）。
- [x] 4.5 增加 JSON Pointer 路径定位校验（含 `~1/~0` 转义与数组下标）。
- [x] 4.6 增加提交期对象定位校验（conditions 零命中、path 不存在、数组越界必须拒绝）。

## 5. 兼容与迁移
- [x] 5.1 保持 `storageClassMapping/ingressClassMapping` 兼容路径。
- [x] 5.2 支持导入/引用现有 Velero `resourceModifierRules` 为 `veleroNative` 规则。
- [x] 5.3 统一方向启用时替代旧 `strictTargetValidation` 失败后反向兜底逻辑（开关保护）。
- [x] 5.4 提供旧配置迁移为 DSL 的转换器（离线工具或服务端转换）。
- [x] 5.5 提供开关控制（灰度启用通用可逆引擎）。

## 6. 测试
- [x] 6.1 单测：方向判定 forward/reverse。
- [x] 6.2 单测：`status` 双空时回退 forward，`directionSource=baselineFallback`。
- [x] 6.3 单测：`status` 单侧为空时返回 `DirectionResolveFailed`。
- [x] 6.4 单测：SC/Ingress/env 三类规则在 forward/reverse 产物正确。
- [x] 6.5 单测：`veleroNative` 规则条件与 patch 透传结果与输入一致（`patches` 类型）。
- [x] 6.6 单测：`veleroNative + ForwardOnly` 在 reverse 必须跳过。
- [x] 6.7 单测：`reversible` 规则命中 `/-` 路径必须拒绝。
- [x] 6.8 单测：trafficless 系统规则可合法移除 `pods.ownerReferences`，用户规则同路径必须拒绝。
- [x] 6.9 单测：`system-protect` 与用户规则冲突时必须确定性覆盖（不报错失败）。
- [x] 6.10 单测：同优先级同冲突键不同值按 `onConflict` 生效。
- [x] 6.11 回归：`UseUnifiedDirectionResolver=false` 时保持旧 `strictTargetValidation` 兜底行为。
- [x] 6.12 回归：`UseUnifiedDirectionResolver=false` + 配置 DSL 时必须返回 `ModifierFeatureDisabled`（不静默忽略）。
- [x] 6.13 回归：`UseUnifiedDirectionResolver=true` 时 DSL 在 DataSync/ResourceSync/Drill 全链路生效。
- [x] 6.14 回归：`UseUnifiedDirectionResolver=false` + 仅 legacy SC/Ingress 映射时不得误判为 `ModifierFeatureDisabled`。
- [x] 6.15 回归：`UseUnifiedDirectionResolver=false` + legacy-only + runtime 状态不完整时不得触发 `DirectionResolveFailed`。
- [x] 6.16 集成：DisasterOperation PreCheck dry-run 失败时必须在 `ScaleDownSource` 前终止。
- [x] 6.17 集成：DataSync/ResourceSync/Drill 三链路产物一致。
- [x] 6.18 回归：现有 restorePolicy（无 DSL）行为不变。
- [x] 6.19 集成：`DisasterInstance` 提交时 path 不存在必须被 Admission/Validate API 拒绝。
- [x] 6.20 集成：数组路径下标越界在提交阶段必须被拒绝。
- [x] 6.21 集成：conditions 零命中在提交阶段必须被拒绝并返回可定位错误信息。
