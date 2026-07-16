# Change: 收窄修复 DataSync trafficless 临时 Pod 调度约束

## Why
DataSync trafficless 数据恢复会恢复源端 Pod 作为临时数据注入 Pod。该临时 Pod 当前可能继承源集群的 `spec.nodeName`、`spec.nodeSelector` 或 `spec.affinity.nodeAffinity`，在目标集群节点标签或节点名不匹配时进入 Pending，进而导致 Velero PodVolumeRestore 卡住和 AppRestore 超时。

该问题发生在 DataSync 数据恢复路径，但 Trafficless 行为非常敏感，必须严格限制变更范围，避免隐式影响 ResourceSync、Failover 扩容和 Drill 演练。

## What Changes
- DataSync 自己构建 data restore AppRestore 时，仅对 DataSync trafficless 临时 Pod 增加调度约束清理，清理源端 `spec.nodeName`、`spec.nodeSelector` 和整个 `spec.affinity`。
- DataSync 的既有 trafficless 隔离语义必须保持：覆盖 labels、清空 ownerReferences、替换镜像、覆盖 command/args、hook marker 兼容逻辑不退化。
- 仅修改 DataSync 专用 modifier 构造路径；不修改 shared restore builder 默认 trafficless modifier。

## Non-Goals
- 不修改 PVC `spec.volumeName` 清理判定、清理范围或目标端 PVC/PV 状态判断逻辑。
- 不修改 ResourceSync 资源恢复逻辑或 ResourceSync 的 ResourceModifierRules。
- 不修改 DisasterOperation 的 Failover 编排、FinalSync 触发逻辑或 ScaleUpTarget 扩容逻辑。
- 不修改 Drill data restore 的 shared builder 默认 trafficless 行为；Drill 是否需要同类调度清理必须另开提案评估。
- 不修改业务工作负载 Deployment/StatefulSet/PodTemplate 的调度约束。
- 不新增 CRD 字段、用户开关或 server/web API。
- 不引入备份期扫描、AppRestore Conditions schema 扩展或更大范围的 dynamic resource modifiers。

## Impact
- Affected specs:
  - `restore-builder`（用于约束 DataSync data restore AppRestore 构建语义）
- Affected code:
  - `internal/controller/datasync/controller.go`
  - `internal/controller/datasync/*_test.go`
- Explicitly out of scope:
  - `internal/controller/restore/builder.go`
  - `internal/controller/resourcesync/*`
  - `internal/controller/disasteroperation/*`

## Safety Boundary
- ResourceSync 使用 `RestoreTypeResource`，不得新增或继承 DataSync trafficless 调度清理规则。
- Failover 的 FinalSync 只会因触发 DataSync 而受益于修复；ScaleUpTarget 只更新业务工作负载副本数，不得被本变更改变。
- Drill data restore 当前通过 shared restore builder 默认 trafficless modifier 构建；本变更不得修改该默认 builder 行为，避免演练数据恢复语义漂移。
- DataSync trafficless 临时 Pod 是数据恢复载体，不承载业务流量；清空 `spec.affinity` 的目标是避免继承源集群调度拓扑导致 Pending，只作用于恢复出的临时 Pod，不修改业务 workload 模板。
- PVC 清理逻辑维持当前基线行为，本变更不调整其触发条件、匹配范围或失败处理方式。
