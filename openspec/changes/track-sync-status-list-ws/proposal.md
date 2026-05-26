---
title: 在容灾列表和 WS 中追踪同步状态
status: Proposed
author: Antigravity
created: 2026-02-02
---

# 在容灾列表和 WS 中追踪同步状态 (Track Sync Status in List and WS)

## 1. 背景 (Background)

当前 `DisasterInstance` 的列表接口 (`GET /instances`) 仅包含实例本身的状态（如 FSM State, cluster roles）。
用户在查看容灾列表时，无法直观看到 **数据同步 (DataSync)** 和 **资源同步 (ResourceSync)** 的实时状态（如 `InProgress`, `Failed`, `Ready`），必须点击进入详情页才能查看。

此外，现有的 WebSocket (`/watch/instances`) 仅监听 `DisasterInstance` 资源的变更。当同步任务（DataSync/ResourceSync）状态发生变化时（例如开始同步、同步完成），列表页无法通过 WS 收到实时更新，导致 UI 状态滞后。

## 2. 目标 (Goals)

1.  **列表接口增强**：`GET /disasterinstances/v1/instances` 返回的 DTO 中，应包含关联的 `DataSync` 和 `ResourceSync` 的简要状态（State, LastSyncTime, LastError）。
2.  **WS 监听增强**：WebSocket 监听服务不仅要监听 `DisasterInstance` 的变更，还需监听 `DataSync` 和 `ResourceSync` 的变更，并将这些变更聚合为实例状态更新推送到前端。

## 3. 设计方案 (Design)

### 3.1 API DTO 变更

修改 `DisasterInstanceDTO`，增加同步状态概要字段：

```go
type DisasterInstanceDTO struct {
    // ... existing fields ...
    
    // 新增字段
    DataSyncStatus     *SyncSummaryDTO `json:"dataSyncStatus,omitempty"`
    ResourceSyncStatus *SyncSummaryDTO `json:"resourceSyncStatus,omitempty"`
}

type SyncSummaryDTO struct {
    State            string       `json:"state"`            // Ready, InProgress, etc.
    LastSyncTime     *metav1.Time `json:"lastSyncTime,omitempty"`
    SyncSuccessCount int          `json:"syncSuccessCount"` // Optional: for dashboard
    SyncFailureCount int          `json:"syncFailureCount"` // Optional
}
```

### 3.2 列表接口实现 (`listInstances`)

为了避免 N+1 查询问题，优化列表查询逻辑：

1.  查询 `DisasterInstanceList`。
2.  查询所有的 `DataSyncList` 和 `ResourceSyncList` (在相同 namespace 下，或根据 label selector)。
    -   优化：利用 `ListOptions` 批量获取。
3.  在内存中进行 Map 关联 (By OwnerReference or Naming Convention `dr-ds-<instance>`).
4.  将匹配到的 Sync 对象状态填充到 `DisasterInstanceDTO` 中。

### 3.3 WebSocket 实现 (`watchInstances`)

当前的 WS Handler 需要扩展监听范围：

1.  **Multi-Resource Watcher**:
    -   建立对 `DisasterInstance` 的 Informer/Watcher。
    -   建立对 `DataSync` 和 `ResourceSync` 的 Informer/Watcher。
2.  **事件聚合 (Event Aggregation)**:
    -   当 `DisasterInstance` 变更 -> 直接推送 Instance DTO。
    -   当 `DataSync` 或 `ResourceSync` 变更 -> 
        -   解析其所属的 `DisasterInstance` (通过 `labels["testudo.softcdata.com/instance"]` 或 naming convention)。
        -   构造一个“合成”的 Instance 更新事件 (或者仅推送 Sync 状态变更事件，取决于前端架构。建议推送完整的 Instance DTO 以保持一致性，或推送 Partial Update)。
        -   **推荐方案**：当 Sync 变更时，查找对应的 Instance (可能Cached)，组装包含最新 SyncStatus 的 InstanceDTO 推送给客户端。

### 3.4 前端/API 协议

WS 消息结构保持不变（通常是 Object 变更事件）。
当 DataSync 状态变为 `InProgress` 时，前端会收到一个 `DisasterInstance` 对象的 UPDATE 事件，其中 `dataSyncStatus.state` 字段为 `InProgress`。

## 4. 实施步骤 (Implementation Steps)

1.  **Server**: 修改 `DisasterInstanceDTO` 定义。
2.  **Server**: 重构 `listInstances` Handler，增加批量查询和聚合 Sync 状态的逻辑。
3.  **Server**: 重构 `Watcher` 模块。
    -   引入 `DynamicInformers` 或针对 CRD 的多路 Watcher。
    -   实现 Sync 对象变更反查 Instance 的逻辑。
4.  **Verification**: 验证列表页是否显示同步状态，并验证触发同步时列表页是否通过 WS 自动刷新。

