# 规范：StorageRepository 控制器

## Requirements

### Requirement: 存储库 BSL 生成
StorageRepository 必须能在目标集群生成对应的 `BackupStorageLocation`。

#### Scenario: 同步 BSL 到远端
- **GIVEN** 有效的存储配置
- **THEN** 在目标集群创建 `velerov1.BackupStorageLocation`
# Storage Repository API Specification

## Requirements

### Requirement: 更新存储配置接口
系统必须提供接口允许用户更新存储的认证信息，同时保护核心配置不被篡改。

#### Scenario: 成功更新密钥
- **GIVEN** 存在有效的 StorageRepository
- **WHEN** 调用 `PATCH /api/v1/storage-repositories/:name`
- **AND** 仅提供 new `accessKey` 和 `secretKey`
- **THEN** 资源被更新，连接检查逻辑将在下一次 Operator Reconcile 时使用新密钥

#### Scenario: 禁止修改 Endpoint
- **GIVEN** 存在有效的 StorageRepository
- **WHEN** 调用 PATCH 接口尝试修改 `endpoint`
- **THEN** 接口应忽略该修改或返回 400 错误
- **AND** 实际资源的 endpoint 保持不变
