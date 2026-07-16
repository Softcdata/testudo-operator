# 使用恢复目标集群私有仓库解析 Trafficless busybox 镜像

## 背景

FSB 数据恢复会通过 Trafficless Restore 创建临时 Pod 挂载 PVC，让 Velero node-agent 将文件系统数据写入目标卷。当前实现默认将临时 Pod 的业务容器镜像替换为 `busybox:1.36`，但客户离线环境通常无法从公网拉取该镜像。

平台在添加受管集群时已经支持配置 `Cluster.spec.veleroInstall.imageRegistry` 与 registry 拉取凭据，用于 Velero、node-agent 和插件镜像安装。Trafficless 临时 Pod 也属于平台恢复运行时资源，应复用“本次恢复目标集群”的私有仓库配置。

本问题与业务镜像前缀替换无关。Trafficless Pod 的 busybox 是平台运行时镜像，不是用户业务镜像，不应使用 `Cluster.spec.imageSources`、实例镜像 rewrite/mapping 或资源同步的业务镜像替换规则。

必须特别注意角色反转：DataSync 的恢复目标不是固定的 `DisasterConfig.spec.targetCluster`。failover/reprotect 后，`DisasterInstance.status.primaryCluster/secondaryCluster` 会反转，后续反向同步必须按当前 `secondaryCluster` 解析 busybox 仓库。

## 目标

- FSB Trafficless 临时 Pod 的默认 busybox 镜像必须按“本次恢复目标集群”动态解析。
- 当本次恢复目标集群配置了 `veleroInstall.imageRegistry` 时，默认 busybox 镜像必须从该私有仓库拉取。
- 当目标集群配置了 registry 凭据时，必须确保恢复命名空间存在可用 pull secret，并在 Trafficless Pod 中引用。
- 保留 `DataSync.spec.trafficlessConfig.image/command` 的显式覆盖能力。
- 统一默认 busybox 版本，避免 `busybox:latest` 与 `busybox:1.36` 不一致。
- Drill 数据恢复必须使用演练目标集群解析 busybox，并按 namespaceMapping 后的目标命名空间同步 pull secret。

## 非目标

- 不重新设计 `Cluster.spec.veleroInstall` 字段名。
- 不使用业务镜像前缀替换、`imageSources`、动态镜像改写或 workload 镜像扫描结果来解析 busybox。
- 不要求前端立即新增 Trafficless 镜像字段。
- 不改变 FSB 通过临时 Pod 注入 PVC 数据的总体机制。
- 不在本提案中新增目标 namespace 冲突 Pod 清理策略。
- 不在本提案中修改 Trafficless Pod 的 `serviceAccountName` 或 `automountServiceAccountToken`。
- 不把 DataSync 专用的调度约束清理扩散到 ResourceSync、Drill 或 shared restore builder。

## 影响范围

- `DataSync` 构建 data restore AppRestore 的 Trafficless modifier。
- `DisasterOperation` Drill 数据恢复构建 data restore AppRestore 的 Trafficless modifier。
- 目标集群 registry pull secret 在业务恢复命名空间中的同步。
- CRD/default 文案中 Trafficless 默认镜像的一致性。

## 风险

- 若目标集群配置了 `imageRegistry` 但客户未同步 `<imageRegistry>/busybox:1.36` 镜像，恢复 Pod 会 `ImagePullBackOff`。
- 若 registry 凭据只存在于 `velero` namespace 而未同步到业务命名空间，Trafficless Pod 仍无法拉取私有镜像。
- 若 Drill 使用 namespaceMapping，需要按映射后的目标 namespace 同步 pull secret，否则演练环境拉镜像失败。
- 若实现误用 `DisasterConfig.spec.targetCluster`，failover 后的反向同步会继续使用旧目标集群仓库，导致 busybox 拉取失败。
