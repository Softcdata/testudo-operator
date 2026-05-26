# Change: 收敛基础配置与实例的同步策略继承模型

## Why
当前用户输入希望收口为单个 `syncPolicy`，但 operator 的真实持久化与执行模型仍是旧双字段：
- `DisasterConfig.spec.dataSyncPolicy`
- `DisasterConfig.spec.resourceSyncPolicy`

同时 `DisasterInstance` 还没有正式的同步策略持久化字段，实例无法稳定覆盖基础配置。现有 proposal 把 `syncPolicy` 继续下沉为 operator 主字段，已经与最新边界不一致。

## What Changes

### 1. operator 保持旧双字段 CRD 持久化模型
- `DisasterConfig` 继续使用 `spec.dataSyncPolicy` / `spec.resourceSyncPolicy` 作为基础配置的真实策略来源。
- 本 change 不在 operator 中新增统一 `syncPolicy` 主字段。

### 2. DisasterInstance 引入显式双字段 override
- `DisasterInstance` 增加 `spec.dataSyncPolicy` / `spec.resourceSyncPolicy` 作为实例级 override 持久化字段。
- `spec.dataSyncPolicy` 与 `spec.resourceSyncPolicy` 按字段独立生效，而不是整组开关。
- 某个实例字段为空时，仅继承 `DisasterConfig` 中对应字段。
- 某个实例字段非空时，仅覆盖对应维度的基础配置。

### 3. operator 统一继承链与收敛语义
- `DisasterInstance` 创建/更新 `DataSync`、`ResourceSync` 时遵循统一优先级：
  - `DisasterInstance.spec.dataSyncPolicy` / `spec.resourceSyncPolicy`
  - `DisasterConfig.spec.dataSyncPolicy` / `spec.resourceSyncPolicy`
- 首期只要求最终一致，但必须绑定到确定性的触发模型：由 `DisasterInstance` 控制器的周期性 reconcile / requeue 负责重算实例有效策略，不要求额外新增 `DisasterPolicy` / `DisasterConfig` 事件驱动 fan-out。
- 基础配置或引用策略变更后，必须在下一次实例周期性 reconcile 中重新计算并下发到 `DataSync/ResourceSync`。
- 若有效策略被清空或禁用，触发该次重算的实例 reconcile 也必须最终清空 `DataSync/ResourceSync` 的 `trigger.schedule` 并移除陈旧调度任务。

### 4. 与 server/web 的统一输入模型对齐
- `syncPolicy` 作为统一输入模型只存在于 server/web 契约层。
- server 负责把统一输入 fan-out 回 operator 的旧双字段 CRD。

## Non-Goals
- 不处理 AutoBackup 策略归并。
- 不在 operator 中新增 `syncPolicy` 主字段。
- 不承诺首期实现“策略修改后立即更新”。

## Impact
- Affected specs:
  - `disaster-config`
  - `disaster-instance`
- Affected code:
  - `pkg/apis/disaster/v1/disasterinstance_types.go`
  - `internal/controller/disasterinstance/controller.go`
  - `internal/controller/datasync/controller.go`
  - `internal/controller/resourcesync/controller.go`
- Cross-repo impact:
  - `disaster-server`：统一 `syncPolicy` 输入、兼容层、实例 override DTO
  - `cluster-disaster-web`：基础配置页、实例表单、详情页
  - `disaster-system-chart`：仅发布 `DisasterInstance` CRD 变更

## Relationship to Existing Changes
- 参考 active change：`fix-sync-schedule-not-applied`
- 本 change 不替代调度下发修复，而是重新定义“策略来源落盘位置”和“实例优先的继承链”。
