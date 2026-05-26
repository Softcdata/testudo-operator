# Tasks

## 1. Proposal
- [x] 1.1 收敛 operator 边界：不新增 `syncPolicy` 主字段，继续使用旧双字段落盘
- [x] 1.2 收敛实例 override 的字段级继承规则与周期性 reconcile 触发语义

## 2. Operator
- [x] 2.1 为 `DisasterInstance` 增加 `dataSyncPolicy` / `resourceSyncPolicy` override 字段
- [x] 2.2 在实例控制器中实现“按字段实例优先、按字段配置兜底”的 DataSync/ResourceSync 策略继承链
- [x] 2.3 绑定到 `DisasterInstance` 周期性 requeue / resync，确保基础配置或策略变化能在下一次实例 reconcile 中收敛
- [x] 2.4 补齐 schedule 清空/禁用后的 steady-state 收敛与 scheduler 清理
- [x] 2.5 补继承、单字段覆盖、双字段覆盖、同名策略 cron 变更、清空、禁用场景的 controller tests

## 3. Server / Web / Chart Alignment
- [x] 3.1 与 server 对齐统一 `syncPolicy` 输入到旧双字段的落盘契约
- [ ] 3.2 与 web 对齐基础配置与实例表单的统一输入和兼容回显
- [ ] 3.3 发布 `DisasterInstance` CRD 变更到 chart

## 4. Verification
- [x] 4.1 `openspec validate refactor-sync-policy-inheritance-model --strict`
