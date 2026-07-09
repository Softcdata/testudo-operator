## ADDED Requirements

### Requirement: Cluster 必须支持独立的 Velero 安装镜像配置
系统必须 (MUST) 为 `Cluster` 提供独立于 `imageSources` 的 Velero 安装配置，用于声明 Velero Helm 安装所使用的镜像仓库前缀和拉取凭据引用。

#### Scenario: Velero 安装配置与 imageSources 分离
- **Given** 用户创建或更新一个 `Cluster`
- **And** 请求中为 Velero 安装提供了自定义镜像仓库前缀和仓库凭据
- **When** 系统持久化该 `Cluster`
- **Then** `Cluster` 必须记录独立的 `veleroInstall` 配置
- **And** 对外写入模型必须以 `imageRegistry`、`username`、`password`、`removeCredential` 表达
- **And** 凭据明文不得写入 `Cluster.spec.imageSources`
- **And** `imageSources` 不得承担 Velero 安装镜像或凭据语义
- **And** 查询模型不得返回 `password` 或 dockerconfigjson 原文

#### Scenario: 编辑集群未提交非空密码时保留已有凭据
- **Given** 一个 `Cluster` 已配置 `veleroInstall.imageRegistry` 和 `registryCredentialSecretRef`
- **And** 管理平面存在对应的 Velero registry dockerconfigjson Secret
- **When** 用户编辑该 `Cluster`，但本次请求未携带 `veleroInstall.password` 或携带 `veleroInstall.password=""`
- **Then** 系统必须保留已有管理平面 Secret
- **And** 必须保留 `Cluster.spec.veleroInstall.registryCredentialSecretRef`
- **And** 不得因为请求未携带密码或提交空密码而清空或覆盖凭据

#### Scenario: 显式轮换或删除凭据
- **Given** 一个 `Cluster` 已配置 Velero registry 凭据
- **When** 用户提交非空 `veleroInstall.username/password`
- **Then** 系统必须轮换管理平面 Secret 内容并保持稳定 Secret 引用
- **When** 用户提交 `veleroInstall.removeCredential=true`
- **Then** 系统必须删除管理平面 Secret 并清空 `Cluster.spec.veleroInstall.registryCredentialSecretRef`
- **And** 删除凭据时不得清空非空的 `Cluster.spec.veleroInstall.imageRegistry`

### Requirement: Cluster 控制器必须先同步 target pull secret，再执行 Helm 安装
系统必须 (MUST) 在 `Cluster` 配置了 Velero registry 凭据时，先将管理平面凭据同步为目标集群可用的 pull secret，再执行 Velero Helm 安装或升级。

#### Scenario: target pull secret 先于 Helm 安装准备完成
- **Given** `Cluster` 已配置 `veleroInstall.registryCredentialSecretRef`
- **When** `ClusterReconciler` 执行一次调谐
- **Then** 目标集群的 `velero` namespace 中必须先存在对应的 pull secret
- **And** 只有当该 pull secret 可用时，控制器才可以继续执行 Helm 安装或升级

### Requirement: Cluster 控制器必须按 Cluster 生成 Velero values overlay
系统必须 (MUST) 基于容器内的基线 `velero.values.yaml` 按 Cluster 生成一次性 overlay values，使安装结果实际引用客户镜像仓库和 target pull secret。

#### Scenario: Helm 安装 values 被按集群覆盖
- **Given** `Cluster` 已配置 `veleroInstall.imageRegistry`
- **When** `ClusterReconciler` 安装或升级 Velero
- **Then** 控制器必须生成 per-cluster 临时 values 文件
- **And** 覆盖 Velero 安装相关镜像地址
- **And** 覆盖 `image.imagePullSecrets`
- **And** 不得原地改写共享的基线 `velero.values.yaml`

### Requirement: Cluster 控制器必须对齐凭据轮换与清理生命周期
系统必须 (MUST) 在凭据轮换、凭据解绑或集群删除时，对齐远端 pull secret、Helm values 引用和残留清理，避免脏状态。

#### Scenario: 未提交非空密码时远端 Secret 引用保持不变
- **Given** `Cluster` 已配置 `veleroInstall.registryCredentialSecretRef`
- **And** 用户编辑集群时未携带 `veleroInstall.password` 或携带 `veleroInstall.password=""`
- **When** `ClusterReconciler` 执行一次调谐
- **Then** 远端 pull secret 必须继续存在
- **And** Helm values 必须继续引用同一个稳定的 pull secret 名称
- **And** 控制器不得将未携带密码或提交空密码解释为解绑凭据

#### Scenario: 轮换凭据时远端 Secret 被对齐
- **Given** `Cluster` 关联的 Velero registry credential Secret 内容发生变化
- **When** `ClusterReconciler` 执行一次调谐
- **Then** 远端 pull secret 必须被更新为最新内容
- **And** 安装结果必须继续引用同一个稳定的 pull secret 名称

#### Scenario: 解绑凭据时移除 target pull secret 引用
- **Given** 用户显式移除 `Cluster` 的 Velero registry 凭据
- **When** `ClusterReconciler` 执行一次调谐
- **Then** 系统必须移除 Helm values 中的 `image.imagePullSecrets` 引用
- **And** 必须清理目标集群中对应的 pull secret

#### Scenario: 删除集群时清理远端凭据资源
- **Given** 用户删除一个已配置 Velero registry 凭据的 `Cluster`
- **When** `ClusterReconciler` 进入删除流程
- **Then** 系统必须清理目标集群中的远端 pull secret
- **And** 不得留下孤立的凭据资源
