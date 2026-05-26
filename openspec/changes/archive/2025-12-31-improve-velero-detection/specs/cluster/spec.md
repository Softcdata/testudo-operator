## ADDED Requirements

### Requirement: Velero 安装检测
Cluster Controller 必须 (MUST) 准确检测目标集群中 Velero 的安装状态,以决定是否需要自动安装 Velero。

#### Scenario: Velero CRD 存在时判定为已安装
- **WHEN** 检测 Velero 安装状态
- **AND** 目标集群中 Velero Backup CRD 可访问 (通过 `List` 操作)
- **THEN** 判定 Velero 已安装
- **AND** 跳过 Velero 安装流程

#### Scenario: Velero CRD 不存在时判定为未安装
- **WHEN** 检测 Velero 安装状态
- **AND** 目标集群中 Velero Backup CRD 不存在 (返回 `meta.NoMatchError`)
- **THEN** 判定 Velero 未安装
- **AND** 触发 Velero 自动安装流程

#### Scenario: CRD 访问失败时判定为未安装
- **WHEN** 检测 Velero 安装状态
- **AND** 访问 Velero Backup CRD 时发生错误 (非 `NoMatchError`,如权限错误)
- **THEN** 判定 Velero 未安装或不可用
- **AND** 记录详细错误日志
- **AND** 不触发自动安装 (避免在权限不足时误操作)

#### Scenario: Velero Deployment 不存在但 CRD 存在时仍判定为已安装
- **GIVEN** Velero CRD 已安装
- **AND** Velero Deployment 被删除或不可用
- **WHEN** 检测 Velero 安装状态
- **THEN** 判定 Velero 已安装 (基于 CRD 存在性)
- **AND** 后续的 `checkVeleroVersion` 方法会检测服务可用性
- **AND** 如果服务不可用,集群状态会被设置为 NotReady

### Requirement: CRD 检测方法一致性
Operator 中所有 Velero CRD 可用性检测必须 (MUST) 使用统一的检测模式,确保逻辑一致性。

#### Scenario: 使用 List + Limit(1) 模式检测 CRD
- **WHEN** 需要检测 Velero CRD 是否可用
- **THEN** 使用 `cli.List(&velerov1.BackupList{}, client.Limit(1))` 进行探测
- **AND** 使用 `meta.IsNoMatchError(err)` 判断 CRD 是否存在
- **AND** 该模式应用于所有 CRD 检测场景 (安装检测、删除保护等)
