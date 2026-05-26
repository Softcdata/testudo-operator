# Proposal: 资源删除保护机制

## Summary
为 `StorageRepository` and `DisasterPolicy` 引入 Finalizer 机制，防止误删正在被使用的资源。

## Motivation
- `StorageRepository` 被 `DisasterPolicy` 引用，如果被删除会导致策略失效。
- `DisasterPolicy` 如果在备份或恢复任务执行期间被删除，会导致任务状态异常。

## Proposed Changes

### StorageRepository
- 添加 Finalizer: `testudo.softcdata.com/storage-finalizer`
- 删除拦截: 检查是否被 `DisasterPolicy` 引用。

### DisasterPolicy
- 添加 Finalizer: `testudo.softcdata.com/policy-finalizer`
- 删除拦截: 检查是否有正在运行的 `DisasterBackup` 或 `DisasterJob`。
