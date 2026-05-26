# Change: 为受管集群补齐 Velero 安装镜像源与拉取凭据链路

## Why
当前“添加集群”链路中，`disaster-server` 只负责创建 `Cluster` CR；`disaster-operator` 在 `ClusterReconciler` 中直接执行 `InstallVeleroInCluster`，并固定使用容器内的 `./velero.values.yaml` 调用 Helm 安装 Velero。

客户现场通常只能从自己的内网 Harbor/Registry 拉取镜像，既需要在添加集群时配置 Velero 安装镜像源，也需要在 Helm 安装前先把该镜像源的拉取凭据变成目标集群可用的 `dockerconfigjson` Secret。

现有 proposal 把这件事错误地绑定到了 `Cluster.spec.imageSources`。但 `imageSources` 在当前代码和 `add-image-source-mapping` 中已经明确是“实例镜像改写目录”，不应承接 Velero 安装镜像与凭据语义。

## What Changes

### 1. Cluster 引入独立的 Velero 安装配置
- `Cluster` 继续保留 `spec.imageSources` 作为实例镜像改写目录。
- `Cluster` 新增独立的 `spec.veleroInstall`（命名以实施为准）用于描述 Velero 安装镜像配置。
- 对外输入模型收敛为：
  - `imageRegistry`：非敏感，可选，例如 `harbor.customer.local/disaster`
  - `username`：write-only，可选
  - `password`：write-only，可选
  - `removeCredential`：write-only，可选
- `Cluster` 持久化时只保留非敏感字段：
  - `imageRegistry`
  - `registryCredentialSecretRef`
- 凭据明文不得写入 `Cluster` CR。

### 2. 添加集群时先准备拉取凭据，再推进 Helm 安装
- server 在 cluster create/update 时，根据用户提交的 registry 账号密码生成或轮换管理平面 `kubernetes.io/dockerconfigjson` Secret。
- `ClusterReconciler` 在安装或升级 Velero 前，必须先将该 Secret 同步到目标集群的 `velero` namespace。
- 若目标 pull secret 未准备好，不得继续执行 Helm 安装，避免出现 `ImagePullBackOff` 的半成品状态。

### 3. ClusterReconciler 负责 per-cluster values overlay 与远端 Secret 生命周期
- `ClusterReconciler` 负责：
  - 读取管理平面 Secret
  - 同步远端 pull secret 到目标集群 `velero` namespace
  - 基于容器内的基线 `velero.values.yaml` 生成一次性临时 overlay values
  - 在凭据轮换、集群重装、集群删除时清理或对齐远端 Secret
- `ClusterReconciler` 不应原地改写共享的 `./velero.values.yaml`。

### 4. Helm values 必须按 Cluster 覆盖镜像仓库地址和 pull secret 名称
- 当 `imageRegistry` 已配置时，operator 必须在 Helm values 中覆盖 Velero 安装相关镜像地址。
- 首期按当前模板约定覆盖以下位点：
  - `image.repository`
  - `kubectl.image.repository`
  - `initContainers[*].image`
  - `image.imagePullSecrets`
- 目标是让 Velero Deployment、NodeAgent DaemonSet 与插件 initContainers 全部使用客户镜像仓库与目标 pull secret。

### 5. Secret 生命周期纳入 add-cluster 主链路
- 创建集群：若存在自定义镜像源和凭据，先确保远端 pull secret 可用，再推进安装。
- 轮换凭据：更新管理平面 Secret 后，reconcile 必须对齐远端 Secret 并重新执行安装对齐。
- 删除凭据：必须移除 Helm values 中的 `imagePullSecrets` 引用，并清理远端 Secret。
- 删除集群：必须清理远端 pull secret，避免在目标集群遗留孤儿凭据。

### 6. 首期 E2E 采用轻量认证 registry，不引入 Harbor
- 首期真实环境 E2E 使用 `registry:2` + `htpasswd` 作为认证私有仓库，不把 Harbor 作为联调前置。
- E2E 从 `disaster-server` 的 cluster create/patch API 发起，贯穿 `Cluster` CR、管理平面 dockerconfigjson Secret、target-cluster pull secret、Helm install/upgrade。
- 首批纳入主验收的场景：
  - 创建集群时携带 `veleroInstall.imageRegistry + username/password`
  - 编辑集群时轮换凭据
  - 编辑集群时显式删除凭据
- 需要验证的核心事实：
  - server 只持久化非敏感引用，不回写明文凭据
  - operator 先对齐 target secret，再生成 overlay 并执行 Helm
  - Velero Deployment、NodeAgent、插件 initContainer 实际引用客户私有仓库与稳定 pull secret 名称

## Non-Goals
- 不改变 `imageSources` 作为实例镜像改写目录的语义。
- 不为 Velero 安装暴露“任意自定义 target secret name”输入，目标 secret 名称由 operator 生成稳定值。
- 不在首期支持逐镜像单独配置 repository 或逐镜像单独 Secret。
- 不原地修改容器内共享的 `velero.values.yaml` 基线文件。
- 不在首期引入 Harbor、Nexus 一类重量级制品仓库作为联调依赖。

## Impact
- Affected specs:
  - `cluster`
- Affected code:
  - `pkg/apis/disaster/v1/cluster_types.go`
  - `internal/controller/cluster_controller.go`
  - `velero.values.yaml`
  - `internal/controller/*velero*`（如需拆出 values overlay / secret sync helper）
- `openspec/changes/add-cluster-registry-credential-flow/e2e-test-procedure.md`
- Cross-repo impact:
  - `disaster-server`：cluster add/edit API、管理平面 Secret 生命周期、脱敏回显
  - `cluster-disaster-web`：集群添加页的 Velero 镜像源与凭据输入

## Relationship to Existing Changes
- 现有 active change：`add-image-source-mapping`
- 本 change 不扩展实例镜像映射语义，只补 Velero 安装镜像与凭据链路。
- 两者可以并存，但应在评审时明确边界：
  - `add-image-source-mapping` 只负责实例镜像改写时的 alias -> registry mapping
  - `add-cluster-registry-credential-flow` 只负责 add-cluster 场景下的 Velero 安装镜像与 pull secret

## Risks
- 若 `imageRegistry` 的派生规则无法覆盖当前模板中的全部镜像字段，仍可能出现局部拉取失败。
- 若 target secret 名称和管理平面 Secret 名称的映射规则不稳定，会增加轮换与清理复杂度。
- 若 proposal 未明确“先建 secret 再 helm install”，实现仍可能停留在时序错误的假闭环。
- 当前仓库 `test/e2e` 仍是 Kubebuilder 默认骨架，首期更适合先落真实环境 runbook，再进入自动化扩展。
