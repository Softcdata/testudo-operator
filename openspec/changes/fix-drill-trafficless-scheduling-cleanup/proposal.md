# Change: Drill data restore 清理 Trafficless 临时 Pod 调度约束

## Why

170/171 E2E 暴露出 Drill data restore 会恢复源端 Pod 作为 Trafficless 临时数据注入 Pod。如果源端 Pod 带有 `spec.nodeName`、`spec.nodeSelector` 或 `spec.affinity.nodeAffinity` 等源集群调度约束，演练目标集群可能无法满足这些约束，临时 Pod 会一直 Pending，导致 PodVolumeRestore 和 Drill 超时失败。

DataSync 已经在专用路径清理这些调度约束，但之前提案明确不扩散到 Drill。当前缺陷发生在 Drill data restore 自己构建的 Trafficless 临时 Pod 上，因此需要一个独立、收窄的 Drill 修复。

## What Changes

- Drill data restore 构建 Trafficless modifier 时，对临时 Pod 清理 `spec.nodeName`、`spec.nodeSelector` 和 `spec.affinity`。
- 保持 Drill 现有 Trafficless 语义：覆盖 labels、清空 ownerReferences、解析目标集群 busybox 镜像、覆盖 command/args、注入目标 namespace pull secret。
- Drill namespaceMapping、PVC `spec.volumeName` 清理、hook marker 规则继续按现有逻辑执行。
- 增加单测和 E2E 回归证据，证明带源端节点亲和性的 PVC workload 可完成 Drill 数据恢复。

## Non-Goals

- 不修改 DataSync 已有调度清理语义。
- 不修改 ResourceSync 资源恢复或 ResourceModifierRules。
- 不修改 shared restore builder 默认 Trafficless modifier。
- 不修改 Failover、Reprotect、ScaleUpTarget 或业务 workload 模板调度约束。
- 不新增 CRD/API 字段、用户开关、AppRestore Conditions schema 或 Web/Server 交互。
- 不改变 PVC `spec.volumeName` 清理的触发条件、范围或状态判断。
- 不清理 `topologySpreadConstraints`、`tolerations`、`schedulerName` 或 `runtimeClassName` 等当前缺陷之外的调度字段。

## Impact

- Affected specs:
  - `restore-builder`（约束 Drill data restore 使用的专用 Trafficless modifier）
- Affected code:
  - `internal/controller/disasteroperation/controller.go`
  - `internal/controller/disasteroperation/velero_hooks_test.go`
- Explicitly out of scope:
  - `internal/controller/restore/builder.go`
  - `internal/controller/resourcesync/*`
  - Failover/ScaleUpTarget 业务工作负载扩容路径
  - CRD/API/schema

## Safety Boundary

- Drill data restore 的 Trafficless Pod 是数据恢复载体，不承载业务流量；清理调度字段只为了让该临时 Pod 能在演练目标集群调度并挂载 PVC。
- 清理整个 `spec.affinity` 会同时移除 `nodeAffinity`、`podAffinity` 和 `podAntiAffinity`，但作用对象仅为恢复出的临时 Pod；不会修改 Deployment/StatefulSet/PodTemplate，也不会影响演练完成后的业务 Pod 扩容语义。
- shared builder 继续保持无 Drill/DataSync 专用调度清理；只有 Drill 控制器显式传入 `DataResourceModifierRules` 时才生效。
- ResourceSync 和 Drill resource restore 不恢复 Pods/PVC 数据载体，不应继承本规则。
- Failover 的业务扩容阶段不应读取或复用本 Drill modifier。
