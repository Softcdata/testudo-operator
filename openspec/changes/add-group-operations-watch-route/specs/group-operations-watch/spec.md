# Capability: 动态单操作与组操作流统一监听支持

## MODIFIED Requirements

### Requirement: disaster-server MUST provide a smart fallback in single operation watch to stream a disaster group's operations

后端服务必须 (MUST) 对外现有的带有路径参数 `:operationName` 的 WebSocket API 路由增加智能探测能力。能够侦测传入的操作名并动态转换为监听全组状态流（采用 List+Watch 防洪泛），若是独立操作则不使用 List+Watch 保持当前状态初始化能力。

#### Scenario: 通过原有单监听路由参数传入容灾组名称获取容灾组下的操作
**Given** 使用合法的 `Upgrade: websocket` 协议请求目标路由 `/watch/groups/operations/:operationName`
**When** 路由参数解析出的值为 `app-group-1` 且后端检测到同名容灾组 (DisasterGroup) 存在时
**Then** K8s watcher 的 `ListOptions` 必须使用完全匹配的标签选择器过滤出包含 `labelSelector: "testudo.softcdata.com/group=app-group-1"` 的 `DisasterOperation` 资源
**And** 使用 List+Watch 获取现存项版本后再启动 Watch
**And** 将流传下来的 `ADDED`、`MODIFIED`、`DELETED` 事件正确推送到 WebSocket

#### Scenario: 通过原有单监听路由监听独立的容灾单次操作
**Given** 请求目标路由为 `/watch/groups/operations/:operationName`
**When** 路由参数对应的操作名不存在同名的容灾组
**Then** 后端必须退化并保持直接对名为参数名对象的单独 `Watch`
**And** 必须触发即时的 `ADDED` 事件以回放现有动作


## ADDED Requirements

### Requirement: cluster-disaster-web MUST provide a dedicated UI-friendly hook for streaming a disaster group's operations

前端必须 (MUST) 要提供一致且清晰的基于统一参数去调用 `watchGroupOperations` Hook 来对接后端重构后的动态推流 API，前端需要直接透传上下文标识用于操作状态更新。

#### Scenario: 建立并动态绑定当前容灾组上下文的操作流
**Given** 导出了或复用的一个名为 `watchGroupOperations(groupName)` 的 Vue 组合式 Hook 或者接口
**When** 用户在代码中实例化并传入特定的上下文字符串
**Then** 它能够正确构建连接串，指向 `ws://{{baseurl}}/apis/disastergroups.testudo.softcdata.com/v1/watch/groups/operations/上下文参数`
**And** 能够解析从新机制 WebSocket 流下来的 `ADDED`、`MODIFIED`、`DELETED` 事件并分发更新。
