# 设计：恢复目标集群私有仓库驱动的 Trafficless busybox

## 现状

DataSync 数据恢复当前在 `internal/controller/datasync/controller.go` 中构建 Trafficless ResourceModifier，默认镜像为 `busybox:1.36`，命令为 `["sleep","3600"]`。`DataSync.spec.trafficlessConfig.image/command` 可以覆盖该默认值，但 `DisasterInstance` 创建 DataSync 时未透传该字段，server/web 也没有对应入口。

共享 `restore/builder.go` 仍保留一套 data restore fallback modifier，同样写死 `busybox:1.36`。Drill 数据恢复当前通过 shared builder 构建 data restore，未传入 DataSync 专用的可配置 modifier。

Cluster 侧已经具备：

- `Cluster.spec.veleroInstall.imageRegistry`：客户私有仓库前缀，例如 `harbor.customer.local/disaster`。
- `Cluster.spec.veleroInstall.registryCredentialSecretRef`：管理平面 dockerconfigjson Secret 引用。
- Cluster Reconciler 会将该凭据同步到目标集群 `velero` namespace，供 Velero 安装镜像使用。

## 核心原则

Trafficless Pod 的 busybox 是“本次恢复目标集群上的平台运行时镜像”，不是源集群业务镜像。因此镜像解析必须基于本次 AppRestore 的 `TargetCluster`，而不是：

- 源集群；
- `DisasterConfig.spec.targetCluster` 的静态值；
- DataSync 创建时缓存的值；
- 业务镜像改写 `imageSources`。
- ResourceSync 的业务镜像前缀替换规则。
- 动态镜像改写规则或 workload 镜像扫描结果。

DataSync 必须通过 `resolveClusters(instance, config)` 获取本次方向：

- 初始化时通常是 `sourceCluster -> targetCluster`。
- failover/reprotect 后使用 `DisasterInstance.status.primaryCluster -> secondaryCluster`，即反向同步时 busybox 必须使用当前 `secondaryCluster` 的 `veleroInstall.imageRegistry`。

Drill 数据恢复必须使用 `executeDrillRestoreData` 传入的演练 `targetCluster`，不能使用实例当前主集群 registry。

## 镜像解析优先级

引入 Trafficless runtime resolver，输入为显式 Trafficless 配置、本次恢复 `targetCluster` 与管理平面 client，输出为 `TrafficlessRuntime`：

```go
type TrafficlessRuntime struct {
    Image          string
    Command        []string
    PullSecretName string
    Source         string // explicit | targetClusterRegistry | default
    TargetCluster  string
}
```

解析规则：

1. 若显式 `trafficlessConfig.image` 非空且不是历史默认值，则直接使用该镜像。
2. 否则读取本次恢复目标 `Cluster`。
3. 若目标 `Cluster.spec.veleroInstall.imageRegistry` 非空，则生成 `<imageRegistry>/busybox:1.36`。
4. 否则回退 `busybox:1.36`。

`Command` 继续沿用：

- 显式 `DataSync.spec.trafficlessConfig.command` 优先；
- 否则默认 `["sleep","3600"]`。

历史 CRD 默认 `busybox:latest` 不应阻断目标集群仓库解析：若对象中出现空值、`busybox:latest` 或 `busybox:1.36`，均应视为默认值，允许按目标集群 registry 重新解析。其他非空镜像视为用户显式覆盖。

## Pull Secret 同步

Trafficless Pod 运行在业务恢复 namespace，不在 `velero` namespace。仅同步 `velero` namespace 的 pull secret 不足以让业务 namespace Pod 拉私有镜像。

当目标 `Cluster.spec.veleroInstall.registryCredentialSecretRef` 存在时：

1. 从管理平面读取 dockerconfigjson Secret。
2. 在目标集群每个恢复 namespace 创建或更新同名稳定 pull secret，建议复用 `velero-regcred-<clusterName>` 命名。
3. 在 Trafficless Pod modifier 中注入：

