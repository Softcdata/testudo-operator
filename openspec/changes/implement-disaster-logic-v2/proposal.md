# Proposal: V2.0 业务逻辑实现与 Server 接口开发

## Why (背景与目标)

`implement-disaster-orchestration-v2` 提案已完成 V2 编排系统的核心 Operator 框架（CRD, 状态机, 基础调度, Failover 流程）。
当前系统的状态：
- **Operator**: 核心框架就绪，但缺失高级特性（NFS/External 模式, Drill 演练, 完整的 Group 编排）。
- **Server**: 完全不支持 V2 CRD，无法进行前端展示和操作。

**本提案的目标**是完成 Operator 侧剩余的业务逻辑，并**重点实现 Server 端的 V2 接口**，打通从 UI 到 Operator 的完整链路。

## What Changes (变更内容)

### 1. Operator 侧功能补全
*   **DataSync 增强**:
    *   支持 `SharedStorage` (仅占位) 和 `External` (手动确认) 模式。
    *   实现 `NFS` 存储后端的连通性检测 (`NFS Monitor`)。
*   **DisasterOperation 增强**:
    *   实现 `Drill` (演练) 模式：Clone 命名空间，修改 Service 类型。
    *   实现 `WaitingConfirmation` 状态处理 (用于 External 模式的 FinalSync 阶段)。
*   **DisasterGroup**:
    *   确保 Operation Controller 支持 Group 级别的级联操作。

### 2. Server 侧接口开发 (新实现)
Server 端需要基于 `disaster-operator` 的 CRD 构建全新的管理接口。

#### 2.1 数据模型 (DTO)
*   定义 `DisasterInstanceV2` DTO，映射 CRD Spec/Status。
*   定义 `DisasterGroup` DTO。
*   定义 `Topology` DTO，用于可视化展示 Instance/Cluster/Sync 关系。

#### 2.2 API 接口
*   **Instance 管理**:
    *   `GET /api/v2/instances`: 列表与详情 (聚合 DataSync/ResourceSync 状态)。
    *   `POST /api/v2/instances`: 创建实例 (自动生成 DisasterInstance CR)。
    *   `GET /api/v2/instances/:id/topology`:以此实例为中心的拓扑视图。
*   **Group 管理**:
    *   `GET /api/v2/groups`: 组列表。
    *   `POST /api/v2/groups`: 创建容灾组。
*   **操作 (Operation)**:
    *   `POST /api/v2/operations`: 提交 Failover/Reprotect/Drill/Pause/Resume 请求 (创建 DisasterOperation CR)。
    *   `POST /api/v2/operations/:id/confirm`: 确认外部存储同步完成 (Patch Operation CR)。

#### 2.3 基础设施
*   **Kube Client**: 确保 Server 能正确 Watch/List V2 CRD。
*   **WebSocket**: 推送 Operation 步骤进度和 Instance 状态变更。

## Impact (影响范围)

*   **disaster-operator**: `internal/controller` 逻辑增强。
*   **disaster-server**: 新增 `internal/apis/v2` 模块，新增 DTOs。

## 任务拆解 (Work Breakdown)

1.  **Operator 补全**: 完成 NFS/External/Drill 逻辑。
2.  **Server 基础**: 定义 DTO，配置 K8s Client。
3.  **Server 业务**: 实现 Instance/Group 的 CRUD 接口。
4.  **Server 操作**: 实现 Failover 等操作的触发与状态映射。

