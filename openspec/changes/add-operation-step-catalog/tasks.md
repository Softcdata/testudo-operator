## 1. Proposal
- [ ] 1.1 盘点所有容灾操作类型与当前真实步骤集合
- [ ] 1.2 明确静态目录、运行期状态、事件时间线的边界
- [ ] 1.3 明确 P0-P4 分期与跨仓职责

## 2. Catalog
- [ ] 2.1 新增 `docs/harness/operation-visibility-catalog.md`
- [ ] 2.2 在目录中标明每种操作的真相字段与当前 server 暴露现状
- [ ] 2.3 明确 Drill 双层状态语义

## 3. Companion Alignment
- [ ] 3.1 与 server 的 `add-operation-detail-view-api` 对齐 `operationName`、`currentStep`、`steps[]`、`autoCancel*`
- [ ] 3.2 把 P4 对 durable history 的依赖写明

## 4. Verification
- [ ] 4.1 `openspec validate add-operation-step-catalog --strict`
