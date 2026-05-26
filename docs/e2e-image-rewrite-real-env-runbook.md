# 镜像源映射真实环境联调手册（170/171）

## 1. 目标与范围

本手册用于在真实环境验证以下链路是否生效：

1. Cluster `imageSources` 可配置并生效。
2. DisasterConfig `imageRewrite` 作为唯一配置入口。
3. DisasterInstance 不再承载 `imageRewrite` 配置入口。
4. ResourceSync/Failover 期间镜像前缀按映射重写。
5. 修改 DisasterConfig `imageRewrite` 后，无需修改 Instance，可在下一次恢复链路动态生效。

本文按你当前环境编写：

- `disaster-server` 本机监听 `127.0.0.1:8080`
- `disaster-operator` 本机运行
- 平台已有 `c170`、`c171`，并可从 Cluster CR 提取 kubeconfig

## 2. 重要前提

1. 当前代码语义是“Config 级入口”：`DisasterConfig.spec.imageRewrite`。
2. 当前线上若仍是旧 CRD（`DisasterInstance.spec.imageRewrite`），必须先升级 CRD 和进程。
3. 本手册会新建独立测试对象，不复用已有业务实例，避免污染存量。

### 2.1 执行门禁（每步都按此规则）

每个步骤都执行以下三件事，避免“命令跑了但结果不可用”：

1. 执行命令后立即检查退出码：`echo $?` 必须为 `0`。
2. 检查关键字段是否达到预期（本文每节都给了 jsonpath/jq 校验命令）。
3. 任何一步失败都先记录证据，再继续下一步排障，不要直接跳步。

建议统一留痕目录：

```bash
export E2E_LOG_DIR="/tmp/dr-e2e-imap-$(date +%Y%m%d-%H%M%S)"
mkdir -p "${E2E_LOG_DIR}"
echo "E2E logs in ${E2E_LOG_DIR}"
```

建议把关键输出都落盘（示例）：

```bash
kubectl get clusters.testudo.softcdata.com -o yaml > "${E2E_LOG_DIR}/clusters.yaml"
kubectl get disasterconfigs.testudo.softcdata.com -o yaml > "${E2E_LOG_DIR}/configs.yaml"
kubectl -n disaster-system get disasterinstances.testudo.softcdata.com -o yaml > "${E2E_LOG_DIR}/instances.yaml"
```

## 3. 一次性准备

### 3.1 命令依赖

```bash
kubectl version --client
jq --version
curl --version
```

`skopeo` 用于向私有 registry 灌镜像，若缺失请安装：

```bash
# Ubuntu
sudo apt-get update
sudo apt-get install -y skopeo
```

### 3.2 测试变量

```bash
# API 根路径
export API_BASE="http://127.0.0.1:8080/apis"

# 复用已有 cluster 的 kubeconfig 源
export SRC_BASE_CLUSTER="c170"
export DST_BASE_CLUSTER="c171"

# 新建测试 cluster 名称（平台对象）
export SRC_TEST_CLUSTER="c170-imap"
export DST_TEST_CLUSTER="c171-imap"

# 新建 config/instance
export TEST_CONFIG="dc-imap-170-171"
export TEST_INSTANCE="di-imap-170-171"
export INSTANCE_NS="disaster-system"

# 业务工作负载
export WORKLOAD_NS="dr-e2e-imap"
export WORKLOAD_LABEL_KEY="app"
export WORKLOAD_LABEL_VAL="imap-demo"
export WORKLOAD_NAME="imap-demo"

# 两端 k3s node IP
export SRC_NODE_IP="192.0.2.170"
export DST_NODE_IP="192.0.2.171"

# 两端 registry NodePort
export SRC_REG_PORT="32070"
export DST_REG_PORT="32071"

# 两个 registry 前缀（用于 imageSources）
export SRC_REG_PREFIX="${SRC_NODE_IP}:${SRC_REG_PORT}/e2e"
export DST_REG_PREFIX="${DST_NODE_IP}:${DST_REG_PORT}/e2e"
export DST_REG_PREFIX_V2="${DST_NODE_IP}:${DST_REG_PORT}/e2e-v2"

# 测试镜像
export TEST_IMAGE_TAG="nginx:1.25"
export SRC_IMAGE="${SRC_REG_PREFIX}/${TEST_IMAGE_TAG}"
export DST_IMAGE="${DST_REG_PREFIX}/${TEST_IMAGE_TAG}"
export DST_IMAGE_V2="${DST_REG_PREFIX_V2}/${TEST_IMAGE_TAG}"
```

