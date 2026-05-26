## MODIFIED Requirements

### Requirement: 避免重复发射事件 (Event Debouncing)
所有控制器在发射结构化事件时，必须 (MUST) 区分"首次状态变更"和"定期检查"，避免重复发射相同状态的事件。

#### Scenario: 定期检查不发射事件
- **Given** 资源已处于 Ready 状态
- **When** 定期健康检查成功完成
- **And** 资源状态依然为 Ready
- **Then** 控制器不得发射任何新的结构化事件

#### Scenario: 状态恢复发射事件
- **Given** 资源之前处于 Ready 状态
- **When** 资源短暂离线后重新连接成功
- **And** 资源状态从 NotReady → Ready 变化
- **Then** 控制器应发射一个"恢复"或"重新就绪"事件

#### Scenario: 使用 wasReady 变量防抖
- **Given** 控制器实现事件发射逻辑
- **When** 进入 Reconcile 循环时
- **Then** 必须在入口处记录初始状态（如 `wasReady := status == Ready`）
- **And** 仅当状态真正变化时（`!wasReady && newStatus == Ready`）才发射事件
- **And** 定期检查成功时（`wasReady == true`）不发射事件
