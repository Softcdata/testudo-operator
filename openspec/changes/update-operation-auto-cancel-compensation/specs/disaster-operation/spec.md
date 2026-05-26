## ADDED Requirements

### Requirement: Failover 自动补偿必须建立在失败回滚矩阵之上
系统必须 (MUST) 为 `Failover` 定义正式的失败回滚矩阵，并以此作为自动补偿触发前提。

#### Scenario: 失败步骤存在明确默认终态
- **Given** 一个 `Failover` 操作在某个步骤失败
- **When** operator 评估是否触发自动补偿
- **Then** 系统必须先知道该失败步骤对应的默认终态和是否允许补偿
- **And** 不得在没有矩阵定义的情况下临时猜测补偿行为

#### Scenario: PreCheck 失败走 DirectRollback
- **Given** 一个 `Failover` 在 `PreCheck` 失败
- **When** operator 处理失败分支
- **Then** 系统必须将其归类为 `DirectRollback`
- **And** Instance 必须收敛回 `Protected`
- **And** 不得进入额外的 cancel cleanup 步骤

#### Scenario: ScaleUpTarget 失败走 CancelPath
- **Given** 一个 `Failover` 在 `ScaleUpTarget` 失败
- **When** operator 处理失败分支
- **Then** 系统必须将其归类为 `CancelPath`
- **And** 必须推进补偿步骤直到成功或失败

#### Scenario: SwitchRoles 失败不进入首期自动补偿
- **Given** 一个 `Failover` 在 `SwitchRoles` 或终态写回边界失败
- **When** operator 处理失败分支
- **Then** 系统不得进入首期自动补偿路径
- **And** 必须标识需要人工介入

### Requirement: 首期自动补偿仅覆盖 Failover
系统必须 (MUST) 在首期仅对 `Failover` 操作失败启用自动补偿能力。

#### Scenario: Reprotect 失败时不进入首期自动补偿
- **Given** 一个 `Reprotect` 操作失败
- **When** operator 处理失败分支
- **Then** 系统不得套用本 proposal 定义的首期自动补偿路径

### Requirement: 自动补偿结果必须成为稳定状态契约
系统必须 (MUST) 记录并暴露自动补偿是否触发、是否成功以及失败原因。

#### Scenario: 补偿状态字段被稳定持久化
- **Given** 一个 `Failover` 失败并进入自动补偿
- **When** operator 更新 `DisasterOperation.status`
- **Then** 状态中必须包含 `autoCancelTriggered`
- **And** 必须包含 `autoCancelStatus`
- **And** 必须包含 `autoCancelReason`
- **And** 必须包含 `manualInterventionRequired`

#### Scenario: 自动补偿成功被稳定记录
- **Given** 一个 `Failover` 失败后触发自动补偿
- **And** 自动补偿执行成功
- **When** 用户查询该操作详情
- **Then** 系统必须返回自动补偿已触发且成功的稳定状态信息
- **And** `Operation.status.state` 必须保持 `Failed`
- **And** 不得只依赖 Event 文本表达结果
- **And** Operation 终态必须可区分“正常成功”和“失败后已补偿成功”

#### Scenario: 自动补偿失败被稳定记录
- **Given** 一个 `Failover` 失败后触发自动补偿
- **And** 自动补偿执行失败
- **When** 用户查询该操作详情
- **Then** 系统必须返回自动补偿失败及失败原因
- **And** 系统必须明确提示仍需人工介入

### Requirement: 复用现有 cancel 路径时不得创建新的 Cancel 子 Operation
系统必须 (MUST) 在复用现有 cancel 语义时继续使用原 failover `DisasterOperation` 作为状态载体。

#### Scenario: CancelPath 在同一 Operation 内推进
- **Given** 一个 `Failover` 失败并被矩阵归类为 `CancelPath`
- **When** operator 推进补偿步骤
- **Then** 系统不得创建新的 `DisasterOperation(cancel)` 子资源
- **And** 必须在原 `Failover` Operation 中持久化补偿状态和步骤
