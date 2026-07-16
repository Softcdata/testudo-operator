## Context

DataSync 的数据面由源端 AppBackup、目标端 AppRestore、Velero Restore、PodVolumeRestore、目标 PVC 和 Trafficless 临时 Pod 共同构成。现有实现把终态主要压缩为 AppRestore 的 Succeeded/Failed，再由 DataSync 清理 Pod 并写入 Ready。该压缩遗漏了两个必须独立确认的事实：

1. 临时 Pod 是否可以在目标集群作为 FSB 数据注入载体工作。
2. 临时 Pod 和失败恢复对象是否已经在目标集群收敛。

本设计不重写全局 AppRestore 状态机。它只为 DataSync 所创建且明确标识为 Trafficless 生命周期的 AppRestore 增加一条受限分支，并由 DataSync 保持唯一的同步终态写入权。

## Goals

- 在 DataSync 路径内将临时 Pod、PVR、PVC 和清理确认串成可观察且有超时的生命周期。
- 让当前已知的调度、镜像、node-agent、Velero/PVR 和 Pod 删除问题转换为明确失败，而不是长期等待或模糊成功。
- 复用现有 DataSync Conditions、AppRestore status、OperatorRuntimeConfig 和 cleanup 标签协议，不新增 CRD 或对外 API。
- 保持 ResourceSync、Drill、业务工作负载模板和普通 AppRestore 的行为不变。

## Non-Goals

- 不尝试在恢复时推断并改写所有业务 PodSpec 字段。
- 不将 Trafficless 临时 Pod 的生命周期扩展为业务应用健康检查。
- 不以自动删除或 finalizer 强拆处理归属不明的历史 Pod/Restore。
- 不把 Velero Completed 解释为业务数据内容校验通过。

## Lifecycle

DataSync 的目标状态只能按以下顺序前进：

Preflight
  -> CleanupBeforeRestore
  -> RestoreCreated
  -> TrafficlessPodObserved
  -> PodVolumeRestoreObserved
  -> PVCReady
  -> CleanupAfterRestore
  -> CleanupConfirmed
  -> Ready

任一阶段失败都写入 DataSync Failed，并保留最具体的 Reason/Message。失败后的自动调度/手动重试继续遵循已有同步策略，不由本提案新增隐式后台重试。

| 阶段 | 责任方 | 成功条件 | 失败关闭条件 |
| --- | --- | --- | --- |
| Preflight | DataSync | 源/目标 Cluster 为 Ready；目标端 Velero Deployment 与 node-agent DaemonSet 当前可用 | Cluster NotReady、目标 API 不可访问、Velero/node-agent 不可用 |
| CleanupBeforeRestore | DataSync | 仅属于当前 DataSync owner 的旧 Trafficless Pod 已 NotFound | 删除请求失败、删除超时、发现无归属历史 trafficless Pod |
| RestoreCreated | AppRestore | Velero Restore 已创建且带 DataSync 生命周期标识 | BSL、ConfigMap、Restore 创建失败 |
| TrafficlessPodObserved | AppRestore 条件分支 | 观察到本 run 的 Pod，或 PVR 已按 Velero 语义推进 | 稳定 Unschedulable、镜像/挂载/容器配置/CrashLoop 错误，或观察超时 |
| PodVolumeRestoreObserved | AppRestore 条件分支 | 全部相关 PVR Completed | PVR Failed、Pending/InProgress 超出有效 timeout、node-agent 不可用 |
| PVCReady | DataSync | 对应目标 PVC Bound 且未处于删除 | PVC 不存在、Pending/删除或读取失败超过有效 timeout |
| CleanupAfterRestore | DataSync | 对应本 run 的 Trafficless Pod 已提交删除 | Delete API 失败 |
| CleanupConfirmed | DataSync | 精确 selector 查询为空 | Pod 长期 Terminating 或清理 deadline 超时 |

## Decision 1: DataSync 是同步终态和 Pod 清理的唯一所有者

