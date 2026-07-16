## 1. Proposal
- [x] 1.1 创建 OpenSpec proposal/tasks/spec delta。
- [x] 1.2 明确 Non-Goals 和 Safety Boundary，锁定只影响 DataSync 专用路径。
- [x] 1.3 重新运行 `openspec validate fix-datasync-initial-restore-safety --strict`。

## 2. DataSync 专用 Trafficless 调度清理
- [x] 2.1 仅在 `DataSyncReconciler.makeTrafficlessModifiers` 中增加临时 Pod 调度约束清理 patch。
- [x] 2.2 清理 `spec.nodeName`，避免恢复 Pod 被绑定到源集群节点名。
- [x] 2.3 清理 `spec.nodeSelector`，避免源集群节点标签选择器阻塞目标集群调度。
- [x] 2.4 清空 `spec.affinity`，避免源集群节点亲和性或 Pod 拓扑亲和性阻塞 DataSync trafficless 临时 Pod 调度。
- [x] 2.5 保持 `spec.topologySpreadConstraints` 不变，避免扩大到当前恢复问题之外的调度语义。
- [x] 2.6 增加单元测试：DataSync trafficless modifier 包含上述调度清理 patch，并保留 labels/ownerReferences/image/command/args 既有 patch。

## 3. 外溢防护测试
- [x] 3.1 增加或更新测试，确认 ResourceSync resource restore 不包含 DataSync trafficless 调度清理规则。
- [x] 3.2 增加或更新测试，确认 shared restore builder 默认 data restore trafficless modifier 未因本变更新增调度清理规则。
- [x] 3.3 增加或更新测试，确认 Drill data restore 路径未因本变更新增 DataSync 专用调度清理规则。
- [x] 3.4 确认 Failover ScaleUpTarget 相关测试无需修改；若误触发改动，应回退实现边界。

## 4. Verification
- [x] 4.1 运行 `go test ./internal/controller/datasync ./internal/controller/resourcesync ./internal/controller/disasteroperation ./internal/controller/restore -count=1`。
- [x] 4.2 运行 `make harness-preflight`：0 fail / 3 warning，冻结 namespace 计划隔离检查通过。
- [x] 4.3 运行 `make harness-lint`：历史 10 项文档结构错误修复后为 0 fail / 0 warning。
- [x] 4.4 运行 `make harness-ci`：通过。
- [x] 4.5 在固定候选运行 `make test` 与 `make lint`：`make test` 退出码 0；`make lint` 退出码 2、262 项，相对父提交同版本基线 255 项净新增 7 项，已登记为 GATE-001/No-Go。
