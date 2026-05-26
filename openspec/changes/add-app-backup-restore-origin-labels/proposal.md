# 提案：为 AppBackup/AppRestore 增加来源标签并收敛默认展示与事件推送范围

## 背景
当前 `DisasterInstance` 下游的 `DataSync` / `ResourceSync` 会自动创建 `AppBackup` 和 `AppRestore`。  
这些自动创建的资源与用户手工创建的 V1 资源混在同一视图中，导致：

1. V1 列表无法直接区分“用户任务”与“实例同步任务”。
2. 统计、筛选、审计时需要依赖命名约定（如 `ds-`/`rs-`）或 `OwnerReference`，不稳定且不直观。
3. Server 侧难以提供可靠的默认过滤策略（例如默认只展示用户任务）。
4. `GET /apis/v1/watch/events` 当前只按 `task-event=true` 监听，会把系统同步产生的应用备份/恢复任务一并推送到前端，造成用户视图噪音。

## 目标
1. 为 `AppBackup` 与 `AppRestore` 引入统一的“来源标签”规范。
2. 能够明确区分：
   - 用户创建（User）
   - 容灾实例同步创建（DisasterInstance 下的 DataSync/ResourceSync）
3. 保持向后兼容，不引入 CRD breaking change。
4. `AppBackup` / `AppRestore` 列表默认仅展示用户创建资源，并支持通过参数查询全部资源。
5. `GET /apis/v1/watch/events` 默认不推送系统同步产生的应用备份/恢复事件，以及其下游 Velero 执行过程对应的任务事件。

## 非目标
1. 不修改 `AppBackup` / `AppRestore` 的 Spec/Status 字段结构。
2. 不改变现有 DataSync/ResourceSync 的同步流程语义。
3. 不在本提案中重构 V1/V2 API 边界。
4. 不修改 Velero 原生事件模型，仅处理 disaster 平台结构化任务事件筛选语义。

## 标签设计
新增统一标签（同时用于 `AppBackup` 和 `AppRestore`）：

1. `testudo.softcdata.com/app-resource-origin`
   - `user`: 用户创建或非实例同步来源
   - `disaster-instance`: 由 `DataSync` / `ResourceSync` 管理产生
2. `testudo.softcdata.com/app-resource-owner-kind`
   - `user` / `datasync` / `resourcesync`
3. `testudo.softcdata.com/app-resource-owner-name`
   - 当 owner-kind 为 `datasync` 或 `resourcesync` 时，记录 owner 名称
   - `user` 时可不设置（或清理）

新增结构化任务事件标签（写入 K8s Event）：

1. `testudo.softcdata.com/task-origin`
   - `user`
   - `disaster-instance`
2. `testudo.softcdata.com/task-origin-kind`
   - `user` / `datasync` / `resourcesync`

## 判定规则
以 `OwnerReference`（controller=true）作为来源判定依据：

1. Owner Kind = `DataSync`：
   - origin=`disaster-instance`
   - owner-kind=`datasync`
   - owner-name=`<DataSyncName>`
2. Owner Kind = `ResourceSync`：
   - origin=`disaster-instance`
   - owner-kind=`resourcesync`
   - owner-name=`<ResourceSyncName>`
3. 无 owner 或非上述 owner：
   - origin=`user`
   - owner-kind=`user`
   - 删除 owner-name

事件标签与资源标签采用相同判定结果：
- `task-origin` 对齐 `app-resource-origin`
- `task-origin-kind` 对齐 `app-resource-owner-kind`

## API 行为调整（server）
1. `GET /apis/v1/appbackups` 默认等价于 `origin=user`。
2. `GET /apis/v1/apprestores` 默认等价于 `origin=user`。
3. 两个列表接口支持 `origin` 参数：
   - `user`（默认）：仅用户创建
   - `instance`：仅实例同步创建
   - `all`：全部
4. `GET /apis/v1/watch/events` 默认仅推送 `task-origin=user` 的结构化事件。
5. 对于需要排障的场景，`watch/events` 支持 `origin=all` 显式获取全量结构化事件（包括系统同步任务）。

## 兼容与迁移策略
1. 存量资源回填：由 `AppBackup` / `AppRestore` 控制器在 `syncLabels` 阶段自动补齐来源标签。
2. 新建资源即时标记：`DataSync` / `ResourceSync` 创建子资源时直接写入来源标签，减少可见性延迟。
3. 事件来源标记：Operator 发射结构化任务事件时同步写入 `task-origin` 标签。
4. Server 过滤兼容：
   - 列表迁移窗口内，对未打标旧资源按 `user` 处理，避免误隐藏历史用户数据。
   - 事件迁移窗口内，对缺少 `task-origin` 的历史结构化事件按兼容策略处理（默认可归为 `user` 直到回填完成）。

## 影响范围
- `disaster-operator`
  - `pkg/metadata/labels.go` 新增来源标签常量
  - `internal/controller/appbackup` 标签同步逻辑
  - `internal/controller/apprestore` 标签同步逻辑
  - `internal/controller/datasync` / `internal/controller/resourcesync` 子资源创建逻辑
  - `pkg/helper/event_reporter.go` 支持结构化事件来源标签
  - `AppBackup` / `AppRestore` 事件上报调用链透传来源信息
  - 相关 BDD/单元测试
- `disaster-server`（联动项）
  - V1 备份/恢复列表默认过滤策略与 `origin` 参数
  - `watch/events` 默认过滤系统同步任务事件
  - API 文档补充来源过滤语义
