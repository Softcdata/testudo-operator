## ADDED Requirements

### Requirement: 结构化任务事件必须携带来源标签
针对 `AppBackup` 与 `AppRestore` 发射的结构化任务事件，系统必须 (MUST) 写入事件来源标签，供 server 端进行稳定过滤。

#### Scenario: 用户来源任务事件打标
- **Given** 一个用户创建的 `AppBackup` 或 `AppRestore`
- **When** 控制器发射结构化任务事件（`ExecutionStarted` / `ExecutionProgress` / `ExecutionFinished`）
- **Then** 事件标签必须 (MUST) 包含 `testudo.softcdata.com/task-origin=user`
- **And** 事件标签必须 (MUST) 包含 `testudo.softcdata.com/task-origin-kind=user`

#### Scenario: 实例同步来源任务事件打标
- **Given** 一个由 `DataSync` 或 `ResourceSync` 管理的 `AppBackup` 或 `AppRestore`
- **When** 控制器发射结构化任务事件
- **Then** 事件标签必须 (MUST) 包含 `testudo.softcdata.com/task-origin=disaster-instance`
- **And** `DataSync` 场景下 `testudo.softcdata.com/task-origin-kind` 必须 (MUST) 为 `datasync`
- **And** `ResourceSync` 场景下 `testudo.softcdata.com/task-origin-kind` 必须 (MUST) 为 `resourcesync`

### Requirement: Watch 事件流默认仅推送用户任务
`GET /apis/v1/watch/events` 在默认参数下必须 (MUST) 仅推送用户来源的结构化任务事件，系统同步任务事件默认不进入用户视图。

#### Scenario: 默认过滤系统同步任务事件
- **Given** 事件流中同时存在 `task-origin=user` 与 `task-origin=disaster-instance` 的结构化事件
- **When** 客户端调用 `GET /apis/v1/watch/events` 且未传 `origin` 参数
- **Then** 服务端必须 (MUST) 仅推送 `task-origin=user` 的事件

#### Scenario: 显式请求全量事件
- **Given** 客户端需要排障并查看系统同步任务
- **When** 客户端调用 `GET /apis/v1/watch/events?origin=all`
- **Then** 服务端必须 (MUST) 推送 `task-origin=user` 与 `task-origin=disaster-instance` 两类结构化事件
