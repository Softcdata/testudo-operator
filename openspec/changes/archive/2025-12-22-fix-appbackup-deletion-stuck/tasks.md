# Tasks

- [x] 修改 `internal/controller/appbackup/appbackup_state.go` 中的 `DeletingHandler`，处理 `GetKubeClient` 返回的 NotFound 错误。 <!-- id: fix-deletion-handler -->
- [x] 验证当 Cluster 不存在时，删除 AppBackup 能够成功移除 Finalizer 并完成删除。 <!-- id: verify-fix -->
