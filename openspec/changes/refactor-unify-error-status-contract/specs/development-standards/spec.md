## ADDED Requirements

### Requirement: 控制器必须输出统一错误状态契约（Reason/Message）

所有控制器在失败路径中必须 (MUST) 提供统一错误语义：`reason` 作为机器可读错误码，`message` 作为人类可读错误描述。

#### Scenario: 资源具备 status.reason/status.message 字段时
- **Given** 某资源 Status 定义中包含 `reason` 与 `message`
- **When** Reconcile 进入失败分支
- **Then** 控制器必须 (MUST) 写入 `status.reason`
- **And** 控制器必须 (MUST) 写入 `status.message`
- **And** `status.reason` 与 `status.message` 必须 (MUST) 语义对应同一错误

#### Scenario: 资源使用 Conditions 表达失败时
- **Given** 某资源以 `status.conditions` 表达失败
- **When** 控制器写入失败 Condition
- **Then** `conditions[].reason` 必须 (MUST) 为稳定错误码
- **And** `conditions[].message` 必须 (MUST) 提供可读错误描述
- **And** 若该资源存在顶层错误字段，二者应 (SHOULD) 保持一致或可直接映射

### Requirement: reason 字段必须使用稳定错误码

为了支持 server/web/自动化策略的稳定判断，`reason` 字段必须 (MUST) 采用统一命名规则，不得写入自然语言句子。

#### Scenario: reason 命名规则校验
- **When** 控制器写入任意 `reason`
- **Then** `reason` 必须 (MUST) 使用 PascalCase
- **And** `reason` 必须 (MUST) 仅包含 ASCII 字母和数字
- **And** `reason` 不得 (MUST NOT) 包含空格、中文句子或完整错误堆栈

### Requirement: 成功收敛后必须清理陈旧错误信息

资源从失败态恢复到成功/就绪态时，控制器必须 (MUST) 清理与当前状态不一致的历史错误信息，避免 UI 与 API 误判。

#### Scenario: 失败后恢复成功
- **Given** 资源此前处于失败状态并已写入错误信息
- **When** 资源进入成功或就绪终态
- **Then** 控制器必须 (MUST) 清理陈旧错误字段
- **And** 不得 (MUST NOT) 保留与当前状态冲突的旧错误描述
