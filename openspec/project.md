# 项目上下文

## 1. 项目定位
`disaster-operator` 是容灾系统的控制平面执行引擎，负责将上层 API 请求转化为 Kubernetes 资源编排动作，并通过 Velero + S3 完成备份/恢复与跨集群同步。

该仓库是三层系统中的下层执行端：
- `cluster-disaster-web`（前端 UI）负责页面交互与可视化。
- `disaster-server`（Hertz API）负责 REST API、聚合查询和 WebSocket 推送。
- `disaster-operator`（本仓库）负责 CRD Reconcile、状态机推进、跨集群操作落地。

关联仓库（当前工作区）：
- `/home/chenxi/YS/disaster-operator`
- `/home/chenxi/YS/disaster-server`
- `/home/chenxi/YS/cluster-disaster-web`
- `/home/chenxi/YS/disaster-system-chart`

## 2. 技术栈与运行时
- 语言与版本：Go `1.24.5`
- Operator 基础：Kubebuilder（v4 layout），controller-runtime `v0.21.0`
- Kubernetes 依赖：`k8s.io/api`/`client-go` `v0.34.1`
- 备份引擎：Velero `v1.17.0`
- 对象存储：AWS SDK v2（兼容 S3/MinIO）
- 调度组件：`go-co-op/gocron/v2`（DataSync/ResourceSync 内置 cron）
- 测试框架：Ginkgo v2 + Gomega + envtest + e2e(kind)

## 3. 系统边界与职责分工
### 3.1 本仓库负责
- 定义并维护 `testudo.softcdata.com/v1` CRD 体系。
- Reconcile 各类容灾资源，推进状态机与操作步骤。
- 连接远端集群（KubeConfig 或 Token+Endpoint）并执行跨集群动作。
- 管理 Velero `Backup/Restore/Schedule/BSL/DeleteBackupRequest` 等对象。
- 维护结构化任务事件、统计 CR、依赖标签、删除保护与清理流程。

### 3.2 不在本仓库负责
- 前端页面、鉴权登录交互。
- 对外 REST API 路由与聚合接口（由 `disaster-server` 负责；例如统计路由 `backuprestorestatistics.testudo.softcdata.com/v1/...`）。
- Helm 一键安装编排（由 `disaster-system-chart` 负责）。

## 4. 核心架构模式
### 4.1 双层能力模型
- V2 编排链路（主干）：`DisasterInstance` + `DataSync` + `ResourceSync` + `DisasterOperation` + `DisasterGroup` + `DisasterDrill`
- V1/V1.5 基础链路（兼容）：`AppBackup`、`AppRestore`、`DisasterBackup`、`DisasterJob`、`DisasterConfig`、`Cluster`、`StorageRepository` 等

### 4.2 Reconcile 设计
- Level-triggered、幂等优先，持续收敛到期望状态。
- Status/Metadata 分离更新（推荐 Split Update Pattern）。
- 删除路径强制使用 Finalizer，跨命名空间级联删除通过显式清理完成（不依赖 OwnerReference）。

### 4.3 启动期机制
- `SyncScheduler` 启动后统一托管 DataSync/ResourceSync 的 cron 触发。
- `dependency-backfill` 在 Manager 启动时执行一次历史资源依赖标签回填（默认开启 `--dependency-backfill-on-start=true`，且仅 leader 执行）。

## 5. CRD 与控制器清单（以代码为准）

| 资源 | Scope | 主控制器 | 作用 |
| --- | --- | --- | --- |
| `AppBackup` | Namespaced | `internal/controller/appbackup` | 应用备份、调度、手动动作（Backup/Retry/Cancel/Delete） |
| `AppRestore` | Namespaced | `internal/controller/apprestore` | 应用恢复、跨集群恢复、恢复资源动态修改 |
| `BackupPolicy` | Namespaced | `backuppolicy_controller.go` | 备份策略 CRD（当前控制器为脚手架占位） |
| `BackupRestoreStatistics` | Namespaced | 由 AppBackup/AppRestore/Operation 同步 | 备份/恢复/操作统计聚合数据 |
| `Cluster` | Cluster | `cluster_controller.go` | 受管集群注册、连通性检查、Velero 安装与探测 |
| `DataSync` | Namespaced | `internal/controller/datasync` | 数据同步（Trafficless Restore + AppBackup/AppRestore 组合） |
| `DisasterBackup` | Namespaced | `disasterbackup_controller.go` | 命名空间资源枚举与快照索引（兼容链路） |
| `DisasterConfig` | Cluster | `disasterconfig_controller.go` | 主备集群+存储+策略基线配置 |
| `DisasterDrill` | Namespaced | `internal/controller/disasterdrill` | 演练两阶段流程与清理流程 |
| `DisasterGroup` | Namespaced | `internal/controller/disastergroup` | 多实例分层编排（Level 串行、层内并行） |
| `DisasterInstance` | Namespaced | `internal/controller/disasterinstance` | 容灾实例状态机，托管 DataSync/ResourceSync |
| `DisasterJob` | Namespaced | `disasterjob_controller.go` | 历史同步任务链路（兼容） |
| `DisasterOperation` | Namespaced | `internal/controller/disasteroperation` | failover/reprotect/undo/cancel/pause/resume/sync/drill 编排 |
| `DisasterPolicy` | Namespaced | `disasterpolicy_controller.go` | DataSync/ResourceSync/AutoBackup 策略定义 |
| `ResourceSync` | Namespaced | `internal/controller/resourcesync` | 资源同步（资源骨架 + 副本数记录/恢复） |
| `StorageRepository` | Namespaced | `storagerepository_controller.go` | S3 仓库校验、容量统计、BSL 凭据来源 |

