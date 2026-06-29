# Change: 修复 Cluster Velero runtime readiness 误判

## Why

`my170` 在 2026-06-29 排障中表现为 `Cluster.status.status=NotReady`，原因是 `VeleroRuntimeNotReady`，但目标集群当前 `velero` Deployment 与 `node-agent` DaemonSet 已经运行正常。

根因是 `ClusterReconciler` 在诊断 Velero runtime 时遍历 `velero` 命名空间内所有匹配 Velero 运行时命名/标签的 Pod，并把历史终态 Pod（例如 `Failed/Evicted/ContainerStatusUnknown`，本次为 `velero-85fb996d77-cw7zn`）当作当前运行时故障，导致 Cluster 长期卡在 `NotReady`，直到人工删除旧 Pod 并触发刷新。

## What Changes

- 调整 Cluster Velero runtime readiness 语义：以当前 controller 聚合状态为主，`velero` Deployment 至少 1 个 ready/available replica，`node-agent` DaemonSet ready 数量达到 desired 数量。
- Pod 级诊断仅用于补充当前 active runtime Pod 的异常细节；历史终态 Pod 不得单独阻断 Cluster Ready。
- 终态历史 Pod 包括但不限于 `Succeeded`、`Failed`、`Evicted`、`ContainerStatusUnknown` 等已退出或被替换的 Pod。
- 当前 active Pod 的真实异常仍必须阻断 Ready，例如 `Pending`/`Running` 但未 ready，且存在 `ImagePullBackOff`、`CrashLoopBackOff`、`CreateContainerConfigError` 等原因。
- 对历史终态 Pod 的清理只能作为 best-effort 辅助动作；清理失败本身不得把 Cluster 标记为 `NotReady`。
- 增加单元测试和必要的回归验证，覆盖历史终态 Pod 不阻断、active Pod 异常阻断、Deployment/DaemonSet 不可用阻断。

## Non-Goals

- 不修改 `Cluster` CRD schema，不新增长期状态字段或 API 字段。
- 不覆盖 Cluster 删除时的 Velero 彻底清理；该范围由现有 `update-cluster-velero-hard-cleanup` change 负责。
- 不重定义 typed namespace refresh signal；该范围由现有 `add-cluster-namespace-refresh-signal` change 负责。
- 不修复既有失败的 `DataSync`、`ResourceSync`、`DisasterInstance` 业务同步对象；本次只修复 Cluster 自身 Ready 自动收敛。
- 不改变 Velero 安装、升级、BSL、备份/恢复执行语义。

## Impact

- Affected specs:
  - `cluster`
- Affected code:
  - `internal/controller/cluster_controller.go`
  - `internal/controller/cluster_controller_test.go` 或等价测试文件
- Runtime impact:
  - 当前健康的受管集群不会再因为历史 Velero Pod 残留长期保持 `NotReady`。
  - 当前 Velero Deployment、node-agent DaemonSet 或 active runtime Pod 的真实故障仍会以 `VeleroRuntimeNotReady` 暴露。
