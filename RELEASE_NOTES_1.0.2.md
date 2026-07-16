# Release 1.0.2

发布日期：2026-07-16

基线版本：`1.0.1`（`60d187f8fa11bdf094f88d1dd37e24344d8189fe`）

功能代码截至：`fd5b67e23bb204f93cc4f72b40518d8209eeb47f`

## 版本概览

本版本集中修复容灾初始化、数据同步、Trafficless 数据恢复和容灾演练链路中的稳定性问题，并增加 Operator 运行时热配置能力。主要变化包括：

- 修复历史 Velero Pod 导致 Cluster 长期 NotReady。
- 提升 AppRestore 对瞬态 API 错误、活跃 PVR 和 Velero 重启的容错能力。
- 支持无 PVC 的 DataSync 成功跳过，以及无 PVC 实例的纯资源演练。
- 清理 DataSync/Drill 临时 Trafficless Pod 的源集群调度约束。
- 支持从本次恢复目标集群的私有仓库拉取 Trafficless busybox，并同步 pull secret。
- 为 DataSync Trafficless 恢复增加精确归属、PVR/PVC 检查和清理确认。
- 增加 `OperatorRuntimeConfig/default` 热配置 CRD。
- 扩展动态镜像扫描覆盖和 modifier 容量。

## 已实现功能与修复

### 1. Cluster Velero 运行时 Ready 判定修复

- Deployment/DaemonSet 聚合状态继续作为 Velero 运行时主判据。
- 忽略历史 `Succeeded`、`Failed`、`Evicted`、`ContainerStatusUnknown` 和正在删除的终态 Pod。
- 当前活跃的 Pending、ImagePullBackOff、CrashLoopBackOff 等 Pod 仍会阻止 Cluster Ready。
- 避免必须人工删除历史 Velero Pod 才能恢复 Cluster Ready。

### 2. Operator 运行时热配置

新增命名空间级单例 `OperatorRuntimeConfig/default`，支持在不重启 Operator 的情况下调整：

- Backup 最大等待和轮询间隔。
- Restore 最大等待、状态宽限、PVR 等待、自动重试次数和退避。
- DisasterOperation 默认超时、步骤重排队和重试间隔。
- DisasterInstance 状态转换看门狗及各状态重排队间隔。
- DataSync/ResourceSync 观察间隔、调度器更新超时和历史保留数量。
- StorageRepository 重排队间隔。
- Cluster 对账、删除重试、Velero 安装超时和 Helm zombie lock 阈值。

配置优先级为：资源 `spec` > 实例/策略 `spec` > `OperatorRuntimeConfig/default` > 启动环境变量/参数 > 代码默认值。

非法配置不会替换当前有效快照；删除 `default` 后回退到启动默认值和代码默认值。

### 3. AppRestore 稳定性和恢复保护

- 获取 Velero Restore 时遇到 timeout、connection reset、HTTP/2 connection lost 等瞬态错误，保持 `Restoring` 并重排队，不立即误判 Failed。
- Restore 项目已完成但仍有活跃 PodVolumeRestore 时，不触发 stall auto-retry，不删除 Restore。
- startup transient、空 status 或仍有运行中 Backup/Restore/PVB/PVR 时，不执行 Velero rollout restart。
- DataSync-owned Trafficless Restore 进入失败终止路径后，必须确认同名 Velero Restore 已删除，才允许最终失败或下一轮重试。
- 不自动剥离 Velero Restore finalizer。

### 4. DataSync 无 PVC 成功跳过

- 按实例 namespace 和 labelSelector 查询可恢复 PVC。
- labelSelector 支持 PVC 自身匹配，或者匹配 Pod 后发现其引用的 PVC。
- 无 PVC 判断先于 StorageRepository readiness 检查。
- 无 PVC 时不创建 AppBackup、AppRestore、Velero Restore 或 PVR。
- DataSync 收敛为 `Ready`，写入 `NoDataVolumes=True`、`Reason=NoPVCFound` 和最新 `Skipped` history。
- `Skipped` 计入同步成功统计，不计入失败统计。
- 源集群 Pod/PVC 查询失败时进入 Failed，禁止把查询故障误报为无 PVC。

### 5. Trafficless 临时 Pod 调度修复

DataSync 和 Drill 各自在自己的临时数据恢复 Pod modifier 中清理：

