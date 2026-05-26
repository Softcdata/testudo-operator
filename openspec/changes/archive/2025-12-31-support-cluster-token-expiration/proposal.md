# 提案：支持集群 Token 过期时间检测

## 背景

当前系统允许通过 Token 将 Kubernetes 集群接入灾难恢复系统。这些 Token 既可以是永久的，也可以是临时的（例如具有过期时间的 ServiceAccount Token）。为了更好地管理集群连接状态并在凭证即将过期时提醒用户，我们需要跟踪 Token 的过期时间。

## 解决方案

1.  更新 `Cluster` CRD 的 `Status` 字段，新增 `TokenExpiration` 字段。
2.  更新 `Cluster` 控制器逻辑：
    *   检测 `Spec.Token` 是否存在。
    *   将 Token 解析为 JWT（不验证签名，主要提取 Payload 中的 Claims）。
    *   提取 `exp` (expiration time) 字段。
    *   将过期时间填充到 `Status.TokenExpiration`。
    *   如果 Token 已过期，将 Status 更新为 `NotReady`，并设置 `Status.Reason` 为 "TokenExpired"，`Status.Message` 为具体的说明。
    *   在连接集群失败（例如 401 Unauthorized）时，明确设置 `Status.Reason` 和 `Status.Message` 以告知用户。

3.  完善 `disaster-server` API：
    *   提供更新集群 Token 的接口 (PATCH /cluster/:id)。
    *   **限制**: 该接口仅允许修改 `Token` 和 `Tags` 字段，禁止修改其他核心字段 (如 Endpoint)，以保证集群标识的稳定性。

## 影响范围

*   `disaster-operator`: 涉及 `Cluster` 控制器逻辑和 CRD 更新。
*   `disaster-server`: 如果 CRD 状态更新，API 返回的 Cluster 详情中会自动包含过期时间，可能无需显式修改（取决于 DTO 映射）。

## 技术细节

使用 JWT 解析库（如 `github.com/golang-jwt/jwt/v4`）解码 Token 片段并检索标准 Claims。
