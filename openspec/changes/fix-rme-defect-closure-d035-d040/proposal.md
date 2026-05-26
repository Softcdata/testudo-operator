# Change: 修复可逆修改器引擎关键缺陷（D-035~D-040）

Depends on:

1. `add-generic-reversible-resource-modifier-engine`（已完成首轮实现）

## Why

`add-generic-reversible-resource-modifier-engine` 已完成首批落地，但最新回归（C58~C76）暴露出 6 个关键缺陷：

1. `D-038 (P0)`：PreCheck fail-closed 失效，dry-run 失败后仍进入破坏步骤（已观测到 `ScaleDownSource` 执行）。
2. `D-037 (P1)`：治理/复杂度提交期校验缺失，高危规则在提交期被 200 接受并落库。
3. `D-036 (P1)`：基础提交期校验缺失，非法规则在提交期被 200 接受并落库。
4. `D-039 (P1)`：SC 映射“编译成功”但“资源实态未变化”，存在成功假象。
5. `D-040 (P2)`：实例状态口径不一致（`currentState` 与 `status.fsmState` 不一致），导致外部轮询误判。
6. `D-035 (P1)`：带 PVC 场景初始化出现 `PodVolumeRestoreFailed`（`expected one matching path ... got 0`）。

其中 `D-038` 直接影响生产安全边界，必须先修。

## What Changes

本提案聚焦“失败关闭一致性 + 实态一致性 + 状态口径一致性”三条主线。

1. 修复执行期 fail-closed（D-038）：
   - Failover/Reprotect 的 modifier dry-run 一旦失败，必须在 `PreCheck` 终止；
   - 禁止进入 `ScaleDownSource`、`ScaleUpTarget`、`SwitchRoles`；
   - 操作终态必须为失败，不得 `Completed`。

2. 补齐提交期 fail-closed（D-036/D-037）：
   - 在 `DisasterInstance` create/update 提交期拒绝以下输入（4xx）：
     - `veleroNative` 缺失 `veleroRule`；
     - 非法 `path` / 数组越界 path；
     - `conditions` 零命中；
     - 命中治理禁区：`status/*`、`/metadata/finalizers`、用户规则 `/metadata/ownerReferences`；
     - 规则条数/复杂度超限。

3. 增加执行后实态核验（D-039）：
   - 对 `storageClassName` 等关键字段，除“已生成 patch”外，还需核验目标资源实态发生预期变化；
   - 若命中资源仍无变化，任务必须失败并返回可定位证据（资源、路径、预期值、实际值）。

4. 修复 PVC 初始化路径匹配问题（D-035）：
   - 收敛 path 生成与定位逻辑，避免运行期出现 `expected one matching path ... got 0`；
   - 带 PVC 场景需在构建期/预检期暴露问题，而非在恢复执行深水区失败。

5. 收敛状态口径契约（D-040）：
   - 明确 `status.fsmState` 到外部聚合态（`currentState`）的映射规则；
   - 增加一致性回归，确保同一时刻不会出现“运行中 + 受保护态”冲突语义。

## Scope

1. `openspec/changes/fix-rme-defect-closure-d035-d040/*`
2. `internal/controller/disasteroperation/*`（PreCheck fail-closed）
3. `internal/controller/restore/*`（提交期校验、路径定位、实态核验、PVC 路径匹配）
4. `internal/webhook/disasterinstance/*`（提交期拒绝）
5. 必要的状态映射契约与回归用例（operator 侧）

## Non-Goals

1. 不引入新的 CRD/规则 CR。
2. 不实现 Phase 2 能力（Profile 复用、merge/strategic patch、可视化模拟 API）。
3. 不改变既有 failover 主流程顺序（仍为 `FinalSync -> ScaleDownSource`）。

## Acceptance Criteria

1. C70/C71/C72 类场景下，dry-run 失败时操作必须停在 `PreCheck`，源副本数不变。
2. C58/C59/C60/C61/C63/C64/C65/C67 类非法输入在提交期必须被 4xx 拒绝，不得入库。
3. C75 类场景中，SC 映射必须“编译成功且实态变化成功”；否则任务失败并给出差异证据。
4. PVC 初始化路径匹配错误不得在执行深水区首次暴露。
5. 状态口径一致性检查通过，不再出现 `currentState` 与 `fsmState` 冲突语义。

## Risk

1. 提交期校验增强会提高 admission 复杂度与时延。
2. 实态核验引入额外读取与等待逻辑，需控制超时和重试预算。
3. 状态口径收敛涉及上下游消费方，需保持兼容过渡。

## Impact

1. 生产安全：优先消除错误规则导致缩容/切换的破坏性风险。
2. 质量前移：高危/非法规则在提交期即阻断。
3. 可观测性：从“patch 已生成”升级为“实态已生效”的可证明执行。
