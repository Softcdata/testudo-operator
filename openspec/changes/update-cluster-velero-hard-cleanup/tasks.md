# Tasks: 集群删除时彻底清理 Velero 残留

## 1. OpenSpec

- [x] 1.1 新增变更提案，明确彻底清理范围与非目标。
- [x] 1.2 为 `cluster` 能力补充增量规范（删除场景）。

## 2. 测试先行（BDD/行为场景）

- [x] 2.1 增加“release not found 仍继续清理”测试。
- [x] 2.2 增加“CR/CRD/RBAC/namespace 被清理”测试。
- [x] 2.3 增加“缺失资源类型（NoMatch/NotFound）不阻断”测试。

## 3. 实现

- [x] 3.1 扩展 `uninstallVelero` 为两阶段：Helm 卸载 + 深度清理。
- [x] 3.2 实现 Velero CR 批量清理与 finalizer 移除。
- [x] 3.3 实现 `velero` namespace 删除。
- [x] 3.4 实现 `*.velero.io` CRD 删除。
- [x] 3.5 实现 `velero*` ClusterRole/ClusterRoleBinding 删除。
- [x] 3.6 保证清理逻辑幂等与可重试。

## 4. 验证

- [x] 4.1 运行 `go test` 覆盖 `cluster` controller 相关测试。
- [x] 4.2 运行 `openspec validate update-cluster-velero-hard-cleanup --strict`。
