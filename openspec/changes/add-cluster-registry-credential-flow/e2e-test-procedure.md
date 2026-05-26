# Velero Registry Credential E2E 手册

## 1. 目标

本手册用于在真实环境验证以下链路：

1. `disaster-server` cluster create/patch API 能接收 `veleroInstall.imageRegistry`、`username`、`password`、`removeCredential`。
2. server 能在管理平面 `disaster-system` namespace 创建、轮换、删除 `cluster-velero-regcred-<cluster-name>`。
3. `Cluster.spec.veleroInstall` 只持久化 `imageRegistry` 和 `registryCredentialSecretRef`，不持久化明文凭据。
4. operator 能在目标集群 `velero` namespace 先对齐 `velero-regcred-<cluster-name>`，再执行 Helm 安装或升级。
5. Velero Deployment、NodeAgent、插件 initContainer 实际从认证私有仓库拉取镜像，并引用稳定的 `imagePullSecrets`。

## 2. 认证私有仓库选型

### 2.1 推荐实现
- 使用官方 `registry:2` 镜像。
- 通过 `htpasswd` 开启 basic auth。
- 不引入 Harbor。

### 2.2 选型原因
- 足够轻：单容器即可提供 push/pull + basic auth。
- 足够准：本 proposal 只需要验证“认证拉镜像 + dockerconfigjson Secret”链路，不需要 Harbor 的项目管理、扫描、复制等能力。
- 足够贴近产品行为：`dockerconfigjson` 中保存的是 registry endpoint + username/password，和 `registry:2` 的认证模型完全匹配。

### 2.3 网络要求
- `veleroInstall.imageRegistry` 必须填写目标集群节点可达的地址，不能写 `127.0.0.1`。
- 推荐使用 `<宿主机IP>:<端口>/<前缀>`。
- 若目标集群运行时默认禁止 HTTP registry，先为测试环境配置 TLS 入口，或预置 insecure-registry 运行时配置，再执行本手册。

## 3. 测试拓扑

1. 管理平面：
   - 本地运行 `disaster-server`
   - 本地运行 `disaster-operator`
   - 当前 kubeconfig 指向管理平面集群
2. 目标集群：
   - 独立的 k8s/k3s/kind 集群
   - 能被 `Cluster.spec.kubeConfig` 正常接入
3. 认证 registry：
   - 在宿主机启动 `registry:2`
   - 通过宿主机 IP 暴露给目标集群节点访问

## 4. 测试场景矩阵

| 场景 | 目标 | API 入口 | 核心断言 |
| --- | --- | --- | --- |
| S1 | 创建集群时直接安装私有仓库版 Velero | `POST /clusters` | 管理平面 Secret 已创建；target secret 已创建；Velero/NodeAgent/插件镜像均指向私有仓库；Pod 引用 `velero-regcred-<cluster>` |
| S2 | 编辑集群时轮换凭据 | `PATCH /clusters/:name` | 管理平面 Secret 内容更新；target secret 内容更新；滚动重启后新 Pod 仍能成功拉取镜像 |
| S3 | 编辑集群时显式删除凭据 | `PATCH /clusters/:name` | `registryCredentialSecretRef` 被清空；管理平面 Secret 删除；target secret 删除；Velero 工作负载不再引用 `imagePullSecrets` |
| S4 | 使用错误凭据做补充诊断 | `POST /clusters` 或 `PATCH /clusters/:name` | Secret 同步链路仍成立，但 Velero Pod 进入 `ImagePullBackOff`；该场景用于观察当前实现边界，不作为主验收通过条件 |

## 5. 一次性准备

### 5.1 依赖

```bash
kubectl version --client
curl --version
jq --version
skopeo --version
docker --version
```

### 5.2 环境变量

```bash
export API_BASE="http://127.0.0.1:8080/apis/cluster.testudo.softcdata.com/v1"
export MGMT_NS="disaster-system"
export TEST_CLUSTER="velero-e2e-auth"
export TEST_CLUSTER_KUBECONFIG="/tmp/${TEST_CLUSTER}.kubeconfig"

export REGISTRY_HOST="<宿主机IP>:5000"
export REGISTRY_PREFIX="${REGISTRY_HOST}/velero-e2e"
export REGISTRY_USER="velero"
export REGISTRY_PASS="<SECRET>"

export TARGET_SECRET="velero-regcred-${TEST_CLUSTER}"
export MGMT_SECRET="cluster-velero-regcred-${TEST_CLUSTER}"
```

说明：
- `REGISTRY_HOST` 必须替换成目标集群节点可达地址。
- `TEST_CLUSTER_KUBECONFIG` 对应要添加的目标集群 kubeconfig 文件。