```json
{
  "operation": "add",
  "path": "/spec/imagePullSecrets",
  "value": "[{\"name\":\"velero-regcred-<clusterName>\"}]"
}
```

恢复 namespace 计算规则：

- DataSync：`instance.spec.namespaces`，方向由 `resolveClusters(instance, config)` 解析。
- Drill：若存在 `namespaceMapping`，使用映射后的目标 namespace；否则使用 `instance.spec.namespaces`。
- 后续 failover/reprotect 反向同步仍通过“本次恢复 targetCluster + 本次恢复 namespace”计算，不能回退到静态 `DisasterConfig.spec.targetCluster`。

若无 registry 凭据，只解析私有仓库镜像，不注入 `imagePullSecrets`，以兼容无需认证的内网 registry。

## Trafficless Modifier

本提案只改变 Trafficless Pod 的镜像与 pull secret。现有 Trafficless 隔离语义保持不变：

- 覆盖 labels 为 `{"trafficless":"true"}`，避免 Service 导流。
- 清空 ownerReferences，避免被源控制器/GC 回收。
- 替换 `/spec/containers/0/image`。
- 覆盖 command 并清空 args。

若 runtime resolver 返回 `PullSecretName`，Trafficless modifier 额外注入：

```json
{
  "operation": "add",
  "path": "/spec/imagePullSecrets",
  "value": "[{\"name\":\"velero-regcred-<clusterName>\"}]"
}
```

本提案不新增 `serviceAccountName`、`automountServiceAccountToken`、冲突 Pod 清理等行为；这些属于单独的恢复安全策略。

## DataSync 链路

`DataSyncReconciler.buildAppRestoreSpec` 在构建 data restore AppRestore 前：

1. 通过 `resolveClusters(instance, config)` 获取本次 `targetCluster`。
2. 解析 Trafficless runtime。
3. 同步 registry pull secret 到本次恢复 namespace。
4. 使用 runtime 构建 `DataResourceModifierRules` 并传给 `restore.BuildAppRestoreSpec`。
5. 再应用实例级 restore policy 与 system protect rules。

## Drill 链路

`DisasterOperationReconciler.executeDrillRestoreData` 不再依赖 shared builder 的默认 Trafficless fallback，而应：

1. 使用当前 `targetCluster` 解析 Trafficless runtime。
2. 根据 `namespaceMapping` 计算目标恢复 namespace。
3. 同步 registry pull secret 到目标恢复 namespace。
4. 显式传入 `DataResourceModifierRules`，确保 Drill 不回退到公网 `busybox:1.36`。

## 与现有能力关系

- `Cluster.spec.veleroInstall` 继续表示平台运行时镜像仓库与凭据，Trafficless Pod 复用该语义。
- `Cluster.spec.imageSources` 继续只用于业务镜像改写，不参与 Trafficless runtime 解析。
- `restorePolicy`/资源修改器继续可以处理业务资源，但不得覆盖系统 Trafficless 安全规则。
- ResourceSync 不创建 Trafficless Pod，不参与本次 busybox 解析。

## 验证

至少覆盖以下单元测试：

1. 目标集群配置 `veleroInstall.imageRegistry` 时，DataSync 生成 `<registry>/busybox:1.36`。
2. 源/目标集群均配置 registry 时，使用目标集群 registry。
3. failover 后角色切换，下一次 DataSync 使用新的恢复目标集群 registry。
4. 用户显式配置自定义 Trafficless 镜像时，不被目标 registry 覆盖。
5. 历史 `busybox:latest` 默认值按未配置处理。
6. registry credential 存在时，业务 namespace 中创建 pull secret，并注入 `/spec/imagePullSecrets`。
7. Drill namespaceMapping 场景同步到映射后的目标 namespace。
8. 无 credential 时不注入 imagePullSecrets。
