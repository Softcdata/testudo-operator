## ADDED Requirements

### Requirement: 系统必须维护容灾操作目录
系统必须 (MUST) 提供一份以当前代码为准的容灾操作目录，明确每种操作类型当前稳定存在的步骤、归属资源与真相字段。

#### Scenario: 查看故障切换与撤销目录
- **Given** 研发需要梳理 `failover` 与 `undo` 的页面展示字段
- **When** 查阅操作目录
- **Then** 目录必须明确列出 `failover` 与 `undo` 当前稳定步骤集合
- **And** 目录必须指出这些步骤的真相字段来自 `DisasterOperation.status.currentStep` 与 `DisasterOperation.status.steps[]`

#### Scenario: 查看 Drill 目录
- **Given** 研发需要梳理 `drill` 的页面展示字段
- **When** 查阅操作目录
- **Then** 目录必须明确区分 `DisasterDrill.status` 的阶段字段与底层 `DisasterOperation(type=drill)` 的执行步骤

### Requirement: 步骤目录与运行期详情必须分层
系统必须 (MUST) 把“静态目录”和“某一次操作的运行期详情”分开定义，不得把 message 文本当成步骤真相源。

#### Scenario: Server 设计详情接口
- **Given** server 需要为某一次操作输出 detail DTO
- **When** 设计 detail DTO
- **Then** 必须以 `currentStep`、`steps[]`、`autoCancel*`、`groupStatus` 作为运行期真相字段
- **And** 不得通过 `message` 文本推断当前步骤

### Requirement: 提案必须给出 P0-P4 的固定边界
系统必须 (MUST) 在目录与 companion proposal 中明确 P0-P4 的边界，避免把静态目录、detail API、时间线合流混成同一批次。

#### Scenario: 评审分期方案
- **Given** 评审者查看“容灾操作步骤可查看”提案
- **When** 对照 P0-P4
- **Then** 必须能明确看到：
- **And** P0 是静态目录
- **And** P2 是 detail API
- **And** P3 是历史列表点击记录后调用 detail API
- **And** P4 依赖 durable history / 事件覆盖能力
