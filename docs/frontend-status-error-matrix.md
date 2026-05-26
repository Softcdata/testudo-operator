# 前端状态与错误判定矩阵（Operator + Server）

## 1. 背景
当前前端“无法判定状态并展示错误原因”的根因，不在于 Operator 未产生错误语义，而在于 **部分模块在 Server DTO 中未完整透出 `reason/message`**。

本文给出：
- 各模块真实状态机与错误来源（Operator）
- 前端当前可读字段（Server API）
- 缺口与改造建议（按优先级）

## 2. 前端判定约定（按模块，不使用笼统失败态）
1. 先读资源状态字段（`status` / `state` / `phase` / `fsmState`）。
2. **按模块失败状态集合**判定是否失败（见 2.1），不要使用单一全局集合。
3. 判定为失败后：
   - 主错误码优先读 `reason`
   - 失败描述读 `message`
4. 资源 DTO 未暴露 `reason/message` 时，退回 HTTP 包络错误：`code + message + trace_id`。

### 2.1 模块级失败状态集合（前端应内置）

| 模块 | 状态字段 | 失败状态集合 |
| --- | --- | --- |
| Cluster | `status.status` | `NotReady` |
| StorageRepository | `status.status` | `Unavailable` |
| DisasterConfig | `status.status` | `Error`, `NotReady` |
| DataSync | `status.state` | `Failed` |
| ResourceSync | `status.state` | `Failed` |
| DisasterOperation | `status.state` | `Failed` |
| DisasterDrill | `status.state` | `Failed` |
| DisasterInstance | `status.fsmState` | `Failed` |
| AppBackup | `status.phase` | `Failed` |
| AppRestore | `status.phase` | `Failed` |

### 2.2 模块级失败 reason 字典（前端应按模块匹配）

| 模块 | reason 字典 |
| --- | --- |
| Cluster | `TokenExpired`, `ConfigError`, `InvalidSpec`, `ClientError`, `InstallVeleroFailed`, `VeleroVersionIncompatible`, `VeleroCRDVersionIncompatible`, `VeleroCRDCheckFailed`, `VeleroUninstallFailed` |
| StorageRepository | `ValidationFailed` |
| DisasterConfig | `SourceClusterNotFound`, `TargetClusterNotFound`, `StorageRepositoryNotFound`, `ClusterNotReady`, `QueryDependencyFailed`, `UpdateConfigFailed`, `ApplyStorageRepositoryFailed` |
| DisasterPolicy | `InvalidSchedule`, `DeletionBlocked`（legacy 删除保护恢复后仍保留） |
| DataSync | `BackupFailed`, `RestoreFailed`, `DependencyFailed`, `StorageUnavailable` |
| ResourceSync | `BackupFailed`, `BuildRestoreSpecFailed`, `RestoreFailed`, `DependencyFailed`, `StorageUnavailable` |
| DisasterOperation | `InvalidOperationType`, `ResourceNotFound`, `TimeoutExceeded`, `SyncFailed`, `InvalidState`, `ClusterConnectionFailed`, `StepFailed`, `OperationFailed` |
| DisasterDrill | `ValidationFailed`, `TopologyChanged`, `OperationNotFound`, `CleanupFailed`, `OperationFailed`, `InternalError`, `DrillFailed` |
| DisasterGroup | `InstanceNotFound`, `InstanceFailed` |
| DisasterInstance | `DataSyncFailed`, `ResourceSyncFailed`, `InitializationFailed`, `InstanceFailed` |
| AppBackup | `ReconcileError`, `TimeoutExceeded`, `BackupFailed`, `BackupPartiallyFailed` |
| AppRestore | `ReconcileError`, `RestoreFailed`, `RestorePartiallyFailed`, `TimeoutExceeded` |

Server 全局错误包络：`internal/transport/response.go`
- `code`: 业务错误码（如 `1000/3004/5000`）
- `message`: 错误描述
- `trace_id`: 链路追踪

## 3. API 字段矩阵（前端实际可读）

