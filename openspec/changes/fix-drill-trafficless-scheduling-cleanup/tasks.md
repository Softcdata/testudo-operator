## 1. Proposal

- [x] 1.1 创建 OpenSpec proposal/tasks/spec delta。
- [x] 1.2 明确 Safety Boundary：只影响 Drill data restore Trafficless 临时 Pod。
- [x] 1.3 运行 `openspec validate fix-drill-trafficless-scheduling-cleanup --strict`。

## 2. Operator 实现

- [x] 2.1 仅在 `makeDrillTrafficlessModifiers` 中增加调度约束清理 patch。
- [x] 2.2 清理 `spec.nodeName`，避免恢复 Pod 绑定源集群节点名。
- [x] 2.3 清理 `spec.nodeSelector`，避免源集群节点标签选择器阻塞演练目标集群调度。
- [x] 2.4 清空 `spec.affinity`，避免源集群节点亲和性或 Pod 亲和/反亲和阻塞 Drill trafficless 临时 Pod。
- [x] 2.5 保持 target registry busybox、pull secret、labels、ownerReferences、command、args、hook marker 和 PVC volumeName cleanup 语义不变。

## 3. 外溢防护测试

- [x] 3.1 更新 Drill data restore 单测，断言包含 `nodeName`、`nodeSelector`、`affinity` 清理 patch。
- [x] 3.2 更新 Drill target registry 单测，确认调度清理与 imagePullSecrets 可共存。
- [x] 3.3 保留 shared builder 单测，确认默认 Trafficless modifier 不包含 Drill/DataSync 专用调度清理。
- [x] 3.4 确认 ResourceSync 相关测试无需引入 Drill 调度清理。

## 4. Verification

- [x] 4.1 运行 `go test ./internal/controller/disasteroperation ./internal/controller/restore ./internal/controller/datasync ./internal/controller/resourcesync -count=1`。
- [x] 4.2 运行 `openspec validate fix-drill-trafficless-scheduling-cleanup --strict`。
- [x] 4.3 重启本地 operator，执行 170/171 DEF-001 Drill 回归。
- [x] 4.4 补齐 API/CR/operator/K8s/Velero/MinIO 证据，并更新 E2E 报告为缺陷已解决。
- [x] 4.5 回滚本次 E2E 临时 registry patch，清理本轮 reader/smoke Pod。
