# 提案：建立全局事件规范

## 背景

当前结构化事件系统存在以下问题：

1. **事件辨识度不足**：相似操作的事件难以区分
2. **事件覆盖不完整**：部分用户操作缺少事件记录
3. **命名格式不统一**：各资源使用不同的命名风格
4. **缺乏全局规范**：没有统一的事件目录供参考

## 目标

1. **建立全局事件规范**：覆盖所有资源（包括 V2）的事件标准
2. **完整记录用户操作**：每个人为触发的操作都有"开始→中间过程→结束"的完整事件链
3. **统一命名格式**：使用中文动作+资源名称的自然语言风格
4. **形成事件目录**：记录系统发出的所有全局事件类型

---

## 全局事件规范

### 核心原则

1. **事件必须由人为触发开始**：系统不主动发起操作
2. **事件必须有始有终**：`ExecutionStarted` + `ExecutionFinished`
3. **中间过程可选**：复杂操作可记录中间状态变更
4. **历史可查**：通过 API 可查询所有操作历史

### 删除操作的特殊处理

**问题**：资源被删除后，关联到该资源的 Kubernetes Event 也会被清理，导致事件永远停留在 "进行中"。

**解决方案**：必须在**移除 Finalizer 之前**发射 `ExecutionFinished` 事件。

```
删除流程事件时序：
1. 检测到 DeletionTimestamp → 发射 `删除xxx [Started]`
2. 执行清理工作（删除外部资源等）
3. 清理完成 → 发射 `删除xxx [Finished]`  ← 关键：在这里发射！
4. 移除 Finalizer → 资源被真正删除
```

### Task 名称格式

使用 `<动作><资源类型> <资源名称> [附加信息]` 的中文描述格式。

---

## 全局事件目录

### 1. Cluster（集群）

| 操作 | Task 名称 | 事件序列 | 说明 |
|------|----------|----------|------|
| **创建集群** | `创建集群 {name}` | Started → (中间过程) → Finished | 用户创建 Cluster 资源，包含 Velero 安装等中间过程 |
| **编辑集群** | `编辑集群 {name}` | Started → Finished | 用户修改 Cluster 配置 |
| **删除集群** | `删除集群 {name}` | Started → (中间过程) → Finished | 用户删除 Cluster 资源，可能包含 Velero 卸载 |

**中间过程事件**（通过 extraMessage 记录，不作为独立事件）：
- 安装 Velero
- 卸载 Velero
- 连接验证
- 等等

**完整生命周期事件链**：
```
用户创建 Cluster
  ├─ 创建集群 xxx [Started] extraMsg: "开始创建集群"
  ├─ (中间过程) extraMsg: "正在安装Velero..."  ← 可选的进度更新事件
  └─ 创建集群 xxx [Finished: Success/Failed] extraMsg: "集群创建完成" / "Velero安装失败"

用户编辑 Cluster
  ├─ 编辑集群 xxx [Started] extraMsg: "开始编辑集群"
  └─ 编辑集群 xxx [Finished: Success/Failed] extraMsg: "集群编辑完成"

用户删除 Cluster
  ├─ 删除集群 xxx [Started] extraMsg: "开始删除集群"
  ├─ (中间过程) extraMsg: "正在卸载Velero..."  ← 可选的进度更新事件
  └─ 删除集群 xxx [Finished: Success/Failed] extraMsg: "集群删除完成"
```

---

### 2. StorageRepository（存储）

| 操作 | Task 名称 | 事件序列 | 说明 |
|------|----------|----------|------|
| **创建存储** | `创建存储 {name}` | Started → Finished | 用户创建 StorageRepository 资源 |
| **编辑存储** | `编辑存储 {name}` | Started → Finished | 用户修改 StorageRepository 配置 |
| **删除存储** | `删除存储 {name}` | Started → Finished | 用户删除 StorageRepository 资源 |

