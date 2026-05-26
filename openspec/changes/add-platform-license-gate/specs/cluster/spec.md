## ADDED Requirements

### Requirement: 添加 Cluster 必须受平台授权额度限制
系统必须 (MUST) 在创建 `Cluster` 时检查平台授权额度；无有效 License 时最多允许 2 个未删除的 `Cluster`。

#### Scenario: 免费版允许创建第 1 个和第 2 个 Cluster
- **Given** 当前 License 状态为 `Free`
- **And** 当前创建前未删除 `Cluster` 数量小于 `2`
- **When** 用户创建新的 `Cluster`
- **Then** 系统必须允许创建请求继续

#### Scenario: 免费版拒绝创建第 3 个 Cluster
- **Given** 当前 License 状态为 `Free`
- **And** 当前创建前未删除 `Cluster` 数量已经为 `2`
- **When** 用户创建新的 `Cluster`
- **Then** 系统必须拒绝该创建请求或阻止该 `Cluster` 推进到 `Ready`
- **And** 错误原因必须稳定表达为 `LicenseLimitExceeded`
- **And** 错误消息必须说明免费版最多允许 2 个集群

#### Scenario: 企业版允许创建第 3 个 Cluster
- **Given** 当前 License 状态为 `Active`
- **And** 当前权益 `maxClusters` 表示无限制
- **And** 当前未删除 `Cluster` 数量已经为 `2`
- **When** 用户创建新的 `Cluster`
- **Then** 系统必须允许创建请求继续

### Requirement: Cluster 计数必须包含未删除的非 Ready 对象
系统必须 (MUST) 将所有未删除的 `Cluster` 计入授权额度，不得仅统计 `Ready` 对象。

#### Scenario: NotReady Cluster 计入额度
- **Given** 当前存在 1 个 `Ready` Cluster
- **And** 当前存在 1 个 `NotReady` Cluster
- **And** 当前 License 状态为 `Free`
- **When** 用户创建新的 `Cluster`
- **Then** 系统必须认为当前已占用 2 个 Cluster 额度
- **And** 必须按超限处理新的创建请求

#### Scenario: 正在删除的 Cluster 不计入额度
- **Given** 当前存在 2 个未删除 `Cluster`
- **And** 另有 1 个 `Cluster` 已设置 `deletionTimestamp`
- **And** 当前 License 状态为 `Free`
- **When** 系统计算 Cluster 额度占用
- **Then** 正在删除的 `Cluster` 不得计入当前额度

### Requirement: 被授权接受的 Cluster 必须写入接受态标记
系统必须 (MUST) 为每个被授权接受的 `Cluster` 写入接受态标记，以区分已占用额度的存量对象和新建未接受对象。

#### Scenario: Cluster 被接受前不得执行外部副作用
- **Given** 用户创建了新的 `Cluster`
- **And** 该 `Cluster` 尚未带有 `testudo.softcdata.com/license-accepted=true`
- **When** `ClusterReconciler` 调谐该对象
- **Then** Reconciler 必须先完成授权接受判定
- **And** 若允许接受，必须写入 `testudo.softcdata.com/license-accepted=true`
- **And** 必须写入 `testudo.softcdata.com/license-accepted-at`
- **And** 必须写入 `testudo.softcdata.com/license-id`
- **And** 接受态标记写入成功前不得连接目标集群、安装 Velero 或推进 `Ready`

#### Scenario: 免费版第 2 个直写 Cluster 被正确接受
- **Given** 当前 License 状态为 `Free`
- **And** 当前已有 1 个未删除且已接受的 `Cluster`
- **And** 用户绕过 server 直接创建第 2 个 `Cluster` CR
- **When** `ClusterReconciler` 调谐第 2 个 `Cluster`
- **Then** Reconciler 必须排除当前未接受对象
- **And** Reconciler 必须看到 accepted sibling count 为 `1`
- **And** Reconciler 必须允许第 2 个 `Cluster` 被接受

#### Scenario: 免费版第 3 个直写 Cluster 被拒绝
- **Given** 当前 License 状态为 `Free`
- **And** 当前已有 2 个未删除且已接受的 `Cluster`
- **And** 用户绕过 server 直接创建第 3 个 `Cluster` CR
- **When** `ClusterReconciler` 调谐第 3 个 `Cluster`
- **Then** Reconciler 必须看到 accepted sibling count 为 `2`
- **And** Reconciler 必须按 `LicenseLimitExceeded` 拒绝当前对象

