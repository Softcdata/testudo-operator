## ADDED Requirements

### Requirement: Cluster 私有仓库凭据必须可供 Trafficless 恢复命名空间使用

当 `Cluster` 配置了 `veleroInstall.registryCredentialSecretRef` 时，系统必须 (MUST) 能将该管理平面 dockerconfigjson 凭据同步到本次 FSB 数据恢复实际创建 Trafficless Pod 的目标 namespace。

#### Scenario: DataSync 恢复命名空间同步 pull secret
- **Given** 目标集群 `cluster-b` 配置了 `veleroInstall.registryCredentialSecretRef`
- **And** DataSync 本次恢复 namespace 为 `blueking`
- **When** Operator 准备创建 data restore AppRestore
- **Then** 目标集群 `blueking` namespace 中必须存在对应 dockerconfigjson pull secret
- **And** Secret 内容必须来自管理平面引用的 registry credential Secret

#### Scenario: Drill namespaceMapping 后同步 pull secret
- **Given** Drill 数据恢复配置了 namespaceMapping `blueking -> blueking-drill`
- **And** 演练目标集群配置了 `veleroInstall.registryCredentialSecretRef`
- **When** Operator 准备创建 Drill data restore AppRestore
- **Then** 目标集群 `blueking-drill` namespace 中必须存在对应 dockerconfigjson pull secret
- **And** 不得只同步到源 namespace `blueking`

#### Scenario: 无 registry credential 时不注入 Secret
- **Given** 目标集群配置了 `veleroInstall.imageRegistry`
- **But** 未配置 `veleroInstall.registryCredentialSecretRef`
- **When** Operator 准备创建 data restore AppRestore
- **Then** 系统不得创建空的 pull secret
- **And** Trafficless Pod modifier 不得注入 `/spec/imagePullSecrets`

#### Scenario: 凭据同步失败阻断恢复创建
- **Given** 目标集群配置了 `veleroInstall.registryCredentialSecretRef`
- **But** 管理平面引用的 Secret 不存在或不是 dockerconfigjson
- **When** Operator 准备创建 data restore AppRestore
- **Then** 本次恢复必须失败在 AppRestore 创建前
- **And** 状态信息必须说明 Trafficless registry pull secret 同步失败
