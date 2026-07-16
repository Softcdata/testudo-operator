# Change: 收敛 DataSync Trafficless 临时 Pod 生命周期

## Why

DataSync 的 FSB 数据恢复会将源端 Pod 恢复为 Trafficless 临时 Pod，再由 Velero node-agent 将数据写入目标 PVC。当前链路已经具备目标仓库 busybox、pull secret 以及 nodeName/nodeSelector/affinity 清理等局部保护，但仍缺少一个有界、可观测的生命周期：

- 临时 Pod 因调度、镜像、init container、挂载或 node-agent 问题不能工作时，DataSync 主要等待 PVR 或 Restore 的通用超时，错误不能快速定位。
- Velero Restore/PVR 卡住或删除未完成时，当前重试和终态没有确认远端对象已收敛。
- DataSync 清理临时 Pod 只提交 Delete 请求，不等待 Pod 消失；清理失败后仍可能写入 Ready。
- 清理按 trafficless 标签或 busybox 镜像兜底，可能误删同命名空间其他运行的临时 Pod 或业务 Pod。

这会导致同步长期 InProgress、失败原因不可解释，或出现 PVC 数据注入/清理未确认却被平台标记成功的风险。

## What Changes

- 为 DataSync 自己生成的 Trafficless AppRestore 建立内部生命周期标识和每次恢复运行标识；恢复出的临时 Pod 必须携带可精确选择的 owner/run cleanup 标签。
- DataSync 在创建恢复前执行有界的 stale Pod 清理，成功后的清理也必须轮询确认 Pod 已经消失。删除 API 成功不等于清理成功。
- DataSync 只在以下条件全部成立时进入 Ready：
  - DataSync 所属 AppRestore 成功；
  - 对应 PodVolumeRestore 未报告失败或超时；
  - 目标 PVC 已进入可用绑定状态；
  - 本次 Trafficless Pod 已按精确 selector 清理并确认不存在。
- 对带有 DataSync Trafficless 生命周期标识的 AppRestore，补充目标 Pod 和 PVR 的定向观测：
  - Pod 长期 Unschedulable、ImagePullBackOff、ErrImagePull、FailedMount、CreateContainerConfigError、CrashLoopBackOff 等必须在有界时间内转化为稳定 failure reason/message；
  - node-agent 缺失、不 Ready 或目标 Pod 已调度节点无 Ready node-agent 时必须暴露为稳定失败；
  - PVR Failed、Pending 或 InProgress 超过有效超时时间时必须结束为失败，不能无限抑制 Restore 收敛。
- 对 DataSync Trafficless Restore 的删除/重试增加确认等待：同名 Velero Restore 未被观察到删除完成前不得创建下一轮同名 Restore；等待超过既有有效超时时间后必须失败关闭并保留诊断信息。
- DataSync 必须将 AppRestore 的具体 Reason/Message 映射到自身 Conditions、事件和同步历史，而不是只报告泛化的 RestoreFailed。
- DataSync 创建的 AppRestore Timeout 必须与该实例的 OperationTimeoutMinutes 对齐；未配置时继续使用现有 RestoreRuntime 默认值。

## Scope

- DataSync 控制器及其生成 AppRestore 的构造、状态和清理逻辑。
- AppRestore 控制器中仅由 DataSync Trafficless 生命周期标识启用的远端 Pod/PVR/Restore 收敛逻辑。
- 现有 cleanup 标签协议在目标集群 Trafficless Pod 上的复用。
- DataSync、AppRestore、运行时配置和目标集群 fake/e2e 测试。

## Non-Goals

- 不修改 ResourceSync、DisasterOperation Drill、Failover ScaleUpTarget 或业务 Deployment/StatefulSet/DaemonSet PodTemplate。
- 不修改 shared restore builder 的默认 Trafficless modifier，也不将本提案的行为自动扩散到 Drill。
- 不扩展或重做现有 nodeName/nodeSelector/affinity 清理为 topologySpreadConstraints、schedulerName、tolerations、runtimeClassName 等更多调度字段；命中这些未覆盖字段时本提案要求快速失败和诊断，而不是静默改写业务语义。
- 不将任意业务 Pod 改造成全新的最小化 Pod，不自动删除 initContainers、sidecars、探针、环境变量或卷定义。
- 不新增 DataSync/AppRestore CRD 字段、Server/Web API、用户开关或新的运行时配置字段。
- 不实现全局 Velero 重启策略，不对非本次 DataSync 所属的 Velero Restore 强制移除 finalizer。
- 不承诺应用业务数据的通用校验和或内容校验；本提案只验证 Velero/PVR 成功、PVC 可用状态和临时 Pod 生命周期。

## Compatibility and Safety Boundary

- 只有带有明确 DataSync Trafficless 生命周期标识的 AppRestore 才会启用新的远端 Pod/PVR 诊断和 Restore 删除确认逻辑；用户创建的 AppRestore、ResourceSync 和 Drill 保持现有行为。
- 清理 selector 必须同时限定 platform-managed、DataSync owner token 与关系类型；不得以 busybox 镜像识别待删对象。
- 历史无 owner/run 标签的 trafficless Pod 视为歧义资源。新逻辑不得删除它们，必须失败关闭并报告名称，交由人工或专门迁移流程处理。
- 不改变已经完成且无清理残留的 DataSync 成功语义；本提案只阻止“远端资源尚未确认收敛”的模糊成功。
- 本提案依赖 fix-datasync-initial-restore-safety 已定义的 DataSync 专用 nodeName/nodeSelector/affinity 清理，但不修改其范围。

## Impact

- Affected specs:
  - data-sync
  - app-restore
- Affected code:
  - internal/controller/datasync/controller.go
  - internal/controller/datasync/*_test.go
  - internal/controller/apprestore/apprestore_restoring.go
  - internal/controller/apprestore/*_test.go
  - internal/controller/trafficless_runtime.go
  - pkg/metadata cleanup label helpers only when an existing helper cannot express the scoped selector contract
- Operational impact:
  - 新版会将过去可能被标记 Ready 的未确认清理、PVR 卡住和目标 Pod 失败改为明确 Failed。
  - 首次升级若发现无归属标签的历史 trafficless Pod，DataSync 将失败关闭并要求人工处置。