说明：
- `config/crd/bases/` 下保留单一 `backuprestorestatistics` CRD 产物，实际 CRD plural 为 `backuprestorestatisticses`。
- `cmd/main.go` 已注册上述控制器（包括 V1/V2 并存）。

## 6. V2 编排关键流程
### 6.1 实例初始化
1. 创建 `DisasterInstance`，进入 `Pending`。
2. 控制器创建/对齐 `DataSync` 和 `ResourceSync`。
3. 两者完成首轮同步后，实例进入 `Protected`。

### 6.2 故障切换（Failover）
`DisasterOperation(type=failover)` 按步骤执行：
1. `PreCheck`
2. `PauseSchedules`
3. `FinalSync`（先同步，后缩容，避免 FSB 无法读取 PVC）
4. `ScaleDownSource`
5. `ScaleUpTarget`
6. `CheckReplicas`
7. `SwitchRoles`

可选行为：
- `skipFinalSync`
- `skipScaleDownSource`
- `waitUntilReady`
- `timeoutMinutes`
- `retryPolicy`

### 6.3 演练（Drill）
- `DisasterDrill` 采用两阶段：`Pending -> Ready(待确认) -> Executing -> Completed/Failed`。
- 支持实例演练与容灾组演练。
- 支持 `namespaceMapping` 与 `cleanup`（清理完成后 `CleanedUp`）。

## 7. 存储与 Velero 集成规则
### 7.1 BSL 命名策略
BSL 名称采用解耦规则：`<StorageRepository>-<Cluster>`，避免多集群同名仓库冲突。

### 7.2 BSL 应用入口
- `internal/controller/BSL.go` 统一维护 Secret + BackupStorageLocation。
- `DisasterConfig`、`AppBackup`、`AppRestore`、Cluster ensure-storage 信号共用该逻辑。

### 7.3 远端集群连接
- 优先 `spec.kubeConfig`。
- 其次 `spec.token + spec.endpoint`（支持 Base64 Token 自动解码）。
- 工具位于 `pkg/tools/kubeconfig.go`。

## 8. 事件、标签与可观测性约定
### 8.1 结构化任务事件
- 统一使用 `pkg/helper/event_reporter.go` 发射。
- 任务事件统一标签：`testudo.softcdata.com/task-event: "true"`。
- 事件消息采用结构化 JSON（Started/Progress/Finished）。

### 8.2 Trace 与审计注解
- `testudo.softcdata.com/trace-id`
- `testudo.softcdata.com/last-trace-id`
- `testudo.softcdata.com/user`

### 8.3 依赖与清理标签
- `testudo.softcdata.com/dependency-token`
- `testudo.softcdata.com/dependency-to-*`
- `testudo.softcdata.com/cleanup-*`

依赖标签工具位于 `pkg/metadata/*`，启动时由 backfill 任务补齐存量资源。

## 9. 目录与代码组织
- `cmd/main.go`：Manager 启动与控制器注册。
- `internal/controller/`：控制器实现（V1/V2 分模块）。
- `internal/dependencybackfill/`：依赖标签回填。
- `pkg/apis/disaster/v1/`：CRD Go 类型定义。
- `pkg/helper/`：统计、事件、JWT 等辅助能力。
- `pkg/metadata/`：标签/依赖/清理策略公共库。
- `config/`：CRD、RBAC、manager、kustomize overlays。
- `docs/`：架构说明、部署指南、故障排查、流程设计。
- `openspec/`：规范、变更提案、项目约定。

## 10. 构建、测试与部署约定
常用命令：
- `make generate`：生成 deepcopy + clientset/lister/informer。
- `make manifests`：生成 CRD 与 RBAC。
- `make test`：单元/集成测试（envtest）。
- `make test-e2e`：e2e（kind）。
- `make lint`：golangci-lint。
- `make deploy IMG=<image>`：部署控制器。
- `make build-installer`：生成 `dist/install.yaml`。

部署模型：
- 默认 kustomize 命名空间：`disaster-system`（`config/default/kustomization.yaml`）。
- 生产 overlay 命名空间：`disaster-operator-system`（`config/overlays/production`）。
- manager 默认 health probe 端口 `:8081`，生产 patch 为 `:8090`。

## 11. 工程约束与已知现状
- `BackupPolicyReconciler` 当前仍是脚手架 TODO，策略主逻辑集中在 `DisasterPolicy`。
- V1 与 V2 资源并存，修改时需明确目标链路，避免误改兼容逻辑。
- 删除保护策略经历过阶段性调整，部分“阻塞删除”逻辑被注释为 legacy（见 `DisasterConfig`/`DisasterPolicy` 控制器），新逻辑以依赖标签与案例化规则演进。

## 12. OpenSpec 协作约定
- 在执行实现前，先检查：
  - `openspec/project.md`
  - `openspec/specs/*`
  - `openspec/changes/*`
- 涉及新能力、架构变化、破坏性修改时，优先走提案流程。
- 提案通过前不应开始实施代码改动。
- 使用 `openspec validate --strict` 保证规范一致性。

当前已落地能力（示例）：
- `app-backup`
- `app-backup-lifecycle`
- `app-restore`
- `backup-restore-statistics`
- `cluster`
- `drill`
- `global-events`
- `operations-stats-filter`

## 13. 文档编写规范（Mermaid）
- 节点文本含特殊字符时，使用双引号包裹。
- `graph/flowchart` 不使用 sequence diagram 的 `Note` 语法。
- `stateDiagram-v2` 中禁止 `classDef/class/style`，禁止复合状态别名语法。
- 状态转换书写格式：`StateA --> StateB : Description`。
- 状态描述文本避免使用冒号 `:`，使用连字符或其他符号替代。

## 14. 语言与沟通规范
- 对话与 OpenSpec 文档统一使用中文。
- 专业术语、代码标识、关键字可保留英文原文。
