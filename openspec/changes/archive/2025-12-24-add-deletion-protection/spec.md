# Spec: 资源删除保护

## ADDED Requirements

### StorageRepository 删除保护
- **Scenario: 删除被引用的存储**
  - Given 一个 StorageRepository 被 DisasterPolicy 引用
  - When 用户尝试删除该 StorageRepository
  - Then 删除操作被阻塞
  - And Status.Reason 更新为 "DeletionBlocked"

### DisasterPolicy 删除保护
- **Scenario: 删除正在执行任务的策略**
  - Given 一个 DisasterPolicy 关联了正在运行 (Backuping/Restoring) 的 DisasterBackup 或 DisasterJob
  - When 用户尝试删除该 DisasterPolicy
  - Then 删除操作被阻塞
  - And Status.Reason 更新为 "DeletionBlocked"
