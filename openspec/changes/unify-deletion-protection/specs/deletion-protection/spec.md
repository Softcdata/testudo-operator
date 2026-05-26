## ADDED Requirements

### Requirement: 新增通用依赖标签协议
The system MUST add a generic dependency-label protocol for all covered resources.

#### Scenario: 资源具备自身依赖 token
- **WHEN** 任一纳入范围的资源被创建或被回填同步
- **THEN** 资源标签中存在 `testudo.softcdata.com/dependency-token`
- **AND** token 值由资源 UID 稳定派生

#### Scenario: 资源写入下游依赖边
- **WHEN** 资源存在下游依赖
- **THEN** 资源自身标签包含 `testudo.softcdata.com/dependency-to-<token>=<relation-code>`
- **AND** 每条标签表示一条独立依赖边

### Requirement: 旧业务标签不可被修改语义
The system MUST keep existing system labels unchanged and only add new generic dependency labels.

#### Scenario: 历史标签保持兼容
- **WHEN** 系统启用通用依赖标签能力
- **THEN** 既有标签键名与语义保持不变
- **AND** 不以替换方式改造 `LabelAppBackupCluster`、`LabelDisasterPolicyName` 等历史标签

### Requirement: 下游标签需在创建与依赖变更时同步
The system MUST write downstream dependency labels on create and resync them on dependency updates.

#### Scenario: 创建时同步下游
- **WHEN** 资源创建成功
- **THEN** 系统写入 `dependency-token` 与当前 `dependency-to-*`

#### Scenario: 更新时覆盖式重建
- **WHEN** 资源依赖字段发生变化
- **THEN** 系统先清理旧 `dependency-to-*` 再写入新集合
- **AND** 不保留过期依赖边

### Requirement: 提供统一删除检查接口返回上下游
The system MUST provide one pre-delete check endpoint that returns `upstream` and `downstream`.

#### Scenario: 检查存在资源
- **WHEN** 调用 `POST /api/v1/deletion/check` 且目标资源存在
- **THEN** 返回 `200 OK`
- **AND** 响应包含 `upstream`、`downstream`、`can_delete`

#### Scenario: 检查不存在资源
- **WHEN** 调用检查接口但目标资源不存在
- **THEN** 返回 `404 Not Found`

### Requirement: can_delete 必须由 upstream 派生
The system MUST derive `can_delete` only from `upstream` emptiness.

#### Scenario: 存在上游引用
- **WHEN** 检查结果 `upstream` 非空
- **THEN** `can_delete` 为 `false`
- **AND** 响应返回完整 `upstream` 列表

#### Scenario: 无上游引用
- **WHEN** 检查结果 `upstream` 为空
- **THEN** `can_delete` 为 `true`

### Requirement: 本阶段删除接口不强制接入检查门禁
The system MUST keep existing delete endpoints behavior unchanged and not enforce check-gate at server side in this phase.

#### Scenario: 删除接口保持兼容
- **WHEN** 调用既有删除接口
- **THEN** 路由与参数保持兼容
- **AND** 不要求先调用检查接口才能执行删除

#### Scenario: 前端自行决策删除
- **WHEN** 前端获取检查结果
- **THEN** 前端可依据 `upstream/downstream/can_delete` 决定是否继续调用删除接口

### Requirement: 需要提供存量资源回填能力
The system MUST provide backfill for existing resources to populate generic dependency labels.

#### Scenario: 存量资源无 token
- **WHEN** 系统发现存量资源缺失 `dependency-token`
- **THEN** 回填流程补齐 token 与 `dependency-to-*`

#### Scenario: 回填期间防漏判
- **WHEN** 查询到缺失依赖标签的目标资源
- **THEN** 系统执行一次即时补写或兜底检查
- **AND** 不因标签缺失直接误判可删除

### Requirement: v1 规则必须覆盖审计范围模块
The system MUST cover all externally deletable modules agreed in the audit baseline.

#### Scenario: 目标模块覆盖
- **WHEN** 加载 v1 删除检查规则
- **THEN** 至少覆盖 `Cluster`、`StorageRepository`、`DisasterPolicy`、`DisasterConfig`、`DisasterInstance`、`DisasterGroup`、`AppBackup`、`AppRestore`、`DisasterDrill`、`DisasterBackup`

#### Scenario: 内部来源模块纳入关系构建
- **WHEN** 计算上下游关系
- **THEN** `DisasterOperation`、`DataSync`、`ResourceSync` 作为内部来源被纳入
- **AND** `DisasterJob` 仅保留在 `DisasterPolicy` 兼容规则中

### Requirement: 规则来源必须标注为模块真实调用依赖矩阵
The system MUST use the real module-call dependency matrix as the single source of truth for rule definition.

#### Scenario: 规则可追溯
- **WHEN** 新增或修改任一模块依赖规则
- **THEN** 规则可追溯到 `docs/platform-resource-dependency-audit.md` 的“模块真实调用依赖矩阵”条目
- **AND** 不使用未在矩阵确认的推测性依赖作为默认规则
