## ADDED Requirements
### Requirement: 资源生命周期审计
AppRestore 控制器必须 (MUST) 上报资源的生命周期事件。

#### Scenario: 资源创建事件
- **Given** 一个新创建的 AppRestore 资源
- **When** 控制器首次处理该资源
- **Then** 控制器必须 (MUST) 发出一个 `Created` 类型的结构化事件
- **And** 任务名称格式应为 `Resource: AppRestore <name> Created`

#### Scenario: 资源删除事件
- **Given** 一个正在被删除的 AppRestore 资源
- **When** 控制器开始执行删除逻辑
- **Then** 控制器必须 (MUST) 发出一个 `Deleted` 类型的结构化事件
- **And** 任务名称格式应为 `Resource: AppRestore <name> Deleted`
