# 任务列表：增强 StorageRepository API 支持

## Server 变更
- [x] 在 `disaster-server` 中实现 `PATCH /api/v1/storage-repositories/:name` 接口。 <!-- id: add-api -->
- [x] 限制该接口仅允许更新 `accessKey`, `secretKey`, `bucket`, `region`，禁止修改 `endpoint`。 <!-- id: api-validation -->
- [x] 添加 API 单元测试。 <!-- id: server-tests -->
