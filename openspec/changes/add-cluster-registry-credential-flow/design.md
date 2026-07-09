# Design: Cluster Velero Install Registry Flow

## 背景
当前 operator 的“添加集群”主链路是：
1. server 创建 `Cluster` CR
2. `ClusterReconciler` 发现目标集群未安装 Velero
3. `InstallVeleroInCluster` 直接执行 `helm upgrade --install -f ./velero.values.yaml`

当前模板文件 `./velero.values.yaml` 是管理容器内的共享基线，不带 per-cluster 上下文。客户现场却往往需要：
- 使用客户自有私有仓库拉取 Velero 镜像
- 在 Helm 安装前，先把仓库账号密码变成目标集群可用的 pull secret

因此本设计要解决的不是通用 `imageSources` registry mapping，而是 “Cluster 级 Velero 安装镜像源 + 拉取凭据 + Helm values overlay” 的完整链路。

## 关键决策

### D1. Velero 安装配置使用独立的 Cluster 字段，不复用 `imageSources`
- `Cluster.spec.imageSources` 继续服务于实例镜像改写。
- 新能力单独建模为 `Cluster.spec.veleroInstall`（字段名以实施为准）。
- 对外最小输入字段集：
  - `imageRegistry`
  - `username`
  - `password`
  - `removeCredential`
- 读模型允许回显非敏感状态：
  - `imageRegistry`
  - `username`（从管理平面 Secret 解析得到时可回显，用于编辑态提示）
  - `credentialConfigured`
- CR 内部持久化字段集：
  - `imageRegistry`
  - `registryCredentialSecretRef`
- 原因：
  - 避免把 Velero 安装配置和 DisasterInstance image rewrite 混在一起。
  - 让 add-cluster API、operator reconcile、Helm overlay 有明确责任边界。

### D2. 管理平面 Secret 固定在 operator manager namespace，由 server 创建
- Secret 类型：`kubernetes.io/dockerconfigjson`
- 命名规则：稳定生成，例如 `cluster-velero-regcred-<cluster-name>`
- `Cluster` 中只保存非敏感引用，不暴露凭据内容。
- 原因：
  - `Cluster` 是 cluster-scoped 资源，不适合开放任意 namespace Secret 引用。
  - server 是用户写入账号密码的唯一入口，operator 只消费。

### D3. target-cluster pull secret 名称由 operator 生成稳定值
- target secret 不由用户显式输入。
- operator 在目标集群 `velero` namespace 中生成稳定名称，例如 `velero-regcred-<cluster-name>`。
- Helm values 中引用该稳定名称。
- 原因：
  - 用户真正关心的是 registry 和账号密码，不是 Kubernetes Secret 命名。
  - 稳定命名更利于轮换、重装和删除时清理。

### D4. ClusterReconciler 承担“先同步 secret，再安装/升级”的职责
- 责任归属：`ClusterReconciler`
- 原因：
  - 它已经负责 cluster 连通性与 Velero 安装。
  - pull secret 是 Helm 安装的前置依赖，而不是独立后台任务。
- 结果：
  - 安装或升级前先读取本地 Secret
  - 再同步 target secret
  - 最后执行 Helm 安装/升级
  - 缺任一步都阻断本轮安装

### D4a. 编辑请求必须区分“未修改凭据”和“删除凭据”
- 密码不回显、也不写入 `Cluster` CR，因此编辑态不能依赖“重新提交旧密码”来保留凭据。
- server / web 的更新语义必须使用字段是否出现表达意图：
  - 未携带 `veleroInstall.password` 或携带空 `veleroInstall.password`：保持现有管理平面 Secret 和 `registryCredentialSecretRef` 不变。
  - 携带非空 `veleroInstall.username/password`：轮换管理平面 Secret 内容，Secret 名称保持稳定。
  - 携带 `veleroInstall.removeCredential=true`：删除管理平面 Secret 并清空 `registryCredentialSecretRef`。
  - PATCH 显式携带 `veleroInstall.username=""` 且未设置 `removeCredential=true`：保持现有凭据不变。
  - PATCH 显式携带 `veleroInstall.imageRegistry=""`：清空整段 Velero 安装配置，并删除 server 管理的 registry Secret。
- 前端编辑页若密码输入框为空且用户没有选择“删除凭据”，可以省略 `password` 字段，也可以发送空字符串；server 必须将其视为不修改凭据。
- 若只修改 `imageRegistry` 且保留旧凭据，operator 会继续同步同一个 Secret；但当 registry host 发生变化时，旧 dockerconfigjson 的 auth key 可能不匹配新仓库，产品应提示用户同时轮换凭据。

### D5. 采用“基线 values + per-cluster 临时 overlay”而不是原地改模板
- 基线文件仍然是容器内共享的 `./velero.values.yaml`
- `InstallVeleroInCluster` 在每次安装前读取基线，生成临时 overlay values 文件，再传给 Helm
- 首期不原地改写共享模板
- 原因：
  - 多 Cluster 调谐共享同一模板，原地改写会产生串扰风险。
  - per-cluster 临时文件更符合当前 Helm 安装模型。

### D6. 首期 E2E 使用 `registry:2 + htpasswd`，不把 Harbor 作为联调前置
- 测试 registry 使用官方 `registry:2`，通过 `htpasswd` 提供 basic auth。
- `veleroInstall.imageRegistry` 必须填写目标集群节点可达地址，例如 `<host-ip>:5000/velero-e2e`。
- 该方案完整覆盖 dockerconfigjson Secret、私有仓库鉴权、镜像地址改写三条主线。
- 原因：
  - Harbor 过重，不适合作为 proposal 首期联调依赖。
  - `registry:2` 足以验证本能力真正关心的认证拉镜像链路。

