# Design: 备份/恢复/容灾动态配置分层

## 1. 配置模型

本设计将配置拆成三类可落地模型。

### 1.1 业务默认配置

业务默认配置是 operator 的外部依赖边界：后续 server/web companion change 应提供配置来源，前端或 server 在创建或更新资源时将最终值写入 CRD `spec`。

示例配置域：

```yaml
backup:
  timeout: 2h
  schedule: "0 2 * * *"
  ttl: 720h
  skipImmediately: false
  defaultVolumesToFsBackup: true
restore:
  timeout: 1h
  itemOperationTimeout: 4h
  restorePVs: true
  existingResourcePolicy: Update
instance:
  operationTimeoutMinutes: 60
  podRestoreMethod: replica
  skipPodReadyCheck: false
operation:
  timeoutMinutes: 60
  retryPolicy:
    maxRetries: 0
    retryIntervalSeconds: 5
  waitUntilReady: false
  skipFinalSync: false
  skipScaleDownSource: false
drill:
  waitUntilReady: false
  skipValidation: false
```

operator 不直接读取这些默认值。operator 只消费已经写入 `AppBackup`、`AppRestore`、`DisasterInstance`、`DisasterOperation`、`DisasterDrill` 等资源的 `spec`。

### 1.2 OperatorRuntimeConfig

operator runtime config 是强类型热加载配置，用于控制 controller 行为。建议新增 namespaced singleton CRD：

- Kind：`OperatorRuntimeConfig`
- Namespace：`ManagementNamespace()`，默认 `disaster-system`
- Name：`default`
- Scope：Namespaced
- Status subresource：启用
- 多实例行为：operator 只读取 singleton `default`；同命名空间其他 `OperatorRuntimeConfig` 对象不得影响运行时行为。

初版 schema：

```yaml
apiVersion: testudo.softcdata.com/v1
kind: OperatorRuntimeConfig
metadata:
  name: default
  namespace: disaster-system
spec:
  backupRuntime:
    inProgressMaxWait: 2h
    unknownMaxWait: 10m
    pollInterval: 5s
  restoreRuntime:
    inProgressMaxWait: 1h
    unknownMaxWait: 1h
    inProgressPollInterval: 5s
    unknownPollInterval: 10s
    progressCompleteGrace: 5m
    startupGrace: 5m
    missingGrace: 90s
    emptyStatusGrace: 5m
    podVolumeRestorePendingMaxWait: 10m
    retryBackoff: 15s
    retryLimit: 1
    retryLimitProgress: 1
    retryLimitStartup: 1
    retryLimitMissing: 2
    retryLimitEmpty: 2
  operationRuntime:
    defaultTimeoutMinutes: 60
    stepStartRequeue: 1s
    stepRunningRequeue: 5s
    defaultRetryInterval: 5s
  instanceRuntime:
    transitionWatchdogTimeout: 2m
    minTransitionWatchdogTimeout: 30s
    initializingRequeue: 10s
    steadyRequeue: 60s
    failedRequeue: 60s
  syncRuntime:
    schedulerUpdateTimeout: 30s
    backupObserveRequeue: 2s
    backupInProgressRequeue: 5s
    historyMissingRequeue: 5s
    restoreObserveRequeue: 10s
    historyRetention: 20
  storageRuntime:
    requeueInterval: 10s
  clusterRuntime:
    reconcileInterval: 1m
    deletionRetryInterval: 10s
    veleroInstallTimeout: 10m
    veleroZombieLockThreshold: 10m
```

#### 字段类型、默认值与范围

所有 duration 字段在 Go API 中使用 `*metav1.Duration`，以区分“未设置”和“显式 0”。所有 count/minutes 字段使用 pointer 数值类型，显式 0 仅在字段表允许时合法。

字段表中的最小值、最大值和字段关系是 controller 语义校验契约，不是 CRD OpenAPI schema 的拦截契约。`OperatorRuntimeConfig` CRD OpenAPI schema 只做结构、字段类型、对象嵌套和 status subresource 等基础校验；不得用 Kubebuilder `Minimum`、`Maximum`、`MinLength` 或等价 CEL 规则拦截下表中的范围错误。这样 API server 能接受形状正确但语义非法的配置对象，operator 才能观测该对象、拒绝激活、写入 `status.conditions=Invalid` 并发出事件。