### 3.3 可选：API Header

如果你的 server 开启了鉴权，在下列 curl 命令统一加 Header：

```bash
export AUTH_HEADER="Authorization: Bearer <YOUR_TOKEN>"
```

未开启鉴权时：

```bash
export AUTH_HEADER=""
```

## 4. 基线检查与快照

```bash
# 服务监听
ss -lntp | rg '(:8080|:8081)'

# 现有对象快照
kubectl get clusters.testudo.softcdata.com -o wide
kubectl get disasterconfigs.testudo.softcdata.com -o wide
kubectl get disasterinstances.testudo.softcdata.com -A -o wide
```

保存快照（便于回放和比对）：

```bash
kubectl get clusters.testudo.softcdata.com -o yaml > /tmp/before-clusters.yaml
kubectl get disasterconfigs.testudo.softcdata.com -o yaml > /tmp/before-configs.yaml
kubectl get disasterinstances.testudo.softcdata.com -A -o yaml > /tmp/before-instances.yaml
```

## 5. 升级 CRD 与进程（必须确认）

### 5.1 升级 CRD

```bash
cd /home/chenxi/YS/disaster-operator

kubectl apply -f config/crd/bases/testudo.softcdata.com_disasterconfigs.yaml
kubectl apply -f config/crd/bases/testudo.softcdata.com_disasterinstances.yaml
```

验证 CRD 语义：

```bash
# DisasterConfig 应包含 imageRewrite
kubectl get crd disasterconfigs.testudo.softcdata.com \
  -o jsonpath='{.spec.versions[?(@.name=="v1")].schema.openAPIV3Schema.properties.spec.properties.imageRewrite}' | jq .

# DisasterInstance 不应再包含 imageRewrite
kubectl get crd disasterinstances.testudo.softcdata.com \
  -o jsonpath='{.spec.versions[?(@.name=="v1")].schema.openAPIV3Schema.properties.spec.properties.imageRewrite}' | jq .
```

### 5.2 重启本地 server/operator（若你已重启可跳过）

```bash
# 停旧进程（按端口定位）
SERVER_PID=$(ss -lntp | awk -F'[=, ]+' '/:8080/{for(i=1;i<=NF;i++) if($i=="pid") {print $(i+1); exit}}')
OP_PID=$(ss -lntp | awk -F'[=, ]+' '/:8081/{for(i=1;i<=NF;i++) if($i=="pid") {print $(i+1); exit}}')

[ -n "$SERVER_PID" ] && kill "$SERVER_PID"
[ -n "$OP_PID" ] && kill "$OP_PID"

# 启动 server
cd /home/chenxi/YS/disaster-server
nohup go run ./cmd/main.go server > /tmp/disaster-server.log 2>&1 &

# 启动 operator
cd /home/chenxi/YS/disaster-operator
nohup go run ./cmd/main.go > /tmp/disaster-operator.log 2>&1 &

sleep 3
ss -lntp | rg '(:8080|:8081)'
```

### 5.3 API 行为冒烟

```bash
# Cluster spec 应能返回 imageSources 字段
curl -sS -H "$AUTH_HEADER" \
  "${API_BASE}/cluster.testudo.softcdata.com/v1/clusters/${SRC_BASE_CLUSTER}" | jq '.data.spec'

# Config spec 应能返回 imageRewrite 字段（可能为空）
curl -sS -H "$AUTH_HEADER" \
  "${API_BASE}/disasterconfigs.testudo.softcdata.com/v1/configs/i171-170" | jq '.data.spec'

# Instance spec 不应包含 imageRewrite
curl -sS -H "$AUTH_HEADER" \
  "${API_BASE}/disasterinstances.testudo.softcdata.com/v1/instances/c171-170?namespace=disaster-system" | jq '.data.spec'
```