AppRestore 负责报告目标集群 Restore/PVR/临时 Pod 的事实，不负责将 DataSync 写为 Ready/Failed，也不删除 DataSync Pod。DataSync 根据 AppRestore 的 status 和自己的远端清理确认推进 DataSync.Status.State。

理由：

- 避免 AppRestore 与 DataSync 竞争写入同步终态。
- Pod 清理需要了解 DataSync run、下一轮 restore 和同命名空间的隔离边界。
- 清理失败不能被 AppRestore 成功相位掩盖。

## Decision 2: 使用 platform labels 建立精确归属，而不是镜像识别

DataSync 构造 Trafficless modifier 时，覆盖后的 Pod labels 必须保留以下平台语义：

- trafficless=true
- 现有 cleanup-owner-token
- 现有 cleanup-relation=datasync.trafficlessPod
- 现有 cleanup-strategy=delete
- 现有 cleanup-managed-by=disaster-operator
- 本次 restore 的 trafficless-run 标识

DataSync 运行前清理使用 owner token 加 relation 的 selector，运行后清理额外限定 run 标识。busybox 镜像永远不是清理 selector。

理由：

- 镜像不是资源所有权，业务 Pod 可以使用相同镜像。
- 同命名空间可能存在其他 DataSync、Drill 或历史资源。
- 现有 cleanup 标签协议可被删除检查和审计能力复用。

## Decision 3: 历史无归属 Pod 失败关闭

发现 trafficless=true 但缺少上述 owner/run 标签的 Pod 时，不做猜测性删除。DataSync 写入 TrafficlessCleanupAmbiguous 失败，消息包含 namespace/name，要求人工确认。

理由：

- 旧逻辑的镜像兜底会产生误删风险。
- 将不确定性转化为明确运维动作比误删 PVC 数据载体更安全。

## Decision 4: AppRestore 的增强按内部生命周期标识启用

DataSync 创建 AppRestore 时写入内部 label，例如 trafficless-lifecycle=datasync。AppRestore 仅在该 label 存在时：

- 根据 DataSync owner/run labels 查询目标 Pod。
- 解析 PodScheduled、container/initContainer waiting、挂载和容器配置失败原因。
- 检查目标 Pod 所在节点的 node-agent Ready 状态。
- 将 PVR Failed、长期 Pending 或长期 InProgress 映射为稳定 Reason/Message。
- 在需要删除或重试同名 Velero Restore 时，先等待远端对象实际消失；未消失达到有效 timeout 时失败关闭。

普通 AppRestore、ResourceSync 和 Drill 不带该标识，继续走当前通用收敛逻辑。

在该内部 termination-confirmation 路径中，AppRestore 保持可重排队的非终态，并以内部 annotation 记录删除请求时间和原始失败原因。只有观察到同名 Restore 已 NotFound 时才写最终 Failed；若等待超时，则写 VeleroRestoreTerminationTimeout。该 annotation 不是新的 CRD schema 或对外 API。

## Decision 5: 超时复用既有运行时配置

不新增 CRD 或新的 OperatorRuntimeConfig 字段：

- AppRestore spec.timeout 使用实例 OperationTimeoutMinutes；缺失时使用既有 RestoreRuntime.InProgressMaxWait。
- Pod/PVR 早期失败观察和清理确认使用既有 RestoreRuntime.PodVolumeRestorePendingMaxWait 作为上限。
- 同名 Velero Restore 删除确认不得超过有效 AppRestore timeout。

理由：

- 将长数据恢复的 Backup/Restore timeout 对齐。
- 避免以一个小故障修复引入新的配置面和 Server/Web 传递链。

## Decision 6: 不扩张 PodSpec 改写范围

本提案只要求观察并快速失败，不自动清空 initContainers、sidecars、probes、schedulerName、topologySpreadConstraints 或其他源 PodSpec 字段。已有 DataSync 专用 nodeName/nodeSelector/affinity 清理保持不变。

理由：

