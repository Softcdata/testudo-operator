# 灾备 API v1 类型说明

该目录保存灾备 Operator 使用的全部 Kubernetes 自定义资源定义（CRD）及其注册逻辑。修改 `pkg/apis/disaster/v1` 下的任意类型后，都需要重新生成客户端、Informer 与控制器代码。

## 文件速览

| 文件 | 作用 |
| --- | --- |
| `groupversion_info.go` | 声明 `testudo.softcdata.com/v1` 的 `GroupVersion`，并提供 `SchemeBuilder` 以便控制器和生成代码统一注册类型。 |
| `register.go` | 封装 `GroupResource` 查询辅助函数，供构建 Informer/Reconciler 使用。 |
| `zz_generated.deepcopy.go` | 自动生成的深拷贝实现，满足 Kubernetes 运行时需求，禁止手动编辑。 |
| `appbackup_types.go` | 定义 `AppBackup` CRD，用于描述基于 Velero `BackupSpec` 的计划任务。 |
| `backuppolicy_types.go` | 定义可复用的备份策略（Cron 表达式、TTL、开始时间等）。 |
| `cluster_types.go` | 集群级资源，注册受管 Kubernetes 集群及连接信息。 |
| `disasterbackup_types.go` | 追踪命名空间级灾备需求、资源选择与执行阶段。 |
| `disasterconfig_types.go` | 保存长期灾备拓扑：源/目标集群、存储仓库、同步节奏。 |
| `disasterjob_types.go` | 表示一次灾备任务实例（正向/反向、一次或周期）。 |
| `storagerepository_types.go` | 管理 Velero 备份所需的对象存储凭据与基本信息。 |

## 主要 CRD 概览

### AppBackup (`appbackup_types.go`)
- `Spec`：绑定目标 `Cluster`、Velero `BackupSpec`、Cron `Schedule`，并提供暂停/跳过、`UseOwnerReferencesInBackup` 等选项。
- `Status`：复用 Velero 的 `ScheduleStatus`、`BackupStatus`，并提供简明 `Status` 字段（Pending/Ready/Backuping…）。

### BackupPolicy (`backuppolicy_types.go`)
- 封装通用保留策略：Cron `Schedule`、可选 `TTL` 与 `StartTime`，便于其他资源引用。

### Cluster (`cluster_types.go`)
- 集群范围资源，用来登记远端集群。`Spec` 支持 Token 或 `KubeConfig`。`Status` 提供生命周期、API `Endpoint`、K8s/Velero 版本等，并通过 `kubebuilder:printcolumn` 在 `kubectl get` 中展示。

### DisasterBackup (`disasterbackup_types.go`)
- `Spec` 将 `DisasterConfig`、原命名空间与可选新命名空间/存储关联起来，可通过 LabelSelector/Workload 列表控制备份对象。
- `Status` 维护标准 `Conditions`、阶段 `Phase`，以及 `Resources`/`Workload` 映射，方便界面展示已保护的资源。

### DisasterConfig (`disasterconfig_types.go`)
- 集群级灾备配置：源/目标集群、`StorageRepository`、同步类型与间隔。`Status` 记录整体可用性与原因。

### DisasterJob (`disasterjob_types.go`)
- 指向某个 `DisasterBackup` 的执行实例，可声明 `once` 或 `cron`，`SyncType` 标识方向（forward/reverse）。
- `Status` 包含 `Conditions`、Phase、失败原因与时间戳，且通过 `disasterjob.disaster.io/finalizer` 防止资源被提前清理。

### StorageRepository (`storagerepository_types.go`)
- 描述 Velero 使用的对象存储（类型、Bucket、Endpoint、凭据等）。`Status` 以可用/不可用区分，供上层流程快速判断。

## 代码生成流程

修改任意类型后需重新生成相关产物：

```bash
make generate
make manifests
```

上述命令会刷新 `zz_generated.deepcopy.go`、`config/crd` 下的 CRD YAML 以及客户端代码。请同时提交类型变更与生成结果，确保 API 表面保持一致。