### 5.3 启动认证 registry

```bash
mkdir -p /tmp/velero-registry-auth
docker run --rm --entrypoint htpasswd httpd:2 -Bbn "${REGISTRY_USER}" "${REGISTRY_PASS}" > /tmp/velero-registry-auth/htpasswd

docker rm -f velero-e2e-registry >/dev/null 2>&1 || true
docker run -d --name velero-e2e-registry \
  -p 5000:5000 \
  -v /tmp/velero-registry-auth:/auth \
  -e REGISTRY_AUTH=htpasswd \
  -e REGISTRY_AUTH_HTPASSWD_REALM="Registry Realm" \
  -e REGISTRY_AUTH_HTPASSWD_PATH=/auth/htpasswd \
  registry:2
```

### 5.4 预热镜像

```bash
skopeo copy --dest-creds "${REGISTRY_USER}:${REGISTRY_PASS}" \
  docker://velero/velero:v1.17.0 \
  docker://${REGISTRY_PREFIX}/velero:v1.17.0

skopeo copy --dest-creds "${REGISTRY_USER}:${REGISTRY_PASS}" \
  docker://velero/velero-plugin-for-aws:v1.13.0 \
  docker://${REGISTRY_PREFIX}/velero-plugin-for-aws:v1.13.0

skopeo copy --dest-creds "${REGISTRY_USER}:${REGISTRY_PASS}" \
  docker://registry.example.com/disaster/kubectl:1.20.14 \
  docker://${REGISTRY_PREFIX}/kubectl:1.20.14
```

## 6. 场景 S1：创建集群即安装私有仓库版 Velero

### 6.1 发起创建请求

```bash
KCFG_B64=$(base64 -w0 "${TEST_CLUSTER_KUBECONFIG}")

curl -sS -X POST "${API_BASE}/clusters" \
  -H 'Content-Type: application/json' \
  -d "{\n    \"name\": \"${TEST_CLUSTER}\",\n    \"description\": \"velero registry e2e\",\n    \"kubeConfig\": \"${KCFG_B64}\",\n    \"veleroInstall\": {\n      \"imageRegistry\": \"${REGISTRY_PREFIX}\",\n      \"username\": \"${REGISTRY_USER}\",\n      \"password\": \"${REGISTRY_PASS}\"\n    }\n  }" | jq .
```

### 6.2 验证 server 侧管理平面状态

```bash
kubectl -n "${MGMT_NS}" get secret "${MGMT_SECRET}" -o jsonpath='{.type}{"\n"}'
kubectl get clusters.testudo.softcdata.com "${TEST_CLUSTER}" -o jsonpath='{.spec.veleroInstall.imageRegistry}{"\n"}'
kubectl get clusters.testudo.softcdata.com "${TEST_CLUSTER}" -o jsonpath='{.spec.veleroInstall.registryCredentialSecretRef.name}{"\n"}'

curl -sS "${API_BASE}/clusters/${TEST_CLUSTER}" | jq '.data.spec.veleroInstall'
```

验收点：
1. Secret 类型为 `kubernetes.io/dockerconfigjson`
2. `Cluster.spec.veleroInstall.imageRegistry == ${REGISTRY_PREFIX}`
3. `Cluster.spec.veleroInstall.registryCredentialSecretRef.name == ${MGMT_SECRET}`
4. API 详情页只回显 `imageRegistry` 与 `credentialConfigured=true`

### 6.3 验证目标集群 Secret 先于 Helm 工作负载出现

```bash
kubectl --kubeconfig "${TEST_CLUSTER_KUBECONFIG}" -n velero get secret "${TARGET_SECRET}" -o jsonpath='{.metadata.creationTimestamp}{"\n"}'
kubectl --kubeconfig "${TEST_CLUSTER_KUBECONFIG}" -n velero get deploy velero -o jsonpath='{.metadata.creationTimestamp}{"\n"}'
```

验收点：
1. `velero-regcred-<cluster>` 已存在
2. target secret 的 `creationTimestamp` 早于或等于 `deploy/velero` 的 `creationTimestamp`

### 6.4 验证镜像地址与 pull secret 引用

```bash
kubectl --kubeconfig "${TEST_CLUSTER_KUBECONFIG}" -n velero get deploy velero \
  -o jsonpath='{.spec.template.spec.containers[0].image}{"\n"}'

kubectl --kubeconfig "${TEST_CLUSTER_KUBECONFIG}" -n velero get deploy velero \
  -o jsonpath='{.spec.template.spec.initContainers[0].image}{"\n"}'

kubectl --kubeconfig "${TEST_CLUSTER_KUBECONFIG}" -n velero get ds node-agent \
  -o jsonpath='{.spec.template.spec.containers[0].image}{"\n"}'

kubectl --kubeconfig "${TEST_CLUSTER_KUBECONFIG}" -n velero get deploy velero \
  -o jsonpath='{.spec.template.spec.imagePullSecrets[0].name}{"\n"}'
```

