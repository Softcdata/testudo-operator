## MODIFIED Requirements
### Requirement: 控制器实现
控制器逻辑必须 (MUST) 健壮、可靠且易于监控。

#### Scenario: 编写 Reconcile 逻辑
- **Given** 一个新的控制器被创建
- **When** 实现 `Reconcile` 方法
- **Then** 该方法必须是**幂等**的（多次执行产生相同结果）
- **And** 必须包含结构化日志记录（使用 `logr`）
- **And** 日志消息必须 (MUST) 使用 **英文** 编写，禁止使用中文
- **And** 如果资源包含 TraceID 注解 (`testudo.softcdata.com/trace-id`)，必须将其注入到 Logger 上下文中
- **And** 应当记录关键指标（Metrics）以确保**可观测性**
- **And** 应当正确处理 Kubernetes 事件（Events），在关键操作（如创建资源、状态变更、错误发生）时记录 Event 以提供上下文