- 将任意业务 Pod 变为最小 FSB Pod 需要动态保留所有 PVC mount 和安全上下文，风险远高于本轮生命周期收敛。
- 这些字段造成失败时应被精确诊断，后续再以独立提案评估安全的最小 Pod 模型。

## Reason Contract

新增或稳定使用的 DataSync/AppRestore reason 必须包含：

| Reason | 触发 |
| --- | --- |
| SourceVeleroRuntimeNotReady | 源 Cluster 未 Ready，无法安全开始 FSB backup |
| TargetVeleroRuntimeNotReady | 目标 Cluster、Velero Deployment 或 node-agent 未就绪 |
| TrafficlessPodUnschedulable | PodScheduled=False 持续超过宽限 |
| TrafficlessPodImagePullFailed | ImagePullBackOff 或 ErrImagePull 持续超过宽限 |
| TrafficlessPodMountFailed | FailedMount 或 CreateContainerConfigError 持续超过宽限 |
| TrafficlessPodRuntimeFailed | init/container CrashLoop 或等价终态错误 |
| NodeAgentUnavailable | 已调度节点无 Ready node-agent |
| PodVolumeRestoreFailed | 任一 PVR Failed |
| PodVolumeRestoreStalled | PVR Pending/InProgress 超过有效 timeout |
| VeleroRestoreTerminationTimeout | 同名 Restore 删除未确认完成 |
| TrafficlessCleanupAmbiguous | 发现无归属的历史 trafficless Pod |
| TrafficlessCleanupTimeout | 精确归属 Pod 删除未确认完成 |

DataSync 必须将 AppRestore 的 reason/message 原样作为上层诊断上下文的一部分，避免只保留 RestoreFailed。

## Verification Matrix

| Case | Expected result |
| --- | --- |
| 源/目标 Cluster NotReady | 不创建 Backup/Restore，DataSync Failed 并带具体 runtime reason |
| 目标 node-agent DaemonSet 不 Ready | 不创建 Restore 或在预检失败，原因可读 |
| Pod nodeName/nodeSelector/affinity 已清理 | 仍保持现有 DataSync 专用调度语义 |
| Pod Unschedulable | 在早期观察上限内 Failed，不等待全局 Restore timeout |
| Busybox ImagePullBackOff | 在早期观察上限内 Failed，信息含 Pod/container reason |
| init container 或 sidecar 阻塞 | 在早期观察上限内 Failed，信息含对应容器 reason |
| 已调度节点无 node-agent | Failed，信息含节点与 node-agent 状态 |
| PVR Failed/Pending/InProgress 卡住 | Failed，信息含 PVR 名称、phase、消息或等待时长 |
| Velero Restore 删除卡住 | 不创建同名新 Restore；到期 Failed 且保留诊断 |
| 成功后 Pod 删除延迟 | DataSync 保持 InProgress，直到 selector 为空 |
| 成功后 Pod 永久 Terminating | DataSync Failed，绝不写 Ready |
| 同 namespace 普通 busybox Pod | 不被查询或删除 |
| 无归属历史 trafficless Pod | 不被删除，失败关闭并列出对象 |
| 长恢复且实例 timeout=180m | AppRestore timeout 为 180m，不被默认 1h 截断 |

## Rollout and Rollback

新标签和内部 AppRestore lifecycle label 对旧对象无副作用。部署后，只有新创建的 DataSync restore 进入严格生命周期。若回滚代码，已写入的标签是惰性元数据，不改变 Kubernetes 调度或业务流量。

回滚前必须确认不存在由新逻辑停留在 CleanupBeforeRestore/CleanupAfterRestore 的 DataSync；不得使用旧的镜像兜底清理去处理归属不明 Pod。需要时按 DataSync owner/run 标签人工清理，并保留 PVC/Velero 对象诊断。

## Open Questions

- PVC Bound 只能证明卷可用，不能证明业务语义的数据内容。业务 marker/checksum 校验是否作为后续独立能力推进，需要由应用恢复契约单独定义。
