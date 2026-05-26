## MODIFIED Requirements

### Requirement: 恢复生命周期控制

AppRestore 控制器必须 (MUST) 在恢复任务处于运行态时保证状态可收敛，不得无限停留在 `Restoring/InProgress`。

#### Scenario: 进度完成但相位长期 InProgress 时触发收敛
- **Given** `AppRestore` 处于 `Restoring`
- **And** Velero Restore 处于 `InProgress`
- **And** `itemsRestored == totalItems` 且 `completionTimestamp` 为空
- **When** 该状态持续超过系统定义的软收敛窗口
- **Then** 系统必须 (MUST) 触发收敛处理
- **And** 在有界时间内进入终态（`Succeeded` 或 `Failed`）

#### Scenario: 自动自愈最多执行一次
- **Given** `AppRestore` 命中“进度完成但未终态”的异常收敛条件
- **When** 系统执行自动自愈
- **Then** 系统必须 (MUST) 至多执行一次自动重试
- **And** 重试后仍异常时必须 (MUST) 进入 `Failed`
- **And** 输出明确 `reason/message`

#### Scenario: restorePVs 关闭时仍可探测运行态 PVR 异常
- **Given** `AppRestore.spec.template.restorePVs=false`
- **And** Velero Restore 处于运行态
- **When** 存在 PodVolumeRestore 异常（失败或长期 pending）
- **Then** 系统必须 (MUST) 继续执行异常探测
- **And** 按异常收敛策略终止恢复，避免无限 `InProgress`

#### Scenario: 启动后长期无进度时触发收敛
- **Given** `AppRestore` 处于 `Restoring`
- **And** Velero Restore `phase` 为空或 `New`
- **And** `startTimestamp` 与 `completionTimestamp` 均为空
- **When** 该状态持续超过系统定义的启动收敛窗口
- **Then** 系统必须 (MUST) 触发收敛处理
- **And** 在有界时间内进入终态（`Succeeded` 或 `Failed`）

#### Scenario: 重启窗口瞬态失败优先自动重试
- **Given** Velero Restore 进入 `PartiallyFailed` 或 `Failed`
- **And** `failureReason` 表示 `during the server starting` 的瞬态失败
- **When** 系统检测到该失败
- **Then** 系统必须 (MUST) 优先执行自动重试（最多一次）
- **And** 若重试后仍异常则进入 `Failed` 并写入明确 `reason/message`

#### Scenario: 自动重试触发后 NotFound 允许重建
- **Given** `AppRestore` 已进入自动重试流程
- **And** 查询 Velero Restore 返回 `NotFound`
- **When** 控制器进入 `Restoring` 分支
- **Then** 系统必须 (MUST) 将该 `NotFound` 视为可重建路径
- **And** 不得立即误判为“意外删除”失败
