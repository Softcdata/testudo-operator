## ADDED Requirements

### Requirement: 配置分层与优先级

系统必须 (MUST) 区分业务默认配置、资源级 `spec` 配置、operator 运行时配置、启动期配置和 Velero rollout 配置，并以确定性优先级解析最终行为。

#### Scenario: 资源级配置覆盖运行时默认值
- **Given** `OperatorRuntimeConfig` 中配置 `restoreRuntime.inProgressMaxWait=2h`
- **And** 一个 `AppRestore` 配置了 `spec.timeout=30m`
- **When** operator 判断该 `AppRestore` 的恢复超时
- **Then** operator 必须 (MUST) 使用 `spec.timeout=30m`
- **And** 不得使用 runtime config 的 `2h` 覆盖用户本次提交的资源级配置

#### Scenario: 未指定资源级配置时使用运行时默认值
- **Given** `OperatorRuntimeConfig` 中配置 `backupRuntime.inProgressMaxWait=4h`
- **And** 一个 `AppBackup` 未配置 `spec.timeout`
- **When** operator 判断该备份的默认 in-progress 最大等待时间
- **Then** operator 必须 (MUST) 使用 runtime config 中的 `4h`

#### Scenario: 配置缺失时回退旧默认行为
- **Given** 集群中不存在 `OperatorRuntimeConfig/default`
- **When** operator 启动并执行备份、恢复、同步或容灾操作
- **Then** operator 必须 (MUST) 使用与变更前一致的默认值
- **And** 现有部署不得因为缺少 runtime config 而失败

### Requirement: OperatorRuntimeConfig singleton 契约

系统必须 (MUST) 使用强类型 `OperatorRuntimeConfig` CRD 作为 operator runtime hot config 的唯一载体。

#### Scenario: singleton 位置固定
- **Given** operator 的 `ManagementNamespace()` 解析为 `disaster-system`
- **When** operator 读取运行时配置
- **Then** operator 必须 (MUST) 只读取 `disaster-system` 命名空间下名称为 `default` 的 `OperatorRuntimeConfig`
- **And** 其他名称或其他命名空间的 `OperatorRuntimeConfig` 不得影响 active runtime config

#### Scenario: status subresource 记录配置状态
- **Given** `OperatorRuntimeConfig/default` 存在
- **When** operator 完成配置校验
- **Then** operator 必须 (MUST) 通过 status subresource 写入 `Ready` 或 `Invalid` condition
- **And** condition message 必须 (MUST) 包含非法字段路径或当前配置已激活的摘要

#### Scenario: RBAC 只授权管理命名空间读取和状态更新
- **Given** operator 部署在管理命名空间
- **When** 安装 runtime config RBAC
- **Then** RBAC 必须 (MUST) 允许 operator watch/read `OperatorRuntimeConfig`
- **And** 允许 operator 更新 `OperatorRuntimeConfig/status`
- **And** 不得要求 operator 读取 server/web 业务默认配置

### Requirement: Operator Runtime Config 热加载

系统必须 (MUST) 提供 operator 运行时配置热加载能力，使控制器运行行为在配置更新后无需重启即可生效。

#### Scenario: 更新恢复卡顿阈值后下一次判断生效
- **Given** 一个正在运行的 `AppRestore`
- **And** 当前 active runtime config 中 `restoreRuntime.progressCompleteGrace=5m`
- **When** 管理员将 `OperatorRuntimeConfig/default.spec.restoreRuntime.progressCompleteGrace` 更新为 `30m`
- **Then** operator 必须 (MUST) 在下一次恢复卡顿判断时读取新的 `30m`
- **And** 不得要求重启 operator Pod

#### Scenario: 更新操作默认超时后新操作生效
- **Given** active runtime config 中 `operationRuntime.defaultTimeoutMinutes=60`
- **When** 管理员将其更新为 `120`
- **And** 用户创建未显式指定 `timeoutMinutes` 且实例也未指定 `operationTimeoutMinutes` 的 `DisasterOperation`
- **Then** operator 必须 (MUST) 按 `120` 分钟作为该操作的默认超时

