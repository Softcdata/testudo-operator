# disaster-orchestration-v2 功能与实现逻辑概览

本文档详细描述了 V2 版本容灾编排系统的核心功能、用户动作以及底层的技术实现逻辑。

## 1. 核心设计理念

V2 版本采用 **Pilot Light (长明火)** 架构，旨在实现数据与资源的解耦同步，提供应用级的容灾视图和状态机管理。

*   **以应用为中心**: 通过 `DisasterInstance` 提供统一的容灾视图。
*   **数据/资源解耦**: 数据高频同步 (DataSync)，资源低频同步 (ResourceSync)。
*   **冷备模式 (Cold Standby)**: 备集群资源存在但 Replicas=0，数据持续注入 PVC，切换时秒级拉起。
*   **操作审计**: 所有运维动作 (Failover/Reprotect/Undo) 通过独立 CRD 记录和执行。

---

## 2. 功能模块与实现逻辑

### 2.1 容灾实例管理 (DisasterInstance)

**功能描述**:
用户创建一个 `DisasterInstance` CR，系统自动完成从源集群到目标集群的容灾初始化，并进入持续保护状态。

**实现逻辑**:
*   **控制器**: `DisasterInstanceController`
*   **状态机 (FSM)**:
    *   `Pending` -> `Initializing`: 自动创建下层的 `DataSync` 和 `ResourceSync` CR。
    *   `Initializing` -> `Protected`: 监听下层 CR 的状态，当首次全量同步完成后流转为受保护状态。
*   **级联管理**: 利用 OwnerReference 机制，`DisasterInstance` 是 `DataSync` 和 `ResourceSync` 的 Owner。

### 2.2 无流量数据同步 (DataSync - Trafficless Restore)

**功能描述**:
在不影响备集群（即使有名为同名的 Service 存在）的情况下，持续将源端的 PVC 数据注入到备集群的 PVC 中。
支持 4 种同步模式，适配不同的网络和存储架构。

**3.2.1 核心数据同步策略**:

1.  **FSB-S3 (文件复制-S3)**:
    *   **架构**: Source -> S3 Object Store -> Target。
    *   **机制**: Velero FSB (Kopia) 引擎，跨网络复制数据。
    *   **配置**: `DefaultVolumesToFsBackup=true`, `SnapshotVolumes=false`。
3.  **Shared Storage (共享存储直连)**:
    *   **架构**: Source PVC -> NFS Server <- Target PVC。
    *   **机制**: **无数据同步**。主备 Pod 挂载同一个物理 NFS 目录。
    *   **配置**: `DefaultVolumesToFsBackup=false`, `SnapshotVolumes=false`。DataSync 实际上空转，仅用于状态报告。
4.  **External (外部触发)**:
    *   **架构**: 依赖底层存储硬件复制 (如 NetApp SnapMirror)。
    *   **机制**: Operator 在 Failover 阶段**暂停等待**，由管理员确认底层数据就绪后手动放行。

**3.2.2 Trafficless Restore 流程**:
(针对 FSB-S3 和 FSB-NFS)
1.  **Backup**: 对源端执行 Velero FSB 备份 (SnapshotVolumes=true)。
2.  **Restore**: 在目标端执行恢复，但应用特殊的 **ResourceModifier**：
    *   **移除标签**: 删除 Pod 的所有 Label，确保 Service 无法路由流量到此 Pod。
    *   **移除 OwnerRef**: 防止被目标端可能存在的 StatefulSet/Deployment 管理或删除。
    *   **替换镜像**: 替换为 `busybox`。
    *   **替换命令**: 替换为 `sleep infinity`。
3.  **数据注入**: 这个“隐形 Pod”挂载着目标 PVC，Velero Node Agent 检测到 Pod 启动后，将数据注入 PVC。
4.  **结果**: 数据进入了 PVC，但业务并未真正运行，且对外不可见。

### 2.3 资源骨架同步 (ResourceSync - Scale to Zero)

**功能描述**:
定期同步 K8s 资源配置（Deployment, Service, ConfigMap 等）到备集群，但保持“关机”状态。

