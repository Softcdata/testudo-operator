# 提案：增强 StorageRepository API 支持

## 背景

当前 `StorageRepository` 资源虽然已有状态反馈，但缺乏安全的接口来更新鉴权信息。为了支持密钥轮换或修正错误配置，需要提供安全的 API 接口。

## 解决方案

1.  **Server 侧**:
    *   新增 `PATCH /api/v1/storage-repositories/:name` 接口。
    *   仅允许修改 `accessKey` 和 `secretKey`（以及 `bucket`、`region`），严禁修改 `endpoint` 等可能彻底改变资源指向的核心字段。

## 影响范围

*   `disaster-server`: 新增 API 接口及 DTO 更新。

## 技术细节

*   新增 `PatchStorageRepositoryRequest` 结构体。
*   实现 Handler 逻辑，读取现有资源，更新指定字段，保留其他字段不变，并调用 K8s Update 接口。
