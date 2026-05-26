- [x] 1. 更新 OpenSpec 文档与 delta
- [x] 1.1 重写 `proposal.md`，将旧 PVC/PV 草案升级为 scoped backup projection + cluster/namespaced phased restore
- [x] 1.2 新增 `design.md`
- [x] 1.3 新增 `specs/resource-sync/spec.md`

- [x] 2. 实现 ResourceSync backup 侧 scoped 投影
- [x] 2.1 在 `buildAppBackupSpec` 中承接 scoped namespaces / labels / namespaced resources / cluster resources
- [x] 2.2 保持 ResourceSync 固定排除 `pods/persistentvolumeclaims/persistentvolumes`
- [x] 2.3 当未显式选择 `includedClusterScopedResources` 时，确保主链路不备份 cluster-scoped 资源
- [x] 2.4 补充单元测试

- [x] 3. 实现 ResourceSync phased restore
- [x] 3.1 扩展 `ResourceSyncStatus`，增加 cluster/namespaced restore 观测字段
- [x] 3.2 在 `handleRestore` 中支持 cluster -> namespaced 顺序执行
- [x] 3.3 cluster phase 使用 `ExistingResourcePolicy=none`
- [x] 3.4 namespaced phase 使用 `ExistingResourcePolicy=update` 且禁止恢复 `pods/persistentvolumeclaims/persistentvolumes`
- [x] 3.5 保持 namespaced phase 的 skeleton / image rewrite / modifier 逻辑
- [x] 3.6 补充单元测试

- [ ] 4. 更新产物与执行校验
- [x] 4.1 运行 `make generate`
- [x] 4.2 运行 `make manifests`
- [x] 4.3 运行 `go test ./internal/controller/resourcesync/... ./internal/controller/restore/...`
- [x] 4.4 运行 `openspec validate resourcesync-two-phase-restore --strict`
- [x] 4.5 运行 `make harness-preflight`
- [x] 4.6 运行 `make harness-lint`
- [ ] 4.7 运行 `make test`
- [ ] 4.8 运行 `make lint`
