---
description: 提议使用结构化 JSON 格式作为 K8s Event 消息内容
---

# 提案：使用结构化 JSON 格式作为 Event 消息

## 1. 背景

目前，`disaster-operator` 使用自定义的括号格式来记录 Kubernetes Event 消息（例如 `[Task: xxx] [Status: Success] Message body`）。虽然这种格式人类可读性较好，但它比较脆弱，难以健壮地解析，且不易扩展。

用户明确要求放弃这种自定义格式，转而采用标准的 JSON 格式，以提高可维护性和解析的可靠性。

## 2. 问题分析

*   **解析脆弱性**：目前的正则解析依赖于特定的分隔符（`] `），如果用户内容中包含类似的字符，解析可能会失败。
*   **扩展性**：添加新的元数据字段需要同时修改发射端（Operator）和解析端（Server），格式会变得越来越混乱。
*   **标准化**：JSON 是结构化日志和数据交换的行业标准。

## 3. 建议方案

我们将更改 Kubernetes Event 的 `Message` 字段，使其存储一个 JSON 字符串。消息中的“人类可读”部分将作为该 JSON 结构中的一个字段存在。

### 3.1 事件载荷结构

定义一个 Go 结构体来表示结构化数据：

```go
type DisasterEventPayload struct {
    Task        string `json:"task"`
    Status      string `json:"status"` // "InProgress", "Success", "Failed"
    Cluster     string `json:"cluster,omitempty"`
    User        string `json:"user,omitempty"`
    TraceID     string `json:"traceId,omitempty"`
    Duration    string `json:"duration,omitempty"` // 用于 Finished 事件
    Message     string `json:"message"` // 实际的人类可读消息
}
```

### 3.2 Kubernetes Event 'Message' 内容

`corev1.Event` 的 `Message` 字段现在将包含上述结构体序列化后的 JSON。

**示例：**
```json
{
  "task": "创建存储 e2e-storage",
  "status": "Success",
  "cluster": "local",
  "user": "admin",
  "traceId": "abc-123",
  "duration": "5s",
  "message": "存储创建完成"
}
```

### 3.3 实施计划

#### 第一阶段：Operator 端 (`disaster-operator`)
1.  修改 `pkg/helper/event_reporter.go`。
2.  创建 `DisasterEventPayload` 结构体。
3.  在 `emitEventWithLabel`（或等效函数）中，不再使用括号拼接字符串，而是填充结构体并将其 Marshal 为 JSON。
4.  设置 `event.Message = string(jsonBytes)`。

#### 第二阶段：Server 端 (`disaster-server`)
1.  修改 `internal/apis/event/v1/list.go`（以及 `types.go`）。
2.  更新 `aggregateEvents` 中的解析逻辑。
3.  不再使用正则解析标签，而是尝试 `json.Unmarshal` 解析 `e.Message`。
4.  **向后兼容**：如果 unmarshal 失败（针对旧事件），则回退到旧的正则标签解析逻辑。这确保了在过渡期间已有的数据不会丢失。

## 4. 优缺点

**优点：**
*   **健壮**：标准库的 JSON 解析器非常稳健。
*   **可扩展**：可以轻松添加像 `ErrorDetails`、`Component` 等字段，而不会破坏解析器。
*   **清晰**：分离了结构化数据和面向用户的文本。

**缺点：**
*   **原始可读性**：在终端直接查看 `kubectl get events` 时，输出将是 JSON 字符串，相比括号格式可能稍显难读。不过，使用标准工具（jq）或我们的 UI 可以解决这个问题。

## 5. 下一步行动

经批准后，我们将重构 operator 中的 `event_reporter.go` 和 server 中的列表逻辑。