API server 仍可拒绝 schema 级错误，例如字段类型不匹配、YAML/JSON 不能反序列化、duration 字符串无法解析为 `metav1.Duration`。这类请求通常返回 422，不会持久化对象，也不会产生 `OperatorRuntimeConfig.status.conditions=Invalid`。

| 字段 | 单位/类型 | 默认值 | 最小值 | 最大值 | 备注 |
| --- | --- | --- | --- | --- | --- |
| `backupRuntime.inProgressMaxWait` | duration | `2h` | `1m` | `24h` | 未设置回退旧默认 |
| `backupRuntime.unknownMaxWait` | duration | `10m` | `1m` | `24h` | 未设置回退旧默认 |
| `backupRuntime.pollInterval` | duration | `5s` | `1s` | `5m` | 防止 tight requeue |
| `restoreRuntime.inProgressMaxWait` | duration | `1h` | `1m` | `24h` | `AppRestore.spec.timeout` 优先 |
| `restoreRuntime.unknownMaxWait` | duration | `1h` | `1m` | `24h` | `AppRestore.spec.timeout` 优先 |
| `restoreRuntime.inProgressPollInterval` | duration | `5s` | `1s` | `5m` | 防止 tight requeue |
| `restoreRuntime.unknownPollInterval` | duration | `10s` | `1s` | `5m` | 防止 tight requeue |
| `restoreRuntime.progressCompleteGrace` | duration | `5m` | `30s` | `24h` | 卡顿判定 |
| `restoreRuntime.startupGrace` | duration | `5m` | `30s` | `24h` | 启动卡顿判定 |
| `restoreRuntime.missingGrace` | duration | `90s` | `30s` | `24h` | Restore missing 判定 |
| `restoreRuntime.emptyStatusGrace` | duration | `5m` | `30s` | `24h` | 空 status 判定 |
| `restoreRuntime.podVolumeRestorePendingMaxWait` | duration | `10m` | `1m` | `24h` | PVR pending 保护 |
| `restoreRuntime.retryBackoff` | duration | `15s` | `1s` | `1h` | 自动重试间隔 |
| `restoreRuntime.retryLimit` | integer | `1` | `0` | `10` | 兼容总开关；未设置 per-type limit 时作为 fallback |
| `restoreRuntime.retryLimitProgress` | integer | `1` | `0` | `10` | 0 表示不自动重试 |
| `restoreRuntime.retryLimitStartup` | integer | `1` | `0` | `10` | 0 表示不自动重试 |
| `restoreRuntime.retryLimitMissing` | integer | `2` | `0` | `10` | 0 表示不自动重试 |
| `restoreRuntime.retryLimitEmpty` | integer | `2` | `0` | `10` | 0 表示不自动重试 |
| `operationRuntime.defaultTimeoutMinutes` | minutes integer | `60` | `1` | `1440` | 资源级 timeout 优先 |
| `operationRuntime.stepStartRequeue` | duration | `1s` | `1s` | `5m` | 防止 tight requeue |
| `operationRuntime.stepRunningRequeue` | duration | `5s` | `1s` | `5m` | 防止 tight requeue |
| `operationRuntime.defaultRetryInterval` | duration | `5s` | `1s` | `1h` | `retryPolicy.retryIntervalSeconds` 优先 |
| `instanceRuntime.transitionWatchdogTimeout` | duration | `2m` | `30s` | `24h` | 不得小于 min |
| `instanceRuntime.minTransitionWatchdogTimeout` | duration | `30s` | `10s` | `1h` | watchdog 下限 |
| `instanceRuntime.initializingRequeue` | duration | `10s` | `1s` | `10m` | 初始化轮询 |
| `instanceRuntime.steadyRequeue` | duration | `60s` | `5s` | `30m` | 稳态轮询 |
| `instanceRuntime.failedRequeue` | duration | `60s` | `5s` | `30m` | 失败态轮询 |
| `syncRuntime.schedulerUpdateTimeout` | duration | `30s` | `1s` | `10m` | cron 更新 context |
| `syncRuntime.backupObserveRequeue` | duration | `2s` | `1s` | `5m` | 备份创建观察 |
| `syncRuntime.backupInProgressRequeue` | duration | `5s` | `1s` | `5m` | 备份进行中观察 |
| `syncRuntime.historyMissingRequeue` | duration | `5s` | `1s` | `5m` | history 缺失观察 |
| `syncRuntime.restoreObserveRequeue` | duration | `10s` | `1s` | `5m` | 恢复观察 |
| `syncRuntime.historyRetention` | integer | `20` | `1` | `500` | 历史保留条数 |
| `storageRuntime.requeueInterval` | duration | `10s` | `5s` | `1h` | S3 校验/统计 |
| `clusterRuntime.reconcileInterval` | duration | `1m` | `10s` | `1h` | Cluster 周期检查 |
| `clusterRuntime.deletionRetryInterval` | duration | `10s` | `1s` | `10m` | 删除/卸载重试 |
| `clusterRuntime.veleroInstallTimeout` | duration | `10m` | `1m` | `2h` | 后续 install/upgrade 调用 |
| `clusterRuntime.veleroZombieLockThreshold` | duration | `10m` | `5m` | `24h` | Helm zombie lock 判定 |

