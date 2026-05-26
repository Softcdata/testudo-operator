# Proposal: V2.0 多集群容灾编排系统

## Why (背景与目标)

当前的 V1 版本 (`AppBackup`/`AppRestore`) 提供了基础的备份和恢复能力，但在面对企业级多集群容灾场景时，存在以下局限性：

1. **缺乏统一的容灾视图**: 用户需要分别管理备份和恢复，无法直观看到"应用 A 正处于从 Cluster-A 到 Cluster-B 的容灾保护中"。
2. **数据与资源耦合**: 在 V1 中，数据(PV)和资源(YAML)通常绑定在一起。但在实际容灾中，数据往往需要高频同步(如 RPO=5min)，而资源配置仅需低频同步(如 RPO=24h)。
3. **编排能力不足**: 缺乏一个顶层控制器来协调源端备份、数据传输和目标端恢复(或预备)的全生命周期。
4. **缺乏状态机**: 无法清晰追踪容灾实例的当前状态（保护中、故障切换中、已切换等）。
5. **操作与实例耦合**: 没有独立的操作资源来审计和管理 Failover/Reprotect 等关键动作。

**V2.0 的目标**是引入**以应用为中心的容灾编排层**，实现：
- 统一的容灾实例管理与状态机
- 数据同步与资源同步的解耦与差异化策略
- 支持多种数据同步模式：S3复制、NFS共享仓库、共享存储直连、外部存储触发
- 引入容灾组 (`DisasterGroup`) 实现批量应用的有序切换
- 独立的操作资源用于 Failover/Reprotect/Undo/Drill

---

## What Changes (变更内容)

我们将引入 **6 个新的 CRD** 来构建 V2 编排层。

### 1. 改造现有 CRD: `DisasterConfig` (容灾配置)
- **定位**: 配置模板，可被多个 `DisasterInstance` 引用。
- **改造内容**: 
  - 移除内联的同步间隔字段，改为引用 `DisasterPolicy`。
  - 新增 `dataSyncType` 及其相关配置。

```yaml
apiVersion: testudo.softcdata.com/v1
kind: DisasterConfig
metadata:
  name: prod-to-dr
spec:
  sourceCluster: cluster-prod        # 引用 Cluster CR
  targetCluster: cluster-dr
  storageRepository: s3-backup       # 引用 StorageRepository CR
  
  # 引用 DisasterPolicy
  dataSyncPolicy: data-sync-every-15min
  resourceSyncPolicy: resource-sync-daily
  
  # 数据同步类型: fsb-s3 / fsb-nfs / shared-storage / external
  dataSyncType: fsb-nfs
  
  # NFS 特有配置 (当 dataSyncType=fsb-nfs 时必填)
  nfsOptions:
    server: "192.168.1.100"
    path: "/data/backup-repo"
```

### 2. 新增 CRD: `DisasterInstance` (容灾实例)
- **定位**: 顶层编排对象，面向用户。
- **职责**: 定义保护对象（命名空间/Label Selector）、引用配置、管理状态机。
- **行为**: 自动创建并管理下层的 `DataSync` 和 `ResourceSync` 资源。

```yaml
apiVersion: testudo.softcdata.com/v1
kind: DisasterInstance
metadata:
  name: my-app-dr
spec:
  config: prod-to-dr                 # 引用 DisasterConfig
  namespaces: [app1, app2]           # 保护的命名空间
  podRestoreMethod: replica          # replica / initContainer
status:
  fsmState: Protected                # 状态机状态
  primaryCluster: cluster-prod
  secondaryCluster: cluster-dr
  availableOperations: [failover, pause, synconce]
```

### 3. 新增 CRD: `DisasterGroup` (容灾组)
- **定位**: 批量管理与编排对象。
- **职责**: 定义多个 `DisasterInstance` 的分组及切换顺序（Level）。
- **执行逻辑**: Level 1 并行执行 -> 成功 -> Level 2 并行执行...

```yaml
apiVersion: testudo.softcdata.com/v1
kind: DisasterGroup
metadata:
  name: finance-system-dr
  annotations:
    testudo.softcdata.com/description: "核心交易系统容灾组"
spec:
  # 分层编排
  levels:
  - ["db-instance-dr"]               # Level 1: 数据库层优先级最高
  - ["backend-instance-dr"]          # Level 2: 后端层
  - ["frontend-instance-dr"]         # Level 3: 前端层
  
  # 策略配置 (新增)
  policy:
    failPolicy: "Stop"               # 失败处理: Continue / Stop
    timeoutMin: 30                   # 超时时间(分钟)
    parallelism: 5                   # 最大并行数
```

### 4. 新增 CRD: `DataSync` (数据同步)
- **定位**: 中间层控制器，由 `DisasterInstance` 管理。
- **职责**: 专注于 PVC 数据同步。
- **策略支持**:
  - **FSB-S3/NFS**: 启用 `DefaultVolumesToFsBackup=true`，使用 Velero Kopia 引擎。恢复使用 **Trafficless Restore** (隐形 Pod) 方案。
  - **Shared Storage**: 禁用数据备份，仅作状态占位。
  - **External**: 禁用定时任务，Failover 时进入 `WaitingConfirmation` 状态。

```yaml
apiVersion: testudo.softcdata.com/v1
kind: DataSync
metadata:
  name: dr-ds-my-app-dr
spec:
  instance: my-app-dr
  trigger:
    schedule: "*/15 * * * *"
  # Trafficless Restore 配置 (用于 FSB 模式)
  trafficlessConfig:
    image: "busybox:latest"
    command: ["sleep", "3600"]
    removeLabels: ["app", "app.kubernetes.io/name"]
```

