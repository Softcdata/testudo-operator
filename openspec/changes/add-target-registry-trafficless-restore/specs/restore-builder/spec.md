## ADDED Requirements

### Requirement: Trafficless busybox 必须按本次恢复目标集群解析

DataSync 和 Drill 的 FSB 数据恢复必须 (MUST) 通过 Trafficless 临时 Pod 接收 PodVolumeRestore 写入，并且该临时 Pod 的 busybox 镜像与拉取凭据必须按本次恢复目标集群解析。Trafficless busybox 属于平台恢复运行时镜像，不得使用业务镜像前缀替换、`Cluster.spec.imageSources`、ResourceSync 镜像规则或 workload 镜像扫描结果。

#### Scenario: DataSync 使用目标集群私有仓库
- **Given** 一个 `DisasterInstance` 正在执行 DataSync 数据恢复
- **And** 本次恢复目标集群为 `cluster-b`
- **And** 源集群 `cluster-a` 也配置了不同的 `veleroInstall.imageRegistry`
- **And** `cluster-b.spec.veleroInstall.imageRegistry=harbor.customer.local/disaster`
- **When** Operator 构建 data restore AppRestore
- **Then** Trafficless Pod 的镜像必须为 `harbor.customer.local/disaster/busybox:1.36`
- **And** 不得使用源集群 registry
- **And** 不得使用 `DisasterConfig.spec.targetCluster` 的静态值绕过当前角色方向

#### Scenario: DataSync 角色反转后使用当前 secondaryCluster
- **Given** `DisasterConfig.spec.targetCluster=cluster-b`
- **And** failover/reprotect 后 `DisasterInstance.status.primaryCluster=cluster-b`
- **And** `DisasterInstance.status.secondaryCluster=cluster-a`
- **And** `cluster-a.spec.veleroInstall.imageRegistry=harbor.reverse.local/platform`
- **When** Operator 构建下一次 DataSync data restore AppRestore
- **Then** Trafficless Pod 的镜像必须为 `harbor.reverse.local/platform/busybox:1.36`
- **And** 不得继续使用静态目标集群 `cluster-b` 的 registry

#### Scenario: 显式 Trafficless 镜像优先
- **Given** `DataSync.spec.trafficlessConfig.image=registry.local/tools/trafficless:v2`
- **And** 本次恢复目标集群配置了 `veleroInstall.imageRegistry`
- **When** Operator 构建 data restore AppRestore
- **Then** Trafficless Pod 的镜像必须保持为 `registry.local/tools/trafficless:v2`
- **And** 不得被目标集群 registry 覆盖

#### Scenario: 历史默认 busybox latest 不阻断离线仓库解析
- **Given** 历史 DataSync 对象中存在 `trafficlessConfig.image=busybox:latest`
- **And** 本次恢复目标集群配置了 `veleroInstall.imageRegistry=harbor.customer.local/disaster`
- **When** Operator 构建 data restore AppRestore
- **Then** `busybox:latest` 必须被视为历史默认值
- **And** Trafficless Pod 的镜像必须为 `harbor.customer.local/disaster/busybox:1.36`

#### Scenario: 历史默认 busybox 1.36 不阻断离线仓库解析
- **Given** 历史 DataSync 对象中存在 `trafficlessConfig.image=busybox:1.36`
- **And** 本次恢复目标集群配置了 `veleroInstall.imageRegistry=harbor.customer.local/disaster`
- **When** Operator 构建 data restore AppRestore
- **Then** `busybox:1.36` 必须被视为默认值
- **And** Trafficless Pod 的镜像必须为 `harbor.customer.local/disaster/busybox:1.36`

#### Scenario: 无目标集群私有仓库时回退默认镜像
- **Given** 本次恢复目标集群未配置 `veleroInstall.imageRegistry`
- **And** DataSync 未显式配置 Trafficless 镜像
- **When** Operator 构建 data restore AppRestore
- **Then** Trafficless Pod 的镜像必须回退为 `busybox:1.36`

#### Scenario: Drill 数据恢复使用演练目标集群
- **Given** 一个 Drill 数据恢复正在执行
- **And** 演练目标集群为 `cluster-drill`
- **And** `cluster-drill.spec.veleroInstall.imageRegistry=harbor.dr.local/platform`
- **When** Operator 构建 Drill data restore AppRestore
- **Then** Trafficless Pod 的镜像必须为 `harbor.dr.local/platform/busybox:1.36`
- **And** 不得使用实例当前主集群 registry

#### Scenario: Trafficless Pod 注入私有仓库 pull secret
- **Given** 本次恢复目标集群配置了 `veleroInstall.registryCredentialSecretRef`
- **When** Operator 构建 data restore AppRestore
- **Then** Trafficless Pod modifier 必须注入 `/spec/imagePullSecrets`
- **And** 引用的 Secret 名称必须与目标恢复 namespace 中同步的 pull secret 一致

#### Scenario: 业务镜像替换规则不参与 Trafficless busybox 解析
- **Given** `Cluster.spec.imageSources` 或 ResourceSync 业务镜像前缀替换规则配置了 `docker.io -> harbor.biz.local`
- **And** 本次恢复目标集群配置了 `veleroInstall.imageRegistry=harbor.platform.local/disaster`
- **When** Operator 构建 data restore AppRestore
- **Then** Trafficless Pod 的镜像必须为 `harbor.platform.local/disaster/busybox:1.36`
- **And** 不得解析为 `harbor.biz.local/busybox:1.36`
