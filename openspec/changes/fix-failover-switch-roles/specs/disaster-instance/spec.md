# 容灾角色切换冲突修复需求

## MODIFIED Requirements

### Requirement: 应对角色切换逻辑并发冲突
当一个 `DisasterOperation` 执行角色切换（比如将原主备集群角色反转，Primary -> Secondary）的时候，或者是为了任何目的去直接修改目标 `DisasterInstance` 的状态时，操作控制器 **MUST** 使用针对冲突的自动重试机制（`RetryOnConflict`）以防止其修改因为竞态条件而被覆盖（例如与其它状态刷新同步发生碰撞和覆盖）。

#### Scenario: 角色切换发生资源版本冲突与最终一致
- **Given** 一个处于活动状态的 `Failover` 类的 `DisasterOperation`，且其已经到达了准备执行 `SwitchRoles` 步的阶段。
- **And** 与此同时，另一个后台协程或系统（如 `DataSync` 定时刷新、调度器缓存刷新），也碰巧在刷新或试图修改 `DisasterInstance` 对象，导致对象的 K8s `ResourceVersion` 发生递增。
- **When** 该 `DisasterOperation` 触发 `SwitchRoles` 并请求更新其关联 `DisasterInstance` 的 `primaryCluster` 和 `secondaryCluster`。
- **Then** 操作的执行逻辑代码 **MUST** 从 Kubernetes API 中强行检索当前最新的 `DisasterInstance` 实例副本。
- **And** 通过重试更新机制来稳固地完成角色参数的写透（Write-Through），不管该更新是否经历了由于并发竞争产生的 API Rejection。
