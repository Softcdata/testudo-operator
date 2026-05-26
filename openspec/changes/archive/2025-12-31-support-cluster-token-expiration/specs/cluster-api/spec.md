# Cluster API Specification

## Requirements

### Requirement: 更新集群 Token 接口
系统必须 (MUST) 提供接口允许用户更新已过期或即将过期的 Token，但不允许修改集群的核心标识属性。

#### Scenario: 成功更新 Token 和 Tags
- **GIVEN** 存在一个有效的集群记录
- **WHEN** 调用 `PATCH /api/v1/clusters/:id`
- **AND** 请求体包含新的 `token` 和/或 `tags`
- **THEN** 集群的 Token 和 Tags 被更新
- **AND** 其他字段 (如 `endpoint`, `kubeConfig`, `name`) 保持不变

#### Scenario: 禁止修改受保护字段
- **GIVEN** 存在一个有效的集群记录
- **WHEN** 调用 `PATCH /api/v1/clusters/:id`
- **AND** 请求体尝试修改 `endpoint` 或 `kubeConfig`
- **THEN** 接口应返回错误 或 忽略这些受保护字段的修改 (建议返回 400 错误以明确告知)
- **AND** 实际数据未被修改

#### Scenario: Token 格式校验
- **GIVEN** 调用更新接口
- **WHEN** 提供的 `token` 为空或格式非法
- **THEN** 接口应返回 400 Bad Request
