## ADDED Requirements

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