#### Scenario: 接受流程必须串行化
- **Given** 多个未接受 `Cluster` 被并发调谐
- **When** `ClusterReconciler` 执行 accepted sibling count 计算和接受态标记写入
- **Then** 接受流程必须串行化
- **And** 若 `MaxConcurrentReconciles > 1`，必须使用进程内锁或 Kubernetes Lease 保护该临界区
- **And** patch 接受态标记发生冲突时必须重新读取并重试

### Requirement: ClusterReconciler 必须兜底阻止超限新增 Cluster 进入 Ready
即使 server 前置校验或 validating webhook 未启用，`ClusterReconciler` 也必须 (MUST) 阻止超限新增 `Cluster` 执行目标集群连接、Velero 安装或 Ready 推进。

#### Scenario: 直接 kubectl 创建第 3 个 Cluster 时被兜底阻断
- **Given** 当前 License 状态为 `Free`
- **And** 当前未删除 `Cluster` 数量已经为 `2`
- **When** 用户绕过 server 直接创建第 3 个 `Cluster` CR
- **Then** `ClusterReconciler` 必须将该 `Cluster.status.status` 设置为 `NotReady`
- **And** `Cluster.status.reason` 必须为 `LicenseLimitExceeded`
- **And** `Cluster.status.message` 必须说明免费版最多允许 2 个集群
- **And** Reconciler 不得为该超限 `Cluster` 安装 Velero

#### Scenario: ClusterReconciler 不信任状态 ConfigMap
- **Given** 当前不存在有效 License
- **And** `disaster-platform-license-status` ConfigMap 被篡改为 `state=Active`
- **And** 当前未删除 `Cluster` 数量已经为 `2`
- **When** 用户直接创建第 3 个 `Cluster` CR
- **Then** `ClusterReconciler` 必须基于签名 License Secret 和当前部署指纹重新计算权益
- **And** `ClusterReconciler` 不得因 ConfigMap 显示 `Active` 而放行该 `Cluster`
- **And** `ClusterReconciler` 必须按 `LicenseLimitExceeded` 处理

### Requirement: 删除和维护已有 Cluster 不得被授权状态阻断
系统必须 (MUST NOT) 因 License 失效阻止用户删除已有 `Cluster` 或维护已被接受的存量 `Cluster`。

#### Scenario: License 过期后允许删除 Cluster
- **Given** 当前 License 状态为 `Expired`
- **And** 系统中存在 3 个已接受的存量 `Cluster`
- **When** 用户删除任一 `Cluster`
- **Then** 系统必须允许删除流程继续

#### Scenario: License 过期后不降级已接受存量 Cluster
- **Given** 某个 `Cluster` 已在有效 License 下被接受
- **And** 该 `Cluster` 带有 `testudo.softcdata.com/license-accepted=true`
- **And** 当前 License 状态变为 `Expired`
- **When** `ClusterReconciler` 调谐该存量 `Cluster`
- **Then** Reconciler 不得仅因 License 过期将该 Cluster 标记为 `LicenseLimitExceeded`
- **And** Reconciler 不得仅因 License 过期停止已有容灾保护链路

### Requirement: 首次启用 license gate 必须 grandfather 已有 Cluster
系统首次升级到启用 license gate 的版本时，必须 (MUST) 保护升级前已存在的 `Cluster`，避免把未注解存量对象误判为新增超限对象。

#### Scenario: 升级前已有 3 个 Cluster 且未安装 License
- **Given** 系统升级前已经存在 3 个未删除 `Cluster`
- **And** 这些 `Cluster` 尚未带有 `testudo.softcdata.com/license-accepted=true`
- **And** 升级后当前不存在有效 License
- **When** Operator 首次启用 license gate
- **Then** Operator 必须 grandfather 这些升级前已存在的 `Cluster`
- **And** 必须为它们回填 `testudo.softcdata.com/license-accepted=true`
- **And** 必须写入 `testudo.softcdata.com/license-id=grandfathered`
- **And** 必须写入 `testudo.softcdata.com/license-accepted-reason=pre-license-gate-upgrade`
- **And** Operator 不得仅因免费版额度为 2 而降级这些存量 `Cluster`

#### Scenario: 启用后新建 Cluster 不被 grandfather
- **Given** Operator 已记录 license gate `enabledAt`
- **And** 已完成升级前存量 Cluster 回填
- **When** 用户在 `enabledAt` 之后创建新的 `Cluster`
- **Then** 该 `Cluster` 不得被 grandfather
- **And** 该 `Cluster` 必须进入正常 License 额度门禁
