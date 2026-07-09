# Design: DataSync 无 PVC no-op 跳过

## 当前逻辑确认

代码当前路径如下：

1. `DataSyncReconciler.shouldSync` 在首次同步、手动触发或进行中状态返回 true。
2. `executeSync` 读取 `DisasterInstance` 和 `DisasterConfig` 后，先检查 `StorageRepository`，再把 `DataSync.status.state` 置为 `InProgress`。
3. 控制器固定创建或触发 `ds-<datasync>` `AppBackup`。
4. `buildAppBackupSpec` 固定设置：
   - `IncludedResources=["pods","persistentvolumeclaims","persistentvolumes"]`
   - `DefaultVolumesToFsBackup=true`
   - `SnapshotVolumes=false`
5. 备份完成后 `handleRestore` 固定构建 data restore `AppRestore`。
6. `restore-builder` 的 data restore 固定设置：
   - `IncludedResources=["pods","persistentvolumeclaims","persistentvolumes"]`
   - `RestorePVs=true`
   - trafficless Pod modifier

因此，当前代码没有“源端无 PVC 时跳过数据同步”的判定。

## 设计决策

### 1. 判定点放在 DataSync 控制器

DataSync 控制器拥有实例、方向、命名空间、labelSelector、源集群 client 等上下文，适合决定本次是否需要执行数据恢复。`restore-builder` 不应承担数据发现职责。

### 2. 跳过发生在 StorageRepository 检查之前

无 PVC no-op 不需要读写备份仓库。将预检放在 `ensureStorageRepositoryReady` 之前，可以避免没有 PVC 的实例因为仓库暂时不可用而初始化失败。

执行顺序建议：

1. 获取 `DisasterInstance`
2. 获取 `DisasterConfig`
3. 解析 source/target
4. 源端 PVC 预检
5. 无 PVC：写 no-op 成功状态并返回
6. 有 PVC：检查 StorageRepository 并继续现有链路

### 3. 可恢复 PVC 的定义

可恢复 PVC 是“本次 DataSync 保护范围内可能产生 PVC/FSB 数据恢复动作的 PVC”。

判定规则：

- 命名空间范围来自 `instance.spec.namespaces`。
- 忽略 `deletionTimestamp` 非空的 PVC。
- 若 `instance.spec.labelSelector` 为空，任意非删除中 PVC 都命中。
- 若 labelSelector 非空：
  - PVC 自身 labels 匹配 selector，则命中。
  - Pod labels 匹配 selector，且 Pod 的 `spec.volumes` 引用某 PVC，则该 PVC 命中。

该规则兼容两种常见标记方式：

- 应用直接给 PVC 打业务标签。
- 应用只给 Pod/Deployment 打业务标签，PVC 由 Pod volume 间接参与备份。

### 4. no-op 状态写入

新增内部 helper，例如 `completeDataSyncNoPVC(ctx, ds, clusterPair, message)`：

- `State=Ready`
- `LastSyncTime=now`
- 清理 `Reason`、`Message`
- `LastBackupName`、`LastRestoreName` 不强制清空，保留最近一次真实备份/恢复引用；本次 no-op 由 history/condition 表达
- `Conditions` 写入或更新：
  - `Type=NoDataVolumes`
  - `Status=True`
  - `Reason=NoPVCFound`
  - `Message=本次保护范围内未发现 PVC，已跳过数据同步`
- 追加 `SyncHistoryRecord`：
  - `StartTime=now`
  - `CompletionTime=now`
  - `BackupName=""`
  - `RestoreName=""`
  - `BackupResourceCount=0`
  - `RestoreResourceCount=0`
  - `Status="Skipped"`
- 调用 `syncStatistics`
- 发射 success task finished 事件

### 5. 统计兼容

`syncStatistics` 当前把非 `Succeeded`、非 `Completed` 的历史记录计入 failed。实现时必须将 `Skipped` 视为 completed，避免无 PVC 实例在统计层表现为失败。

### 6. 与 in-progress 运行的关系

实现优先覆盖“新运行尚未触发 AppBackup”的场景。

若已有 `LastBackupName` 或 `LastRestoreName` 表明运行已经进入 Velero 子任务阶段，本变更不主动删除 Backup/Restore，也不取消 Velero 任务，避免引入清理风险。后续可以在单独变更中设计取消语义。

## 实现草图

新增发现函数：

```go
type dataSyncVolumePlan struct {
    PVCs []types.NamespacedName
}

func (r *DataSyncReconciler) discoverDataSyncVolumePlan(
    ctx context.Context,
    sourceClient client.Client,
    instance *disasterv1.DisasterInstance,
) (dataSyncVolumePlan, error)
```

主流程伪代码：

```go
sourceCluster, targetCluster := resolveClusters(instance, config)
clusterPair := fmt.Sprintf("%s->%s", sourceCluster, targetCluster)

if dataSync.Status.State != disasterv1.DataSyncStateInProgress ||
   dataSync.Status.LastBackupName == "" && dataSync.Status.LastRestoreName == "" {
    sourceClient, err := r.getClusterClient(ctx, sourceCluster)
    if err != nil {
        return r.failDataSync(...)
    }
    plan, err := r.discoverDataSyncVolumePlan(ctx, sourceClient, instance)
    if err != nil {
        return r.failDataSync(...)
    }
    if len(plan.PVCs) == 0 {
        return r.completeDataSyncNoPVC(ctx, dataSync, clusterPair)
    }
}

if err := r.ensureStorageRepositoryReady(...); err != nil {
    return r.failDataSync(...)
}
```

## 测试策略

- `DataSync` 首次同步：源命名空间没有 PVC，不创建 `AppBackup`，状态变为 `Ready`，history 记录 `Skipped`。
- 手动触发：源命名空间没有 PVC，更新 `lastSyncTime`，不触发旧 `AppBackup` action。
- 源命名空间存在 PVC：继续创建或触发 `AppBackup`，现有行为不变。
- labelSelector 只匹配 Pod、PVC 无标签：Pod 引用的 PVC 不应被跳过。
- labelSelector 不匹配 Pod/PVC：跳过。
- 源集群 list PVC 失败：DataSync 失败，reason 为依赖或发现失败，不应伪装为 skipped。
- StorageRepository 不可用且无 PVC：仍然 skipped success。
- StorageRepository 不可用且有 PVC：保持现有 StorageUnavailable 失败。
- `syncStatistics`：`Skipped` 计入 completed，不计入 failed。