## 6. 提取 170/171 kubeconfig（用于远端操作）

```bash
kubectl get clusters.testudo.softcdata.com ${SRC_BASE_CLUSTER} -o jsonpath='{.spec.kubeConfig}' | base64 -d > /tmp/${SRC_BASE_CLUSTER}.kubeconfig
kubectl get clusters.testudo.softcdata.com ${DST_BASE_CLUSTER} -o jsonpath='{.spec.kubeConfig}' | base64 -d > /tmp/${DST_BASE_CLUSTER}.kubeconfig

kubectl --kubeconfig /tmp/${SRC_BASE_CLUSTER}.kubeconfig get nodes -o wide
kubectl --kubeconfig /tmp/${DST_BASE_CLUSTER}.kubeconfig get nodes -o wide
```

## 7. 在 170/171 搭建测试 registry

### 7.1 部署 registry（每个集群一个）

```bash
cat >/tmp/registry-manifest.yaml <<'YAML'
apiVersion: v1
kind: Namespace
metadata:
  name: dr-e2e-registry
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: local-registry
  namespace: dr-e2e-registry
spec:
  replicas: 1
  selector:
    matchLabels:
      app: local-registry
  template:
    metadata:
      labels:
        app: local-registry
    spec:
      containers:
      - name: registry
        image: registry:2
        imagePullPolicy: IfNotPresent
        ports:
        - containerPort: 5000
        env:
        - name: REGISTRY_STORAGE_DELETE_ENABLED
          value: "true"
        volumeMounts:
        - name: data
          mountPath: /var/lib/registry
      volumes:
      - name: data
        emptyDir: {}
YAML

kubectl --kubeconfig /tmp/${SRC_BASE_CLUSTER}.kubeconfig apply -f /tmp/registry-manifest.yaml
kubectl --kubeconfig /tmp/${DST_BASE_CLUSTER}.kubeconfig apply -f /tmp/registry-manifest.yaml
```

分别暴露 NodePort：

```bash
cat >/tmp/registry-svc-src.yaml <<YAML
apiVersion: v1
kind: Service
metadata:
  name: local-registry-nodeport
  namespace: dr-e2e-registry
spec:
  type: NodePort
  selector:
    app: local-registry
  ports:
  - name: registry
    port: 5000
    targetPort: 5000
    nodePort: ${SRC_REG_PORT}
YAML

cat >/tmp/registry-svc-dst.yaml <<YAML
apiVersion: v1
kind: Service
metadata:
  name: local-registry-nodeport
  namespace: dr-e2e-registry
spec:
  type: NodePort
  selector:
    app: local-registry
  ports:
  - name: registry
    port: 5000
    targetPort: 5000
    nodePort: ${DST_REG_PORT}
YAML

kubectl --kubeconfig /tmp/${SRC_BASE_CLUSTER}.kubeconfig apply -f /tmp/registry-svc-src.yaml
kubectl --kubeconfig /tmp/${DST_BASE_CLUSTER}.kubeconfig apply -f /tmp/registry-svc-dst.yaml

kubectl --kubeconfig /tmp/${SRC_BASE_CLUSTER}.kubeconfig -n dr-e2e-registry rollout status deploy/local-registry --timeout=120s
kubectl --kubeconfig /tmp/${DST_BASE_CLUSTER}.kubeconfig -n dr-e2e-registry rollout status deploy/local-registry --timeout=120s
```

### 7.2 配置 k3s/containerd 允许拉取 HTTP registry（节点侧）

在 `192.0.2.170` 和 `192.0.2.171` 分别执行：

```bash
sudo mkdir -p /etc/rancher/k3s
sudo tee /etc/rancher/k3s/registries.yaml >/dev/null <<YAML
mirrors:
  "${SRC_NODE_IP}:${SRC_REG_PORT}":
    endpoint:
      - "http://${SRC_NODE_IP}:${SRC_REG_PORT}"
  "${DST_NODE_IP}:${DST_REG_PORT}":
    endpoint:
      - "http://${DST_NODE_IP}:${DST_REG_PORT}"
configs:
  "${SRC_NODE_IP}:${SRC_REG_PORT}":
    tls:
      insecure_skip_verify: true
  "${DST_NODE_IP}:${DST_REG_PORT}":
    tls:
      insecure_skip_verify: true
YAML

sudo systemctl restart k3s
```