Controller 语义校验：

- `instanceRuntime.transitionWatchdogTimeout` 必须大于等于 `instanceRuntime.minTransitionWatchdogTimeout`。
- 所有 requeue/poll interval 不得为 0。
- 所有 max wait/timeout 不得为 0。
- 未设置字段使用兼容默认值；显式设置 0 只有 retry limit 字段合法。
- `restoreRuntime.retryLimit` 是兼容 fallback；当 `retryLimitProgress`、`retryLimitStartup`、`retryLimitMissing`、`retryLimitEmpty` 未显式设置时继承该值，per-type 字段优先。
- 兼容迁移规则必须由 controller 处理，例如旧 env 默认值与新 CRD 字段同时存在时的优先级和可观测性。
- schema 不方便表达的语义规则必须由 controller 处理，例如 duration 字段组合后的实际调度行为是否会造成 tight requeue。

### 1.3 VeleroRuntimeInstallConfig

Velero 运行参数作用在远端集群的 Velero workload 上，不属于 operator 内存热加载。本 change 不定义该配置载体，只把它作为边界说明。后续若要配置这些参数，必须另开 change 定义载体、upgrade/rollout 触发、状态反馈和失败语义。

```yaml
velero:
  image: velero/velero:v1.17.0
  awsPluginImage: velero/velero-plugin-for-aws:v1.13.0
  backupStorageLocation:
    validationFrequency: 15s
  extraArgs:
    itemBlockWorkerCount: 4
    clientQPS: 200
    clientBurst: 400
    backupSyncPeriod: 15s
    itemOperationSyncFrequency: 3s
```

## 2. 热加载机制

### 2.1 配置读取

新增 runtime config provider：

- 启动时构造默认配置。
- 读取 `OperatorRuntimeConfig/default` 并合并非空字段。
- watch `OperatorRuntimeConfig` 变更，更新内存快照。
- controller 每次需要配置时读取快照，不缓存单个字段到长期对象中。
- 如果 singleton 不存在，active snapshot 使用启动默认值和代码默认值。
- 如果 singleton 在运行中被删除，active snapshot 必须回退到启动默认值和代码默认值，并记录 normal event。

### 2.2 错误处理

- 配置缺失：使用 env/flag 兼容默认值和代码默认值。
- 结构非法且被 API server 拒绝：对象不会被持久化，operator 不负责写 status。
- 结构合法但语义非法：拒绝整份配置进入 active snapshot，保持最后一份有效配置。
- 配置状态：对结构合法但语义非法的已持久化对象，在 `status.conditions` 中记录 `Ready` 或 `Invalid`，并写明字段路径。
- 事件：配置从 invalid 恢复为 valid 时发 normal event；invalid 时发 warning event。
- 删除 singleton 与非法配置不同：删除表示用户选择回退默认值，不应保留最后有效自定义快照。

