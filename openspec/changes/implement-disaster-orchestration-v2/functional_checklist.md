# 功能清单与技术实现概览 (Detailed & UI Aligned)

---

## 1. 容灾基础配置 (Disaster Basic Config)
> **UI 描述**: 管理容灾的基础设施，建立主备集群与存储的连接关系，设定全局同步策略。

### 1.1 核心字段实现
| 字段 | 实现方式 (V2 Backend) | 备注 |
| :--- | :--- | :--- |
| **基础信息管理** | `DisasterConfig` CRD (name, annotations)。 | - |
| **集群绑定** | `sourceCluster`, `targetCluster` 字段引用 `Cluster` CRD。 | 需验证集群连通性。 |
| **存储绑定** | `storage` 字段引用 `StorageRepository` CRD。 | 统一使用 S3 接口。 |

### 1.2 数据同步类型实现详情 (Data Sync Types)
UI 原型中列出的 4 种同步类型，对应 `DataSync` 控制器在创建 `AppBackup` 和 执行恢复时的不同策略：

#### 1. 文件复制-S3-多副本 (FSB-S3)
*   **技术原理**: 使用 Velero 的文件系统备份 (FSB) 能力，将 Pod 数据备份到 **S3 对象存储**。
*   **DataSync 行为**: `DefaultVolumesToFsBackup = true`。需要数据搬运 (Source -> S3 -> Target)。
*   **场景**: 跨云、跨中心，网络不直连。

#### 2. 文件复制-NFS共享 (FSB-NFS) ---待验证YS1000流程
*   **配置扩展**: 选择此类型时，用户需提供:
    *   `NfsServer`: NFS 服务器地址 (如 `192.168.1.100`)。
    *   `NfsPath`: 共享路径 (如 `/data/velero-repo`)。
*   **核心定义**: 使用 NFS 作为 **备份仓库 (Backup Repository)** 的文件系统备份。
*   **技术原理**:
    *   **Source**: Kopia 引擎从业务 Pod 读取文件 -> 打包加密 -> **写入 NFS 仓库路径**。
    *   **Target**: Kopia 引擎从 **同一 NFS 仓库路径** 读取数据 -> 还原到业务 Pod。
*   **为什么叫“文件复制”?**: 虽然物理介质共享，但逻辑上数据被**复制**了一份到仓库中。这提供了**快照历史 (RPO)** 和 **数据隔离**，防止主备同时操作同一份数据导致损坏。
*   **实现细节**:
    *   AppBackup: **`DefaultVolumesToFsBackup = true`**。
    *   `StorageLocation`: 自动创建或引用类型为 NFS 的 BSL。
    *   **DataSync 增强**:
        *   **定时检测**: DataSync Controller 必须周期性 (如每 1min) 检查 NFS 挂载点的连通性。
        *   **状态报告**: 在 `Status.Conditions` 中增加 `StorageReady` 状态。如果 NFS 不可达，置为 False 并报警。
*   **Server 端验证**:
    *   提供 API `POST /api/v1/utils/verify-nfs`，在用户提交配置前验证 NFS 参数的有效性 (尝试 mount/ls)。

#### 3. 不同步数据 (No Data Sync / Shared Storage)
*   **核心定义**: **直连挂载 (Direct Mount)**。主备 PVC 指向同一个 NFS Server 目录。
*   **技术原理**: 仅同步 Kubernetes 资源 YAML，**完全跳过 PV/PVC 的数据搬运**。
*   **适用场景**: 无状态应用、支持多写的集群应用、或者静态文件共享。
*   **实现细节**:
    *   AppBackup: `DefaultVolumesToFsBackup = false`, `SnapshotVolumes = false`。
    *   AppBackup: **`ExcludedResources = ["persistentvolumeclaims", "persistentvolumes"]`** (明确排除 PVC)。
    *   **DataSync 行为**: 不执行数据相关操作。

