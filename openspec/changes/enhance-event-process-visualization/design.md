# Design: Global Event Timeline & Progress Reporting

## Context
目前用户只能看到任务的“开始”和“结束”两个状态。对于耗时较长的操作（如 AppBackup 涉及 Velero 交互），用户在等待期间处于黑盒状态，无法判断系统是否卡死或正在进行哪个步骤。本设计旨在明确需要上报的“中间步骤”，并定义 Timeline 的数据结构。

## Goals
1.  **全过程可视**: 为核心长耗时任务定义关键粒度的中间状态 (Milestones)。
2.  **Timeline 聚合**: Server 端能够自动将分散的 Events 聚合为连贯的时间轴。
3.  **兼容性**: 保持现有 API 结构兼容，新增字段不破坏旧逻辑。

## 1. 数据模型设计 (Data Model)

### 1.1 Server 端 API 响应 (`TaskEvent`)

在 `TaskEvent` 中新增 `Timeline` 字段：

```go
type EventNode struct {
    Time      time.Time `json:"time"`
    Status    string    `json:"status"` // InProgress, Success, Failed
    Message   string    `json:"message"`
    Reason    string    `json:"reason"` // ExecutionStarted, ExecutionProgress, ExecutionFinished
}

type TaskEvent struct {
    // ... existing fields ...
    Timeline []EventNode `json:"timeline"` 
}
```

### 1.2 Kubernetes Event 约定

复用现有 Event 机制，引入新的 `Reason` 来标识中间进度：

| Reason | 含义 | 状态 (Status) | 示例 Message |
| :--- | :--- | :--- | :--- |
| `ExecutionStarted` | 任务初始化 | `InProgress` | 开始执行应用备份... |
| **`ExecutionProgress`** | **中间进度** | `InProgress` | **已创建 Velero Backup 资源，等待完成...** |
| `ExecutionFinished` | 任务终态 | `Success`/`Failed`| 备份成功，耗时 30s |

## 2. 关键任务中间步骤定义 (Milestones)

我们需要在 Operator 的 Controller 逻辑中插入以下埋点：

### 2.1 应用备份 (AppBackup)

*   **Step 1: 初始化 (Started)**
    *   Msg: "开始处理应用备份请求"
    *   Trigger: Reconcile 收到新请求。
*   **Step 2: 参数检查 (Progress)**
    *   Msg: "正在验证存储仓库与集群连接..."
    *   Trigger: 检查 Storage 和 Cluster CRD 存在性后。
*   **Step 3: Velero 交互 (Progress)**
    *   Msg: "已提交 Velero Backup 请求 (name: backup-x)，等待执行..."
    *   Trigger: 成功 Create `velero.Backup` 资源后。
*   **Step 4: 轮询等待 (Progress - Optional)**
    *   Msg: "Velero Backup 正在进行中 (Phase: InProgress)..."
    *   Trigger: 轮询检查 Velero 状态时（可限制频率，避免刷屏，例如每阶段变化时上报）。
*   **Step 5: 结束 (Finished)**
    *   Msg: "应用备份完成" / "应用备份失败: xxx"

### 2.2 应用恢复 (AppRestore)

*   **Step 1: 初始化 (Started)**
    *   Msg: "开始执行应用恢复"
*   **Step 2: 前置检查 (Progress)**
    *   Msg: "正在检查备份点可用性..."
*   **Step 3: 清理旧资源 (Progress)** (如果配置了)
    *   Msg: "正在清理目标命名空间资源..."
*   **Step 4: Velero 交互 (Progress)**
    *   Msg: "已提交 Velero Restore 请求，等待执行..."
*   **Step 5: 结束 (Finished)**

### 2.3 集群添加 (Cluster Create)

*   **Step 1: 初始化 (Started)**
*   **Step 2: kubeconfig 验证 (Progress)**
    *   Msg: "正在验证集群连接..."
*   **Step 3: 组件安装 (Progress)**
    *   Msg: "正在安装 Velero 及相关组件..."
*   **Step 4: 服务健康检查 (Progress)**
    *   Msg: "正在等待 Velero 服务就绪..."
*   **Step 5: 结束 (Finished)**

## 3. Server 端聚合逻辑 (Aggregation Logic)

`aggregateEvents` 函数逻辑调整：

1.  **Grouping**: 按 `TaskID` (即 Event 的 Label `Task`) 对所有 Events 进行分组。
2.  **Timeline Construction**:
    *   遍历该组下的所有 Event。
    *   为每个 Event 创建一个 `EventNode`。
    *   `Time` = `event.FirstTimestamp` (或者 `FirstTimestamp` 代表开始，`LastTimestamp` 代表最新一次去重，这里统一用 First 为准)。
3.  **Sorting**: 对 `Timeline` 按 `Time` 升序排序。
4.  **State Synthesis**:
    *   `StartTime` = Timeline 中最早的时间。
    *   `EndTime` = Timeline 中 Reason 为 `ExecutionFinished` 的时间。
    *   `Status` = 取 Timeline 中最后一个节点的 Status (或者优先取 Finished 节点状态)。
    *   `Message` = 取 Timeline 中最后一个节点的 Message (反映当前状态)。

## 4. 风险与权衡

*   **Event 数量爆炸**: K8s Event 默认保留 1小时。过多的 Progress 事件可能导致 Etcd 压力或频繁被驱逐。
    *   *Mitigation*: 严格控制 Progress 上报频率。仅在关键状态跃迁（如 Phase 变化）时上报，禁止在 Reconcile 轮询循环中无条件上报（必须判断状态变更）。

## 5. Migration
无数据迁移需求，仅 API 响应格式增强。