### 2.3 生效边界

- runtime 控制参数下一次 reconcile 或下一次判断立即生效。
- 已创建的 Velero `Backup`/`Restore` spec 不因 runtime config 变化而被隐式 patch。
- 单次操作 `DisasterOperation.spec.timeoutMinutes` 已设置时，不被 runtime 默认值覆盖。
- env `APPRESTORE_*` 仅作为启动兼容默认值；存在 runtime config 字段时 runtime config 优先。

## 3. Controller 接入点

### 3.1 AppBackup

- 将默认 `BackupPhaseInProgressMaxWait`、`BackupPhaseUnknownMaxWait` 纳入 runtime config。
- 将观察/重试 requeue 间隔纳入 runtime config。
- `AppBackup.spec.timeout` 继续最高优先级。

### 3.2 AppRestore

- 将 `RestoreRuntimeConfig` 从启动期 options 扩展为 runtime provider。
- `APPRESTORE_*` env 作为兼容默认值，覆盖 `APPRESTORE_IN_PROGRESS_MAX_WAIT`、`APPRESTORE_UNKNOWN_MAX_WAIT`、`APPRESTORE_PROGRESS_COMPLETE_GRACE`、`APPRESTORE_STARTUP_GRACE`、`APPRESTORE_MISSING_GRACE`、`APPRESTORE_EMPTY_STATUS_GRACE`、`APPRESTORE_PVR_PENDING_MAX_WAIT`、`APPRESTORE_RETRY_LIMIT`、各 per-type retry limit 和 `APPRESTORE_RETRY_BACKOFF`。
- `AppRestore.spec.timeout` 继续最高优先级。
- `ProgressCompleteGrace`、`StartupGrace`、`MissingGrace`、`EmptyStatusGrace`、PVR pending、retry limit、retry backoff 热加载。
- 读取 Velero Restore 时遇到 transient Kubernetes API 错误，应保持 `Restoring` 并按 `retryBackoff` requeue，不应直接进入 Failed。
- completed-progress stall 检测前必须检查活跃 `PodVolumeRestore`；仍有活跃 PVR 时不应自动删除/重建 Restore。
- Velero rollout restart 是 AppRestore 的受保护恢复动作，不是配置驱动的 Velero workload rollout：startup transient/empty status 不触发 restart，仍有运行中 Backup/Restore/PVB/PVR 时跳过 restart 并记录事件。

### 3.3 DisasterOperation

- 全局默认 operation timeout、step requeue、默认 retry interval 热加载。
- `DisasterOperation.spec.timeoutMinutes` 与 `DisasterInstance.spec.operationTimeoutMinutes` 优先。
- 运行中的 step 下一次判断读取最新 runtime config。

### 3.4 DisasterInstance

- transition watchdog、初始化等待、稳态/失败轮询热加载。
- 实例级 operation timeout 仍作为业务语义优先。

### 3.5 DataSync / ResourceSync

- scheduler update timeout、backup/restore observe requeue、history retention 热加载。
- DataSync/ResourceSync 创建或对齐托管 AppBackup 时，必须将 `DisasterInstance.spec.operationTimeoutMinutes` 转换为 `AppBackup.spec.timeout`；未设置或小于等于 0 时保持 AppBackup 内置默认超时 fallback。
- AppBackup desired spec 对齐必须比较 cluster、template、timeout；即使已有 `LastBackupName`，实例级 timeout 变化也应更新托管 AppBackup spec。
- 业务触发 schedule/manual/paused 仍来自资源 spec。

### 3.6 StorageRepository / Cluster

- StorageRepository 校验/统计刷新间隔热加载。
- Cluster reconcile、Velero install timeout、删除/卸载重试间隔、zombie lock threshold 热加载。
- Velero image/extraArgs 不在此处热加载，必须走 upgrade/rollout。

## 4. 与前端动态传参的关系