- `/spec/nodeName=""`
- `/spec/nodeSelector={}`
- `/spec/affinity={}`

清空整个 affinity 会移除 nodeAffinity、podAffinity 和 podAntiAffinity，但只作用于用于写入 PVC 的 Trafficless 临时 Pod。

本版本不清理 topologySpreadConstraints、tolerations、schedulerName 或 runtimeClassName，也不修改 ResourceSync、业务 Deployment/StatefulSet PodTemplate 或 Failover ScaleUpTarget。

### 6. Trafficless 目标私有仓库和凭据

- 默认 Trafficless 镜像由 `busybox:latest` 调整为 `busybox:1.36`。
- 历史 `busybox:latest` 和 `busybox:1.36` 均被识别为平台默认值。
- 平台默认值按本次恢复目标 Cluster 的 `spec.veleroInstall.imageRegistry` 派生。
- Failover/Reprotect 角色反转后，按当前恢复目标集群解析镜像。
- 显式配置的非默认 Trafficless 镜像优先，不被目标 registry 覆盖。
- 将管理面 dockerconfigjson Secret 同步到实际恢复 namespace，并注入 `imagePullSecrets`。
- Drill 支持同步到 namespaceMapping 后的目标 namespace。
- 未配置 credential 时不创建空 Secret；已配置但 Secret 缺失、类型错误或内容为空时，在创建 AppRestore 前失败。
- Trafficless 镜像解析与业务 `imageSources`/镜像前缀改写保持隔离。

### 7. DataSync Trafficless 生命周期加固

- AppRestore 和临时 Pod 增加 DataSync lifecycle、cleanup owner、relation、strategy、managed-by 和 restore-run 标签。
- 恢复前按 owner 范围清理旧 Pod，恢复后按 owner+run 精确清理本轮 Pod。
- 删除请求成功不代表清理成功；必须轮询确认 Pod 已不存在。
- 取消按 busybox 镜像识别和删除 Pod，避免误删使用相同镜像的普通业务 Pod。
- 发现无 owner/run 标签的历史 `trafficless=true` Pod 时失败关闭，返回 `TrafficlessCleanupAmbiguous`，不做猜测性删除。
- DataSync Ready 前检查相关 PVR、目标 PVC Bound 状态和本轮 Pod cleanup 收敛。
- AppRestore timeout 与实例 `operationTimeoutMinutes` 对齐。
- 增加 Unschedulable、镜像拉取、挂载、容器运行、node-agent、PVR stalled、Restore termination 和 cleanup timeout 等稳定 reason。

### 8. 无 PVC 纯资源演练

新增恢复模式：

- `ResourceOnly`：单实例只恢复 Kubernetes 资源并 ScaleUp，不执行 RestoreData。
- `Mixed`：仅用于容灾组状态聚合；子 Operation 仍分别使用 FullRestore 或 ResourceOnly。

ResourceOnly 必须同时满足：

- DataSync 为 Ready。
- `NoDataVolumes=True` 且 `Reason=NoPVCFound`。
- 最新 DataSync history 为 `Skipped`。
- ResourceSync 为 Ready 且存在资源备份。

普通数据备份缺失仍在 Drill Pending 阶段失败，`skipValidation` 不能绕过。Drill 在 Ready 阶段冻结逐实例恢复模式，Confirm 时重新检查成员和模式，发生漂移时失败关闭。

### 9. Drill cleanup 修复

- Restore 已成功后，不再等待 Trafficless Pod init container 完成才执行删除。
- Pending 或 init 未完成的临时 Pod也能进入 cleanup，避免 Drill 长期停留在清理阶段。
- 该行为只发生在 Restore 成功后的 Drill cleanup，不用于中断仍在执行的数据恢复。

### 10. DataSync/ResourceSync 和镜像改写优化

- `DisasterInstance.spec.operationTimeoutMinutes` 下传到托管 AppBackup，并对齐已有 AppBackup timeout。
- 新增 `APPBACKUP_PARALLEL_FILES_UPLOAD`，控制 DataSync FSB 上传并发。
- DataSync/ResourceSync 观察间隔、调度器更新超时和历史保留数量接入 runtime config。
- 动态镜像扫描新增 ReplicaSet 和 ReplicationController。
- modifier 单实例规则上限从 200 提高到 1000。
- 补充 Drill 继承实例 rewriteImage 并覆盖多个 initContainer 的回归测试。

