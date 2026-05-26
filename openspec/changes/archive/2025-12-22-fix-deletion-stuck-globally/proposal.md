# Fix Deletion Stuck Globally

## Summary
修复项目中所有控制器在删除资源时，因依赖集群不存在而导致无法移除 Finalizer 的问题。

## Motivation
类似于 `AppBackup` 的问题，`AppRestore` 和 `DisasterJob` 控制器在删除资源时，也会尝试连接到关联的集群以清理外部资源。如果关联的集群不存在（NotFound），控制器会报错并重试，导致 Finalizer 无法被移除，资源一直处于 `Deleting` 状态。

## Proposed Changes

### AppRestore
修改 `internal/controller/apprestore/apprestore_state.go` 中的 `DeletingHandler.Handle`：
- 在调用 `GetKubeClient` 后检查错误。
- 如果错误是 `NotFound`，记录警告日志，跳过外部资源清理，直接移除 Finalizer。

### DisasterJob
修改 `internal/controller/disasterjob_controller.go` 中的 `deleteExternalResources`：
- 在调用 `GetKubeClientSet` 获取源集群或目标集群客户端时检查错误。
- 如果错误是 `NotFound`，记录警告日志，跳过相应的清理步骤（如果是源集群不存在，跳过源集群清理；如果是目标集群不存在，跳过目标集群清理），不返回错误，允许流程继续。

## Alternatives Considered
- **忽略所有错误**: 在删除时忽略所有错误。这可能导致在网络抖动时未能清理资源，不推荐。只应忽略明确的 `NotFound` 错误。
