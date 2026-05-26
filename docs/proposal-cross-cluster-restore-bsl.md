# 提案：跨集群恢复时的 BSL 预加载与源信息验证

## 1. 问题背景

在执行 `AppRestore` 恢复任务时，Operator 需要调用 `GetBackupSourceInfo` 来获取备份的详细信息（如备份包含的命名空间、标签选择器等），以便正确配置恢复参数。

**当前流程的死锁（Deadlock）：**
1.  **获取备份信息**：Operator 尝试读取指定的备份（`Backup` CR）。
2.  **依赖 BSL**：在跨集群场景下，目标集群（恢复集群）初始状态下并没有源集群的备份记录。Velero 只有在配置了指向源集群存储路径的 `BackupStorageLocation` (BSL) 后，才能从对象存储同步这些备份记录。
3.  **缺失 BSL**：Operator 尚未创建指向源集群的 BSL，因为它还不知道源集群是谁，也不知道用哪个 `StorageRepository`（这些信息目前是试图从备份信息里反推的，或者期望环境里已经有了）。
4.  **结果**：因为没有 BSL -> Velero 无法同步备份 -> Operator 找不到备份 CR -> 无法获取源集群信息 -> 无法创建 BSL。形成死循环。

## 2. 解决方案：显式传参与 BSL 预加载

为了打破这个循环，我们需要在尝试读取备份信息**之前**，先构建好访问备份数据的通道（即 BSL）。这意味着我们需要在 `AppRestore` 的定义中显式提供构建 BSL 所需的元数据。

### 2.1 API 变更建议

建议在 `AppRestore.Spec` 中增加（或复用）以下字段，用于定位备份源：

```go
type AppRestoreSpec struct {
    // ... 现有字段

    // SourceCluster 指定备份数据来源的集群名称。
    // 如果为空，默认为当前集群（同集群恢复）。
    // 在跨集群恢复场景下必填。
    SourceCluster string `json:"sourceCluster,omitempty"`

    // StorageRepository 指定存储备份数据的仓库名称（如 storage-1）。
    // 在跨集群恢复场景下必填，用于构建 BSL 名称和路径。
    StorageRepository string `json:"storageRepository,omitempty"`
}
```

### 2.2 控制器逻辑变更 (PendingHandler)

修改 `internal/controller/apprestore/apprestore_state.go` 中的 `PendingHandler` 逻辑，引入“BSL 预加载”阶段。

**新流程如下：**

1.  **参数校验**：
    *   检查 `AppRestore.Spec.SourceCluster` 和 `AppRestore.Spec.StorageRepository`。
    *   如果这两个字段存在且 `SourceCluster != TargetCluster`，标记为**跨集群恢复模式**。

2.  **BSL 预加载 (Pre-load BSL)**：
    *   **验证 SR**：根据 `Spec.StorageRepository` 查询管理平面的 `StorageRepository` 资源，确保其存在且有效。
    *   **构建 BSL 名称**：遵循之前的命名规范 `bslName = {StorageRepository}-{SourceCluster}`。
    *   **应用 BSL**：调用 `ApplyStorageRepository`，在目标集群（`Spec.Cluster`）上创建或更新该 BSL。
        *   Bucket: SR 定义的 Bucket
        *   Prefix: SR 定义的 Prefix + `SourceCluster`
        *   AccessMode: ReadOnly (建议，防止误写源数据)

3.  **等待同步与超时控制**：
    *   BSL 创建后，Velero 需要时间从对象存储列出备份并创建本地的 `Backup` CR。
    *   **超时机制**：为了防止无限重试（例如因 BSL 配置错误导致永远无法同步），需要引入超时控制。
    *   逻辑：
        *   尝试获取 Backup CR。
        *   如果找不到 (NotFound)：
            *   检查 `time.Since(appRestore.CreationTimestamp)`。
            *   如果 **< 超时阈值** (建议 5分钟)：返回 `RequeueAfter: 10s`，继续等待。
            *   如果 **> 超时阈值**：记录错误事件 "BackupSyncTimeout"，将状态置为 `Failed`，停止重试。

4.  **获取备份信息**：
    *   此时 BSL 已就绪且备份已同步。
    *   调用 `GetBackupSourceInfo` 能够成功获取备份详情。

5.  **后续流程**：
    *   继续执行原有的恢复逻辑。

## 3. 伪代码示例

```go
const BackupSyncTimeout = 5 * time.Minute

func (h *PendingHandler) Handle(ctx context.Context, r *AppRestoreReconciler, appRestore *disasterv1.AppRestore) (...) {
    // ... Client 获取 ...

    // [新增] 阶段 0: BSL 预加载 (针对跨集群)
    if appRestore.Spec.SourceCluster != "" && appRestore.Spec.SourceCluster != appRestore.Spec.Cluster {
        if appRestore.Spec.StorageRepository == "" {
            return Failed, err("StorageRepository is required for cross-cluster restore")
        }

        // 1. 获取 StorageRepository 定义
        sr := &disasterv1.StorageRepository{}
        if err := r.Get(..., sr); err != nil {
            return Pending, err // 等待 SR 就绪
        }

        // 2. 在目标集群创建 BSL
        bslName := fmt.Sprintf("%s-%s", sr.Name, appRestore.Spec.SourceCluster)
        // 注意：这里 Prefix 应该是源集群的标识
        err := defaultBSL.ApplyStorageRepository(ctx, cli, sr, bslName, appRestore.Spec.SourceCluster)
        if err != nil {
            return Pending, err
        }
        
        // BSL 创建成功，Velero 开始后台同步...
    }

    // 阶段 1: 获取备份信息 (原逻辑)
    // 现在即使是跨集群，因为上面已经创建了 BSL，这里最终会成功找到 Backup CR
    backupSource, err := r.GetBackupSourceInfo(ctx, cli, appRestore)
    if err != nil {
        if IsNotFound(err) {
            // 超时检查
            if time.Since(appRestore.CreationTimestamp.Time) > BackupSyncTimeout {
                r.Recorder.Event(appRestore, corev1.EventTypeWarning, "BackupSyncTimeout", "Timed out waiting for backup to sync")
                return disasterv1.PhaseFailed, ctrl.Result{}, fmt.Errorf("timeout waiting for backup sync")
            }
            
            // 可能是 Velero 还没同步完，Requeue 等待
            r.Recorder.Event(appRestore, corev1.EventTypeNormal, "WaitingForBackupSync", "Waiting for Velero to sync backup metadata...")
            return disasterv1.PhasePending, ctrl.Result{RequeueAfter: 10 * time.Second}, nil
        }
        return disasterv1.PhaseFailed, ctrl.Result{}, err
    }

    // ... 后续逻辑 ...
}
```

## 4. 优势

1.  **解耦依赖**：不再依赖“先有备份信息再创建 BSL”，而是“先创建 BSL 再读取备份信息”。
2.  **明确意图**：用户在提交恢复请求时明确指定了数据源，减少了 Operator 猜测或反推的逻辑复杂度。
3.  **鲁棒性**：能够处理目标集群完全空白（没有任何历史备份记录）的冷启动恢复场景。
