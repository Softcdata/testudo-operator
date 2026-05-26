# 功能清单与技术实现概览 (Detailed & UI Aligned)

本文档基于 UI 原型 (`http://192.168.120.180:8080/`) 梳理出用户可见的**四大核心功能模块**，并详细说明了**四大核心数据同步类型**的后端技术实现细节，以及 **Server 端所需 API**。

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

#### 2. 文件复制-NFS共享仓库 (FSB-NFS)
*   **关键区别**: 您提到的 **“数据加多少，从集群就加多少”** 是 **直连挂载** (即下面的类型 3)。在这里，NFS 被用作 **备份仓库 (Backup Repository)**，而不是直接作为业务卷。
*   **技术原理**:
    *   **Source**: 读取业务 PVC 文件 -> 打包/加密/压缩 -> **写入 NFS 上的 `/velero-repo/` 目录** (变成不可读的 Blob 数据)。
    *   **Target**: 从 NFS 上的 `/velero-repo/` 读取 Blob -> 解压/还原 -> **写入 Target 业务 PVC**。
*   **为什么选它而不是直连?**: 
    1.  **防误删/防勒索**: 直连挂载时，主集群删了文件，备集群马上也就没了。FSB-NFS 提供 **由 Time-lock 保护的历史快照**，主集群删了文件，备集群可以从 1 小时前的快照恢复。
    2.  **应用兼容性**: 很多应用 (如 MySQL) 不支持两个集群同时挂载同一个文件目录 (会损坏数据库文件)。必须通过 Backup/Restore 流程来确保数据一致性。
*   **实现细节**:
    *   AppBackup: `DefaultVolumesToFsBackup = true`, `StorageLocation` = NFS BSL。

#### 3. 不同步数据 (No Data Sync / Shared Storage)
*   **这才是您描述的场景**: 主备集群的 PVC **直接挂载同一个 NFS Server 的同一个目录**。
*   **技术原理**: 仅同步 Kubernetes 资源 YAML，**完全跳过 PV/PVC 的数据搬运**。
*   **适用场景**:
    *   **Web 服务 / 静态文件**: Nginx 集群读取共享的 HTML/图片。
    *   **支持多写/多读的应用**: 如特定的集群版中间件。
    *   **注意**: 如果您的应用是单实例数据库 (MySQL/Redis)，直接这样用会导致数据损坏，除非您确保主备永远不同时启动 (Pilot Light 架构支持这点)。
*   **实现细节**:
    *   AppBackup: `DefaultVolumesToFsBackup = false`, `SnapshotVolumes = false`。
    *   **DataSync 行为**: 不执行数据搬运。备集群 Pod 启动时，直接 mount 这个 NFS PVC，立刻就能看见所有数据。

#### 4. 外部触发 (External Trigger)
*   **技术原理**: 依赖底层硬件复制 (NetApp/Ceph RBD Mirror)。
*   **DataSync 行为**: Operator 暂停并等待用户 Confirm。

### 1.3 Server API 需求
| 方法 | 路径 | 描述 |
| :--- | :--- | :--- |
| `GET` | `/api/v1/configs` | 分页查询容灾配置列表。支持关键字搜索。 |
| `POST` | `/api/v1/configs` | 创建新的容灾配置。 |
| `GET` | `/api/v1/configs/{name}` | 获取配置详情。使用 `name` 作为标识符。 |
| `PUT` | `/api/v1/configs/{name}` | 更新配置（如修改同步策略）。 |
| `DELETE` | `/api/v1/configs/{name}` | 删除配置。需校验主要是否被 Instance 引用。 |

---

## 2. 容灾实例配置 (Disaster Instance Config)
> **UI 描述**: "以应用为中心" 定义保护对象。用户只需选择容灾配置和命名空间，即可开启保护。详情页展示拓扑、健康度和历史记录。

