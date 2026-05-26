## ADDED Requirements

### Requirement: 外部资源清理的幂等性 (Idempotent External Cleanup)

控制器在执行外部资源（如 Velero）清理时，必须 (MUST) 具备幂等性，能够处理资源已存在或已删除的情况，确保清理过程不被阻塞。

#### Scenario: 忽略已存在的 DeleteBackupRequest
- **GIVEN** AppBackup 正在被删除
- **AND** 目标集群中已经存在针对某个备份的 `DeleteBackupRequest`
- **WHEN** 控制器执行 `deleteExternalResources` 清理外部资源
- **THEN** 控制器在创建 `DeleteBackupRequest` 遇到 `AlreadyExists` 错误时应忽略该错误
- **AND** 继续执行后续清理流程，最终移除 Finalizer
