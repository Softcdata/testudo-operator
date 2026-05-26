## ADDED Requirements

### Requirement: DisasterInstance 必须以双字段持久化同步策略 override
系统必须 (MUST) 允许 `DisasterInstance` 通过 `spec.dataSyncPolicy` 与 `spec.resourceSyncPolicy` 显式持久化实例级同步策略 override。

#### Scenario: 实例关闭 override 时继承基础配置
- **Given** 一个 `DisasterInstance` 的 `spec.dataSyncPolicy` 和 `spec.resourceSyncPolicy` 为空
- **When** operator 计算该实例的 DataSync/ResourceSync 策略来源
- **Then** operator 必须继承关联 `DisasterConfig` 的默认同步策略

#### Scenario: 仅覆盖数据同步策略时资源策略继续继承基础配置
- **Given** 一个 `DisasterInstance` 已设置 `spec.dataSyncPolicy`
- **And** 其 `spec.resourceSyncPolicy` 为空
- **When** operator 计算该实例的 DataSync/ResourceSync 策略来源
- **Then** operator 必须对 `DataSync` 使用实例级 `spec.dataSyncPolicy`
- **And** 必须对 `ResourceSync` 继续继承 `DisasterConfig.spec.resourceSyncPolicy`

#### Scenario: 仅覆盖资源同步策略时数据策略继续继承基础配置
- **Given** 一个 `DisasterInstance` 已设置 `spec.resourceSyncPolicy`
- **And** 其 `spec.dataSyncPolicy` 为空
- **When** operator 计算该实例的 DataSync/ResourceSync 策略来源
- **Then** operator 必须对 `ResourceSync` 使用实例级 `spec.resourceSyncPolicy`
- **And** 必须对 `DataSync` 继续继承 `DisasterConfig.spec.dataSyncPolicy`

#### Scenario: 实例同时设置双字段 override 时优先使用实例值
- **Given** 一个 `DisasterInstance` 已设置 `spec.dataSyncPolicy` 和 `spec.resourceSyncPolicy`
- **When** operator 计算该实例的 DataSync/ResourceSync 策略来源
- **Then** operator 必须优先使用实例级 override 值
- **And** 不得继续回退到基础配置中的同名字段

### Requirement: 实例周期性 reconcile 必须负责 DS/RS 调度收敛
系统必须 (MUST) 由 `DisasterInstance` 控制器的 reconcile / 周期性 requeue 将实例有效策略的变更最终收敛到 `DataSync` 与 `ResourceSync` 的 `trigger.schedule`。

#### Scenario: 实例更新 override 后在本对象 reconcile 中收敛到新策略
- **Given** 一个已创建的 `DisasterInstance` 更新了自身的同步策略 override
- **When** `DisasterInstance` 控制器重新 reconcile 该实例
- **Then** 该实例关联的 `DataSync` 与 `ResourceSync` 必须最终收敛到新的实例策略

#### Scenario: 有效策略被清空或禁用后在同次收敛中清理陈旧调度
- **Given** 一个 `DisasterInstance` 的有效策略因为 override 清空或引用策略被禁用而变为空
- **When** `DisasterInstance` 控制器重新对齐该实例的 `DataSync` 与 `ResourceSync`
- **Then** `trigger.schedule` 必须最终被清空
- **And** 已注册的陈旧 cron 调度任务必须最终被移除