说明：initContainers 镜像替换在 1.0.1 已存在，本版本新增的是更多资源类型覆盖和 Drill 回归证据，不应描述为首次支持 initContainers。

## API、CRD 和 RBAC 变化

本版本新增或修改：

- 新增 `operatorruntimeconfigs.testudo.softcdata.com` CRD。
- ClusterRole 增加 `operatorruntimeconfigs` 与 `operatorruntimeconfigs/status` 权限。
- DataSync CRD 的 Trafficless 默认镜像更新为 `busybox:1.36`。
- DisasterDrill status 增加 `ResourceOnly`、`Mixed` 和 `instanceRestoreModes`。
- DisasterOperation DrillConfig 增加 `restoreMode` 和 `instanceRestoreModes`。

推荐升级顺序：

1. 更新 CRD。
2. 更新 Operator RBAC。
3. 更新 Operator 镜像/Deployment。
4. 更新配套 Server 及前端类型。
5. 确认客户私有仓库已包含 `<imageRegistry>/busybox:1.36`。

## 兼容性与明确边界

- PVC `spec.volumeName` 清理没有扩大：仅实例 Initializing 且 DataSync 从未同步过的首次数据恢复执行；后续同步不清理。
- Drill namespaceMapping 的 PVC cleanup 保持独立既有逻辑。
- ResourceOnly 对历史未携带 restoreMode 的 Operation 默认 FullRestore，避免升级后静默少恢复数据。
- Trafficless lifecycle 新标签为内部实现标签，回滚代码后不会改变 Pod 调度或业务流量。
- `OperatorRuntimeConfig` 不配置时保持原有代码默认行为。

## 验证状态

已通过：

- DataSync、AppRestore、DisasterDrill、DisasterOperation、restore、ResourceSync 定向 Go 测试。
- 8 个本版本相关 OpenSpec change 的 strict 校验。
- `git diff --check`。
- Drill FullRestore 主容器和两个 initContainer 镜像改写真实环境回归，PVC marker、PVR、Velero 和 MinIO 结果一致。
- DataSync Trafficless 正常 FSB 主路径：PVR Completed、PVC Bound、marker 一致、精确 cleanup 为空。
- 无归属历史 Trafficless Pod 失败关闭：普通 busybox Pod 未被误删。
- Pod 删除长期不收敛时保持 InProgress，并以 `TrafficlessCleanupTimeout` 失败，不误写 Ready。

## 已知问题和发布限制

### P1：坏镜像 Trafficless Pod 仍可能 false-ready

真实 `ImagePullBackOff` 和 `ErrImageNeverPull` 故障注入中，恢复 Pod 在 AppRestore 写入稳定错误前被 DataSync 清理，DataSync 最终进入 Ready。当前版本不能声明镜像拉取失败一定会稳定收敛为 Failed。

### ResourceOnly Server 契约尚未完整同步

配套 Server 1.0.2 的 Drill OpenAPI 仍只声明 `Reuse/FullRestore`，且未回显 `instanceRestoreModes`。Operator 可以写入 ResourceOnly/Mixed，但生成客户端和前端类型可能无法正确识别。

### 门禁状态

- 相关定向测试和 OpenSpec 校验通过。
- 全仓 `make lint` 仍受历史 lint 债务阻断，最近记录为 258 项。
- ResourceOnly 尚未完成真实集群 E2E。
- 当前 tag 代表源码版本冻结，不等同于无条件生产准入结论。

## 本版本包含但尚未实现的提案

以下 OpenSpec 已随版本入库，但不能作为 1.0.2 已实现能力：

- DisasterDrill CLI/Harness 提案。
- Trafficless DaemonSet 支持提案。
- Trafficless E2E 正式验收治理提案。

## 功能提交

- `d121927`：修复 Cluster Velero runtime readiness。
- `a2c2066`：增加 runtime config 和 AppRestore/DataSync/ResourceSync 稳定性更新。
- `ca48691`：修复 Drill Pending Trafficless Pod cleanup。
- `fd5b67e`：完善无 PVC、目标私仓、调度、ResourceOnly 和 Trafficless 生命周期。
