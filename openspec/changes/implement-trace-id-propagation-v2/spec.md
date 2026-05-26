# 规范：V2 链路追踪 (Trace ID Propagation V2)

## 概览
本规范定义了 V2 版本的灾难恢复系统中 `trace_id` 的传递标准，确保全链路可观测性。

## Trace ID 格式
*   **Key**: `testudo.softcdata.com/trace-id` (用于 CRD Annotations, Metadata, Headers 等)
*   **Legacy Key**: `testudo.softcdata.com/last-trace-id` (部分资源作为历史记录使用)
*   **Value**: 字符串，推荐 UUID 格式。

## 传播规则
所有的异步操作和资源创建必须携带触发该操作的原始 Trace ID。

### 1. Server -> Operator
*   Server 在创建 `DisasterOperation` 资源时 **必须** 将 HTTP 请求头中的 Trace ID (如有) 或新生成的 Trace ID 写入 `metadata.annotations["testudo.softcdata.com/trace-id"]`。

### 2. Group Operation -> Instance Operation
*   当 `DisasterOperation` (Group) 创建子 `DisasterOperation` (Instance) 时，**必须** 复制 Trace ID annotation。

### 3. Operation -> Sync Resources
*   当 `DisasterOperation` 触发 `DataSync` 或 `ResourceSync` 时（通常设置 `spec.trigger.manual`），**必须** 先更新 Sync 资源的 `metadata.annotations`，写入 `testudo.softcdata.com/last-trace-id`。

### 4. Operation -> External Resources
*   **ConfigMap**: 记录副本数的 ConfigMap **必须** 包含 Trace ID annotation。
*   **Velero**: (如果 V2 涉及 Velero 操作) **必须** 遵循 V1 标准，将 Trace ID 写入 Backup/Restore CR 及 Pod 环境变量（如有）。
