# 灾难恢复系统部署实施指南

本文档详细描述了如何从源码构建 `disaster-operator` 和 `disaster-server`，并将其部署到生产环境（支持 x86_64 和 ARM64 架构）。

## 1. 环境依赖

### 开发/构建环境
- **操作系统**: Linux/macOS
- **Docker**: v19.03+ (支持 BuildX)
- **Golang**: v1.20+
- **Make**: v3.81+
- **Kubectl**: v1.19+
- **Helm**: v3.0+

### 生产环境
- **Kubernetes**: v1.19+ (支持 x86_64 或 ARM64)
- **Velero**: v1.10+ (可选，operator 会尝试安装 CRD)
- **Container Registry**: 私有镜像仓库 (如 Harbor/Registry)

---

## 2. 源码构建与推送

### 2.1 设置镜像仓库信息
在开始之前，请确认你的私有镜像仓库地址。

```bash
export REGISTRY="192.168.120.138:5100"
export VERSION="v1.0.0-arm64"
```

### 2.2 构建 disaster-operator
支持多架构构建（x86_64 和 ARM64）。

```bash
cd disaster-operator

# 方式一：自动构建多架构并推送（推荐）
make docker-buildx IMG=${REGISTRY}/disaster-operator:${VERSION}

# 方式二：手动构建特定架构
make docker-build-arm64  # 或 docker-build (x86)
docker tag disaster-operator:latest ${REGISTRY}/disaster-operator:${VERSION}
docker push ${REGISTRY}/disaster-operator:${VERSION}
```

### 2.3 构建 disaster-server

```bash
cd disaster-server

# 方式一：使用脚本构建多架构并推送（推荐）
./scripts/docker-build.sh --buildx --push --image ${REGISTRY}/disaster-server --tag ${VERSION}

# 方式二：手动构建特定架构
./scripts/docker-build.sh --platform linux/arm64
docker tag disaster-server:latest ${REGISTRY}/disaster-server:${VERSION}
docker push ${REGISTRY}/disaster-server:${VERSION}
```

---

## 3. 生成部署清单

在构建机器上生成部署所需的 YAML 文件。

### 3.1 生成 CRD 清单
用于单独安装/升级 CRD。

```bash
cd disaster-operator
./bin/kustomize build config/crd > dist/crds.yaml
```

### 3.2 生成 Operator 部署清单
提供测试环境和生产环境两种配置。

**生产环境 (Production)**
- 镜像: ARM64 版本
- 端口: 8090 (健康检查)
- Namespace: disaster-system

```bash
cd disaster-operator
# 确保 config/overlays/production/kustomization.yaml 中的镜像地址正确
./bin/kustomize build config/overlays/production > dist/install-prod.yaml
```

**测试环境 (Test)**
- 镜像: x86 版本
- 端口: 8081 (健康检查)

```bash
cd disaster-operator
./bin/kustomize build config/default > dist/install-test.yaml
```

### 3.3 生成 Server 部署清单
生成 deployment 和 service 配置。

```bash
cd disaster-server
helm template disaster-server deploy/helm/disaster-server \
  --namespace disaster-system \
  --set image.repository=${REGISTRY}/disaster-server \
  --set image.tag=${VERSION} \
  --set service.type=NodePort \
  | sed 's/^metadata:$/metadata:\n  namespace: disaster-system/' \
  > dist/disaster-server-install.yaml
```

---

## 4. 生产环境部署

### 4.1 准备工作
将生成的清单文件复制到生产环境的主节点。

```bash
# 假设生产环境 IP 为 ip138
scp disaster-operator/dist/crds.yaml root@ip138:~/disaster-prod/
scp disaster-operator/dist/install-prod.yaml root@ip138:~/disaster-prod/
scp disaster-server/dist/disaster-server-install.yaml root@ip138:~/disaster-prod/
```

### 4.2 配置私有仓库信任 (如果需要)
如果私有仓库且使用 HTTP 协议，需在每个 K8s 节点配置信任。

**Containerd / RKE2 配置:**
创建 `/etc/k3s/registries.yaml` 或 `/etc/rancher/rke2/registries.yaml`:
```yaml
configs:
  "192.168.120.138:5100":
    tls:
      insecure_skip_verify: true
```

### 4.3 执行部署

登录到生产环境机器执行以下命令：

**第一步：安装 CRD**
```bash
# 安装 Disaster 系统 CRD
kubectl apply -f crds.yaml

# 安装 Velero CRD (如果集群未安装 Velero)
# 可从 disaster-operator/dist/velero-crds.yaml 获取
kubectl apply -f velero-crds.yaml
```

**第二步：部署 Operator**
```bash
# 创建命名空间（如果不存在）
kubectl create namespace disaster-operator-system

# 部署
kubectl apply -f install-prod.yaml

# 验证状态
kubectl get pods -n disaster-operator-system
```

**第三步：部署 Server**
```bash
# 创建命名空间（如果不存在）
kubectl create namespace disaster-system

# 部署
kubectl apply -f disaster-server-install.yaml

# 验证状态
kubectl get pods -n disaster-system
kubectl get svc -n disaster-system
```

---

## 5. 常见问题排查

### 镜像拉取失败 (ImagePullBackOff)
- 检查节点是否配置了私有仓库的 insecure registry。
- 检查镜像标签是否正确。
- 检查网络连通性。

### Operator 启动失败
- 检查日志: `kubectl logs -n disaster-operator-system <pod-name>`
- 常见原因: 缺少 Velero CRD。请确保已安装 `Backup`, `Restore` 等 CRD。

### Server 无法访问
- 检查 Service 类型 (NodePort/ClusterIP)。
- 检查 Pod 是否运行正常 (Readiness probe)。
- 检查数据库/集群连接配置 (ConfigMap)。