重启后检查节点 Ready：

```bash
kubectl --kubeconfig /tmp/${SRC_BASE_CLUSTER}.kubeconfig get nodes
kubectl --kubeconfig /tmp/${DST_BASE_CLUSTER}.kubeconfig get nodes
```

### 7.3 灌入测试镜像

```bash
# 源 registry
time skopeo copy --all \
  --dest-tls-verify=false \
  docker://docker.io/library/${TEST_IMAGE_TAG} \
  docker://${SRC_IMAGE}

# 目标 registry（v1 前缀）
time skopeo copy --all \
  --dest-tls-verify=false \
  docker://docker.io/library/${TEST_IMAGE_TAG} \
  docker://${DST_IMAGE}

# 目标 registry（v2 前缀，用于动态读取验证）
time skopeo copy --all \
  --dest-tls-verify=false \
  docker://docker.io/library/${TEST_IMAGE_TAG} \
  docker://${DST_IMAGE_V2}

# catalog 验证
curl -sS "http://${SRC_NODE_IP}:${SRC_REG_PORT}/v2/_catalog" | jq .
curl -sS "http://${DST_NODE_IP}:${DST_REG_PORT}/v2/_catalog" | jq .
```

## 8. 通过 API 新建测试 Cluster，并配置 imageSources

### 8.1 复制已有 c170/c171 kubeconfig，创建测试 Cluster

```bash
SRC_KCFG_B64=$(kubectl get clusters.testudo.softcdata.com ${SRC_BASE_CLUSTER} -o jsonpath='{.spec.kubeConfig}')
DST_KCFG_B64=$(kubectl get clusters.testudo.softcdata.com ${DST_BASE_CLUSTER} -o jsonpath='{.spec.kubeConfig}')

curl -sS -H "$AUTH_HEADER" -H 'Content-Type: application/json' \
  -X POST "${API_BASE}/cluster.testudo.softcdata.com/v1/clusters" \
  -d "{\"name\":\"${SRC_TEST_CLUSTER}\",\"description\":\"e2e image rewrite src\",\"kubeConfig\":\"${SRC_KCFG_B64}\"}" | jq .

curl -sS -H "$AUTH_HEADER" -H 'Content-Type: application/json' \
  -X POST "${API_BASE}/cluster.testudo.softcdata.com/v1/clusters" \
  -d "{\"name\":\"${DST_TEST_CLUSTER}\",\"description\":\"e2e image rewrite dst\",\"kubeConfig\":\"${DST_KCFG_B64}\"}" | jq .
```

等待两条 Cluster Ready：

```bash
for c in ${SRC_TEST_CLUSTER} ${DST_TEST_CLUSTER}; do
  echo "waiting cluster ${c} ready..."
  for i in {1..60}; do
    st=$(kubectl get clusters.testudo.softcdata.com "$c" -o jsonpath='{.status.status}' 2>/dev/null || true)
    if [ "$st" = "Ready" ]; then
      echo "cluster ${c} is Ready"
      break
    fi
    sleep 5
  done
done
```

### 8.2 PATCH imageSources

```bash
# 源 cluster：prod-main -> 170 registry
curl -sS -H "$AUTH_HEADER" -H 'Content-Type: application/json' \
  -X PATCH "${API_BASE}/cluster.testudo.softcdata.com/v1/clusters/${SRC_TEST_CLUSTER}" \
  -d "{\"imageSources\":[{\"name\":\"prod-main\",\"registry\":\"${SRC_REG_PREFIX}\"}]}" | jq .

# 目标 cluster：dr-main + dr-main-v2 -> 171 registry
curl -sS -H "$AUTH_HEADER" -H 'Content-Type: application/json' \
  -X PATCH "${API_BASE}/cluster.testudo.softcdata.com/v1/clusters/${DST_TEST_CLUSTER}" \
  -d "{\"imageSources\":[{\"name\":\"dr-main\",\"registry\":\"${DST_REG_PREFIX}\"},{\"name\":\"dr-main-v2\",\"registry\":\"${DST_REG_PREFIX_V2}\"}]}" | jq .

# 复核
curl -sS -H "$AUTH_HEADER" "${API_BASE}/cluster.testudo.softcdata.com/v1/clusters/${SRC_TEST_CLUSTER}" | jq '.data.spec.imageSources'
curl -sS -H "$AUTH_HEADER" "${API_BASE}/cluster.testudo.softcdata.com/v1/clusters/${DST_TEST_CLUSTER}" | jq '.data.spec.imageSources'
```