**实现逻辑**:
采用 **Standby Modifier** 方案：
1.  **Backup**: 仅备份资源 YAML (SnapshotVolumes=false)，排除 PVC/PV。
2.  **Restore**: 在目标端恢复，应用 **ResourceModifier**：
    *   **保存副本数**: 将 Deployment/STS 的原始 `replicas` 值保存到注解 `testudo.softcdata.com/original-replica-count` 中。
    *   **缩容归零**: 强制将 `spec.replicas` 设置为 `0`。
3.  **结果**: 备集群中存在整套应用架构（Service, Ingress 等），StatfulSet/Deployment 也存在，但没有 Pod 运行 (Cost Saving)。

### 2.4 故障切换 (Failover)

**功能描述**:
当源集群故障时，用户创建 `DisasterOperation` (type=failover)，一键将业务切换到备集群。

**实现逻辑**:
`DisasterOperationController` 执行线性工作流：
1.  **PauseSchedules**: 暂停 `DataSync` 和 `ResourceSync` 的定时调度，防止冲突。
2.  **ScaleDownSource** (可选): 尝试连接源集群并将副本数降为 0，防止脑裂 (Split-brain)。如果源集群失联，支持 `force: true` 跳过。
3.  **FinalSync** (可选): 触发一次最后的数据同步，尽可能减少 RPO。如果同步类型为 `External`，在此处暂停等待 `Confirm` API 调用。
4.  **ScaleUpTarget**: 读取备集群 Deployment/STS 上的 `original-replica-count` 注解，恢复 `spec.replicas` 到原始值。
5.  **SwitchRoles**: 更新 `DisasterInstance` 状态为 `Active`，交换主备集群记录。

---

## 3. 用户操作指南 (动作映射)

| 用户动作 | 对应 CRD 操作 | 后台执行逻辑 |
| :--- | :--- | :--- |
| **开启容灾** | 创建 `DisasterInstance` | 1. 创建 DataSync/ResourceSync<br>2. 触发首次 Initial Sync<br>3. 进入 Protected 状态 |
| **修改策略** | 修改 `DisasterConfig` | 1. Controller 监听到 Config 变化<br>2. 级联更新 DataSync/ResourceSync 的 Schedule |
| **故障切换** | 创建 `DisasterOperation`<br>(type=`failover`) | 1. 暂停同步<br>2. 缩容源端<br>3. 最后同步 (External类型需确认)<br>4. 扩容备端<br>5. 状态变更为 Active |
| **反向保护** | 创建 `DisasterOperation`<br>(type=`reprotect`) | 1. 建立反向同步 (Target->Source)<br>2. 维持 Target 为主集群 (Active)<br>3. Source 保持 Standby (0副本) |
| **撤销切换** | 创建 `DisasterOperation`<br>(type=`undo`) | 1. 缩容当前主 (Target)<br>2. 扩容原主 (Source)<br>3. 恢复正向同步<br>4. 状态变更为 Protected |
| **暂停保护** | 创建 `DisasterOperation`<br>(type=`pause`) | 1. 暂停所有下层 Sync 任务<br>2. 状态变更为 Paused |
| **强制切换** | `DisasterOperation`<br>(force=`true`) | 跳过源端操作和 FinalSync，直接拉起备端 |

---

## 4. 关键 CRD 结构速查

### DisasterInstance (状态机载体)
```yaml
spec:
  config: prod-to-dr
  namespaces: [mysql, redis]
status:
  fsmState: Protected  # 核心状态字段
  availableOperations: [failover, pause]
```

### DataSync (数据搬运工)
```yaml
status:
  trafficlessPods:     # 记录当前的隐形 Pod
  - name: mysql-0
    phase: Running
```

### ResourceSync (资源管家)
```yaml
spec:
  standbyModifier:
    scaleToZero: true  # 开启冷备模式
```

### DisasterOperation (操作指令)
```yaml
spec:
  operationType: failover
  directives:
  - phase: execute
    force: true        # 强制模式
```
