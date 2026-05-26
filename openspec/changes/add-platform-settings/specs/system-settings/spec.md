## ADDED Requirements

### Requirement: 系统必须提供系统设置的 Key-Value 模型

系统必须 (MUST) 使用固定字段的配置项模型来表达系统设置，每条配置项必须 (MUST) 包含：

- `name`
- `config_key`
- `value`
- `remark`

#### Scenario: 配置项字段完整性
- **When** Server 返回任意系统设置配置项
- **Then** 该配置项必须 (MUST) 包含 `name/config_key/value/remark` 四个字段

### Requirement: 系统必须提供系统设置管理接口（CRUD）

系统必须 (MUST) 提供系统设置的增删改查接口，用于系统管理员维护配置项。

#### Scenario: 管理员查询配置项列表
- **Given** 当前用户是 System Admin
- **When** 调用 `GET /api/v1/system-settings`
- **Then** 响应必须 (MUST) 返回配置项列表
- **And** 返回列表必须 (MUST) 按 `config_key` 升序排序（保证稳定性）

#### Scenario: 管理员创建配置项
- **Given** 当前用户是 System Admin
- **When** 调用 `POST /api/v1/system-settings` 创建一个新的配置项
- **Then** Server 必须 (MUST) 将配置项持久化到 etcd（通过 Kubernetes ConfigMap）
- **And** 响应必须 (MUST) 返回创建后的配置项

#### Scenario: 管理员更新配置项
- **Given** 当前用户是 System Admin
- **And** 已存在 `config_key=example.platformName` 的配置项
- **When** 调用 `PUT /api/v1/system-settings/example.platformName` 更新该配置项的 `value`
- **Then** Server 必须 (MUST) 将更新持久化到 etcd
- **And** 响应必须 (MUST) 返回更新后的配置项

#### Scenario: 管理员删除配置项
- **Given** 当前用户是 System Admin
- **And** 已存在某个配置项
- **When** 调用 `DELETE /api/v1/system-settings/{config_key}`
- **Then** Server 必须 (MUST) 删除该配置项

### Requirement: 非管理员不得写入系统设置

系统必须 (MUST) 限制系统设置写入权限，仅允许系统管理员（System Admin）执行创建/更新/删除/上传。

#### Scenario: 非管理员写入被拒绝
- **Given** 当前用户不是 System Admin
- **When** 调用任意写入接口（`POST/PUT/DELETE /api/v1/system-settings` 或 `POST /api/v1/system-settings/assets/{config_key}`）
- **Then** Server 必须 (MUST) 返回 403
- **And** 不得写入任何设置变更

### Requirement: config_key 必须唯一且受约束

系统必须 (MUST) 保证每条配置项的 `config_key` 全局唯一，并对 `config_key` 进行格式与长度约束，以保持可维护性。

#### Scenario: 创建重复 config_key 被拒绝
- **Given** 当前用户是 System Admin
- **And** 已存在 `config_key=example.helpLink` 的配置项
- **When** 再次创建 `config_key=example.helpLink` 的配置项
- **Then** Server 必须 (MUST) 返回 400 或 409（按既有错误码规范统一）
- **And** 不得写入任何设置变更

#### Scenario: 非法 config_key 被拒绝
- **Given** 当前用户是 System Admin
- **When** 创建或更新配置项时提供非法 `config_key`（例如包含空格或超长）
- **Then** Server 必须 (MUST) 返回 400
- **And** 不得写入任何设置变更

### Requirement: 系统必须支持 public 按 keys 读取（用于未登录展示）

系统必须 (MUST) 提供 public 读取接口以支持未登录页面展示（例如平台名称/Logo），并避免提供可枚举全部配置项的接口。

#### Scenario: public 读取指定 keys
- **When** 客户端调用 `GET /api/v1/system-settings/public?keys=example.platformName,example.logo`
- **Then** Server 必须 (MUST) 仅返回被请求的 keys 对应的配置项
- **And** 返回列表必须 (MUST) 按 `config_key` 升序排序（保证稳定性）

### Requirement: 媒体资源必须以 Base64 存储在配置项 value

系统必须 (MUST) 将媒体文件编码为 Base64（建议 data URL 格式）并直接存储在配置项 `value` 中。

#### Scenario: 管理员上传媒体资源成功
- **Given** 当前用户是 System Admin
- **When** 客户端调用 `POST /api/v1/system-settings/assets/{config_key}` 上传一个合法的文件
- **Then** Server 必须 (MUST) 将文件编码为 Base64
- **And** Server 必须 (MUST) 将编码结果写入对应配置项的 `value`

#### Scenario: 编码失败时不得更新配置项
- **Given** 当前用户是 System Admin
- **And** 编码过程失败
- **When** 客户端调用 `POST /api/v1/system-settings/assets/{config_key}`
- **Then** Server 必须 (MUST) 返回明确错误（5xx）
- **And** etcd 中的配置项 `value` 不得被更新（避免出现悬挂引用）

#### Scenario: 上传文件超限被拒绝
- **Given** 当前用户是 System Admin
- **When** 客户端调用 `POST /api/v1/system-settings/assets/{config_key}` 上传超过大小限制的文件
- **Then** Server 必须 (MUST) 返回 400
- **And** etcd 中的配置项 `value` 不得被更新

### Requirement: 系统必须处理并发更新冲突

当多个管理员并发更新系统设置时，系统必须 (MUST) 通过乐观并发控制避免“静默覆盖”。

#### Scenario: 并发写入触发冲突
- **Given** 两个管理员几乎同时更新系统设置
- **When** Server 写入 etcd 时发生冲突（例如 Kubernetes `resourceVersion` 冲突）
- **Then** Server 必须 (MUST) 重试有限次数或返回 409
- **And** 不得在冲突情况下静默覆盖对方更新