## 9. 在源集群部署业务 workload（使用源镜像前缀）

```bash
kubectl --kubeconfig /tmp/${SRC_BASE_CLUSTER}.kubeconfig create ns ${WORKLOAD_NS} --dry-run=client -o yaml | kubectl --kubeconfig /tmp/${SRC_BASE_CLUSTER}.kubeconfig apply -f -

cat >/tmp/imap-workload.yaml <<YAML
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ${WORKLOAD_NAME}
  namespace: ${WORKLOAD_NS}
  labels:
    ${WORKLOAD_LABEL_KEY}: ${WORKLOAD_LABEL_VAL}
spec:
  replicas: 1
  selector:
    matchLabels:
      ${WORKLOAD_LABEL_KEY}: ${WORKLOAD_LABEL_VAL}
  template:
    metadata:
      labels:
        ${WORKLOAD_LABEL_KEY}: ${WORKLOAD_LABEL_VAL}
    spec:
      containers:
      - name: web
        image: ${SRC_IMAGE}
        imagePullPolicy: IfNotPresent
        ports:
        - containerPort: 80
YAML

kubectl --kubeconfig /tmp/${SRC_BASE_CLUSTER}.kubeconfig apply -f /tmp/imap-workload.yaml
kubectl --kubeconfig /tmp/${SRC_BASE_CLUSTER}.kubeconfig -n ${WORKLOAD_NS} rollout status deploy/${WORKLOAD_NAME} --timeout=180s
kubectl --kubeconfig /tmp/${SRC_BASE_CLUSTER}.kubeconfig -n ${WORKLOAD_NS} get deploy/${WORKLOAD_NAME} -o jsonpath='{.spec.template.spec.containers[0].image}'; echo
```

## 10. 创建 DisasterConfig（包含 imageRewrite）

复用现网已有配置 `i171-170` 的策略与存储，减少变量：

```bash
BASE_CFG_JSON=$(curl -sS -H "$AUTH_HEADER" "${API_BASE}/disasterconfigs.testudo.softcdata.com/v1/configs/i171-170")

STORAGE_REPO=$(echo "$BASE_CFG_JSON" | jq -r '.data.spec.storageRepository')
DATA_SYNC_POLICY=$(echo "$BASE_CFG_JSON" | jq -r '.data.spec.dataSyncPolicy')
RESOURCE_SYNC_POLICY=$(echo "$BASE_CFG_JSON" | jq -r '.data.spec.resourcesSyncPolicy')
DATA_SYNC_TYPE=$(echo "$BASE_CFG_JSON" | jq -r '.data.spec.dataSyncType')

echo "STORAGE_REPO=${STORAGE_REPO}"
echo "DATA_SYNC_POLICY=${DATA_SYNC_POLICY}"
echo "RESOURCE_SYNC_POLICY=${RESOURCE_SYNC_POLICY}"
echo "DATA_SYNC_TYPE=${DATA_SYNC_TYPE}"
```

创建测试 Config：

```bash
curl -sS -H "$AUTH_HEADER" -H 'Content-Type: application/json' \
  -X POST "${API_BASE}/disasterconfigs.testudo.softcdata.com/v1/configs" \
  -d "{
    \"name\": \"${TEST_CONFIG}\",
    \"description\": \"e2e image rewrite config\",
    \"sourceCluster\": \"${SRC_TEST_CLUSTER}\",
    \"targetCluster\": \"${DST_TEST_CLUSTER}\",
    \"storageRepository\": \"${STORAGE_REPO}\",
    \"dataSyncType\": \"${DATA_SYNC_TYPE}\",
    \"resourcesSyncPolicy\": \"${RESOURCE_SYNC_POLICY}\",
    \"dataSyncPolicy\": \"${DATA_SYNC_POLICY}\",
    \"imageRewrite\": {
      \"enabled\": true,
      \"applyTo\": [\"resourceSync\", \"drill\"],
      \"unmatchedPolicy\": \"Fail\",
      \"mappings\": [
        {\"sourceImageSource\": \"prod-main\", \"targetImageSource\": \"dr-main\"}
      ]
    }
  }" | jq .

curl -sS -H "$AUTH_HEADER" \
  "${API_BASE}/disasterconfigs.testudo.softcdata.com/v1/configs/${TEST_CONFIG}" | jq '.data.spec'
```

