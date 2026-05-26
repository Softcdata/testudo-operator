## ADDED Requirements

### Requirement: Cluster 必须支持 typed one-shot namespace refresh signal
系统必须 (MUST) 允许 `Cluster` 接收一次性 typed refresh signal，并按 signal 的 `type` 只重算指定统计。

#### Scenario: 收到 namespaceStats 刷新信号后立即重算通用统计
- **Given** 一个 `Cluster` 被写入注解 `testudo.softcdata.com/refresh-cluster-stats=namespaceStats`
- **When** `ClusterReconciler` 调谐该对象
- **Then** operator 必须立即重算 `status.namespaceCount`、`status.namespaceStats`、`status.resourceTotalCount`
- **And** 上述字段必须统一采用 namespace 级备份统计口径
- **And** 必须同步 label `testudo.softcdata.com/cluster-namespace-count` 与 `testudo.softcdata.com/cluster-resource-total-count`
- **And** 在通用统计写回成功后清理该 signal

#### Scenario: 收到 workloadNamespaceStats 刷新信号后立即重算 workload 统计
- **Given** 一个 `Cluster` 被写入注解 `testudo.softcdata.com/refresh-cluster-stats=workloadNamespaceStats`
- **When** `ClusterReconciler` 调谐该对象
- **Then** operator 必须立即重算 `status.workloadNamespaceCount`、`status.workloadNamespaceStats`、`status.workloadTotalCount`
- **And** `status.workloadNamespaceStats` 与 `status.workloadTotalCount` 必须沿用相同的 namespace 级备份统计口径，只统计 running workload 命名空间子集
- **And** 必须同步 label `testudo.softcdata.com/cluster-workload-namespace-count` 与 `testudo.softcdata.com/cluster-workload-total-count`
- **And** 在 workload 统计写回成功后清理该 signal

#### Scenario: 收到 all 刷新信号后立即重算两类统计
- **Given** 一个 `Cluster` 被写入注解 `testudo.softcdata.com/refresh-cluster-stats=all`
- **When** `ClusterReconciler` 调谐该对象
- **Then** operator 必须在同一次 one-shot 处理中重算通用 namespace 统计与 workload namespace 统计
- **And** 两类统计必须共享同一套 namespace 级备份统计口径
- **And** 在两类统计全部写回成功后清理该 signal

### Requirement: Cluster 必须维护 namespace 级备份统计
系统必须 (MUST) 为 `Cluster` 维护与平台默认 ResourceSync/Velero 一致的 namespace 级备份统计。

#### Scenario: 重算通用 namespace 统计
- **Given** 目标集群存在多个 namespace
- **When** `ClusterReconciler` 重算通用 namespace 统计
- **Then** `status.namespaceCount` 只统计纳入统计口径的非系统 namespace
- **And** `status.namespaceStats` 与 `status.resourceTotalCount` 只统计 namespace 级备份资源
- **And** 不得将 `velero`、`kube-system` 命名空间计入上述三个字段
- **And** 不得将 cluster-scoped 资源计入 `status.namespaceStats` 或 `status.resourceTotalCount`
- **And** 不得将 `pods`、`persistentvolumeclaims`、`persistentvolumes` 以及 `events`、`leases`、`endpoints`、`endpointslices`、`controllerrevisions` 等运行时/派生资源计入 `status.namespaceStats` 或 `status.resourceTotalCount`

### Requirement: Cluster 必须维护 running workload namespace 资源统计
系统必须 (MUST) 为 `Cluster` 维护“存在 running Deployment/StatefulSet 的命名空间”聚合统计，并沿用同一套 namespace 级备份统计输出模型。

#### Scenario: 重算 workload namespace 统计
- **Given** 目标集群存在多个 namespace
- **And** 仅部分 namespace 中存在 ready/available 的 `Deployment` 或 ready 的 `StatefulSet`
- **When** `ClusterReconciler` 重算 workload namespace 统计
- **Then** 必须将存在 running `Deployment/StatefulSet` 的 namespace 数量写入 `status.workloadNamespaceCount`
- **And** 必须将这些 namespace 内、按同一 namespace 级备份统计口径得到的资源总数写入 `status.workloadNamespaceStats`
- **And** 必须将 `status.workloadNamespaceStats` 的总和写入 `status.workloadTotalCount`
- **And** 不得将 cluster-scoped 资源或被该统计口径排除的运行时/派生资源计入 `status.workloadNamespaceStats` 或 `status.workloadTotalCount`
- **And** 必须同步 label `testudo.softcdata.com/cluster-workload-namespace-count` 与 `testudo.softcdata.com/cluster-workload-total-count`

### Requirement: Cluster 必须按终态结果管理 typed refresh signal 生命周期
系统必须 (MUST) 将 typed refresh signal 视为一次性请求，并根据处理结果决定 signal 是否保留。

#### Scenario: 提交不支持的 type
- **Given** 一个 `Cluster` 被写入注解 `testudo.softcdata.com/refresh-cluster-stats=unknownType`
- **When** `ClusterReconciler` 调谐该对象
- **Then** operator 必须将该输入判定为终态非法 `type`
- **And** 不得执行任何统计重算
- **And** 必须清理该 signal

#### Scenario: 刷新过程中出现瞬时错误
- **Given** 一个 `Cluster` 被写入受支持的 typed refresh signal
- **And** operator 在远端列表、状态写回、标签写回阶段出现瞬时错误
- **When** `ClusterReconciler` 调谐该对象
- **Then** operator 必须保留该 signal
- **And** 必须返回错误，使后续调谐继续处理同一 `type`

### Requirement: Cluster controller 必须将 refresh signal annotation-only 更新视为立即调谐事件
系统必须 (MUST) 在 `Cluster` update 事件入口中将 refresh signal 的 annotation-only 更新视为需要立即调谐的变化。

#### Scenario: Generation 不变但 refresh signal 值发生变化
- **Given** 一个 `Cluster` update 事件中，新旧对象的 Generation 相同
- **And** `testudo.softcdata.com/refresh-cluster-stats` 的值发生变化
- **When** `Cluster` controller 处理该 update 事件
- **Then** controller 必须立即入队该 `Cluster`
- **And** 不得等待现有 `RequeueAfter: 1 * time.Minute` 才处理本次 refresh

### Requirement: refresh signal annotation 不得污染编辑集群事件判定
系统必须 (MUST) 将 refresh signal annotation 视为操作控制信号，而不是用户编辑元数据。

#### Scenario: 只写入 refresh signal annotation
- **Given** 一个已存在的 `Cluster`
- **And** 本次 metadata 变化仅涉及 `testudo.softcdata.com/refresh-cluster-stats`
- **When** controller 计算 metadata hash 并判断是否需要发出“编辑集群”任务事件
- **Then** 该 annotation 必须被排除在 metadata hash 之外
- **And** 不得因此发出“编辑集群 Started”事件
- **And** 不得因此发出“编辑集群 Finished”事件

#### Scenario: 只清理 refresh signal annotation
- **Given** 一个已存在的 `Cluster`
- **And** 本次 metadata 变化仅涉及移除 `testudo.softcdata.com/refresh-cluster-stats`
- **When** controller 计算 metadata hash 并判断是否需要发出“编辑集群”任务事件
- **Then** 该 annotation 必须被排除在 metadata hash 之外
- **And** 不得因此发出“编辑集群 Started”事件
- **And** 不得因此发出“编辑集群 Finished”事件
