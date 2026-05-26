# 提案：自动清理 Helm 锁 (Automatic Helm Lock Cleanup)

## 1. 背景 (Background)
在 `disaster-operator` 安装或升级 Velero 时，如果之前的操作异常中断（如 Pod 重启、超时或被手动终止），Helm 会在目标集群中遗留状态为 `pending-install` 或 `pending-upgrade` 的 Release Secret。这些残留的 Secret 充当了“锁”的角色，导致后续的 `helm upgrade --install` 操作因检测到 "another operation is in progress" 而失败，形成死锁。

目前解决此问题的唯一方法是手动介入，执行 `kubectl delete secret` 清理这些锁。为了提高 Operator 的自愈能力和鲁棒性，我们需要在代码中集成自动检测并清理这些僵尸锁的逻辑。

## 2. 目标 (Goals)
1.  **自动检测**：在执行 Helm 安装/升级之前，自动检测目标集群是否存在处于 `pending` 状态且已过期的 Velero Release Secret。
2.  **自动清理**：如果发现死锁，自动删除对应的 Secret，释放锁资源，允许新的安装流程继续。
3.  **安全性**：确保只清理属于 Velero 的、确实已卡死（例如最后更新时间超过一定阈值）的锁，避免误删正在正常运行的操作。

## 3. 设计方案 (Design)

### 3.1 检测逻辑
在 `ClusterReconciler` 的 `InstallVeleroInCluster` 方法执行 `helm upgrade` 之前，增加一个 `ensureNoHelmLocks` 步骤：

1.  使用目标集群的 Client 列出 `velero` 命名空间下所有标签为 `owner=helm, name=velero` 的 Secret。
2.  解析这些 Secret（Helm 3 使用 base64 编码和 gzip 压缩存储 Release 对象）。
3.  检查 Release 的状态 (`Info.Status`)。如果状态是 `pending-install`、`pending-upgrade` 或 `pending-rollback`。
4.  检查 Release 的最后更新时间。如果距离现在已经超过了“安全阈值”（例如 5-10 分钟，略大于 Helm 的默认超时时间），则认定为“僵尸锁”。

### 3.2 清理逻辑
- 对判定为“僵尸锁”的 Secret，执行 `client.Delete` 操作。
- 记录详细的日志（包含被删除的 Secret 名称、Release 版本、状态及判定原因）。
- 清理后，无需等待，直接继续执行后续的 `helm upgrade` 操作。

### 3.3 兜底策略
- 即使自动清理失败（如权限不足），也应尝试继续执行 `helm upgrade`，并记录错误，不阻断主流程。
- 可选：引入 `Annotation` 配置开关（如 `testudo.softcdata.com/auto-clean-helm-lock: "true"`），默认开启，允许用户关闭此特定行为。

## 4. 任务清单 (Tasks)

### 4.1 引入依赖
- [ ] 引入 Helm SDK 相关的包（用于解析 Release Secret），或者复用现有的 Secret 解析逻辑（若只需通过 Label 和简单的状态字段判断）。
  - *注意：为了减小依赖体积，如果不需要完整的 Helm SDK，可以直接操作 K8s Secret 并解析其 Labels/Status 字段（Helm Secret 的 Labels 中通常包含状态信息）。*
  - 根据 Helm 原理，状态存储在 Secret 的 `.data.release` 字段（经过编码）。更简单的方式是检查 Secret 的 Labels，Helm v3 的 Secret Labels 包含 `status` 吗？
  - *调研*：Helm Secret 的 Labels 为 `owner=helm`, `name=<release-name>`, `status=<status>`。如果 Label 中直接有 status，则无需完整解析 Secret data，性能更高且无额外依赖。
  - **任务调整**：先验证 Helm Secret 的 Labels 是否包含 `status` 字段。
    - 经查，Helm 3 的 Secret Labels 确实包含 `status` (key: `status`)。
    - 因此，无需引入庞大的 Helm SDK，只需使用 K8s Client 过滤 Labels 即可。

### 4.2 实现清理逻辑 (`internal/controller/cluster_controller.go`)
- [ ] 实现 `PreInstallCheck` 或 `CleanupZombieLocks` 函数。
  - 入参：`ctx`, `targetClient` (目标集群客户端), `namespace` ("velero")。
  - 逻辑：
    1. List Secrets with Labels: `owner=helm`, `name=velero`, `status` in [`pending-install`, `pending-upgrade`, `pending-rollback`].
    2. 遍历结果，检查 Secret 的 `metadata.modified` (如有) 或 `creationTimestamp`。考虑到 `pending` 状态通常是临时的，如果存在且时间较旧（> 10m），则视为僵尸。
    3. 执行 Delete。
- [ ] 在 `InstallVeleroInCluster` 中，在调用 `CommandExecutor.Run` 之前调用此清理函数。

### 4.3 测试 (`internal/controller/cluster_controller_test.go`)
- [ ] 增加单元测试用例：
  - 场景：存在一个 `status=pending-upgrade` 且创建时间为 1 小时前的 Secret。
  - 预期：`CleanupZombieLocks` 被调用后，该 Secret 被删除。
  - 场景：存在一个 `status=deployed` 的 Secret。
  - 预期：该 Secret **不** 被删除。
  - 场景：存在一个 `status=pending-upgrade` 但创建时间为 10 秒前的 Secret（正在进行中）。
  - 预期：该 Secret **不** 被删除。

## 5. 验证 (Verification)
- 编译通过。
- 单元测试通过。
- 在模拟环境手动创建一个 Label 为 `status=pending-upgrade` 的 Secret，触发 Reconcile，验证是否被自动删除。
