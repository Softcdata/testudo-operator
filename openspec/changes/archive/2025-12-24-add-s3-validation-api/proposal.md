# Proposal: S3 配置连接验证

## Summary
在 `StorageRepository` 的 Reconcile 循环中增加 S3 连接验证逻辑。

## Motivation
用户创建 `StorageRepository` 后，需要及时知道配置是否正确（Endpoint, AK/SK, Bucket 是否可用）。

## Proposed Changes

### StorageRepository Controller
- 在 Reconcile 中调用 AWS SDK 验证 S3 连接。
- 如果 Bucket 不存在，尝试自动创建。
- 更新 Status:
  - `Available`: 验证成功
  - `Unavailable`: 验证失败