### 5. 新增 CRD: `ResourceSync` (资源同步)
- **定位**: 中间层控制器，由 `DisasterInstance` 管理。
- **职责**: 构建**资源骨架**，使用 **Scale-to-Zero** 方案 (Deployment replicas=0)。

```yaml
apiVersion: testudo.softcdata.com/v1
kind: ResourceSync
metadata:
  name: dr-rs-my-app-dr
spec:
  instance: my-app-dr
  standbyModifier:
    scaleToZero: true
```

### 6. 新增 CRD: `DisasterOperation` (容灾操作)
- **定位**: 审计级操作对象。
- **职责**: 执行 Failover、Reprotect (Reverse Sync)、Undo、Drill (演练)、Pause、Resume。
- **流程**: Check -> Pause Schedules -> ScaleDown Source -> FinalSync (with Confirmation) -> ScaleUp Target -> Switch Roles。

```yaml
apiVersion: testudo.softcdata.com/v1
kind: DisasterOperation
metadata:
  name: failover-20260106
spec:
  instanceName: my-app-dr   # 或 groupName
  operationType: failover
  
  # 流程控制指令
  directives:
  - phase: execute
    confirmed: true         # 外部存储切换确认
```

---

## 架构图

```
                           ┌─────────────────────┐
                           │   DisasterConfig    │
                           │(S3/NFS, SyncTypes)  │
                           └──────────┬──────────┘
                                      │ ref
                                      ▼
                      ┌────────────────────────────────┐
                      │        DisasterGroup           │
                      │ Levels: [ [L1], [L2]...]       │
                      └───────────────┬────────────────┘
                                      │ manages
                                      ▼
┌─────────────────────────────────────────────────────────────────┐
│                      DisasterInstance                            │
│  State: Protected / FailingOver / Active / Paused               │
│  Operations: Failover / Reprotect / Undo / Drill / Pause        │
└──────────────────────────┬────────────────────────┬─────────────┘
                           │                        │
              owns/manages │                        │ owns/manages
                           ▼                        ▼
              ┌────────────────────┐   ┌────────────────────┐
              │     DataSync       │   │   ResourceSync     │
              │  (FSB-S3/NFS)      │   │  (Scale-to-Zero)   │
              │  (External Confirm)│   └─────────┬──────────┘
              └─────────┬──────────┘             │
                        │                        ▼
               AppBackup/AppRestore      AppBackup/AppRestore
               (SnapshotVolumes=true)    (SnapshotVolumes=false)
```

## 关键设计决策

### 1. 多模式数据同步
针对不同用户场景提供差异化实现：
- **FSB-NFS**: 使用 NFS 作为 Kopia 仓库，兼顾备份历史与共享存储架构，提供 RPO 保护。
- **External**: 承认 Operator 在某些场景下无法控制底层数据，转为流程编排者 (Orchestrator)。

### 2. 容灾组与分层编排
通过 `DisasterGroup` 和 Level 机制，解决微服务架构下应用启动依赖问题（如 DB -> Middleware -> App）。

### 3. Server API 扩展
- 支持手动触发同步 (`sync-data`, `sync-resource`)
- 支持操作确认 (`verify-nfs`, `confirm-operation`)
- 支持拓扑可视化数据查询

### 4. 稳健的资源副本管理 (Robust Replica Management)
为了解决 `Scale-to-Zero` 过程中无法可靠保存原始副本数的问题（如源资源缺失 Annotations 导致 JSON Patch copy 操作失败），采取以下策略：
- **Record (记录)**: `ResourceSync` 控制器在触发备份前，扫描源集群 Workload (Deployment/StatefulSet)，将原始副本数记录到管理集群的 ConfigMap 中 (`replicas-<rs-name>`)。
- **Zero (置零)**: 使用 JSON Patch 的 `add` 操作强制将目标集群资源的 `spec.replicas` 置为 0。`add` 操作具备天然的 upsert 属性，无论源字段是否存在均能成功，解决了 `replace` 操作的脆弱性。
- **ScaleUp (恢复)**: Failover 流程中的 `ScaleUpTarget` 步骤不再依赖目标资源的 Annotations，而是直接读取 ConfigMap 中的记录，精确恢复副本数。

### 5. Intermediate State Handling (Undo & Retry)
为解决 Failover 失败（如超时）导致的中间状态僵死问题：
- **Intermediate State**: Failover 启动即进入 `FailingOver` 状态。
- **Failure Handling**: 失败时保持 `FailingOver` (而非回滚到 Protected)，这允许管理员诊断现场。
- **Undo Operation**: 允许在 `FailingOver` 状态下执行 Undo，用于放弃切换并恢复源端（ScaleUp Source）。
- **Retry Mechanism**: 允许在 `FailingOver` 状态下创建新的 Failover 操作以接管/重试流程。

---

## 关键里程碑

| 阶段 | 内容 | 优先级 |
|------|------|--------|
| P0 | 实现 6 个 CRD 结构定义 | 必须 |
| P0 | 实现 DataSync (含 FSB-NFS/External 逻辑) | 必须 |
| P0 | 实现 ResourceSync (Scale-to-Zero) | 必须 |
| P0 | 实现 DisasterInstance 状态机 | 必须 |
| P1 | 实现 DisasterGroup 分层编排 Controller | 高 |
| P1 | 实现 DisasterOperation (含 Drill/Failover) | 高 |
| P1 | Server API 适配与验证接口 | 高 |