前端动态传参应基于配置读取，而不是硬编码默认值。该行为由 server/web companion change 承接，不属于本 operator change 的交付范围。

推荐链路：

1. server/web companion change 提供强类型业务默认配置 API。
2. 前端创建备份、恢复、实例、操作、演练时，用默认配置填充表单。
3. 用户确认后，server 将最终值写入 CRD `spec`。
4. operator 按 CRD `spec` 执行。

`add-platform-settings` 的泛 KV 只能作为可能的底层存储；不能替代强类型业务默认配置 API。

operator runtime config 的链路：

1. 管理员在配置页面维护 operator runtime config。
2. server 写入 `OperatorRuntimeConfig/default`。
3. operator watch 到变更并热加载。
4. 下一轮 reconcile 生效。

## 5. 迁移策略

1. 保留现有代码默认值，作为最终 fallback。
2. 保留现有 `APPRESTORE_*` env，作为启动默认值兼容。
3. 新增 runtime config 后，逐步将硬编码字段替换为 provider 读取。
4. 初期只接入高风险超时/重试字段，再扩大到同步、存储、集群检查。
5. 文档标注哪些字段热加载、哪些字段重启加载、哪些字段触发 Velero rollout。

## 6. 风险与隐患

- 热加载不是强实时：从 API server 更新、watch/cache 收敛到 controller 下一次 reconcile 或下一次判断存在延迟。
- requeue/poll interval 配置过小会提高 controller 负载，甚至导致 API server 压力升高；controller 语义校验必须拦截 0 或过小值。
- 删除 `OperatorRuntimeConfig/default` 会回退启动默认值和代码默认值，可能改变正在运行对象后续 timeout、watchdog 或 requeue 判断结果，必须通过事件或日志可观测。
- 非法配置保留最后有效 snapshot 时，如果 status 更新失败，用户可能看不到“配置未生效”；实现应记录 warning 日志/事件并重试 status 更新。
- 一次性接入过多 controller 会扩大回归面；落地应分批推进，优先 AppRestore、AppBackup、DisasterOperation，再扩展到同步、存储和集群检查。
- server/web 业务默认配置仍是 companion change，本 change 归档时不得宣称前端配置读取或 server 强类型默认值 API 已实现。
- Velero workload 参数和 rollout 状态仍是 companion change，本 change 归档时不得宣称 Velero image、extraArgs、node-agent 或 BSL validationFrequency 已具备配置/rollout 能力。
- AppRestore 中已有的 Velero restart 恢复动作和配置驱动的 Velero rollout 是两件事；提案只能承诺前者的安全保护，不得把它解释为 Velero workload 参数 rollout 能力。

## 7. 测试策略

- 单测：
  - 默认配置构造。
  - env 兼容默认值。
  - runtime config 覆盖默认值。
  - 非法配置拒绝激活并保留最后有效快照。
  - 单次 CRD spec 优先级。
  - `APPRESTORE_*` 启动 env 覆盖所有 AppRestore runtime 默认值，并验证 `retryLimit` 到 per-type retry limit 的兼容 fallback。
- controller 单测：
  - AppRestore retry/grace 热加载后下一次判断生效。
  - AppRestore transient Get Restore 错误保持 Restoring 并按 retryBackoff requeue。
  - AppRestore completed-progress stall 遇到活跃 PVR 不触发 auto retry。
  - AppRestore Velero restart 在 startup transient、empty status 或仍有运行中 Velero 操作时跳过并记录事件。
  - AppBackup timeout 默认值热加载。
  - DataSync/ResourceSync 将实例级 `operationTimeoutMinutes` 传递到托管 AppBackup，并在既有 AppBackup 上同步 timeout。
  - DisasterOperation 默认 timeout 与 step requeue 生效。
  - StorageRepository/Cluster requeue 生效。
- envtest：
  - 创建/更新 `OperatorRuntimeConfig` 后 provider snapshot 更新。
  - 删除 runtime config 后回退默认值。
- OpenSpec/harness：
  - `openspec validate add-dynamic-runtime-configuration --strict`
  - `make harness-lint`
  - `make harness-preflight`
