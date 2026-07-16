## 1. 设计与规范

- [x] 1.1 确认 Trafficless runtime 复用 `Cluster.spec.veleroInstall.imageRegistry`，不引入新 API 字段。
- [x] 1.2 明确 DataSync、Drill、failover/reprotect 后反向同步场景均按“本次恢复目标集群”解析 busybox 镜像。
- [x] 1.3 明确业务恢复 namespace 需要独立 pull secret，不能只依赖 `velero` namespace。
- [x] 1.4 明确本提案与业务镜像前缀替换、`imageSources`、动态镜像改写无关。

## 2. Operator 实现

- [x] 2.1 新增 Trafficless runtime resolver，支持 explicit、targetClusterRegistry、default 三类来源。
- [x] 2.2 将 CRD/default 文案统一为 `busybox:1.36`，并兼容历史 `busybox:latest`。
- [x] 2.3 将 DataSync data restore 改为使用 `resolveClusters(instance, config)` 得到的本次 targetCluster 解析 busybox。
- [x] 2.4 将 Drill data restore 改为使用 `executeDrillRestoreData` 传入的 targetCluster 解析 busybox，并显式传入 runtime modifier。
- [x] 2.5 将目标集群 registry credential 同步到 DataSync 恢复 namespace。
- [x] 2.6 将目标集群 registry credential 同步到 Drill 映射后的目标 namespace。
- [x] 2.7 在 Trafficless modifier 中按需注入 `/spec/imagePullSecrets`。
- [x] 2.8 保持现有 Trafficless labels、ownerReferences、command、args 语义，不新增业务 Pod 冲突清理或 serviceAccount patch。

## 3. 测试

- [x] 3.1 单测：DataSync 使用目标集群 `veleroInstall.imageRegistry` 派生 busybox 镜像。
- [x] 3.2 单测：源/目标集群均配置 registry 时使用目标集群 registry。
- [x] 3.3 单测：failover 角色反转后 DataSync 使用当前恢复目标集群 registry。
- [x] 3.4 单测：历史 `busybox:latest` 被视为默认值并按目标 registry 重写。
- [x] 3.5 单测：registry credential 同步到业务 namespace 并注入 imagePullSecrets。
- [x] 3.6 单测：Drill namespaceMapping 同步 pull secret 到目标 namespace。
- [x] 3.7 回归：无 registry 配置时仍使用 `busybox:1.36`。
- [x] 3.8 单测：显式 `trafficlessConfig.image` 优先于目标 registry。
- [x] 3.9 单测：Drill 未显式配置 trafficlessConfig 时仍按演练 targetCluster 派生 busybox，不能回退公网 busybox。
- [x] 3.10 回归：ResourceSync 业务镜像前缀替换规则不参与 Trafficless busybox 解析。

## 4. 验证与交付

- [x] 4.1 运行 `go test ./internal/controller/datasync ./internal/controller/disasteroperation ./internal/controller/restore ./internal/controller`。
- [x] 4.2 运行 `make manifests` 并检查 CRD 变更。
- [x] 4.3 运行 `openspec validate add-target-registry-trafficless-restore --strict`。
- [x] 4.4 更新离线镜像清单，要求客户仓库包含 `<imageRegistry>/busybox:1.36`。
