# Design: Cluster Typed Namespace Refresh Signal

## 关键决策
- 首期选择单键 one-shot typed signal：`testudo.softcdata.com/refresh-cluster-stats=<type>`，而不是长期驻留字段。
- `Cluster` controller 的 update 事件入口必须显式放行 `testudo.softcdata.com/refresh-cluster-stats` 值变化。当前 `IgnoreStatusUpdatesPredicate` 只接受 Generation 变化，不满足 typed refresh signal 需求，因此实现必须调整该 update 判定，使 annotation-only 的 signal 写入在 Generation 不变时仍然立即入队。
- typed refresh signal 不得依赖现有 `RequeueAfter: 1 * time.Minute` 才被处理。
- typed signal 采用终态清理语义：
  - 统计写回成功：清理 signal。
  - `type` 不受支持：判定为终态非法输入并清理 signal。
  - 远端列表失败、状态写回失败、标签写回失败：保留 signal，返回错误，等待后续调谐继续处理同一 `type`。
- 刷新动作只负责“立即重算指定统计”，不额外定义复杂作业状态机。
- workload namespace 统计使用 `Deployment/StatefulSet` 作为“running workload namespace”判定源，但 `workloadNamespaceStats/workloadTotalCount` 本身不是 workload 对象数，而是这些命名空间内的 namespace 级备份资源总数。
- “running workload namespace” 首期定义为：namespace 中至少存在一个 `Deployment` 满足 `status.readyReplicas > 0` 或 `status.availableReplicas > 0`，或至少存在一个 `StatefulSet` 满足 `status.readyReplicas > 0`。
- `namespaceCount/namespaceStats/resourceTotalCount/workloadNamespaceStats/workloadTotalCount` 必须统一采用同一套 namespace 级备份统计口径。
- 该统计口径以平台默认下发给 Velero 的 namespace 级 ResourceSync 备份为准：排除 `velero`、`kube-system` 命名空间；不统计 cluster-scoped 资源；排除 `pods`、`persistentvolumeclaims`、`persistentvolumes` 以及 `events`、`leases`、`endpoints`、`endpointslices`、`controllerrevisions` 等运行时/派生资源。
- `workloadNamespaceStats` 为上述统计口径在 running workload 命名空间子集上的投影；`workloadTotalCount` 为 `workloadNamespaceStats` 的总和。
- `testudo.softcdata.com/refresh-cluster-stats` 属于操作控制信号，不属于用户编辑元数据，必须从 metadata hash 与“编辑集群 Started/Finished”事件判定中排除。
- server 侧 `refresh-namespaces` action 必须保持 signal-only 写入；operator 仅保证排除 `testudo.softcdata.com/refresh-cluster-stats`，不为额外审计 annotation 提供事件降噪兜底。

## 信号路径
1. server 收到 `refresh-namespaces` action，并校验 `type`。
2. server 写入 `Cluster.metadata.annotations["testudo.softcdata.com/refresh-cluster-stats"]=<type>`。
3. `Cluster` controller 在 update 事件入口识别 refresh annotation 值变化；即使 Generation 不变，也必须立即入队并开始 reconcile。
4. operator 根据 `type` 执行以下分支：
  - `namespaceStats`：按 namespace 级备份统计口径重算 `namespaceCount/namespaceStats/resourceTotalCount`
  - `workloadNamespaceStats`：先识别存在 running `Deployment/StatefulSet` 的命名空间，再按同一统计口径重算这些命名空间的 `workloadNamespaceCount/workloadNamespaceStats/workloadTotalCount`
   - `all`：依次执行以上两类统计
5. 对应统计写回成功后，operator 清理 signal 注解并结束本次 one-shot 刷新。
6. `type` 不受支持时，operator 将其判定为终态非法输入，清理 signal 注解并结束本次处理。
7. 远端列表失败、状态写回失败、标签写回失败时，operator 保留 signal 注解并返回错误；后续调谐继续处理同一 `type`。
8. signal 写入、signal 清理都不得进入 metadata hash 与“编辑集群”事件流。
