## MODIFIED Requirements

### Requirement: DataSync 的数据恢复必须使用 Trafficless 临时 Pod

DataSync 和 Drill 的 FSB 数据恢复必须 (MUST) 通过 Trafficless 临时 Pod 接收 PodVolumeRestore 写入，并且该临时 Pod 必须按本次恢复目标集群解析运行时镜像与拉取凭据。

#### Scenario: DataSync 使用目标集群私有仓库
- **Given** 一个 `DisasterInstance` 正在执行 DataSync 数据恢复
- **And** 本次恢复目标集群为 `cluster-b`
- **And** `cluster-b.spec.veleroInstall.imageRegistry=harbor.customer.local/disaster`
- **When** Operator 构建 data restore AppRestore
- **Then** Trafficless Pod 的镜像必须为 `harbor.customer.local/disaster/busybox:1.36`
- **And** 不得使用源集群 registry
- **And** 不得使用 `DisasterConfig.spec.targetCluster` 的静态值绕过当前角色方向

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

#### Scenario: Trafficless Pod 不依赖源业务 ServiceAccount
- **Given** 源业务 Pod 使用了目标集群不存在的 `serviceAccountName`
- **When** Operator 构建 data restore AppRestore
- **Then** Trafficless Pod modifier 应当将 `/spec/serviceAccountName` 设置为 `default`
- **And** 应当将 `/spec/automountServiceAccountToken` 设置为 `false`

#### Scenario: Trafficless 恢复不得更新已存在 Pod 的不可变字段
- **Given** 目标恢复 namespace 中已存在与备份 Pod 同名的 Pod
- **And** Trafficless modifier 需要修改 `spec.nodeName`、`spec.containers[*].command` 或 `spec.containers[*].args`
- **When** Operator 准备创建 data restore AppRestore
- **Then** Operator 必须在 AppRestore 创建前清理可安全删除的冲突 Pod
- **And** 不得依赖 Velero `ExistingResourcePolicy=Update` 去原地修改该 Pod

#### Scenario: 冲突 Pod 无法安全删除时恢复前失败
- **Given** 目标恢复 namespace 中存在冲突 Pod
- **And** Operator 无法确认该 Pod 可安全删除，或删除该 Pod 失败
- **When** Operator 准备创建 data restore AppRestore
- **Then** 本次恢复必须在 AppRestore 创建前失败
- **And** 错误信息必须说明存在 Pod immutable-field update 风险
