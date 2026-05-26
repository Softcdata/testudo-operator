# 设计文档：来源标签驱动的列表默认过滤与事件流收敛

## 1. 问题定义
`DataSync` / `ResourceSync` 会自动创建 `AppBackup` / `AppRestore`。  
当前上层接口存在两个直观问题：

1. `AppBackup`、`AppRestore` 列表默认混合展示用户任务和系统同步任务。
2. `GET /apis/v1/watch/events` 仅按 `testudo.softcdata.com/task-event=true` 监听，系统同步任务事件会直接推送到用户端。

结论：需要把“来源信息”显式投影到资源标签与事件标签，并由 server 端形成确定性的默认过滤行为。

## 2. 设计目标
1. 不改 CRD 结构，保持向后兼容。
2. 统一 `AppBackup` 与 `AppRestore` 的来源语义。
3. 列表接口默认只展示用户任务，但支持显式参数获取全部。
4. `watch/events` 默认只推送用户任务事件，避免系统同步噪音。
5. 对调试场景保留显式开关（`origin=all`）。

## 3. 标签模型

### 3.1 资源来源标签（CR）
写入对象：`AppBackup`、`AppRestore`

1. `testudo.softcdata.com/app-resource-origin`
   - `user`
   - `disaster-instance`
2. `testudo.softcdata.com/app-resource-owner-kind`
   - `user`
   - `datasync`
   - `resourcesync`
3. `testudo.softcdata.com/app-resource-owner-name`
   - owner-kind 为 `datasync/resourcesync` 时写入 owner 名称
   - owner-kind 为 `user` 时删除

### 3.2 事件来源标签（K8s Event）
写入对象：结构化任务事件（`LabelTaskEvent=true`）

1. `testudo.softcdata.com/task-origin`
   - `user`
   - `disaster-instance`
2. `testudo.softcdata.com/task-origin-kind`
   - `user`
   - `datasync`
   - `resourcesync`

约束：事件来源标签必须与所属业务 CR 的来源判定结果一致。

## 4. 来源判定算法
判定输入：业务 CR 的 `OwnerReference(controller=true)`。

1. `kind == DataSync`
   - resource-origin=`disaster-instance`
   - owner-kind=`datasync`
   - owner-name=`ownerRef.name`
2. `kind == ResourceSync`
   - resource-origin=`disaster-instance`
   - owner-kind=`resourcesync`
   - owner-name=`ownerRef.name`
3. 其他情况
   - resource-origin=`user`
   - owner-kind=`user`
   - 删除 owner-name

事件来源标签直接映射上述结果：
- `task-origin = app-resource-origin`
- `task-origin-kind = app-resource-owner-kind`

## 5. 写入时机

### 5.1 新建路径
`DataSync` / `ResourceSync` 创建 `AppBackup` / `AppRestore` 时立即写入资源来源标签。  
目标：创建后第一时间可被 server 精确过滤。

### 5.2 回填路径
`AppBackup` / `AppRestore` 控制器每次执行 `syncLabels` 时幂等修正来源标签。  
目标：历史资源与漏标资源自动收敛。

### 5.3 事件路径
`helper.ReportTask*WithClient` 发射结构化事件时追加事件来源标签。  
来源值由事件归属的 `AppBackup` / `AppRestore` 当前标签计算并透传。

## 6. Server 过滤行为

### 6.1 列表接口默认行为
1. `GET /apis/v1/appbackups` 默认 `origin=user`
2. `GET /apis/v1/apprestores` 默认 `origin=user`

参数规范：
- `origin=user`：仅用户任务
- `origin=instance`：仅实例同步任务
- `origin=all`：全部

### 6.2 事件流默认行为
`GET /apis/v1/watch/events` 默认只推送 `task-origin=user` 的结构化事件。  
当 `origin=all` 时，推送全部结构化事件。  
因此系统同步产生的应用备份/恢复事件，以及其下游 Velero 执行阶段对应的任务事件，在默认用户视图中不会出现。

## 7. 风险与缓解
1. 风险：旧资源未打标导致迁移期短暂误判
   - 缓解：控制器回填 + server 默认兼容缺省标签按 `user` 处理
2. 风险：事件未携带来源标签导致 `watch/events` 过滤不稳定
   - 缓解：统一通过 `ReportTask*WithClient` 注入来源标签，禁止分散式手写事件标签
3. 风险：标签更新与现有搜索标签冲突
   - 缓解：来源标签仅增量写入，不覆盖现有 `app-backup-*`/`app-restore-*` 标签

## 8. 测试策略（BDD）
1. AppBackup/AppRestore 来源判定测试
   - DataSync owner
   - ResourceSync owner
   - User owner
2. 存量回填测试
   - 无来源标签资源在 Reconcile 后自动补齐
3. 事件标签测试
   - 用户任务事件包含 `task-origin=user`
   - 系统同步任务事件包含 `task-origin=disaster-instance`
4. Server 行为测试（跨仓）
   - `appbackups` / `apprestores` 默认仅返回 `origin=user`
   - `origin=all` 返回全部
   - `watch/events` 默认不推送 `task-origin=disaster-instance`