#### 4. 外部触发 (External Trigger)
*   **核心定义**: **Storage-Orchestrated Data Sync**。Operator ***完全不碰数据***，仅负责流程编排。数据复制与切换完全依赖外部存储系统（如：华为云 SDRS, NetApp SnapMirror, Ceph RBD Mirroring）。
*   **详细工作流 (Failover Workflow)**:
    1.  **Phase 1: ScaleDownSource**: Operator 正常缩容源集群应用，停止写入。
    2.  **Phase 2: FinalSync (关键差异)**:
        *   Operator 检测到 SyncType 为 `External`。
        *   `DisasterOperation` 状态变更为 **`WaitingConfirmation`** (等待确认)。
        *   **暂停流程**: Operator 挂起，等待外部信号。此时 UI 显示弹窗提示：“请前往存储控制台执行数据切换，确认完成后点击继续”。
    3.  **User Action**:
        *   管理员登录存储控制台，执行断开同步、提升从库 (Promote/Break-pair) 操作。
        *   存储就绪后，管理员回到容灾平台，点击 **“确认已切换”**。
    4.  **Confirm API**: 前端调用 `POST /api/v1/operations/{name}/confirm`。
    5.  **Phase 3: ScaleUpTarget**: Operator 收到确认信号，状态变更为 `Running`，继续执行备集群扩容，挂载已提升为读写模式的 PVC。
*   **特殊实现**: `DataSync` CRD 在此模式下仅作为占位符，状态始终为 `Manual`，不执行任何定时备份任务。

### 1.3 Server API 需求
| 方法 | 路径 | 描述 |
| :--- | :--- | :--- |
| `GET` | `/api/v1/configs` | 分页查询容灾配置列表。支持关键字搜索。 |
| `POST` | `/api/v1/configs` | 创建新的容灾配置。 |
| `GET` | `/api/v1/configs/{name}` | 获取配置详情。使用 `name` 作为标识符。 |
| `PUT` | `/api/v1/configs/{name}` | 更新配置（如修改同步策略）。 |
| `DELETE` | `/api/v1/configs/{name}` | 删除配置。需校验主要是否被 Instance 引用。 |
| `POST` | `/api/v1/utils/verify-nfs` | 验证 NFS 连通性。输入: `{ server, path }`。 |

---

## 2. 容灾实例配置 (Disaster Instance Config)
> **UI 描述**: "以应用为中心" 定义保护对象。用户只需选择容灾配置和命名空间，即可开启保护。详情页展示拓扑、健康度和历史记录。

### 2.1 核心功能实现
| 功能点 | 实现方式 (V2 Backend) |
| :--- | :--- |
| **实例创建与纳管** | 创建 `DisasterInstance` CRD。自动级联创建 `DataSync` 和 `ResourceSync`。 |
| **持续资源同步** | **[ResourceSync]** Scale-to-Zero 方案。备份 YAML -> 恢复时 ResourceModifier 强制 `replicas=0`。 |
| **无流量数据同步** | **[DataSync]** Trafficless Restore 方案。 恢复时 ResourceModifier 替换 Image 为 `busybox`，移除所有 Labels，确保 Service 不导流。 |
| **状态统计展示** | `DataSync`/`ResourceSync` 在 Status 中统计 `LastSyncAssetCount` (如资源数 156) 和 `SuccessRate`。 |
| **实例运维能力** | 支持 **容灾切换 (Failover)**, **反向保护 (Reprotect)**, **手动数据同步**, **手动资源同步**, **暂停/恢复保护**。 |

### 2.2 Server API 需求
| 方法 | 路径 | 描述 |
| :--- | :--- | :--- |
| `GET` | `/api/v1/instances` | 分页查询。支持按 `config_name` 或 `status` 过滤。 |
| `POST` | `/api/v1/instances` | 创建实例。输入：Namespace 列表, Config Name。 |
| `GET` | `/api/v1/instances/{name}` | 获取详情。使用 `name` 作为标识符。 |
| `GET` | `/api/v1/instances/{name}/sync-status` | 获取详细同步状态 (UI Tab 2)。 |
| `GET` | `/api/v1/instances/{name}/history` | 获取同步历史 (UI Tab 1 底部)。 |
| `GET` | `/api/v1/instances/{name}/topology` | **获取可视化拓扑数据**。返回节点（源/备集群、存储、应用）与连线（同步链路状态、RPO延时）的图结构数据，用于在前端绘制架构监控图。 |
| `POST` | `/api/v1/instances/{name}/sync-data` | **手动触发数据同步**。立即生成一个 DataSyncBackup 任务。 |
| `POST` | `/api/v1/instances/{name}/sync-resource` | **手动触发资源同步**。立即生成一个 ResourceSyncBackup 任务。 |
| `POST` | `/api/v1/instances/{name}/pause` | **暂停保护**。暂停底层的 DataSync 和 ResourceSync 调度。 |
| `POST` | `/api/v1/instances/{name}/resume` | **恢复保护**。恢复底层的 DataSync 和 ResourceSync 调度。 |