## 11. 创建 DisasterInstance（不携带 imageRewrite）

```bash
curl -sS -H "$AUTH_HEADER" -H 'Content-Type: application/json' \
  -X POST "${API_BASE}/disasterinstances.testudo.softcdata.com/v1/instances" \
  -d "{
    \"name\": \"${TEST_INSTANCE}\",
    \"namespace\": \"${INSTANCE_NS}\",
    \"config\": \"${TEST_CONFIG}\",
    \"namespaces\": [\"${WORKLOAD_NS}\"],
    \"labelSelector\": {\"matchLabels\": {\"${WORKLOAD_LABEL_KEY}\": \"${WORKLOAD_LABEL_VAL}\"}}
  }" | jq .

# 验证 instance spec 不含 imageRewrite
curl -sS -H "$AUTH_HEADER" \
  "${API_BASE}/disasterinstances.testudo.softcdata.com/v1/instances/${TEST_INSTANCE}?namespace=${INSTANCE_NS}" | jq '.data.spec'
```

等待 Instance 到 `Protected`：

```bash
for i in {1..120}; do
  state=$(kubectl -n ${INSTANCE_NS} get disasterinstances.testudo.softcdata.com ${TEST_INSTANCE} -o jsonpath='{.status.fsmState}' 2>/dev/null || true)
  echo "instance state=${state}"
  [ "$state" = "Protected" ] && break
  sleep 5
done
kubectl -n ${INSTANCE_NS} get disasterinstances.testudo.softcdata.com ${TEST_INSTANCE} -o wide
```

## 12. 先验证 ResourceSync 的镜像重写规则

```bash
RS_NAME=$(kubectl -n ${INSTANCE_NS} get disasterinstances.testudo.softcdata.com ${TEST_INSTANCE} -o jsonpath='{.status.resourceSyncName}')
echo "RS_NAME=${RS_NAME}"

LAST_AR=$(kubectl -n ${INSTANCE_NS} get apprestores.testudo.softcdata.com \
  -l testudo.softcdata.com/app-resource-owner-name=${RS_NAME} -o json | \
  jq -r '.items | sort_by(.metadata.creationTimestamp) | last | .metadata.name')
echo "LAST_AR=${LAST_AR}"

kubectl -n ${INSTANCE_NS} get apprestores.testudo.softcdata.com ${LAST_AR} -o json | \
  jq '.spec.resourceModifierRules[]? | select(.conditions.groupResource=="deployments.apps") | .patches[]? | select(.path|test("/image$"))'
```

期望：镜像 patch 的 `value` 指向 `${DST_IMAGE}` 前缀。

## 13. 执行 Failover 并验证镜像映射

### 13.1 触发 Failover

```bash
FAILOVER_RESP=$(curl -sS -H "$AUTH_HEADER" -H 'Content-Type: application/json' \
  -X POST "${API_BASE}/disasterinstances.testudo.softcdata.com/v1/instances/${TEST_INSTANCE}/actions?namespace=${INSTANCE_NS}" \
  -d '{
    "operation":"failover",
    "config":{
      "force":true,
      "skipFinalSync":false,
      "skipScaleDownSource":false,
      "waitUntilReady":true,
      "timeoutMinutes":30
    }
  }')

echo "$FAILOVER_RESP" | jq .
OP_ID=$(echo "$FAILOVER_RESP" | jq -r '.data.operationID')
echo "OP_ID=${OP_ID}"
```

### 13.2 轮询 Operation

