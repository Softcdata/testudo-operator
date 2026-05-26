## ADDED Requirements
### Requirement: 存储库 BSL 生成
StorageRepository 必须能在目标集群生成对应的 `BackupStorageLocation`。

#### Scenario: 同步 BSL 到远端
- **GIVEN** 有效的存储配置
- **THEN** 在目标集群创建 `velerov1.BackupStorageLocation`
