# Spec: S3 配置验证

## ADDED Requirements

### S3 连接验证
- **Scenario: 验证有效的 S3 配置**
  - Given 一个包含正确 S3 配置的 StorageRepository
  - When Controller 进行 Reconcile
  - Then 能够成功连接 S3
  - And Status.Status 变为 "Available"

- **Scenario: 自动创建 Bucket**
  - Given 配置正确但 Bucket 不存在
  - When Controller 进行 Reconcile
  - Then 自动创建 Bucket
  - And Status.Status 变为 "Available"

- **Scenario: 验证无效的配置**
  - Given 配置错误的 StorageRepository (如错误的 AK/SK)
  - When Controller 进行 Reconcile
  - Then 连接 S3 失败
  - And Status.Status 变为 "Unavailable"
  - And Status.Message 包含错误信息
