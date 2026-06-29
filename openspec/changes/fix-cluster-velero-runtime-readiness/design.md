# Design: Cluster Velero runtime readiness 修复

## Context

本次事故中，`my170` 的 `Cluster` 状态卡在：

- `status.status=NotReady`
- `status.reason=VeleroRuntimeNotReady`
- `status.message=waiting for Velero runtime to become ready ... pod velero-85fb996d77-cw7zn Completed`
- `lastCheckTime=2026-06-22T13:26:59Z`

目标集群当前 Velero runtime 实际健康，但 `velero` 命名空间残留了一个旧 Pod：

- `velero-85fb996d77-cw7zn`
- `Failed/Evicted/ContainerStatusUnknown`
- 驱逐原因：node ephemeral-storage low

人工删除该历史 Pod 并触发 `refresh-cluster-stats=all` 后，`my170` 恢复 `Ready`。这说明自动收敛缺陷位于 runtime readiness 诊断逻辑，而不是当前 Velero 服务不可用。

## Goals

- 历史终态 Velero runtime Pod 不再单独阻断 Cluster Ready。
- 仍能准确暴露当前 Velero runtime 故障。
- 保留可读诊断信息，便于定位 Deployment、DaemonSet 或 active Pod 异常。
- 保持实现局部化，不引入新 API 字段。

## Non-Goals

- 不处理 Cluster 删除时清理所有 Velero 残留资源。
- 不定义新的 refresh signal 协议。
- 不自动修复已经失败的业务同步对象。
- 不调整 Velero Helm values 或安装参数。

## Decisions

### D1: controller 聚合状态作为 runtime readiness 主判据

`diagnoseVeleroStatusPending` 应优先读取：

- `Deployment/velero`
- `DaemonSet/node-agent`

若 `Deployment/velero` 存在但 `ReadyReplicas < 1` 或 `AvailableReplicas < 1`，则返回 `VeleroRuntimeNotReady`。

若 `DaemonSet/node-agent` 存在且 `DesiredNumberScheduled > 0`，但 `NumberReady < DesiredNumberScheduled`，则返回 `VeleroRuntimeNotReady`。

原因：Deployment/DaemonSet status 表示当前控制器视角的运行时可用性，比命名空间中残留的历史 Pod 更适合作为 Cluster Ready 判据。

### D2: Pod 级检查只诊断 active Pod

Pod 级遍历仍保留，但必须跳过历史终态 Pod。建议定义 `isTerminalVeleroRuntimePod` 或等价逻辑，至少覆盖：

- `pod.Status.Phase == Succeeded`
- `pod.Status.Phase == Failed`
- `pod.Status.Reason == Evicted`
- container/initContainer terminated reason 为 `ContainerStatusUnknown`
- `DeletionTimestamp != nil`

只有 active Pod 才参与 `summarizeVeleroPodIssue`，例如：

- `Pending` 且 container waiting reason 为 `ImagePullBackOff`
- `Running` 但 PodReady 为 false
- `Unknown` 且没有被判定为历史终态

### D3: 历史终态 Pod 清理为 best-effort

实现可以在 readiness 诊断或独立辅助函数中尝试删除超过阈值的终态 Velero runtime Pod，但该动作不得成为 Ready 判据。

清理结果语义：

- `NotFound`：忽略。
- `Forbidden`、连接错误、删除失败：记录诊断日志或事件即可，不得单独设置 `VeleroRuntimeNotReady`。
- 未实现清理也可接受，只要 readiness 不再被历史终态 Pod 阻断。

### D4: 不扩大 refresh signal 范围

本提案只要求在健康检查成功后 Cluster 能自然恢复 Ready。`refresh-cluster-stats` 的入队、清理和审计降噪仍归属 `add-cluster-namespace-refresh-signal`。

## Risks / Trade-offs

- 风险：如果直接忽略所有 `Failed` Pod，可能漏掉一个正在 rollout 的新 Velero Pod 启动失败。
  - 缓解：Deployment/DaemonSet 聚合状态仍会在 ready/available 不足时阻断 Ready；active Pod 异常仍作为详情输出。
- 风险：终态 Pod 清理需要额外 RBAC。
  - 缓解：清理是可选 best-effort；若当前 RBAC 不足，先完成 readiness 修复，不把清理作为硬依赖。
- 风险：Pod phase/reason 在不同 Kubernetes 版本上表现略有差异。
  - 缓解：测试覆盖本次实际组合 `Failed + Evicted + ContainerStatusUnknown`，并覆盖 `Succeeded`/`DeletionTimestamp`。

## Validation Plan

- `openspec validate fix-cluster-velero-runtime-readiness --strict`
- 针对 `diagnoseVeleroStatusPending` 增加单元测试：
  - Deployment/DaemonSet ready，存在历史 `Failed/Evicted/ContainerStatusUnknown` Pod，返回非 `VeleroRuntimeNotReady`。
  - Deployment/DaemonSet ready，存在 active `Pending/ImagePullBackOff` Pod，返回 `VeleroRuntimeNotReady`。
  - Deployment unavailable，返回 `VeleroRuntimeNotReady`。
  - node-agent ready 小于 desired，返回 `VeleroRuntimeNotReady`。
- 修复部署后回归：
  - 在测试集群保留历史终态 Velero Pod 或构造等价 fake client 对象。
  - 确认 `Cluster.status.status` 可恢复 `Ready`。
  - 确认真实 Velero runtime 异常仍显示 `VeleroRuntimeNotReady`。