**完整生命周期事件链**：
```
用户创建 StorageRepository
  ├─ 创建存储 xxx [Started] extraMsg: "开始创建存储"
  ├─ (S3验证中...)
  └─ 创建存储 xxx [Finished: Success/Failed] extraMsg: "存储创建完成" / "S3验证失败"

用户编辑 StorageRepository
  ├─ 编辑存储 xxx [Started] extraMsg: "开始编辑存储"
  └─ 编辑存储 xxx [Finished: Success/Failed] extraMsg: "存储编辑完成"

用户删除 StorageRepository
  ├─ 删除存储 xxx [Started] extraMsg: "开始删除存储"
  └─ 删除存储 xxx [Finished: Success/Failed] extraMsg: "存储删除完成"
```

---

### 3. AppBackup（应用备份）

| 操作 | Task 名称 | 事件序列 | 说明 |
|------|----------|----------|------|
| **创建应用备份** | `创建应用备份 {name}` | Started → Finished(Success/Failed) | 用户创建 AppBackup 资源 |
| **执行备份** | `应用备份 {name} 执行备份 {backupName}` | Started → Finished(Success/Failed) | 首次自动备份/手动触发备份 |
| **取消备份** | `应用备份 {name} 取消备份 {backupName}` | Started → Finished(Success/Canceled) | 用户取消正在执行的备份 |
| **重试备份** | `应用备份 {name} 重试备份 {backupName}` | Started → Finished(Success/Failed) | 用户重试失败的备份 |
| **删除备份历史** | `应用备份 {name} 删除备份 {backupName}` | Started → Finished(Success/Failed) | 用户删除某个备份记录 |
| **删除应用备份** | `删除应用备份 {name}` | Started → Finished(Success/Failed) | 用户删除 AppBackup 资源 |

**Action 类型对应**：
- `Backup` → 执行备份
- `Cancel` → 取消备份
- `Retry` → 重试备份
- `Delete` → 删除备份历史

**完整生命周期事件链**：
```
用户创建 AppBackup
  ├─ 创建应用备份 my-app [Started]
  ├─ 创建应用备份 my-app [Finished: Success]
  ├─ (首次备份) 应用备份 my-app 执行备份 backup-001 [Started]
  └─ (首次备份) 应用备份 my-app 执行备份 backup-001 [Finished: Success/Failed]

用户手动触发备份 (Action: Backup)
  ├─ 应用备份 my-app 执行备份 backup-002 [Started]
  └─ 应用备份 my-app 执行备份 backup-002 [Finished: Success/Failed]

用户取消备份 (Action: Cancel)
  ├─ 应用备份 my-app 取消备份 backup-002 [Started]
  └─ 应用备份 my-app 取消备份 backup-002 [Finished: Canceled]

用户重试备份 (Action: Retry)
  ├─ 应用备份 my-app 重试备份 backup-002 [Started]
  └─ 应用备份 my-app 重试备份 backup-002 [Finished: Success/Failed]

用户删除备份历史 (Action: Delete)
  ├─ 应用备份 my-app 删除备份 backup-001 [Started]
  └─ 应用备份 my-app 删除备份 backup-001 [Finished: Success]

用户删除 AppBackup
  ├─ 删除应用备份 my-app [Started]
  └─ 删除应用备份 my-app [Finished: Success]
```

---

### 4. AppRestore（应用恢复）

| 操作 | Task 名称 | 事件序列 | 说明 |
|------|----------|----------|------|
| **创建应用恢复** | `创建应用恢复 {name}` | Started → Finished(Success/Failed) | 用户创建 AppRestore 资源（触发恢复） |
| **执行恢复** | `应用恢复 {name} 执行恢复 {restoreName}` | Started → Finished(Success/Failed) | Velero Restore 执行过程 |
| **取消恢复** | `应用恢复 {name} 取消恢复` | Started → Finished(Canceled) | 用户取消正在执行的恢复 |
| **重试恢复** | `应用恢复 {name} 重试恢复` | Started → Finished(Success/Failed) | 用户重试失败的恢复 |
| **删除应用恢复** | `删除应用恢复 {name}` | Started → Finished(Success/Failed) | 用户删除 AppRestore 资源 |

