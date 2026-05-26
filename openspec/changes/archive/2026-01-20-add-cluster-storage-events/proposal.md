# 提案：Cluster 与 StorageRepository 结构化事件发射

## 背景
根据 `enhance-event-reporting` 提案，我们已经为 `AppBackup` 和 `AppRestore` 实现了结构化事件发射。现在需要将这一能力扩展到 **Cluster** 和 **StorageRepository** 控制器，以便 `disaster-server` 的全局历史事件接口能够展示这些资源的关键运维事件。

## 目标
1. 为 `Cluster` 控制器增加关键阶段的事件发射
2. 为 `StorageRepository` 控制器增加关键阶段的事件发射
3. 复用 `pkg/helper/event_reporter.go` 中的结构化消息格式

## 事件设计

### Cluster 事件

| 阶段 | Reason | Type | 触发条件 |
|------|--------|------|----------|
| 创建成功 | `ClusterCreated` | Normal | 资源首次创建完成 |
| 连接中 | `ClusterConnecting` | Normal | 开始建立集群连接 |
| 就绪 | `ClusterReady` | Normal | 集群状态变为 Ready (连接成功 + Velero 就绪) |
| 连接失败 | `ClusterFailed` | Warning | 集群连接失败或健康检查失败 |
| Velero 安装中 | `VeleroInstalling` | Normal | 开始安装 Velero |
| Velero 安装成功 | `VeleroInstalled` | Normal | Velero 部署完成 |
| Velero 安装失败 | `VeleroInstallFailed` | Warning | Velero 安装过程报错 |
| 删除中 | `ClusterDeleting` | Normal | 开始删除集群资源 |

### StorageRepository 事件

| 阶段 | Reason | Type | 触发条件 |
|------|--------|------|----------|
| 创建成功 | `StorageCreated` | Normal | 资源首次创建完成 |
| 验证中 | `StorageValidating` | Normal | 开始 S3 连接验证 |
| 就绪 | `StorageReady` | Normal | S3 连接验证通过，存储可用 |
| 验证失败 | `StorageUnavailable` | Warning | S3 连接验证失败 |
| 删除中 | `StorageDeleting` | Normal | 开始删除存储配置 |

### 消息格式
复用现有格式：
```
[Task: Cluster/e2e-cluster-source] [Status: Ready] [Duration: 45s] [User: admin]
```

## 影响范围
- `internal/controller/cluster_controller.go`
- `internal/controller/storagerepository_controller.go` (如果存在独立控制器)

## 实现注意事项

### 1. 复用现有事件 Helper
- **必须使用** `helper.ReportTaskStartedWithClient` 和 `helper.ReportTaskFinishedWithClient`
- 这些函数会自动添加 `testudo.softcdata.com/task-event: "true"` Label
- Server 端依赖此 Label 进行高效查询

### 2. 消息格式规范
```
[Task: Cluster/e2e-cluster-source] [Status: Ready] [Duration: 45s] [Cluster: -] [User: admin] [TraceID: xxx] Ready for use
```

对于 Cluster/Storage 资源，`Cluster` 字段可以填 `-` 或管理集群名。

### 3. 避免重复发射事件
参考 `appbackup_ready.go:456` 的实现：
```go
// 只在状态变更时发射一次
if helper.IsTerminalPhase(newPhase) && !helper.IsTerminalPhase(oldPhase) {
    helper.ReportTaskFinishedWithClient(...)
}
```

对于 Cluster/Storage，需要记录上一次状态，避免每次 Reconcile 都发射相同事件。

**建议方案**:
- 使用 Annotation 记录 `testudo.softcdata.com/last-event-phase` 来追踪已发射的事件
- 或在 Status 中增加 `LastEventPhase` 字段

### 4. taskName 格式
| 资源 | taskName 格式 |
|------|---------------|
| Cluster | `Cluster: {name}` |
| StorageRepository | `Storage: {name}` |

### 5. 事件与现有事件的区别
| 对比项 | AppBackup/AppRestore | Cluster/Storage |
|--------|---------------------|-----------------|
| 有 StartTime/EndTime | ✅ 有（来自 Velero） | ⚠️ 需要自己记录 |
| 有 Cluster 字段 | ✅ 有（目标集群） | ⚠️ 可留空或填管理集群 |
| 有 TraceID | ✅ 有（从 Annotation 提取） | ✅ 有（同样从 Annotation 提取） |

### 6. Duration 计算
Cluster/Storage 没有像 Velero 那样的 StartTimestamp/CompletionTimestamp，需要自己记录：
- 在 Status 中增加 `CreationTimestamp` 或复用 `metadata.creationTimestamp`
- 在状态变为 Ready 时记录 `ReadyTimestamp`

## 关联提案
- **disaster-server**: `add-cluster-storage-events` - Server 端事件聚合适配

## 变更 ID
`add-cluster-storage-events`