#### Scenario: 运行中的步骤轮询使用新配置
- **Given** 一个正在执行步骤的 `DisasterOperation`
- **When** 管理员更新 `operationRuntime.stepRunningRequeue`
- **Then** operator 必须 (MUST) 在后续未完成步骤的 requeue 结果中使用新间隔

#### Scenario: 删除 singleton 后回退默认值
- **Given** active runtime config 来自合法的 `OperatorRuntimeConfig/default`
- **When** 管理员删除 `OperatorRuntimeConfig/default`
- **Then** operator 必须 (MUST) 停止使用被删除对象中的自定义值
- **And** active runtime config 必须 (MUST) 回退到启动 env/flag 默认值和代码默认值
- **And** operator 应 (SHOULD) 发出 normal event 或日志说明 runtime config 已回退默认值

### Requirement: 非法运行时配置保护

系统必须 (MUST) 采用宽松 CRD 结构校验与 controller 语义校验分层。CRD OpenAPI schema 必须 (MUST) 校验对象结构、字段类型和嵌套形状，但不得 (MUST NOT) 用 schema min/max 或等价规则拦截运行时字段表中的范围错误；范围、默认值合并、兼容迁移和跨字段关系必须 (MUST) 由 operator controller 校验。对 API server 已接受但语义非法的配置，operator 必须 (MUST) 拒绝激活非法值，并保持最后一份有效配置继续服务。

#### Scenario: 结构非法由 API server 拒绝
- **Given** 管理员提交的 `OperatorRuntimeConfig/default` 中 `syncRuntime.historyRetention` 是字符串 `"many"` 而不是 integer
- **When** Kubernetes API server 校验该对象
- **Then** API server 必须 (MUST) 拒绝该请求，通常返回 422
- **And** operator 不需要为这次未持久化的请求写入 `Invalid` status
- **And** 不得产生 `OperatorRuntimeConfig.status.conditions=Invalid`

#### Scenario: schema 不拦截范围错误
- **Given** 管理员提交的 `OperatorRuntimeConfig/default` 结构正确
- **And** `restoreRuntime.retryBackoff=0s`
- **When** Kubernetes API server 校验该对象
- **Then** CRD schema 不得 (MUST NOT) 因该范围错误拒绝对象持久化
- **And** 该范围错误必须 (MUST) 由 operator controller 语义校验处理

#### Scenario: 非法 duration 不影响当前有效配置
- **Given** active runtime config 中 `restoreRuntime.retryBackoff=15s`
- **When** API server 已接受管理员提交的语义非法配置 `restoreRuntime.retryBackoff=0s`
- **Then** operator 必须 (MUST) 拒绝激活该非法字段所属配置快照
- **And** 后续恢复重试仍使用最后有效值 `15s`
- **And** `OperatorRuntimeConfig/default.status.conditions` 必须 (MUST) 记录 `Invalid` 状态和字段路径

#### Scenario: poll interval 不得为 0
- **Given** active runtime config 中 `backupRuntime.pollInterval=5s`
- **When** API server 已接受管理员提交的 `backupRuntime.pollInterval=0s`
- **Then** operator 必须 (MUST) 拒绝激活该配置
- **And** 后续备份观察不得进入 tight requeue

#### Scenario: historyRetention 不得为 0
- **Given** active runtime config 中 `syncRuntime.historyRetention=20`
- **When** API server 已接受管理员提交的 `syncRuntime.historyRetention=0`
- **Then** operator 必须 (MUST) 拒绝激活该配置
- **And** 后续同步历史不得被清空为 0 条保留

#### Scenario: operation 默认超时不得为 0
- **Given** active runtime config 中 `operationRuntime.defaultTimeoutMinutes=60`
- **When** API server 已接受管理员提交的 `operationRuntime.defaultTimeoutMinutes=0`
- **Then** operator 必须 (MUST) 拒绝激活该配置
- **And** 未显式配置 timeout 的新 `DisasterOperation` 不得因此失去超时保护