| 模块 | 主要 API | 状态字段 | 错误字段(reason) | 错误描述(message) | 现状 |
| --- | --- | --- | --- | --- | --- |
| Cluster | `disaster_cluster` | `status.status` | `status.reason` | `status.message` | 完整 |
| StorageRepository | `disaster_storage` | `status.status` | `status.reason` | `status.message` | 完整（存储模块已覆盖） |
| DisasterConfig | `disaster_config` | `status.status` | `status.reason` | `status.message` | 完整 |
| DisasterPolicy | `disaster_policy` | `status.phase` | `status.reason` | `status.message` | 完整（无效 schedule 已标准化） |
| AppBackup | `app_backup` | `status.phase` + `status.latestBackupStatus` | `status.reason` | `status.message` | 基本完整 |
| AppRestore | `app_restore` | `status.phase` | `status.reason` | `status.message` | 完整 |
| DisasterInstance 列表/详情 | `disaster_instance` | `status.fsmState`, `currentState` | `status.reason` | `status.message` | 完整 |
| DisasterInstance 同步状态 | `GET /instances/:name/sync-status` | `dataSync.status`, `resourceSync.status` | `dataSync.reason`, `resourceSync.reason` | `dataSync.message`, `resourceSync.message` | 完整 |
| DisasterDrill | `disaster_drill` | `status.state` | `status.reason` | `status.message` | 完整（已取消扁平字段） |
| DisasterGroup | `disaster_group` | `status.fsmState` | `status.reason` | `status.message` | 完整（组级错误出口） |
| Group Operation Watch | `watch/groups/operations` | `state` | `reason` | `message` | 完整 |

## 4. Operator 真实错误 -> 状态映射（核心模块）

### 4.1 Cluster
来源：`internal/controller/cluster_controller.go`

- 状态：`Pending / Ready / NotReady / Deleting`
- 常见失败 `reason`：
  - `TokenExpired`
  - `ConfigError`
  - `InvalidSpec`
  - `ClientError`
  - `InstallVeleroFailed`
  - `VeleroVersionIncompatible`
  - `VeleroCRDVersionIncompatible`
  - `VeleroCRDCheckFailed`
  - 删除阶段：`VeleroUninstallFailed`

### 4.2 StorageRepository
来源：`internal/controller/storagerepository_controller.go`

- 状态：`Available / Unavailable`
- 失败 `reason`：`ValidationFailed`
- 成功 `reason`：`Available`
- 失败/成功细节在 `status.message`

### 4.3 DisasterConfig
来源：`internal/controller/disasterconfig_controller.go`

- 状态：`Pending / Ready / NotReady / Error`
- 失败 `reason`（稳定枚举）：
  - `SourceClusterNotFound`
  - `TargetClusterNotFound`
  - `StorageRepositoryNotFound`
  - `ClusterNotReady`
  - `QueryDependencyFailed`
  - `UpdateConfigFailed`
  - `ApplyStorageRepositoryFailed`

### 4.4 DisasterGroup
来源：`internal/controller/disastergroup/controller.go`

- 聚合状态：`totalInstances / readyInstances`
- 失败 `reason`（稳定枚举）：
  - `InstanceNotFound`
  - `InstanceFailed`
- 失败描述：`status.message`

### 4.5 DataSync
来源：`internal/controller/datasync/controller.go`

- 状态：`Ready / InProgress / Failed`
- 失败 `reason`（稳定常量）：
  - `BackupFailed`
  - `RestoreFailed`
  - `DependencyFailed`
  - `StorageUnavailable`
- 失败描述：`status.message`

### 4.6 ResourceSync
来源：`internal/controller/resourcesync/controller.go`

- 状态：`Ready / InProgress / Failed`
- 失败 `reason`（稳定常量）：
  - `BackupFailed`
  - `BuildRestoreSpecFailed`
  - `RestoreFailed`
  - `DependencyFailed`
  - `StorageUnavailable`
- 失败描述：`status.message`

### 4.7 DisasterOperation
来源：`internal/controller/disasteroperation/controller.go`

- 状态：`Pending / Running / Completed / Failed`
- 失败 `reason`（失败时自动归因）：
  - `InvalidOperationType`
  - `ResourceNotFound`
  - `TimeoutExceeded`
  - `SyncFailed`
  - `InvalidState`
  - `ClusterConnectionFailed`
  - `StepFailed`
  - `OperationFailed`
- 失败描述：`status.message`

### 4.8 DisasterDrill
来源：`internal/controller/disasterdrill/controller.go`

- 状态：`Pending / Ready / Executing / Completed / CleaningUp / CleanedUp / Failed`
- 失败 `reason`（失败时自动归因）：
  - `ValidationFailed`
  - `TopologyChanged`
  - `OperationNotFound`
  - `CleanupFailed`
  - `OperationFailed`
  - `InternalError`
  - `DrillFailed`