验收点：
1. Deployment 主容器镜像前缀为 `${REGISTRY_PREFIX}/velero`
2. 插件 initContainer 镜像前缀为 `${REGISTRY_PREFIX}/velero-plugin-for-aws`
3. NodeAgent 镜像前缀为 `${REGISTRY_PREFIX}/velero`
4. `imagePullSecrets[0].name == ${TARGET_SECRET}`

## 7. 场景 S2：轮换凭据

### 7.1 轮换 registry 密码

```bash
export REGISTRY_PASS_ROTATED="<SECRET-ROTATED>"

docker run --rm --entrypoint htpasswd httpd:2 -Bbn "${REGISTRY_USER}" "${REGISTRY_PASS_ROTATED}" > /tmp/velero-registry-auth/htpasswd
docker restart velero-e2e-registry

curl -sS -X PATCH "${API_BASE}/clusters/${TEST_CLUSTER}" \
  -H 'Content-Type: application/json' \
  -d "{\n    \"veleroInstall\": {\n      \"username\": \"${REGISTRY_USER}\",\n      \"password\": \"${REGISTRY_PASS_ROTATED}\"\n    }\n  }" | jq .
```

### 7.2 验证 Secret 内容已同步

```bash
kubectl -n "${MGMT_NS}" get secret "${MGMT_SECRET}" -o jsonpath='{.data.\.dockerconfigjson}' | base64 -d
kubectl --kubeconfig "${TEST_CLUSTER_KUBECONFIG}" -n velero get secret "${TARGET_SECRET}" -o jsonpath='{.data.\.dockerconfigjson}' | base64 -d
```

验收点：
1. 两边 Secret 的认证内容均已变成新密码
2. Secret 名称保持稳定，不创建新名称

### 7.3 重启 Velero 工作负载验证新凭据可用

```bash
kubectl --kubeconfig "${TEST_CLUSTER_KUBECONFIG}" -n velero rollout restart deploy/velero
kubectl --kubeconfig "${TEST_CLUSTER_KUBECONFIG}" -n velero rollout restart ds/node-agent

kubectl --kubeconfig "${TEST_CLUSTER_KUBECONFIG}" -n velero rollout status deploy/velero --timeout=180s
kubectl --kubeconfig "${TEST_CLUSTER_KUBECONFIG}" -n velero rollout status ds/node-agent --timeout=180s
```

## 8. 场景 S3：显式删除凭据

```bash
curl -sS -X PATCH "${API_BASE}/clusters/${TEST_CLUSTER}" \
  -H 'Content-Type: application/json' \
  -d '{
    "veleroInstall": {
      "removeCredential": true
    }
  }' | jq .
```

### 8.1 验证引用与 Secret 清理

```bash
kubectl get clusters.testudo.softcdata.com "${TEST_CLUSTER}" -o jsonpath='{.spec.veleroInstall.registryCredentialSecretRef.name}{"\n"}'
kubectl -n "${MGMT_NS}" get secret "${MGMT_SECRET}"
kubectl --kubeconfig "${TEST_CLUSTER_KUBECONFIG}" -n velero get secret "${TARGET_SECRET}"
kubectl --kubeconfig "${TEST_CLUSTER_KUBECONFIG}" -n velero get deploy velero -o jsonpath='{.spec.template.spec.imagePullSecrets}'
```

验收点：
1. `registryCredentialSecretRef` 已为空
2. 管理平面 Secret 已删除
3. 目标集群 Secret 已删除
4. Velero Deployment 不再引用 `imagePullSecrets`

## 9. 场景 S4：错误凭据诊断

该场景只用于观察当前实现边界，不作为 proposal 主验收。

建议步骤：
1. 以错误密码创建或 patch 集群。
2. 确认管理平面 Secret 与 target secret 仍然被创建。
3. 观察 `velero` namespace Pod 状态。

预期：
- Secret 生命周期链路成立。
- Pod 进入 `ImagePullBackOff`。
- 当前 controller 仍可能完成 Helm 命令，因为它没有把镜像实际拉取成功纳入 Helm 成功判定。

## 10. 清理

```bash
curl -sS -X DELETE "${API_BASE}/clusters/${TEST_CLUSTER}" | jq .
docker rm -f velero-e2e-registry >/dev/null 2>&1 || true
rm -rf /tmp/velero-registry-auth
```
