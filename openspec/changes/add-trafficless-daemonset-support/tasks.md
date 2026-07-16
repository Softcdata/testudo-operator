## 1. Proposal

- [x] 1.1 完成当前实现、活跃 OpenSpec 变更和资源恢复边界扫描。
- [x] 1.2 创建 proposal、design、restore-builder 与 drill 规范增量。
- [x] 1.3 根据源/目标节点不一致的反馈，将源 selector 快照改为显式 target placement mapping。
- [x] 1.4 明确 reversible pair 仅覆盖基线双集群；第三集群 Drill 的 DaemonSet 路径必须失败关闭。
- [ ] 1.5 与动态镜像改写负责人确认 DaemonSet containers/initContainers 覆盖的实际合并状态。
- [ ] 1.6 获得提案批准；批准前不得修改产品代码。

## 2. Target Placement Model

- [ ] 2.1 为 DaemonSet 定义 placement path 集：nodeSelector、affinity、tolerations、topologySpreadConstraints、schedulerName。
- [ ] 2.2 复用 restorePolicy.modifierRules 的 reversible pair 解析 target placement，拒绝使用 veleroNative 作为 DaemonSet placement 输入。
- [ ] 2.3 校验每个保护范围内的 DaemonSet 存在唯一的 nodeSelector pair，并对源字段进行结构化 sourceValue 比对。
- [ ] 2.4 对源端非空的 affinity、tolerations、topologySpreadConstraints、schedulerName 强制要求显式 target pair 或显式清除值。
- [ ] 2.5 在资源恢复构建器增加 daemonsets.apps system-protect standby ResourceModifierRule，使其优先于用户 target nodeSelector。
- [ ] 2.6 在 replicas-<resourceSyncName> ConfigMap 增加向后兼容的 daemonset-placement-v1 快照读写，保存目标 placement、待机 marker、targetCluster 和规则指纹。
- [ ] 2.7 拒绝 targetCluster 与当前方向的 reversible pair 或 placement 快照不一致的 DaemonSet Drill，不得复用二元 pair 的 targetValue 到第三集群。

## 3. ResourceSync and Operation Lifecycle

- [ ] 3.1 ResourceSync 按 namespaces 与 labelSelector 收集 DaemonSet，解析当前方向的 target placement 并写入快照。
- [ ] 3.2 在同步前验证 target nodeSelector 与 required nodeAffinity 至少匹配一个目标节点；零候选失败关闭。
- [ ] 3.3 将 Failover ScaleDownSource 和公共 doScaleDown 扩展为 DaemonSet 待机隔离。
- [ ] 3.4 将 Failover ScaleUpTarget 和公共 doScaleUp 扩展为从目标 placement 快照写入 DaemonSet 调度字段。
- [ ] 3.5 覆盖 Undo、Cancel、Reprotect 和无 namespaceMapping Drill cleanup 的公共调用链。
- [ ] 3.6 对 namespaceMapping Drill 按源 namespace 查快照、确认 Drill targetCluster 与快照一致后，在映射 namespace 应用 Drill 的 target placement。
- [ ] 3.7 扩展 CheckReplicas 和 waitUntilReady 的 DaemonSet 收敛和阻塞原因判定。

## 4. Data Restore and Image Boundaries

- [ ] 4.1 为 DaemonSet 所属 Pod 增加 Trafficless 数据恢复单测，验证 labels、ownerReferences、调度清理、镜像和 cleanup。
- [ ] 4.2 保证 DataSync 仍只恢复 Pods/PVC/PV，不把 DaemonSet 控制器对象加入数据恢复范围。
- [ ] 4.3 验证并补齐 DaemonSet 的业务镜像扫描、containers/initContainers 规则生成和 unmatchedPolicy 覆盖。

## 5. Verification

- [ ] 5.1 单测：nodeSelector pair 正向/反向解析，源值漂移和缺少唯一 mapping 的失败关闭。
- [ ] 5.2 单测：affinity、tolerations、topologySpreadConstraints、schedulerName 的 target placement 解析与显式清除。
- [ ] 5.3 单测：system-protect standby selector 不被用户 target nodeSelector 覆盖。
- [ ] 5.4 单测：ConfigMap placement round-trip、nil/空值、旧 ConfigMap、targetCluster 不匹配和缺失快照。
- [ ] 5.5 单测：目标 Node 候选为零或因未容忍 NoSchedule/NoExecute taint 被静态排除时失败，DaemonSet observedGeneration、desiredNumberScheduled、numberAvailable、numberUnavailable 的就绪语义。
- [ ] 5.6 单测：ScaleDown/ScaleUp/Undo/Cancel/Drill cleanup 对 DaemonSet 的幂等行为。
- [ ] 5.7 定向测试：go test ./internal/controller/restore ./internal/controller/resourcesync ./internal/controller/disasteroperation ./internal/controller/datasync -count=1。
- [ ] 5.8 E2E：源/目标使用不同 node labels 的 DaemonSet 完成待机同步、Drill、Failover、Undo 或 Cancel，验证待机不运行且激活只落在目标映射节点。
- [ ] 5.9 执行 make test、make lint、make harness-preflight、make harness-lint、make harness-ci 和 openspec validate --strict。
- [ ] 5.10 单测：包含 DaemonSet 的 Drill 指向配置基线之外的第三集群时，在恢复或激活前失败，且不修改已待机对象。
