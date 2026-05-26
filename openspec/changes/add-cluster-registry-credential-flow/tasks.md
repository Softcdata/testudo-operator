# Tasks

## 1. Proposal
- [x] 1.1 明确 `imageSources` 与 `veleroInstall` 的能力边界，确认不再混用
- [x] 1.2 评审并确认 `veleroInstall.imageRegistry` 的对外命名，以及 `registryCredentialSecretRef` 作为内部持久化字段
- [x] 1.3 评审并确认“server 建管理平面 Secret -> operator 建 target secret -> helm install”的顺序契约
- [x] 1.4 评审并确认“基线 values + per-cluster 临时 overlay”作为首期安装模型

## 2. Operator
- [x] 2.1 为 `Cluster` 增加独立的 `veleroInstall` 配置字段
- [x] 2.2 实现管理平面 Secret -> target-cluster `velero` namespace Secret 的同步与清理
- [x] 2.3 实现 `velero.values.yaml` 的 per-cluster overlay 生成，不原地改写基线模板
- [x] 2.4 在 Helm 安装前注入镜像仓库地址与 `image.imagePullSecrets`
- [x] 2.5 为创建、轮换、解绑、删除、重装路径补 controller tests

## 3. Server
- [x] 3.1 为 cluster create/update/patch API 增加 `veleroInstall` 输入对象
- [x] 3.2 根据 write-only 凭据生成或更新管理平面 dockerconfigjson Secret
- [x] 3.3 将非敏感 `registryCredentialSecretRef` 写回 `Cluster`
- [x] 3.4 为 detail/list API 增加 `veleroInstall.credentialConfigured` 脱敏回显
- [x] 3.5 为“未修改凭据 / 显式删除凭据 / 修改 imageRegistry”补 handler tests

## 4. Web
- [ ] 4.1 在集群添加/编辑页面增加 Velero 镜像源配置入口
- [ ] 4.2 编辑态显示镜像源前缀和 `credentialConfigured`，不回显明文
- [ ] 4.3 增加凭据轮换和删除交互

## 5. Verification
- [x] 5.1 `openspec validate add-cluster-registry-credential-flow --strict`
- [x] 5.2 输出 `e2e-test-procedure.md`，明确 `registry:2 + htpasswd` 作为轻量认证 registry 与场景矩阵
- [x] 5.3 E2E 主链路：create cluster 时 server 创建管理平面 Secret，operator 先同步 target secret，再执行 Helm 安装
- [x] 5.4 E2E 凭据轮换：patch 集群后管理平面 Secret 与 target secret 内容更新，Velero 工作负载滚动后仍可拉取镜像
- [x] 5.5 E2E 删除凭据：`removeCredential=true` 后清空 Secret 引用并删除管理平面 / target secret
