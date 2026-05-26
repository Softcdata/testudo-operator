# Change: 为 Cluster 增加 typed namespace 刷新信号与 workload namespace 统计契约

## Why
当前前端只能通过重新请求列表或详情来“刷新命名空间”，但 operator 并没有正式的一次性刷新契约，因此用户很容易遇到“刷新了但数据还是旧的”。

此外，`Cluster` 当前只有通用 `namespace/resource` 统计，没有“只统计存在 running workload 的命名空间”的派生统计，无法单独表达“哪些命名空间当前有运行中的工作负载，以及这些命名空间内的资源总量”。

因此，条目 17 需要的不是简单前端重试，而是一个 `server -> Cluster -> operator` 的 typed one-shot refresh 通道，以及与之配套的 workload namespace 统计契约。

## What Changes
- `Cluster` 接收一次性 typed refresh 信号，支持 `namespaceStats`、`workloadNamespaceStats`、`all` 三种类型。
- `ClusterReconciler` 收到 typed signal 后必须立即重算指定统计，而不是固定刷新全部状态。
- `Cluster` controller 的 update 事件入口必须显式放行 `testudo.softcdata.com/refresh-cluster-stats` 的 annotation-only 变化，不能继续只按 Generation 变化判定，也不得退化为等待现有 `RequeueAfter: 1 * time.Minute`。
- `Cluster` 现有 `namespaceCount/namespaceStats/resourceTotalCount` 与新增的 workload namespace 统计，统一切换为 namespace 级备份统计口径。
- 上述统计口径以平台默认下发给 Velero 的 namespace 级 ResourceSync 备份为准：排除 `velero`、`kube-system` 命名空间；不统计 cluster-scoped 资源；排除 `pods`、`persistentvolumeclaims`、`persistentvolumes` 以及 `events`、`leases`、`endpoints`、`endpointslices`、`controllerrevisions` 等运行时/派生资源。
- `workloadNamespaceCount` 表示存在 running `Deployment/StatefulSet` 的命名空间数量。
- `workloadNamespaceStats` 与 `workloadTotalCount` 复用上述 namespace 级备份统计口径，但只统计上述命名空间。
- typed signal 的生命周期按终态结果管理：统计写回成功后清理；`type` 不受支持并被判定为终态失败后清理；遇到瞬时错误时保留 signal 进入后续重试。
- refresh signal annotation 必须从 metadata hash 与“编辑集群”事件判定中排除，避免 signal 写入、signal 清理污染任务事件与审计流。

## Non-Goals
- 不在本 proposal 中引入新的长期 action 状态字段。
- 不在本 proposal 中同时完成 `runningNamespaces` 显式列表模型扩展。
- 不在本 proposal 中将 `DaemonSet/Job/CronJob` 等其他 workload kind 纳入“running workload namespace”判定。

## Impact
- Affected specs:
  - `cluster`
- Affected code:
  - `internal/controller/cluster_controller.go`
  - `pkg/apis/disaster/v1/cluster_types.go`
  - `config/crd/bases/testudo.softcdata.com_clusters.yaml`
- Cross-repo impact:
  - `disaster-server`：action request 的 `type` 契约、signal 写入方式、响应结构、Cluster 读取 DTO 对 workload 统计的暴露方式
  - `cluster-disaster-web`：refresh type 选择与加载态
