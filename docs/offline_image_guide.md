# Velero 离线部署场景与镜像预备方案

## 1. 场景背景 (Context)

本系统采用集中式管理架构，由 **管理集群** 统一管控多个 **业务集群（目标集群）** 的灾备能力。

### 1.1 部署流程
1.  **管理侧执行**：Velero 的安装过程是由管理集群触发的。
2.  **容器化 Helm**：管理服务内部启动一个容器，该容器集成了 Helm 工具。
3.  **配置注入**：系统将预定义的 `velero.values.yaml` 配置文件复制到该 Helm 容器中。
4.  **远程安装**：Helm 容器连接到目标业务集群，并根据 `velero.values.yaml` 的定义，在目标集群中部署 Velero。

### 1.2 关键约束 (Connectivity Constraints)
*   **纯内网环境**：客户的业务集群通常位于完全隔离的私有网络中，**无法访问互联网**（如 Docker Hub、Quay.io 等公网仓库）。
*   **镜像拉取限制**：目标集群在尝试启动 Velero Pod 时，只能从客户内网的私有镜像仓库（如 Harbor）拉取镜像。

## 2. 问题陈述 (Problem Statement)
如果 `velero.values.yaml` 配置文件中依然保留官方默认的公网镜像地址（如 `docker.io/velero/velero`），在触发安装后，目标集群将因为网络不可达而无法拉取镜像，导致 `ImagePullBackOff` 错误，整个灾备初始化流程失败。

因此，必须解决**配置文件的镜像地址**与**客户内网仓库实际镜像**的一致性问题。

## 3. 解决方案 (Solution)

### 3.1 镜像预备 (Image Pre-staging)
在产品交付前或部署阶段，必须将所有依赖的容器镜像“搬运”到客户的内网仓库中。

**必需镜像清单**：

| 组件 | 说明 | 原始镜像 | 内网仓库目标 (示例) |
| :--- | :--- | :--- | :--- |
| **Velero 主程序** | 核心控制器与 Agent | `velero/velero:v1.17.0` | `registry.example.com/disaster/velero:v1.17.0` |
| **AWS 插件** | 对接 S3/MinIO 存储 | `velero/velero-plugin-for-aws:v1.13.0` | `registry.example.com/disaster/velero-plugin-for-aws:v1.13.0` |

### 3.2 配置固化 (Configuration Alignment)
管理模块中使用的 `velero.values.yaml` 必须进行固化修改，将所有 `repository` 字段指向内网仓库地址。

**修改点 1：主镜像**
```yaml
image:
  # 必须修改为内网仓库地址
  repository: registry.example.com/disaster/velero
  tag: v1.17.0
  pullPolicy: IfNotPresent
```

**修改点 2：插件镜像 (InitContainers)**
```yaml
initContainers:
  - name: velero-plugin-for-aws
    # 必须填写完整的内网镜像地址
    image: registry.example.com/disaster/velero-plugin-for-aws:v1.13.0
    imagePullPolicy: IfNotPresent
    volumeMounts:
      - mountPath: /target
        name: plugins
```

## 4. 实施总结
通过提前将镜像同步至私有仓库，并确保 Helm 模板配置 (`velero.values.yaml`) 指向该仓库，我们确保了在离线隔离的客户环境中，管理集群能够成功地将 Velero 系统“推送”并运行在各个业务集群上。