**Action 类型对应**：
- `cancel` → 取消恢复
- `retry` → 重试恢复

**完整生命周期事件链**：
```
用户创建 AppRestore
  ├─ 创建应用恢复 my-restore [Started]
  ├─ 应用恢复 my-restore 执行恢复 app-restore-my-restore [Started]
  ├─ 应用恢复 my-restore 执行恢复 app-restore-my-restore [Finished: Success/Failed]
  └─ 创建应用恢复 my-restore [Finished: Success/Failed]

用户取消恢复 (Action: cancel)
  ├─ 应用恢复 my-restore 取消恢复 [Started]
  └─ 应用恢复 my-restore 取消恢复 [Finished: Canceled]

用户重试恢复 (Action: retry)
  ├─ 应用恢复 my-restore 重试恢复 [Started]
  ├─ 应用恢复 my-restore 执行恢复 app-restore-my-restore [Started]
  ├─ 应用恢复 my-restore 执行恢复 app-restore-my-restore [Finished: Success/Failed]
  └─ 应用恢复 my-restore 重试恢复 [Finished: Success/Failed]

用户删除 AppRestore
  ├─ 删除应用恢复 my-restore [Started]
  └─ 删除应用恢复 my-restore [Finished: Success]
```

---

## V2 资源事件规范（预留）

### 5. DisasterInstance（容灾实例）

| 操作 | Task 名称 | 事件序列 |
|------|----------|----------|
| 创建容灾实例 | `创建容灾实例 {name}` | Started → Finished |
| 启用容灾 | `容灾实例 {name} 启用保护` | Started → Finished |
| 暂停容灾 | `容灾实例 {name} 暂停保护` | Started → Finished |
| 恢复容灾 | `容灾实例 {name} 恢复保护` | Started → Finished |
| 删除容灾实例 | `删除容灾实例 {name}` | Started → Finished |

### 6. DisasterOperation（容灾操作）

| 操作 | Task 名称 | 事件序列 |
|------|----------|----------|
| 执行 Failover | `容灾实例 {instanceName} 执行切换` | Started → (各步骤) → Finished |
| 执行 Failback | `容灾实例 {instanceName} 执行回切` | Started → (各步骤) → Finished |

### 7. DataSync/ResourceSync（同步任务）

| 操作 | Task 名称 | 事件序列 |
|------|----------|----------|
| 数据同步 | `容灾实例 {instanceName} 数据同步` | Started → Finished |
| 资源同步 | `容灾实例 {instanceName} 资源同步` | Started → Finished |

---

## 消息格式规范

```
[Task: <任务描述>] [Status: <状态>] [Duration: <耗时>] [Cluster: <集群>] [User: <用户>] [TraceID: <追踪ID>] <补充说明>
```

### 字段说明

| 字段 | 说明 | 示例 |
|------|------|------|
| Task | 中文动作+资源描述 | `创建集群 prod-cluster` |
| Status | `InProgress` / `Success` / `Failed` / `Canceled` | `Success` |
| Duration | 进行中为 `-`，结束为耗时 | `2m30s` |
| Cluster | 关联集群，全局资源为 `-` | `prod-cluster` |
| User | 触发用户 | `admin` |
| TraceID | 追踪 ID | `trace-xxx` |
| 补充说明 | 可选的额外信息 | `Velero installed successfully` |

---

## 影响范围

### disaster-operator
- `pkg/helper/event_reporter.go` - 无需修改函数签名
- `internal/controller/cluster_controller.go`
- `internal/controller/storagerepository_controller.go`
- `internal/controller/appbackup/*.go`
- `internal/controller/apprestore/*.go`
- (V2) `internal/controller/v2/*.go`

### disaster-server
- 无需修改，Task 字段直接返回

---

## 变更 ID

`add-event-action-field`
