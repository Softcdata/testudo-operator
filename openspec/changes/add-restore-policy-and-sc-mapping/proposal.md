# Change: 补齐实例级恢复策略与 Class 映射

## Why

当前容灾主链路（`DisasterInstance -> DataSync/ResourceSync/Drill -> AppRestore`）在恢复参数方面仍有明显缺口：

1. 自动恢复路径大多仍使用固定构造，Velero 恢复参数缺少实例级细粒度控制。
2. 跨集群恢复中常见的 `StorageClass` 与 `IngressClass` 差异，缺少统一映射入口与失败策略。
3. 故障切换时 Pod 就绪校验策略仅能通过操作入参控制，缺少实例级默认策略，跨批次执行一致性较差。

该变更将恢复策略统一收敛到 **容灾实例配置**，覆盖应用恢复与演练恢复，确保“可配置、可校验、可审计、可复盘”。

## What Changes

### 1. 恢复策略仅在 DisasterInstance 配置

新增 `DisasterInstance.spec.restorePolicy` 作为唯一恢复策略入口（不再在 `DisasterConfig` 提供恢复策略字段），至少包含以下能力域：

- `resourceSelection`：恢复资源范围控制（include/exclude、标签过滤、是否包含集群级资源）。
- `execution`：恢复执行策略（`existingResourcePolicy`、`restorePVs`、超时等）。
- `storageClassMapping`：SC 映射策略（`mappings[]` + `unmatchedPolicy`）。
- `ingressClassMapping`：IngressClass 映射策略（`mappings[]` + `unmatchedPolicy`）。

### 2. 打通 AppRestore 构建链路

`DataSync`、`ResourceSync`、`DisasterOperation (drill)` 在构建 `AppRestoreSpec` 时，必须统一使用实例级恢复策略，并写入：

- `AppRestore.spec.template`（Velero RestoreSpec）。
- `AppRestore.spec.resourceModifierRules`（包含 SC/IngressClass 映射转译规则）。

策略优先级为：
`operation override (future) > instance restorePolicy > built-in default`。

本提案首期实现：`instance restorePolicy > built-in default`。

### 3. Class 映射行为标准化（SC + IngressClass）

支持在恢复前对以下字段进行映射：

- PVC/PV 的 `storageClassName`
- Ingress 的 `spec.ingressClassName`（必要时兼容旧注解）

统一能力要求：

- 映射规则来源：`restorePolicy.storageClassMapping` 与 `restorePolicy.ingressClassMapping`
- 支持按命名空间限定规则作用域
- 未命中策略：`Keep` / `Fail`
- 目标类校验：严格模式下，目标集群缺少目标 Class 必须提前失败并输出明确原因

### 4. 可观测性

恢复任务需记录策略来源与摘要（至少包含）：

- 来源层级（instance/default）
- SC 映射命中/未命中统计
- IngressClass 映射命中/未命中统计

### 5. 增加实例级就绪校验默认策略（Operation 可覆盖）

新增 `DisasterInstance.spec.skipPodReadyCheck` 作为实例级默认策略：

- `true`：跳过容器就绪验证（仅校验副本配置已下发）。
- `false`：执行容器就绪验证（检查 `readyReplicas`）。

`DisasterOperation` 入参可覆盖实例默认值，覆盖优先级为：
`DisasterOperation 输入 > DisasterInstance 默认策略`。

该策略用于 Failover/Drill 等扩容后就绪判定链路，保证批量与组操作下行为可预测。

## Non-Goals

- 本提案不重构 `DisasterJob` 历史链路。
- 本提案不引入新的多阶段恢复编排（如 two-phase restore 的独立编排变更）。
- 本提案不覆盖 Web/Server 全部交互细节，只定义 Operator 与 CRD 侧契约。

## Compatibility Commitment

- 本次修改为增量能力，不引入对现有 `DisasterInstance` 的破坏性变更。
- 对未配置 `spec.restorePolicy` 的实例，恢复行为必须保持与当前版本一致。
- 既有恢复链路（DataSync/ResourceSync/Drill -> AppRestore）在未使用新字段时，输出与执行语义不得变化。
- 对未配置 `spec.skipPodReadyCheck` 的实例，沿用当前就绪校验行为（由操作入参控制）。

## Impact

### Affected Specs

- `disaster-instance`（新增）
- `disaster-operation`（新增）
- `app-restore`（修改）
- `restore-builder`（修改）

### Affected Components

- `pkg/apis/disaster/v1/disasterinstance_types.go`
- `pkg/apis/disaster/v1/disasteroperation_types.go`
- `internal/controller/disasterinstance/*`
- `internal/controller/restore/builder.go`
- `internal/controller/datasync/*`
- `internal/controller/resourcesync/*`
- `internal/controller/disasteroperation/*`
- `internal/controller/apprestore/*`
- `config/crd/bases/*`

## Risks & Mitigations

1. 风险：实例策略字段增多导致误配概率上升。
   - 缓解：增加字段级校验、冲突检测与默认值策略。

2. 风险：Class 映射误配导致恢复异常。
   - 缓解：提供严格模式预检与标准化错误码（如 `StorageClassTargetNotFound`、`IngressClassTargetNotFound`）。

3. 风险：组操作未正确透传就绪校验参数，导致子操作行为与父操作不一致。
   - 缓解：统一透传并增加父子参数一致性回归测试。

## Rollout Plan

1. 先落地 `DisasterInstance.spec.restorePolicy` 的 CRD 与构建器接入（默认行为兼容）。
2. 再接入 SC/IngressClass 映射转译与严格预检。
3. 补齐单测、集成测试与回归验证后启用。
