# Resource Naming Mismatch and Reconciliation Loop Troubleshooting

## 1. 问题描述 (Problem Description)

在对 AppRestore 和 Velero Backup 进行命名规范优化后，观察到以下异常现象：
1.  **无限调谐循环 (Reconcile Loop)**：`ResourceSync` 控制器频繁触发调谐，导致大量日志刷屏 (`DEBUG ... 正在调谐 ResourceSync`)。
2.  **旧资源报错**：存量的 `AppRestore` 资源频繁报错 `Reconciler error ... context canceled` 或 `Velero Restore deleted unexpectedly`。
3.  **资源重复创建**：部分旧资源似乎被重新创建，导致产生新的命名格式的资源 (`rec-rs-...`)，与旧命名格式 (`restore-...`) 共存。
4.  **日志噪音**：`DisasterPolicy` 控制器由于无效的 Cron 表达式频繁输出错误日志。

## 2. 原因分析 (Root Cause Analysis)

### 2.1 命名规范变更导致的不兼容

我们实施了新的命名规范：
-   **旧**: `AppRestore` -> Velero Restore: `app-restore-<Name>`
-   **新**: `AppRestore` -> Velero Restore: `res-<Name>`

对于在代码更新前创建的旧 `AppRestore` CR，其对应的 Velero Restore 已经以 `app-restore-` 前缀创建。
代码更新后，`AppRestore` 控制器使用新逻辑 (`GenRestoreName`) 计算期望的 Velero Restore 名称 (`res-`)。
由于找不到名为 `res-` 的资源，控制器误认为 Velero Restore 丢失或未创建，从而尝试新建（或在检查状态时报错）。

此不匹配导致：
-   旧 `AppRestore` 认为自身状态异常（Failed）。
-   `ResourceSync` 发现关联的 `AppRestore` 异常或丢失（因为它也在寻找新命名），于是触发重新创建逻辑，生成了新的 `AppRestore`。

### 2.2 Reconcile Loop (ResourceSync)

`ResourceSync` 的“无限循环”现象由以下几个因素共同作用产生：
1.  **InProgress 状态轮询**：当 `ResourceSync` 处于 `InProgress`（正在备份或恢复）状态时，设计上会每隔 5 秒重新排队以检查进度。这是正常的业务逻辑。
2.  **高频事件触发**：`ResourceSync` 拥有 (`Owns`) 下属的 `AppBackup` 资源。Velero 在备份过程中会频繁更新 `AppBackup` 的进度 Status。每次更新都会触发 `ResourceSync` 的 Reconcile。
3.  **昂贵的远程调用**：在 `executeSync` 方法的主流程中，包含了一个 `recordReplicasToConfigMap` 调用。该函数会建立到源集群的 K8s 客户端连接并 List 所有 Deployments/StatefulSets。由于主流程被频繁触发（上述第 2 点），导致这个昂贵操作被重复执行，加剧了系统负载和日志输出，造成“刷屏”的感官体验。

### 2.3 DisasterPolicy 日志

`DisasterPolicy` 控制器对无效的 Cron 表达式（如包含秒字段的 6 段式 Cron）记录了 `Error` 级别的日志。Controller Runtime 对返回 Error 的重试机制导致该错误被频繁记录。

## 3. 排查过程 (Investigation Process)

1.  **日志分析**：通过 `make run` 观察实时日志，发现 `ResourceSync` 和 `DisasterPolicy` 是主要噪声源。
2.  **代码审查 (ResourceSync)**：检查 `internal/controller/resourcesync/controller.go`，发现 `recordReplicasToConfigMap` 位于 `executeSync` 的通用路径上，且该函数涉及远程调用。
3.  **代码审查 (AppRestore)**：检查 `internal/controller/apprestore/`，发现 `GenRestoreName` 方法简单地返回新格式名称，未考虑旧格式的兼容性。
4.  **调试验证**：修改日志级别并在本地运行，确认循环频率和触发原因。

## 4. 解决思路 (Resolution Strategy)

1.  **兼容旧资源**：修改 `AppRestore` 控制器逻辑，在查找 Velero Restore 时，如果新名称不存在，尝试查找旧名称。如果旧名称存在，则接管它。
2.  **优化调谐性能**：将 `ResourceSync` 中昂贵的 `recordReplicasToConfigMap` 操作移出主循环。仅在状态**从其它状态变更为 InProgress** 的瞬间执行一次。
3.  **降低日志噪音**：
    -   将 `ResourceSync` 的常规 "正在调谐" 日志调整为 V(2) 级别。
    -   将 `DisasterPolicy` 的校验错误日志降级为 Info 级别（因为这是用户配置错误，非系统故障）。

## 5. 解决方案实施 (Solution Implementation)

### 5.1 AppRestore 兼容性修复

在 `internal/controller/apprestore/apprestore_utils.go` 中新增 `getVeleroRestore` 辅助函数：

```go
func (r *AppRestoreReconciler) getVeleroRestore(...) (*velerov1.Restore, error) {
    // 1. 尝试新名称: res-{Name}
    // ...
    // 2. 尝试旧名称: app-restore-{Name}
    // ...
}
```

并更新了 `RestoringHandler`, `FailedHandler`, `CancelledHandler` 使用此函数。

### 5.2 ResourceSync 性能优化

修改 `internal/controller/resourcesync/controller.go`：

```go
// Before: 每次 Reconcile 都执行
// recordReplicasToConfigMap(...)

// After: 仅在状态迁移时执行
if resourceSync.Status.State != disasterv1.ResourceSyncStateInProgress {
    // 1.5 记录原始副本数 (Before Backup)
    recordReplicasToConfigMap(...)
    
    resourceSync.Status.State = disasterv1.ResourceSyncStateInProgress
    // ...
}
```

### 5.3 日志优化

-   `ResourceSync`: `log.V(1).Info` -> `log.V(2).Info`
-   `DisasterPolicy`: `log.Error` -> `log.Info`

## 6. 结果 (Conclusion)

修复后，系统日志显著减少，旧有的 `AppRestore` 资源能够正常结束或被正确识别，`ResourceSync` 不再进行无意义的高频远程调用。系统稳定性得到提升。