### D7. E2E 分两层推进
- 第一层：真实环境 runbook，直接覆盖 `disaster-server -> Cluster -> operator -> target cluster` 主链路。
- 第二层：待 `test/e2e` 补齐多集群 harness 后，再把主链路迁入自动化 black-box E2E。
- 原因：
  - 当前仓库 `test/e2e` 仍是 Kubebuilder 默认骨架。
  - 本能力依赖真实 Helm 安装和目标集群拉镜像，先做 runbook 更稳。

## 模型草案

### Cluster 模型
- `Cluster.spec.imageSources`：保持现有 alias -> registry，继续给实例镜像改写使用
- `Cluster.spec.veleroInstall.imageRegistry`：可选，表示客户镜像仓库前缀，例如 `harbor.customer.local/disaster`
- `Cluster.spec.veleroInstall.registryCredentialSecretRef`：可选，表示管理平面 dockerconfigjson Secret 名称

### 管理平面 Secret 约定
- 类型：`kubernetes.io/dockerconfigjson`
- 命名：`cluster-velero-regcred-<cluster-name>`
- 所在 namespace：operator manager namespace

### 远端 Secret 约定
- 类型：`kubernetes.io/dockerconfigjson`
- 命名：稳定生成，例如 `velero-regcred-<cluster-name>`
- 所在 namespace：目标集群 Velero namespace

### Helm values overlay 草案
- 从基线 `velero.values.yaml` 读取当前 tag 和非镜像字段
- 当 `imageRegistry` 已配置时，派生：
  - `image.repository=<imageRegistry>/velero`
  - `kubectl.image.repository=<imageRegistry>/kubectl`
  - `initContainers[*].image=<imageRegistry>/velero-plugin-for-aws:<existing-tag>`
- 当 `registryCredentialSecretRef` 已配置时，派生：
  - `image.imagePullSecrets[0].name=<target-secret-name>`

## 生命周期

### 创建 / 首次安装
1. 用户在 add-cluster 请求中提供 `veleroInstall.imageRegistry`
2. 若同时提供凭据，server 创建或更新管理平面 Secret
3. server 将 `imageRegistry` 和 `registryCredentialSecretRef` 写入 `Cluster`
4. `ClusterReconciler` 读取基线 values
5. 若存在 Secret 引用，则先在目标集群创建或更新 target secret
6. operator 生成临时 overlay values
7. operator 执行 Helm 安装或升级

### 轮换
1. server 更新管理平面 Secret 内容
2. `ClusterReconciler` 对齐 target secret
3. operator 重新执行 Helm upgrade，确保安装结果仍引用稳定 secret 名称

### 保留凭据的编辑
1. 用户编辑 `Cluster` 的非凭据字段，或仅修改 `veleroInstall.imageRegistry`
2. 请求体不携带 `veleroInstall.password`，或携带 `veleroInstall.password=""`
3. server 保留已有 `registryCredentialSecretRef` 和管理平面 Secret
4. `ClusterReconciler` 继续同步既有 Secret 到目标集群，并在 Helm values 中引用稳定 target secret 名称

### 删除
- 删除凭据引用：
  - operator 删除远端 Secret
  - 同步移除 overlay values 中的 `image.imagePullSecrets`
- 删除集群：
  - operator 卸载 Velero 与相关资源
  - 再清理远端 pull secret

## E2E 设计

### 测试拓扑
1. 管理平面：本地运行 `disaster-server` 与 `disaster-operator`，当前 kubeconfig 指向管理平面集群。
2. 目标集群：单独准备一个可被 `Cluster.spec.kubeConfig` 接入的集群。
3. 认证 registry：在宿主机启动 `registry:2 + htpasswd`，通过宿主机 IP 暴露给目标集群节点访问。

### 主验收场景
1. S1 创建集群：`POST /clusters` 携带 `veleroInstall.imageRegistry + username/password`，验证管理平面 Secret、target secret、镜像地址改写与 `imagePullSecrets` 注入。
2. S2 保留凭据编辑：`PATCH /clusters/:name` 携带空 `password`，验证管理平面 Secret、`registryCredentialSecretRef` 和 target secret 未被清空。
3. S3 轮换凭据：`PATCH /clusters/:name` 只更新凭据，验证管理平面 Secret 与 target secret 内容同步更新，滚动重启后 Velero 工作负载仍可拉取镜像。
4. S4 删除凭据：`PATCH /clusters/:name` 发送 `removeCredential=true`，验证 Secret 引用清空、管理平面 Secret 清理、target secret 清理，以及 Helm 结果不再引用 `imagePullSecrets`。

### 补充诊断场景
- S5 错误凭据：用于观察当前实现边界。预期是 Secret 生命周期链路仍成立，但 Velero Pod 会进入 `ImagePullBackOff`。该场景不作为 proposal 主验收通过条件。

## 失败语义
- 本地 Secret 缺失：`Cluster` 不进入安装成功态，写明确 reason/message。
- 远端 Secret 写入失败：阻断安装或升级，避免产生没有 pull secret 的半成品安装。
- 临时 overlay values 生成失败：阻断本轮安装对齐，并写事件。

## 备选方案

### 方案 A：继续使用 `imageSources` 承担 Velero 安装镜像和凭据
- 放弃原因：契约脆弱，不适合作为长期 API 与 controller 边界。

### 方案 B：原地改写共享 `velero.values.yaml`
- 放弃原因：共享模板会被多个 Cluster 调谐过程复用，原地修改容易造成串扰与并发污染。

### 方案 C：让用户直接输入 target secret name
- 放弃原因：增加前端和 API 复杂度，却不解决真正的核心问题；稳定命名更适合 operator 生命周期管理。
