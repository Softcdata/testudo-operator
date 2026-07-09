# Change: 添加备份/恢复/容灾动态配置分层机制

## Why

当前备份、恢复、同步、容灾操作中存在多类配置来源混杂的问题：

- 一部分参数写死在 operator 代码中，例如备份/恢复默认超时、轮询间隔、AppRestore 卡顿判定和自动重试阈值。
- 一部分参数通过环境变量配置，例如 `APPRESTORE_*`，但只在进程启动时读取，无法热加载。
- 一部分参数由前端或 server 创建 CRD 时写入 `spec`，但这些默认值本身缺少统一配置来源，容易继续散落在前端或后端代码中。
- Velero 自身参数位于 Helm values 或安装流程中，和 operator 内存配置不是同一种生效机制。

因此需要建立明确的配置分层机制，区分：

1. 前端读取配置后动态写入 CRD `spec` 的业务默认值。
2. operator 必须热加载的控制器运行时参数。
3. 允许重启后加载的进程启动参数。
4. 需要 Velero upgrade/rollout 才能生效的安装与运行参数。

该分层必须让用户可配置，同时避免把所有参数都塞进环境变量或每次前端请求中。

## Goals

- 定义备份/恢复/容灾参数的配置分层和优先级。
- 新增 operator 运行时热加载配置能力，用于控制超时、轮询、卡顿判定、自动重试、状态机 watchdog、存储/集群检查频率等控制器行为。
- 保持 CRD `spec` 作为单次资源/单次操作的最高优先级配置来源。
- 明确前端动态传参的外部依赖边界：业务默认值应由 server/web companion change 提供配置来源，再由外部系统写入资源 `spec`。
- 保留现有 env/flag 作为启动期默认值或兼容入口，但不再作为动态配置主路径。
- 明确 Velero 镜像、extraArgs、node-agent、BSL validation 等参数需要通过 upgrade/rollout 生效。
- 提供严格校验、默认值回退、配置错误可观测性，避免错误配置导致 controller 崩溃或误判业务状态。

## Non-Goals

- 不在本变更中实现前端 UI。
- 不在本变更中实现 `disaster-server` 的系统设置 CRUD API；若复用 `add-platform-settings`，由 server 仓库后续接入。
- 不在本变更中定义或交付 server/web 的业务默认配置 API；本变更只说明 operator 与该外部能力的边界。
- 不改变现有 `AppBackup.spec.timeout`、`AppRestore.spec.timeout`、`DisasterOperation.spec.timeoutMinutes` 等资源级字段的语义。
- 不将敏感密钥放入运行时配置；S3 accessKey/secretKey、Cluster token/kubeConfig 仍由现有资源字段和 Secret 机制管理。
- 不实现 Velero workload 参数的配置载体、upgrade/rollout 触发、状态回写或失败回滚；这类能力需要单独提案。
- 不在提案阶段修改 `internal/`、`pkg/`、`cmd/`、`config/` 代码。

## Scope

本提案覆盖 operator 侧配置契约与后续实施计划：

- 新增强类型 `OperatorRuntimeConfig` singleton CRD。
- 新增 API type、CRD、deepcopy、RBAC 和 scheme 注册。
- 新增 runtime config provider、watch/cache 和 atomic snapshot 读取接口。
- 将当前硬编码的备份/恢复/容灾运行时参数纳入配置域。
- 接入 AppBackup、AppRestore、DisasterOperation、DisasterInstance、DataSync/ResourceSync、StorageRepository、Cluster 等 controller。
- 修改默认值解析、`APPRESTORE_*` env 兼容、requeue/timeout 获取路径。
- 明确 `DisasterInstance.spec.operationTimeoutMinutes` 向 DataSync/ResourceSync 托管的 `AppBackup.spec.timeout` 传递，并在 AppBackup desired spec 对齐时比较/更新 timeout。
- 明确 AppRestore 对 Velero Restore transient API 读取错误、活跃 PVR、以及 Velero restart 安全保护的运行时行为。
- 更新 sample、部署说明和回滚说明。
- 明确配置优先级和生效边界。
- 明确前端动态传参的外部依赖边界：operator 只消费最终 CRD `spec`，业务默认配置 API 由 server/web companion change 承接。
- 明确 Velero 安装/运行参数不属于本 change 的热加载范围，后续由 Velero rollout companion change 承接。

## Proposed Configuration Layers

### Layer 1: CRD Spec Override

适用于用户本次明确选择的业务参数，优先级最高：

- `AppBackup.spec.timeout`
- `AppRestore.spec.timeout`
- `DisasterInstance.spec.operationTimeoutMinutes`
- `DisasterInstance.spec.restorePolicy`
- `DisasterOperation.spec.timeoutMinutes`
- `DisasterOperation.spec.retryPolicy`
- `DisasterOperation.spec.skipFinalSync`
- `DisasterOperation.spec.skipScaleDownSource`
- `DisasterOperation.spec.skipPodReadyCheck`
- `DisasterOperation.spec.waitUntilReady`
- `DisasterDrill.spec.restorePolicy`
- `DisasterDrill.spec.waitUntilReady`

### Layer 2: Business Operation Defaults

适用于前端读取后写入 CRD `spec` 的默认值，例如：