- 失败描述：`status.message`

### 4.9 DisasterInstance
来源：`internal/controller/disasterinstance/controller.go`

- 状态：`fsmState`（`Pending/Initializing/Protected/Paused/FailingOver/Active/FailingBack/Failed`）
- 失败 `reason`（聚合子资源并兜底）：
  - `DataSyncFailed`
  - `ResourceSyncFailed`
  - `InitializationFailed`
  - `InstanceFailed`
- 失败描述：`status.message`

### 4.10 AppBackup
来源：`internal/controller/appbackup/*.go`

- 状态：`Pending / Ready / Failed / Deleting`
- 任务态：`latestBackupStatus`（`InProgress/Completed/Failed/Canceled`）
- 常见 `reason`：`ReconcileError`、`TimeoutExceeded`

### 4.11 AppRestore
来源：`internal/controller/apprestore/*.go`

- 状态：`Pending / Initiating / Restoring / Succeeded / Failed / Cancelled / Deleting`
- 常见 `reason`：`ReconcileError`（由控制器统一兜底）
- 详细失败语义通常在 `status.message` 与 `status.restoreStatus.phase`

## 5. 造成“前端难判定”的剩余关键缺口
1. 历史存量对象可能缺少 `reason`（改造前创建），前端需做空值兜底（`UnknownError` + `message`）。

## 6. 改造建议（按优先级）

### P0（先做）
1. 前端统一判定逻辑切换到模块级失败状态集合（见 2.1）+ `reason/message`。
2. Drill 页面状态读取统一改为 `status.state`（不再使用顶层 `state`）。

### P1（补齐）
1. 对组详情页补充 `status.reason` 的分组文案映射（`InstanceNotFound` / `InstanceFailed`）。

### P2（规范化）
1. 文本兼容：前端继续展示 `message`，但逻辑判定只依赖“模块级失败状态集合 + reason 字典”。
2. 对外文档固定失败态集合与 reason 字典，避免以中文 message 文本做逻辑分支。

## 7. 前端落地示例（推荐）

```ts
type ModuleKey =
  | "cluster"
  | "storage"
  | "disasterConfig"
  | "dataSync"
  | "resourceSync"
  | "operation"
  | "drill"
  | "instance"
  | "appBackup"
  | "appRestore";

const FAILED_STATES: Record<ModuleKey, Set<string>> = {
  cluster: new Set(["NotReady"]),
  storage: new Set(["Unavailable"]),
  disasterConfig: new Set(["Error", "NotReady"]),
  dataSync: new Set(["Failed"]),
  resourceSync: new Set(["Failed"]),
  operation: new Set(["Failed"]),
  drill: new Set(["Failed"]),
  instance: new Set(["Failed"]),
  appBackup: new Set(["Failed"]),
  appRestore: new Set(["Failed"]),
};

function resolveError(module: ModuleKey, obj: any) {
  const st =
    obj?.status?.status ??
    obj?.status?.phase ??
    obj?.status?.state ??
    obj?.state ??
    obj?.phase;
  const reason = obj?.status?.reason ?? obj?.reason;
  const message = obj?.status?.message ?? obj?.message;

  const isFailed = FAILED_STATES[module].has(String(st));
  if (!isFailed) return { isFailed: false };

  return {
    isFailed: true,
    code: reason || "UnknownError",
    message: message || reason || "Unknown error",
  };
}
```

## 8. 代码定位索引

- Operator
  - `internal/controller/cluster_controller.go`
  - `internal/controller/storagerepository_controller.go`
  - `internal/controller/datasync/controller.go`
  - `internal/controller/resourcesync/controller.go`
  - `internal/controller/disasteroperation/controller.go`
  - `internal/controller/disasterdrill/controller.go`
  - `internal/controller/disasterinstance/controller.go`
  - `internal/controller/appbackup/*.go`
  - `internal/controller/apprestore/*.go`

- Server
  - `internal/apis/disaster_cluster/v1/types.go`
  - `internal/apis/disaster_storage/v1/types.go`
  - `internal/apis/disaster_config/v1/types.go`
  - `internal/apis/disaster_instance/v1/types.go`
  - `internal/apis/disaster_instance/v1/handler.go`
  - `internal/apis/disaster_drill/v1/types.go`
  - `internal/apis/disaster_group/v1/types.go`
  - `internal/transport/response.go`
