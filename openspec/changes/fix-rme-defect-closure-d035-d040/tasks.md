## 0. 前置与基线
- [x] 0.1 复核缺陷证据目录（r67/r68）并固化 case 到测试输入夹具。
- [x] 0.2 复核 `add-generic-reversible-resource-modifier-engine` 现有实现与本提案差异，明确仅修复偏差不扩展 Phase 2 能力。

## 1. P0 修复（D-038）
- [x] 1.1 修复 Failover PreCheck：modifier dry-run 失败必须失败关闭。
- [x] 1.2 修复 Reprotect PreCheck：modifier dry-run 失败必须失败关闭。
- [x] 1.3 增加保护断言：PreCheck 失败不得进入 `ScaleDownSource` 及后续破坏步骤。
- [x] 1.4 增加回归：失败场景下源副本数保持不变，操作终态不得为 `Completed`。

## 2. 提交期 fail-closed 修复（D-036/D-037）
- [x] 2.1 拒绝 `veleroNative` 缺失 `veleroRule`。
- [x] 2.2 拒绝非法 JSON Pointer path。
- [x] 2.3 拒绝数组越界 path。
- [x] 2.4 拒绝 `conditions` 零命中规则。
- [x] 2.5 拒绝治理禁区：`status/*`、`/metadata/finalizers`、用户规则 `/metadata/ownerReferences`。
- [x] 2.6 拒绝规则条数/复杂度超限输入。
- [x] 2.7 补齐 create/update admission 回归，确保非法规则不落库。

## 3. 执行实态一致性修复（D-039）
- [x] 3.1 增加关键字段实态核验（至少覆盖 PVC `spec.storageClassName`）。
- [x] 3.2 对“编译成功但实态未变化”返回失败并输出差异证据。
- [x] 3.3 补齐 forward/reverse 双向回归，禁止 no-op 伪成功。

## 4. PVC 初始化稳定性修复（D-035）
- [x] 4.1 修复路径生成与匹配逻辑，避免 `expected one matching path ... got 0`。
- [x] 4.2 将带 PVC 初始化场景纳入构建期/预检期回归。

## 5. 状态口径一致性修复（D-040）
- [x] 5.1 定义并固化 `status.fsmState` 到外部聚合态的映射契约。
- [x] 5.2 增加一致性回归，避免 `currentState` 与 `fsmState` 语义冲突。

## 6. 验证与交付
- [x] 6.1 `openspec validate fix-rme-defect-closure-d035-d040 --strict`。
- [x] 6.2 `go test ./internal/controller/restore ./internal/controller/disasteroperation ./internal/webhook/disasterinstance -count=1`。
- [x] 6.3 最小端到端回归（覆盖 C58~C61、C63~C67、C70~C72、C75 的等价场景）。
- [x] 6.4 更新 `docs/harness/ERROR_CENTER.tsv` 对应缺陷处置状态与证据链接。