- 默认备份 timeout、TTL、schedule、backup template。
- 默认恢复 timeout、itemOperationTimeout、restore policy、resource modifier。
- 默认实例 operation timeout、podRestoreMethod、skipPodReadyCheck。
- 默认操作 retryPolicy、waitUntilReady、skipFinalSync。
- 默认演练 namespaceMapping、cleanup、waitUntilReady。

这些配置不由本 change 交付；后续 server/web companion change 应提供强类型 API 和校验。`add-platform-settings` 的泛 KV 最多作为底层存储，不是业务默认配置 API 本身。operator 不直接依赖前端默认值；operator 只读取最终写入 CRD 的 `spec`。

### Layer 3: Operator Runtime Hot Config

适用于控制器内部行为，必须由 operator watch `OperatorRuntimeConfig/default` 后热加载：

- backup/restore 默认最大等待与轮询间隔。
- AppRestore 卡顿判定、PVR pending 阈值、自动重试次数和 backoff。
- AppRestore transient API 错误重试间隔、活跃 PVR 保护和受保护的 Velero restart 恢复动作。
- DisasterOperation 默认超时、步骤轮询、默认 retry interval。
- DisasterInstance transition watchdog、初始化/稳态/失败轮询。
- DataSync/ResourceSync 观察轮询、历史保留、以及托管 AppBackup desired spec 中的 timeout 对齐。
- StorageRepository 校验/统计刷新间隔。
- Cluster/Velero 安装、卸载、检查的重试间隔和 Helm timeout。

### Layer 4: Startup Configuration

适用于进程启动形态：

- manager metrics/probe 监听地址。
- leader election。
- webhook/metrics TLS 文件路径。
- management namespace。
- license namespace 和 CA path。
- dependency backfill on start。
- `VELERO_CRDS_PATH`。

这些参数允许重启后加载。

### Layer 5: Velero Upgrade/Rollout Configuration

适用于远端 Velero 组件：

- Velero image 和 plugin image。
- Velero extraArgs，例如 qps/burst、backup-sync-period、item-operation-sync-frequency。
- node-agent 参数。
- BSL validationFrequency。

这类参数可配置，但必须通过 Helm upgrade、patch 或 rollout 生效。本 change 不实现该配置载体和 rollout 状态；只要求 operator runtime config 不把这类参数声明为纯热加载。

## Precedence

配置解析必须遵循以下优先级：

1. 单次资源/操作 CRD `spec`。
2. 实例级或策略级 CRD `spec`。
3. operator runtime hot config。
4. 启动期 env/flag 默认值。
5. 代码内置默认值。

业务默认配置只在前端/server 创建或更新 CRD 前参与填充，不参与 operator 运行时的隐式覆盖。该业务默认配置 API 不由本 change 交付。

## Impact

- API 面：需要新增 `OperatorRuntimeConfig` API type、CRD、deepcopy、RBAC、scheme 注册和默认 sample。
- Runtime 面：需要新增 provider/watch/cache/atomic snapshot，并实现默认值合并、env 兼容和删除/非法配置处理。
- Controller 面：需要逐步替换 AppBackup、AppRestore、DisasterOperation、DisasterInstance、DataSync/ResourceSync、StorageRepository、Cluster 中的硬编码超时、轮询、requeue 和 watchdog 获取路径。
- 同步面：DataSync/ResourceSync 托管的 AppBackup desired spec 必须包含 cluster、template、timeout；实例级 `operationTimeoutMinutes` 变化需要同步到现有 AppBackup。
- 恢复面：AppRestore transient Kubernetes API 错误不得直接失败；活跃 PVR 不得触发 completed-progress stall auto retry；Velero restart 恢复动作必须避开 startup transient/empty status 和仍在运行的 Velero Backup/Restore/PVB/PVR。
- 校验面：CRD OpenAPI schema 只做结构/类型校验；范围、跨字段关系和兼容迁移等语义错误由 controller 校验并写 `status.conditions=Invalid`。
- 发布面：需要更新部署说明、默认 `OperatorRuntimeConfig` sample、`APPRESTORE_*` 迁移说明和删除 singleton 的回滚说明。
- 需要 server/web 后续配合，把业务默认值配置化后再动态写入 CRD `spec`。
- 需要对 Velero 安装参数单独实现 upgrade/rollout 状态反馈。

## Decisions

- operator runtime config 载体正式选择新的强类型 `OperatorRuntimeConfig` CRD，而不是泛 ConfigMap。
- `OperatorRuntimeConfig` 初版采用 namespaced singleton：namespace 为 `ManagementNamespace()`，name 固定为 `default`。
- `OperatorRuntimeConfig` CRD OpenAPI schema 只拦截结构/类型错误；Kubernetes API server 对这类错误通常返回 422，且不会产生 `OperatorRuntimeConfig.status.conditions=Invalid`。
- controller 级语义错误才进入 `Invalid` condition，并保持最后有效 runtime snapshot；这类错误包括跨字段关系、兼容迁移、schema 不方便表达的语义规则，以及本 change 明确选择不由 OpenAPI 拦截的范围错误。
- 业务默认配置不由本 change 交付；后续 server/web companion change 应提供强类型 API，`add-platform-settings` 只能作为可能的底层存储。
- 已运行中的 controller runtime 判断读取最新有效 runtime snapshot；已创建 Velero Backup/Restore 的 spec 不做隐式改写。
- 删除 `OperatorRuntimeConfig/default` 后，operator 回退到启动默认值和代码默认值，而不是保留最后有效快照。
