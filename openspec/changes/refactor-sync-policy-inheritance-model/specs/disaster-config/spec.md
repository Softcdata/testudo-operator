## ADDED Requirements

### Requirement: DisasterConfig 必须继续作为双字段基线策略来源
系统必须 (MUST) 继续使用 `DisasterConfig.spec.dataSyncPolicy` 与 `DisasterConfig.spec.resourceSyncPolicy` 作为 operator 的基础配置策略来源。

#### Scenario: 上游统一输入被 fan-out 后写入基础配置
- **Given** 上游兼容层已将统一 `syncPolicy` 输入拆分为数据同步和资源同步策略值
- **When** operator 读取该 `DisasterConfig`
- **Then** operator 必须只依赖 `spec.dataSyncPolicy` 与 `spec.resourceSyncPolicy`
- **And** 不得要求额外存在新的 `syncPolicy` CRD 字段

### Requirement: 无实例 override 时基础配置变更必须通过实例周期性 reconcile 最终收敛
系统必须 (MUST) 在实例未配置 override 时，将基础配置策略的改动通过 `DisasterInstance` 控制器的下一次周期性 reconcile 最终收敛到现有 `DataSync` 与 `ResourceSync`。

#### Scenario: 基础配置策略变更后实例在下一次周期性 reconcile 继续继承
- **Given** 一个 `DisasterInstance` 未设置实例级同步策略 override
- **And** 其关联 `DisasterConfig` 的策略字段已发生变更
- **And** operator 首期未新增 `DisasterConfig` 或 `DisasterPolicy` 的事件 fan-out watch
- **When** `DisasterInstance` 控制器对该实例执行下一次周期性 reconcile
- **Then** `DataSync` 与 `ResourceSync` 必须最终收敛到新的基础配置策略

#### Scenario: 同名引用策略的 schedule 变化后实例在下一次周期性 reconcile 刷新调度
- **Given** 一个 `DisasterInstance` 未设置实例级同步策略 override
- **And** 其关联 `DisasterConfig` 仍引用同一个 `dataSyncPolicy` 或 `resourceSyncPolicy` 名称
- **And** 被引用 `DisasterPolicy.spec.schedule` 已发生变更，但策略名未变化
- **And** operator 首期未新增 `DisasterConfig` 或 `DisasterPolicy` 的事件 fan-out watch
- **When** `DisasterInstance` 控制器对该实例执行下一次周期性 reconcile
- **Then** 该实例关联的 `DataSync` 或 `ResourceSync` 必须最终刷新为新的 `trigger.schedule`