### 2.1 核心功能实现
| 功能点 | 实现方式 (V2 Backend) |
| :--- | :--- |
| **实例创建与纳管** | 创建 `DisasterInstance` CRD。自动级联创建 `DataSync` 和 `ResourceSync`。 |
| **持续资源同步** | **[ResourceSync]** Scale-to-Zero 方案。备份 YAML -> 恢复时 ResourceModifier 强制 `replicas=0`。 |
| **无流量数据同步** | **[DataSync]** Trafficless Restore 方案。详见下方 §2.1.1。 |
| **状态统计展示** | `DataSync`/`ResourceSync` 在 Status 中统计 `LastSyncAssetCount` (如资源数 156) 和 `SuccessRate`。 |

#### 2.1.1 Trafficless Restore 技术实现

**目标**: 在目标集群恢复一个"无流量"的临时 Pod，仅用于 Velero FSB 数据同步，不影响生产流量。

**ResourceModifier 配置** (JSON Patch 操作顺序很重要):

| 序号 | 操作 | Path | 目的 |
|:---:|:---|:---|:---|
| 1 | `remove` | `/metadata/name` | 删除原名称，避免 StatefulSet 控制器识别 |
| 2 | `add` | `/metadata/generateName` = `"trafficless-sync-"` | 使用自动生成名称 |
| 3 | `replace` | `/metadata/labels` = `{"trafficless": "true"}` | 清除所有原有标签，避免 Service 导流 |
| 4 | `replace` | `/metadata/annotations` = `{}` | 清除所有注释 |
| 5 | `replace` | `/spec/containers/0/image` = `"busybox:1.36"` | 替换为轻量镜像 |
| 6 | `replace` | `/spec/containers/0/command` = `["sleep", "infinity"]` | 容器仅保持存活 |
| 7 | `remove` | `/metadata/ownerReferences` | 防止 K8s GC 删除 |

**⚠️ 重要: StatefulSet vs Deployment 管理方式差异**

| 控制器类型 | Pod 识别方式 | 移除 Labels 是否有效 |
|:---|:---|:---|
| **Deployment / ReplicaSet** | Label Selector | ✅ 有效 - Pod 变成"隐形" |
| **StatefulSet** | Pod 名称模式 (`{name}-{ordinal}`) | ❌ 无效 - 必须修改名称 |

**问题场景**: 当 ResourceSync 在目标集群恢复了一个 `replicas: 0` 的 StatefulSet `e2e-nginx`，DataSync 随后恢复名为 `e2e-nginx-0` 的 Pod。即使移除了所有 Labels，StatefulSet 控制器仍会通过名称模式识别并删除该 Pod。

**解决方案**: 使用 `generateName` 重命名 Pod 为 `trafficless-sync-xxxxx`，使其脱离 StatefulSet 的管理范围。
### 2.2 Server API 需求
| 方法 | 路径 | 描述 |
| :--- | :--- | :--- |
| `GET` | `/api/v1/instances` | 分页查询。支持按 `config_name` 或 `status` 过滤。 |
| `POST` | `/api/v1/instances` | 创建实例。输入：Namespace 列表, Config Name。 |
| `GET` | `/api/v1/instances/{name}` | 获取详情。使用 `name` 作为标识符。 |
| `GET` | `/api/v1/instances/{name}/sync-status` | 获取详细同步状态 (UI Tab 2)。 |
| `GET` | `/api/v1/instances/{name}/history` | 获取同步历史 (UI Tab 1 底部)。 |
| `GET` | `/api/v1/instances/{name}/topology` | 获取拓扑数据 (UI Tab 3)。 |

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
> **UI 描述**: 提供一键切换 (Failover)、回切 (Failback) 以及沙箱演练 (Drill)。

### 4.1 核心功能实现
| 功能点 | 实现方式 (V2 Backend) |
| :--- | :--- |
| **故障切换 (Failover)** | `DisasterOperation` (type=failover)。工作流: Pause -> ScaleDown Source -> FinalSync -> ScaleUp Target -> Switch Roles。 |
| **故障恢复 (Failback)** | `DisasterOperation` (type=failback)。反向工作流。 |
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