---

## 3. 容灾组配置 (Disaster Group Config)
> **UI 描述**: 逻辑分组，通过 **"级别 (Level)"** 编排执行模式。支持快捷设置"全部并行"或"全部串行"，也支持手动拖拽分配级别。

### 3.1 核心功能实现
| 功能点 | 实现方式 (V2 Backend) |
| :--- | :--- |
| **执行模式编排** | **[DisasterGroup]** CRD 采用分层结构：`Spec.Levels`。每个 Level 包含一组 `DisasterInstance` 名称。 |
| **优先级逻辑** | **Priority by Level**: Level 1 (最高优) -> Level 2 -> Level 3。 |
| **并行/串行控制** | **Level 内并行**: 同一个 Level 列表中的所有 Instance 并行执行 Failover。 |
| **Level 间串行**: 只有当 Level N 的所有 Instance 状态都变为 `Active` 后，Controller 才会触发 Level N+1 的操作。 |
| **默认策略** | "全部并行" = 所有 Instance 都在 Level 1。<br>"全部串行" = 每个 Instance 独占一个 Level (1, 2, 3...)。 |

### 3.2 Server API 需求
| 方法 | 路径 | 描述 |
| :--- | :--- | :--- |
| `GET` | `/api/v1/groups` | 分页查询容灾组。 |
| `POST` | `/api/v1/groups` | 创建容灾组。输入结构需支持分层：`{ "levels": [ ["inst1", "inst2"], ["inst3"] ] }`。 |
| `GET` | `/api/v1/groups/{name}` | 获取组详情及编排结构。 |
| `PUT` | `/api/v1/groups/{name}` | 更新编排。前端拖拽改变 Level 结构，后端全量更新 Spec。 |
| `DELETE` | `/api/v1/groups/{name}` | 删除组（解散分组，不删除实例）。 |

---

## 4. 容灾演练与运维 (Drills & Operations)
> **UI 描述**: 提供一键切换 (Failover)、反向保护 (Reprotect) 以及沙箱演练 (Drill)。

### 4.1 核心功能实现
| 功能点 | 实现方式 (V2 Backend) |
| :--- | :--- |
| **故障切换 (Failover)** | `DisasterOperation` (type=failover)。工作流: Pause -> ScaleDown Source -> FinalSync -> ScaleUp Target -> Switch Roles。 |
| **反向保护 (Reprotect)** | `DisasterOperation` (type=reprotect)。反向同步工作流（维持主备角色，仅恢复数据同步方向）。 |
| **撤销切换 (Undo)** | `DisasterOperation` (type=undo)。回滚工作流（缩容 Target，扩容 Source，恢复 A 为主）。 |
| **容灾演练 (Drill)** | `DisasterOperation` (type=drill)。执行 Sandbox Restore：Clone 资源到临时 Namespace (`dr-drill-xxx`)，修改 Service 类型或 NodePort 以便验证，不影响生产。 |
| **操作审计** | 操作记录本身即为审计日志。 |

### 4.2 Server API 需求
| 方法 | 路径 | 描述 |
| :--- | :--- | :--- |
| `GET` | `/api/v1/operations` | 查询操作记录列表。支持按 `instance_name` 或 `type` 过滤。 |
| `POST` | `/api/v1/operations` | **触发操作**。输入：`target_name` (Instance/Group), `type` (failover/drill), `params`。 |
| `POST` | `/api/v1/operations/{name}/confirm` | **确认操作**。用于 `External` 同步类型的 Failover 流程中。 |
| `GET` | `/api/v1/operations/{name}` | 获取操作详情。实时返回 `steps` 执行进度。 |
| `GET` | `/api/v1/operations/{name}/logs` | 获取操作日志。流式返回详细 Log。 |
