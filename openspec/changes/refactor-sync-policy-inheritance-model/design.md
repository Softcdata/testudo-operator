# Design: Sync Policy Inheritance Model

## 背景
当前用户已经明确：
- `syncPolicy` 可以是统一输入，但最终仍写回原来的双字段 CRD
- `DisasterInstance` 需要正式存储自己的策略 override
- 不要求策略修改后立即 fan-out，只要求后续 reconcile 能收敛

因此本设计不再把 `syncPolicy` 作为 operator 主字段，而是把它限定为 server/web 的输入抽象。

## 关键决策

### D1. `DisasterConfig` 保持双字段基线策略模型
- operator 继续以 `DisasterConfig.spec.dataSyncPolicy` / `spec.resourceSyncPolicy` 为基础配置的真实持久化来源。
- 本轮不在 operator 中新增统一 `syncPolicy` 主字段。

### D2. `DisasterInstance` 以双字段持久化 override
- 实例 override 必须作为 `DisasterInstance.spec` 的正式字段存在。
- 采用 `spec.dataSyncPolicy` / `spec.resourceSyncPolicy`，而不是统一 `syncPolicyOverride`。
- 两个字段按维度独立继承：未设置的字段继承 `DisasterConfig` 对应字段，已设置的字段只覆盖自身维度。
- 这样可以避免再引入一层 operator 内部兼容翻译，也避免把 override 误实现成“整组开关”。

### D3. operator 继承链固定为“实例优先，配置兜底”
- `DisasterInstance.spec.dataSyncPolicy` 优先于 `DisasterConfig.spec.dataSyncPolicy`
- `DisasterInstance.spec.resourceSyncPolicy` 优先于 `DisasterConfig.spec.resourceSyncPolicy`
- `DataSync` / `ResourceSync` 只消费继承链计算后的最终策略名和最终 schedule。

### D4. 首期采用最终一致，但必须绑定到实例周期性 reconcile
- 不强制新增 `DisasterPolicy` / `DisasterConfig` watch 来保证立刻更新。
- 实现必须把“基础配置/策略变化后的最终一致”绑定到 `DisasterInstance` 控制器的周期性 requeue / resync。
- 基础配置或引用策略变化后，在下一次实例周期性 reconcile 中必须重算有效策略并对齐 `DataSync/ResourceSync`。

### D5. schedule 清空/禁用也必须收敛
- 若实例 override 或基础配置变为空，或者引用策略被禁用，controller 必须最终把 `trigger.schedule` 清空。
- 触发该次重算的实例 reconcile 必须驱动调度清理闭环。
- 若对象未暂停但 schedule 被清空，调度器中的旧 cron job 也必须被移除。

## 继承链
1. `effectiveDataSyncPolicy = DisasterInstance.spec.dataSyncPolicy`；若为空则回退到 `DisasterConfig.spec.dataSyncPolicy`
2. `effectiveResourceSyncPolicy = DisasterInstance.spec.resourceSyncPolicy`；若为空则回退到 `DisasterConfig.spec.resourceSyncPolicy`
3. `DisasterPolicy.spec.schedule`
4. `DataSync/ResourceSync.spec.trigger.schedule`

## 触发模型
1. `DisasterInstance` 自身创建/更新时，实例控制器必须立即进入本对象 reconcile。
2. `DisasterConfig` 或引用 `DisasterPolicy` 变化时，不要求新增 fan-out watch，但必须依赖 `DisasterInstance` 控制器的周期性 requeue / resync 触发该实例的下一次重算。
3. 该次实例 reconcile 必须同时负责：
   - 重算字段级有效策略
   - 对齐 `DataSync/ResourceSync.spec.trigger.schedule`
   - 在有效 schedule 为空时清理陈旧 cron 任务

## 备选方案
- 方案 A：在 operator 中新增统一 `syncPolicy` 主字段
  - 放弃原因：与用户确认的“双字段 CRD 持久化”边界冲突。
- 方案 B：把实例 override 直接写到 `DataSync/ResourceSync`
  - 放弃原因：DS/RS 是派生资源，无法作为稳定的 desired state 来源，且会被实例控制器覆盖。
- 方案 C：首期同时引入立即 fan-out watch
  - 放弃原因：用户已明确“不要求立即更新”，先把来源落盘与最终收敛模型收敛清楚更重要。