```bash
for i in {1..180}; do
  st=$(kubectl -n ${INSTANCE_NS} get disasteroperations.testudo.softcdata.com ${OP_ID} -o jsonpath='{.status.state}' 2>/dev/null || true)
  step=$(kubectl -n ${INSTANCE_NS} get disasteroperations.testudo.softcdata.com ${OP_ID} -o jsonpath='{.status.currentStep}' 2>/dev/null || true)
  echo "operation state=${st}, step=${step}"
  if [ "$st" = "Completed" ] || [ "$st" = "Failed" ]; then
    break
  fi
  sleep 5
done

kubectl -n ${INSTANCE_NS} get disasteroperations.testudo.softcdata.com ${OP_ID} -o yaml > /tmp/${OP_ID}.yaml
kubectl -n ${INSTANCE_NS} get disasteroperations.testudo.softcdata.com ${OP_ID} -o json | jq '.status'
```

### 13.3 验证切换结果

```bash
# Instance 角色变化
kubectl -n ${INSTANCE_NS} get disasterinstances.testudo.softcdata.com ${TEST_INSTANCE} -o json | jq '.status | {fsmState,primaryCluster,secondaryCluster}'

# 目标集群 deployment 镜像
kubectl --kubeconfig /tmp/${DST_BASE_CLUSTER}.kubeconfig -n ${WORKLOAD_NS} get deploy/${WORKLOAD_NAME} -o jsonpath='{.spec.template.spec.containers[0].image}'; echo

# 目标集群副本数
kubectl --kubeconfig /tmp/${DST_BASE_CLUSTER}.kubeconfig -n ${WORKLOAD_NS} get deploy/${WORKLOAD_NAME} -o jsonpath='{.status.readyReplicas}'; echo
```

期望：

1. `primaryCluster` 已切到 `${DST_TEST_CLUSTER}`。
2. 目标 deployment 镜像前缀为 `${DST_REG_PREFIX}`（非源前缀）。
3. Operation `status.state=Completed`。

## 14. 动态读取验证（不改 Instance）

### 14.1 仅修改 Config 的映射目标别名到 `dr-main-v2`

```bash
# 先读全量 config
CFG_FULL=$(curl -sS -H "$AUTH_HEADER" "${API_BASE}/disasterconfigs.testudo.softcdata.com/v1/configs/${TEST_CONFIG}")
DESC=$(echo "$CFG_FULL" | jq -r '.data.description // ""')

curl -sS -H "$AUTH_HEADER" -H 'Content-Type: application/json' \
  -X PUT "${API_BASE}/disasterconfigs.testudo.softcdata.com/v1/configs/${TEST_CONFIG}" \
  -d "{
    \"description\": \"${DESC}\",
    \"imageRewrite\": {
      \"enabled\": true,
      \"applyTo\": [\"resourceSync\", \"drill\"],
      \"unmatchedPolicy\": \"Fail\",
      \"mappings\": [
        {\"sourceImageSource\": \"prod-main\", \"targetImageSource\": \"dr-main-v2\"}
      ]
    }
  }" | jq .

curl -sS -H "$AUTH_HEADER" "${API_BASE}/disasterconfigs.testudo.softcdata.com/v1/configs/${TEST_CONFIG}" | jq '.data.spec.imageRewrite'
```

### 14.2 触发一次 `sync-resource`

```bash
SYNC_RESP=$(curl -sS -H "$AUTH_HEADER" -H 'Content-Type: application/json' \
  -X POST "${API_BASE}/disasterinstances.testudo.softcdata.com/v1/instances/${TEST_INSTANCE}/actions?namespace=${INSTANCE_NS}" \
  -d '{"operation":"sync-resource","config":{}}')

echo "$SYNC_RESP" | jq .
SYNC_OP=$(echo "$SYNC_RESP" | jq -r '.data.operationID')

for i in {1..120}; do
  st=$(kubectl -n ${INSTANCE_NS} get disasteroperations.testudo.softcdata.com ${SYNC_OP} -o jsonpath='{.status.state}' 2>/dev/null || true)
  echo "sync-resource operation state=${st}"
  if [ "$st" = "Completed" ] || [ "$st" = "Failed" ]; then
    break
  fi
  sleep 5
done
```

