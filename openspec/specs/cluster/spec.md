```markdown
# Cluster Specification

## Purpose
`Cluster` 资源在灾难恢复系统中扮演着核心角色，用于定义和管理参与灾备的所有 Kubernetes 集群的连接信息、认证凭据以及状态监控。本规范定义了 Cluster 资源的行为、生命周期管理以及与 Velero 的集成要求。
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

### Requirement: 过期状态警示
系统必须 (MUST) 在 Token 即将过期或已过期时，通过 Status 或 Condition 发出警示。

#### Scenario: 生成警示事件
- **GIVEN** Token 即将过期
- **THEN** 系统应当在 Status 中记录警告信息
- **AND** 产生相应的 Kubernetes Event
```

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

### Requirement: Velero 完整性检查 (MUST)

在判定集群中的 Velero 是否安装完成时，Operator (MUST) 必须同时校验 `velero` Deployment 和 `node-agent` DaemonSet 的存在性。仅当两者都存在于目标集群时，才视为 Velero 安装也已完成。这确保了文件系统备份能力（依赖 node-agent）是可用的。

#### Scenario: 检测 node-agent 缺失
- **GIVEN** 目标集群已安装 Velero Deployment
- **BUT** 缺少 `node-agent` DaemonSet
- **WHEN** Operator 执行 Velero 安装检查
- **THEN** 判定为未安装完成
- **AND** 触发 Velero 安装/修复流程

### Requirement: Cluster ensure-storage 信号支持 sourceCluster 后缀
系统必须 (MUST) 支持通过 `Cluster` 双注解触发跨集群恢复所需的 BSL 创建，命名规则必须对齐 `SourceCluster` 语义。

#### Scenario: 双注解触发 sourceCluster 语义
- **Given** `Cluster` 带有注解 `testudo.softcdata.com/ensure-storage=<storageRepository>`
- **And** `Cluster` 带有注解 `testudo.softcdata.com/ensure-storage-source-cluster=<sourceCluster>`
- **When** `ClusterReconciler` 处理 ensure-storage 信号
- **Then** 必须计算 `bslName=<storageRepository>-<sourceCluster>`
- **And** 必须计算 `prefix=<sourceCluster>`
- **And** 必须调用 `ApplyStorageRepository` 将 BSL 对齐到目标集群

### Requirement: Cluster ensure-storage 信号兼容旧格式
系统必须 (MUST) 兼容仅携带 `ensure-storage` 的历史触发格式，保证已有调用链不回归。

#### Scenario: 缺失 sourceCluster 注解时的回退规则
- **Given** `Cluster` 带有注解 `testudo.softcdata.com/ensure-storage=<storageRepository>`
- **And** `Cluster` 未携带注解 `testudo.softcdata.com/ensure-storage-source-cluster`
- **When** `ClusterReconciler` 处理 ensure-storage 信号
- **Then** 必须计算 `bslName=<storageRepository>-<cluster.Name>`
- **And** 必须计算 `prefix=<cluster.Name>`
- **And** 必须继续执行 BSL 对齐

### Requirement: ensure-storage 信号消费后必须清理触发注解
系统必须 (MUST) 在一次性触发流程结束后清理触发注解，避免重复处理以及无效重试。

#### Scenario: BSL 对齐成功后清理双注解
- **Given** `ClusterReconciler` 成功完成 `ApplyStorageRepository`
- **When** Reconcile 进入信号收尾阶段
- **Then** 必须移除注解 `testudo.softcdata.com/ensure-storage`
- **And** 必须移除注解 `testudo.softcdata.com/ensure-storage-source-cluster`

#### Scenario: StorageRepository 缺失时清理双注解
- **Given** `ClusterReconciler` 根据 `ensure-storage` 查找 `StorageRepository`
- **When** 查询返回 `NotFound`
- **Then** 必须移除注解 `testudo.softcdata.com/ensure-storage`
- **And** 必须移除注解 `testudo.softcdata.com/ensure-storage-source-cluster`
- **And** 必须记录失败事件说明存储名称无效

### Requirement: 添加集群时必须执行 Velero 版本兼容门禁

当 `Cluster` 处于创建流程时，Operator 必须 (MUST) 校验目标集群 Velero 版本是否在受支持范围内；不兼容时必须阻断进入 `Ready`。

#### Scenario: Velero 版本不兼容时添加集群失败
- **Given** 用户正在添加一个 `Cluster`
- **And** 目标集群 Velero `serverVersion` 不在 Operator 支持范围内
- **When** `ClusterReconciler` 完成 Velero 版本探测
- **Then** 必须将 `Cluster.status.status` 设置为 `NotReady`
- **And** 必须将 `Cluster.status.reason` 设置为 `VeleroVersionIncompatible`
- **And** 必须在 `Cluster.status.message` 中包含 `expected` 与 `actual` 版本信息
- **And** 必须发射 `Warning` 事件 `VeleroCompatibilityFailed`

#### Scenario: Velero 版本兼容时允许继续创建
- **Given** 用户正在添加一个 `Cluster`
- **And** 目标集群 Velero `serverVersion` 满足受支持范围
- **When** `ClusterReconciler` 执行兼容校验
- **Then** 应当 (SHOULD) 继续后续流程
- **And** 不应 (SHOULD NOT) 因版本门禁阻断 `Ready` 状态推进

### Requirement: 添加集群时必须执行 Velero CRD 版本兼容门禁

当 `Cluster` 处于创建流程时，Operator 必须 (MUST) 校验关键 Velero CRD 的存在性与版本兼容性；关键 CRD 缺失或版本不兼容时必须报错。

#### Scenario: 关键 CRD 缺失或版本不兼容时添加集群失败
- **Given** 用户正在添加一个 `Cluster`
- **And** 目标集群缺失关键 Velero CRD，或关键 CRD 未提供受支持的 `velero.io/v1` 版本
- **When** `ClusterReconciler` 执行 CRD 兼容校验
- **Then** 必须将 `Cluster.status.status` 设置为 `NotReady`
- **And** 必须将 `Cluster.status.reason` 设置为 `VeleroCRDVersionIncompatible`
- **And** 必须在 `Cluster.status.message` 中包含不兼容的 CRD 名称与期望版本
- **And** 必须发射 `Warning` 事件 `VeleroCompatibilityFailed`

#### Scenario: CRD 版本检测失败时添加集群失败
- **Given** 用户正在添加一个 `Cluster`
- **And** Operator 无法完成 CRD 兼容校验（例如权限不足或连接异常）
- **When** `ClusterReconciler` 执行 CRD 兼容校验
- **Then** 必须将 `Cluster.status.status` 设置为 `NotReady`
- **And** 必须将 `Cluster.status.reason` 设置为 `VeleroCRDCheckFailed`
- **And** 必须在 `Cluster.status.message` 中包含检测失败原因

### Requirement: 兼容性失败必须可被上层直接感知为“添加集群失败”

兼容性校验失败时，Operator 必须 (MUST) 输出稳定错误语义，确保 server/web 能在添加流程中直接报错。

#### Scenario: 创建阶段兼容性失败时发射失败结束事件
- **Given** 用户正在执行“创建集群”操作
- **And** Velero 版本或 CRD 兼容性校验失败
- **When** `ClusterReconciler` 处理失败分支
- **Then** 必须发射结构化 `TaskFinished` 事件且状态为 `Failed`
- **And** 失败描述必须与 `Cluster.status.message` 保持语义一致

