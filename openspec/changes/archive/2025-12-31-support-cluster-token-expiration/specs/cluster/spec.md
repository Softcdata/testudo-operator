# Cluster Specification

## Requirements

### Requirement: Token 支持与过期时间追踪
当通过 Token 添加集群时，系统必须 (MUST) 解析 Token 并记录其过期时间。

#### Scenario: 记录 Token 过期时间
- **GIVEN** 用户创建或更新 `Cluster` 资源，并提供了 `Spec.Token`
- **AND** 该 Token 是一个有效的 JWT
- **WHEN** 控制器进行调和 (Reconcile)
- **THEN** 控制器应解析 Token 的 `exp` (Expires At) claim
- **AND** 将过期时间记录到 `Status.TokenExpiration` 字段

#### Scenario: Token 无过期时间 (永久)
- **GIVEN** 用户提供的 `Spec.Token` 是无限期的 (无 `exp` claim)
- **THEN** `Status.TokenExpiration` 应为空或置为 nil，表示永不过期

#### Scenario: Token 已过期或鉴权失败
- **GIVEN** 用户提供的 `Spec.Token` 已过期或无效
- **WHEN** 控制器无法连接集群或 JWT 解析显示已过期
- **THEN** `Status.Status` 应为 `NotReady`
- **AND** `Status.Reason` 应设置为 "TokenExpired" 或 "AuthenticationFailed"
- **AND** `Status.Message` 应包含具体错误信息

#### Scenario: 非 JWT Token
- **GIVEN** 用户提供的 `Spec.Token` 不是 JWT 格式 (例如 Opaque Token)
- **THEN** 控制器应跳过解析，`Status.TokenExpiration` 保持为空
- **AND** 系统不应因此报错（兼容非 JWT Token）

### Requirement: 过期状态警示 (Optional)
建议在 Token 即将过期或已过期时，通过 Status 或 Condition 发出警示。