### 14.3 验证目标镜像已切为 v2 前缀

```bash
kubectl --kubeconfig /tmp/${DST_BASE_CLUSTER}.kubeconfig -n ${WORKLOAD_NS} get deploy/${WORKLOAD_NAME} -o jsonpath='{.spec.template.spec.containers[0].image}'; echo
```

期望：镜像前缀变为 `${DST_REG_PREFIX_V2}`，且过程中未修改 Instance spec。

## 15. 错误路径验证（unmatchedPolicy=Fail）

### 15.1 构造未命中镜像

```bash
# 把源 workload 改成不会命中的镜像前缀
kubectl --kubeconfig /tmp/${SRC_BASE_CLUSTER}.kubeconfig -n ${WORKLOAD_NS} \
  set image deploy/${WORKLOAD_NAME} web=docker.io/library/nginx:1.25
```

### 15.2 触发 sync-resource，预期失败

```bash
UNMATCH_RESP=$(curl -sS -H "$AUTH_HEADER" -H 'Content-Type: application/json' \
  -X POST "${API_BASE}/disasterinstances.testudo.softcdata.com/v1/instances/${TEST_INSTANCE}/actions?namespace=${INSTANCE_NS}" \
  -d '{"operation":"sync-resource","config":{}}')

echo "$UNMATCH_RESP" | jq .
UNMATCH_OP=$(echo "$UNMATCH_RESP" | jq -r '.data.operationID')

for i in {1..120}; do
  st=$(kubectl -n ${INSTANCE_NS} get disasteroperations.testudo.softcdata.com ${UNMATCH_OP} -o jsonpath='{.status.state}' 2>/dev/null || true)
  echo "unmatched operation state=${st}"
  if [ "$st" = "Completed" ] || [ "$st" = "Failed" ]; then
    break
  fi
  sleep 5
done

kubectl -n ${INSTANCE_NS} get disasteroperations.testudo.softcdata.com ${UNMATCH_OP} -o json | jq '.status'
```

期望：Operation 失败，错误信息包含 unmatched 语义。

## 16. 清理步骤

按顺序清理，避免依赖冲突：

```bash
# 删除 instance/config
kubectl -n ${INSTANCE_NS} delete disasterinstances.testudo.softcdata.com ${TEST_INSTANCE} --ignore-not-found
kubectl delete disasterconfigs.testudo.softcdata.com ${TEST_CONFIG} --ignore-not-found

# 删除测试 cluster（确保无对象依赖后执行）
kubectl delete clusters.testudo.softcdata.com ${SRC_TEST_CLUSTER} --ignore-not-found
kubectl delete clusters.testudo.softcdata.com ${DST_TEST_CLUSTER} --ignore-not-found

# 删除业务 workload 命名空间
kubectl --kubeconfig /tmp/${SRC_BASE_CLUSTER}.kubeconfig delete ns ${WORKLOAD_NS} --ignore-not-found
kubectl --kubeconfig /tmp/${DST_BASE_CLUSTER}.kubeconfig delete ns ${WORKLOAD_NS} --ignore-not-found

# 删除 registry
kubectl --kubeconfig /tmp/${SRC_BASE_CLUSTER}.kubeconfig delete ns dr-e2e-registry --ignore-not-found
kubectl --kubeconfig /tmp/${DST_BASE_CLUSTER}.kubeconfig delete ns dr-e2e-registry --ignore-not-found
```

如果不再需要 insecure registry 配置，可在 170/171 删除 `/etc/rancher/k3s/registries.yaml` 并重启 `k3s`。

## 17. 验收结论模板

测试结束后，建议按以下模板记录：

1. 环境版本：server commit、operator commit、CRD 版本。
2. 主路径结果：`Create Cluster -> Patch imageSources -> Create Config(imageRewrite) -> Create Instance -> Failover` 是否全部通过。
3. 证据：
   - Failover Operation ID
   - 目标 Deployment 最终镜像
   - AppRestore 中镜像 patch 片段
4. 动态读取结果：仅改 Config 后下一次 sync-resource 是否生效。
5. 错误路径结果：unmatchedPolicy=Fail 是否按预期失败并可观测。
