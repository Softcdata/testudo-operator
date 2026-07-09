## 1. 设计与规范

- [ ] 1.1 确认 Trafficless runtime 复用 `Cluster.spec.veleroInstall.imageRegistry`，不引入新 API 字段。
- [ ] 1.2 明确 DataSync、Drill、反向同步场景均按“本次恢复目标集群”解析镜像。
- [ ] 1.3 明确业务恢复 namespace 需要独立 pull secret，不能只依赖 `velero` namespace。

## 2. Operator 实现

- [ ] 2.1 新增 Trafficless runtime resolver，支持 explicit、targetClusterRegistry、default 三类来源。
- [ ] 2.2 将 CRD/default 文案统一为 `busybox:1.36`，并兼容历史 `busybox:latest`。
- [ ] 2.3 将 DataSync data restore 改为使用 resolver 输出构建 Trafficless modifier。
- [ ] 2.4 将 Drill data restore 改为显式传入目标集群 runtime modifier。
- [ ] 2.5 将目标集群 registry credential 同步到 DataSync 恢复 namespace。
- [ ] 2.6 将目标集群 registry credential 同步到 Drill 映射后的目标 namespace。
- [ ] 2.7 在 Trafficless modifier 中按需注入 `/spec/imagePullSecrets`。
- [ ] 2.8 评估并实现 `serviceAccountName=default` 与 `automountServiceAccountToken=false` 安全 patch。
- [ ] 2.9 确保 cleanupTrafficlessPods 能识别私有仓库派生后的 Trafficless Pod。
- [ ] 2.10 在 DataSync data restore 前清理目标 namespace 中旧 Trafficless Pod 与可安全删除的冲突 Pod，避免更新 Pod 不可变字段。
- [ ] 2.11 在 Drill data restore 前按 namespaceMapping 清理目标 namespace 中旧 Trafficless Pod 与可安全删除的冲突 Pod。
- [ ] 2.12 若目标 Pod 仍存在且无法安全删除，在 AppRestore 创建前失败并输出明确错误，不让 Velero 进入 Pod Update。

## 3. 测试

- [ ] 3.1 单测：DataSync 使用目标集群 `veleroInstall.imageRegistry` 派生 busybox 镜像。
- [ ] 3.2 单测：源/目标集群均配置 registry 时使用目标集群 registry。
- [ ] 3.3 单测：显式 `trafficlessConfig.image` 优先于目标 registry。
- [ ] 3.4 单测：历史 `busybox:latest` 被视为默认值并按目标 registry 重写。
- [ ] 3.5 单测：registry credential 同步到业务 namespace 并注入 imagePullSecrets。
- [ ] 3.6 单测：Drill namespaceMapping 同步 pull secret 到目标 namespace。
- [ ] 3.7 回归：无 registry 配置时仍使用 `busybox:1.36`。
- [ ] 3.8 单测：目标 namespace 存在同名 Pod 时，DataSync 在 AppRestore 创建前先清理冲突 Pod。
- [ ] 3.9 单测：Drill namespaceMapping 后存在同名 Pod 时，先清理映射后目标 namespace 的冲突 Pod。
- [ ] 3.10 单测：冲突 Pod 无法删除时，构建/准备恢复失败且不会创建 AppRestore。

## 4. 验证与交付

- [ ] 4.1 运行 `go test ./internal/controller/datasync ./internal/controller/disasteroperation ./internal/controller/restore ./internal/controller`。
- [ ] 4.2 运行 `make manifests` 并检查 CRD 变更。
- [ ] 4.3 运行 `openspec validate add-target-registry-trafficless-restore --strict`。
- [ ] 4.4 更新离线镜像清单，要求客户仓库包含 `<imageRegistry>/busybox:1.36`。