#### Scenario: 跨字段关系错误由 controller 写 Invalid
- **Given** active runtime config 中 `instanceRuntime.transitionWatchdogTimeout=2m`
- **When** API server 已接受管理员提交的 `instanceRuntime.transitionWatchdogTimeout=20s`
- **And** 同一配置中 `instanceRuntime.minTransitionWatchdogTimeout=30s`
- **Then** operator 必须 (MUST) 拒绝激活该配置
- **And** `OperatorRuntimeConfig/default.status.conditions` 必须 (MUST) 记录 `Invalid` 状态和字段路径
- **And** 后续实例 transition watchdog 仍使用最后有效配置

#### Scenario: 未设置和显式 0 必须可区分
- **Given** `OperatorRuntimeConfig/default.spec.restoreRuntime.retryLimitProgress` 未设置
- **When** operator 合并配置
- **Then** operator 必须 (MUST) 使用默认值 `1`
- **But** 当该字段显式设置为 `0`
- **Then** operator 必须 (MUST) 接受 `0` 并表示该类型卡顿不自动重试

#### Scenario: 通用 retryLimit 作为 per-type fallback
- **Given** `OperatorRuntimeConfig/default.spec.restoreRuntime.retryLimit=3`
- **And** `retryLimitProgress`、`retryLimitStartup`、`retryLimitMissing`、`retryLimitEmpty` 均未设置
- **When** operator 合并 restore runtime config
- **Then** operator 必须 (MUST) 将这四类 retry limit 解析为 `3`
- **But** 如果 `retryLimitMissing=1` 被显式设置
- **Then** `retryLimitMissing` 必须 (MUST) 使用显式值 `1`

#### Scenario: 配置修复后恢复 Ready
- **Given** `OperatorRuntimeConfig/default` 因非法字段处于 `Invalid`
- **When** 管理员修正该字段为合法值
- **Then** operator 必须 (MUST) 激活新的配置快照
- **And** 将配置状态更新为 `Ready`

### Requirement: Operator 只消费最终 CRD spec

系统必须 (MUST) 将前端动态传入视为外部系统写入 CRD `spec` 的结果。operator 不得读取或依赖 server/web 的业务默认配置。

#### Scenario: AppBackup spec 是 operator 的事实来源
- **Given** 外部 server/web 创建了 `AppBackup` 并写入 `spec.timeout=2h`
- **When** operator 执行该备份
- **Then** operator 必须 (MUST) 只依据 `AppBackup.spec.timeout` 和 operator runtime config 判断超时
- **And** operator 不得读取 server/web 业务默认配置来隐式覆盖该对象

#### Scenario: DisasterOperation spec 是 operator 的事实来源
- **Given** 外部 server/web 创建了 `DisasterOperation` 并写入 `spec.retryPolicy.maxRetries=2`
- **When** operator 执行该操作
- **Then** operator 必须 (MUST) 按该 `spec.retryPolicy` 执行重试语义
- **And** operator 不得读取 server/web 业务默认配置来改变已创建操作

#### Scenario: DataSync 托管 AppBackup 继承实例级操作超时
- **Given** 一个 `DisasterInstance` 配置了 `spec.operationTimeoutMinutes=180`
- **And** `DataSync` 需要创建或对齐其托管的 `AppBackup`
- **When** operator 构造 desired `AppBackup.spec`
- **Then** `AppBackup.spec.timeout` 必须 (MUST) 被设置为 `180m`
- **And** 后续 `DisasterInstance.spec.operationTimeoutMinutes` 变化时，operator 必须 (MUST) 将 timeout 差异纳入 AppBackup spec 对齐判断

#### Scenario: ResourceSync 托管 AppBackup 继承实例级操作超时
- **Given** 一个 `DisasterInstance` 配置了 `spec.operationTimeoutMinutes=180`
- **And** `ResourceSync` 需要创建或对齐其托管的 `AppBackup`
- **When** operator 构造 desired `AppBackup.spec`
- **Then** `AppBackup.spec.timeout` 必须 (MUST) 被设置为 `180m`
- **And** 如果 `operationTimeoutMinutes<=0`
- **Then** operator 必须 (MUST) 保持 `AppBackup.spec.timeout` 未设置，以使用 AppBackup 内置默认 timeout fallback

#### Scenario: 业务默认配置 API 是外部依赖
- **Given** 产品需要前端读取默认备份 timeout 或默认 retryPolicy
- **When** 规划 server/web 实现
- **Then** 本 operator change 必须 (MUST) 将该能力记录为外部依赖
- **And** 本 operator change 不得宣称该 server/web 行为已实现
- **And** 后续 companion change 应 (SHOULD) 定义强类型 API 和校验

