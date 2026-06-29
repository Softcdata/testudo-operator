## 1. Implementation

- [x] 1.1 梳理 `internal/controller/cluster_controller.go` 中 `diagnoseVeleroStatusPending`、`summarizeVeleroPodIssue`、`isVeleroRuntimePod` 的现有行为。
- [x] 1.2 新增或调整终态 Pod 判定逻辑，覆盖 `Succeeded`、`Failed`、`Evicted`、`ContainerStatusUnknown`、`DeletionTimestamp` 等历史残留形态。
- [x] 1.3 调整 runtime readiness 判定：Deployment/DaemonSet 聚合状态作为主判据，Pod 级异常只统计 active runtime Pod。
- [x] 1.4 保持 `VeleroRuntimeNotReady` 的错误语义稳定，message 中输出当前阻断 Ready 的 Deployment/DaemonSet/active Pod 详情。
- [x] 1.5 如实现历史终态 Pod 清理，确保清理为 best-effort，删除失败不得单独阻断 Cluster Ready。

## 2. Tests

- [x] 2.1 增加单元测试：Deployment/DaemonSet ready 且存在历史 `Failed/Evicted/ContainerStatusUnknown` Pod 时，不返回 `VeleroRuntimeNotReady`。
- [x] 2.2 增加单元测试：Deployment/DaemonSet ready 且 active Pod `Pending/ImagePullBackOff` 时，返回 `VeleroRuntimeNotReady`。
- [x] 2.3 增加单元测试：`Deployment/velero` ready/available 不足时，返回 `VeleroRuntimeNotReady`。
- [x] 2.4 增加单元测试：`DaemonSet/node-agent` ready 小于 desired 时，返回 `VeleroRuntimeNotReady`。
- [x] 2.5 增加或更新回归用例，覆盖本次 `my170` 事故中的历史 Evicted Pod 组合。

## 3. Validation

- [x] 3.1 运行 `openspec validate fix-cluster-velero-runtime-readiness --strict`。
- [x] 3.2 运行受影响 Go 测试包。
- [x] 3.3 运行 `make harness-preflight` 与 `make harness-lint`。
- [x] 3.4 如修改代码，按 AGENTS 要求运行 `make test` 与 `make lint`；若失败，记录失败是否为既有债务或本次引入。

## 4. Rollout / Regression

- [ ] 4.1 部署修复版本到测试环境。
- [ ] 4.2 构造或保留历史终态 Velero Pod，验证 Cluster 可恢复 `Ready`。
- [ ] 4.3 构造当前 Velero runtime 异常，验证 Cluster 仍进入 `NotReady/VeleroRuntimeNotReady`。
- [ ] 4.4 验证 `my170` 类似场景无需人工删除旧 Pod 即可自动收敛。
