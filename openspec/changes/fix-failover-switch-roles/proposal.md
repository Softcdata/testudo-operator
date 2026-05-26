# 修复故障切换 (Failover) 的角色属性冲突问题

## Context (背景)
在执行容灾组 (DisasterGroup) 的故障切换操作时，如果组内包含多个层级（例如 Level 0 和 Level 1），不同层级的实例在切换角色时可能会出现状态未完全更新的情况。具体表现为：底层 `DisasterOperation` 的执行步骤（如 `SwitchRoles`）实际上已成功执行并报告目标集群，但是目标 `DisasterInstance` 的 `Status` 字段（`PrimaryCluster` 和 `SecondaryCluster`）要么没有发生变更，要么被旧状态的数据彻底覆盖回去了。

这主要是由于在集群运行中存在竞态条件导致：
1. **高频后台同步**：比如 `DataSync` 或其他定时刷新任务，可能会在最关键的 `SwitchRoles` 瞬间写入被旧数据的实例状态。
2. **控制器更新竞争**：虽然 Operation Workflow 里有 `PauseSchedules`，但已经在执行中的旧任务如果不加锁或不校验 `ResourceVersion`，很容易写回陈旧的缓存。
3. **缺少重试机制**：如果 `SwitchRoles` 更新时触发了 K8s 的版本冲突错误 (Conflict)，如果不自动重新拉取并重试，本次合法的状态机更新就会静默失败并被丢弃。

## Proposal (提案)
1. **重构角色切换逻辑**：确保任何需要修改 `DisasterInstance.Status.PrimaryCluster` 和 `SecondaryCluster` 的组件或控制器必须使用标准库的 `retry.RetryOnConflict` 包装。
2. **实时状态获取机制**：在冲突重试的循环体内，**必须**绕过本地 Informer 发起 Live Client Get，拿到 API Server 中最新版本的 `DisasterInstance` 实例后，再应用修改并立即发起 Update。
3. **防御式更新拦截**：确保由于其他不相关的状态改变（比如某次时间同步）不会间接导致主备角色错乱。

## Alternate Solutions Considered (考虑过的替代方案)
*   **分布式选主或加锁 (Leader Election/DLocks)**：作为本地执行的 Operator，引入复杂的分布式锁机制过于臃肿。
*   **使用 K8s Patch 操作**：`Patch` 只应用增量虽然能一定程度规避冲突，但在 Kubebuilder 生态下，应对高并发 Status 修改最标准的范式仍是 `RetryOnConflict` 结合完整的全量 Get 和 Update。

## Impact (影响范围)
*   **用户体验**：彻底杜绝容灾切换后产生“假成功/切换异常”或角色没拉过来的情况，提升自动化演练和真实容灾切换时的 100% 成功率。
*   **代码边界**：修改范围非常集中，仅限 `DisasterOperation` 里的核心逻辑执行引擎以及相关的 Status 更新钩子函数。