### Requirement: 启动期配置与运行时配置边界

系统必须 (MUST) 保留进程启动形态参数的重启加载语义，并禁止将这类参数误归入热加载配置。

#### Scenario: leader election 仍为启动期配置
- **Given** operator 使用 `--leader-elect` 启动
- **When** 管理员更新 `OperatorRuntimeConfig/default`
- **Then** 该更新不得 (MUST NOT) 改变当前 manager 的 leader election 行为

#### Scenario: APPRESTORE env 作为兼容默认值
- **Given** operator 启动时设置了 `APPRESTORE_RETRY_BACKOFF=25s`
- **And** `OperatorRuntimeConfig/default` 未配置 `restoreRuntime.retryBackoff`
- **When** operator 构造 active runtime config
- **Then** operator 应 (SHOULD) 使用 env 中的 `25s` 作为兼容默认值
- **But** 如果 `OperatorRuntimeConfig/default` 显式配置了 `restoreRuntime.retryBackoff`
- **Then** runtime config 必须 (MUST) 优先于 env 默认值

### Requirement: Velero workload 参数不得归入 operator 热加载

系统必须 (MUST) 区分 operator 热加载参数和 Velero workload 参数。本 change 不交付 Velero workload 参数的配置载体或 rollout 能力。

#### Scenario: OperatorRuntimeConfig 不包含 Velero extraArgs
- **Given** 管理员希望修改 Velero `clientQPS` 或 `backupSyncPeriod`
- **When** 管理员查看 `OperatorRuntimeConfig` schema
- **Then** 该 schema 不得 (MUST NOT) 提供 Velero workload `extraArgs` 字段
- **And** 文档必须 (MUST) 说明这类参数需要独立 rollout change

#### Scenario: Helm timeout 只影响后续安装或升级调用
- **Given** runtime config 中 `clusterRuntime.veleroInstallTimeout=10m`
- **When** 管理员将其更新为 `20m`
- **Then** 该配置必须 (MUST) 影响后续 Helm install/upgrade 调用
- **And** 不得影响已经完成或正在由 Helm 执行的历史调用

#### Scenario: AppRestore 恢复动作不是配置驱动 rollout
- **Given** AppRestore 检测到 restore stall 并准备尝试 Velero restart 恢复动作
- **When** 当前 stall 类型是 startup transient 或 empty status
- **Then** operator 必须 (MUST) 跳过 Velero restart 并记录事件或日志
- **And** 当仍存在运行中的 Velero Backup、Restore、PodVolumeBackup 或 PodVolumeRestore 时
- **Then** operator 必须 (MUST) 跳过 Velero restart
- **And** 该恢复动作不得 (MUST NOT) 被文档或状态描述为 Velero image/extraArgs/node-agent 的配置驱动 rollout

### Requirement: AppRestore transient 与 PVR 安全保护

系统必须 (MUST) 使用 runtime retry/backoff 和 PVR 状态保护 AppRestore convergence，避免 transient API 错误或活跃 PVR 导致误失败、误删或误重试。

#### Scenario: transient Get Restore 错误保持 Restoring
- **Given** 一个 `AppRestore` 处于 `Restoring`
- **When** operator 读取目标集群 Velero `Restore` 时遇到 transient Kubernetes API 错误
- **Then** operator 必须 (MUST) 保持 `AppRestore` 为 `Restoring`
- **And** 按 `restoreRuntime.retryBackoff` 返回 requeue
- **And** 不得将该 `AppRestore` 标记为 Failed

#### Scenario: completed progress 但仍有活跃 PVR 不触发自动重试
- **Given** Velero `Restore` 仍是 `InProgress`
- **And** restore progress 显示 items 已全部恢复
- **And** 该 restore 仍有关联的非终态 `PodVolumeRestore`
- **When** operator 执行 completed-progress stall 检测
- **Then** operator 必须 (MUST) 保持 `AppRestore` 为 `Restoring`
- **And** 不得删除该 Velero `Restore`
- **And** 不得增加 progress stall retry count
