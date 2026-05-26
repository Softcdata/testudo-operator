# Proposal: V2 链路追踪 (Trace ID Propagation for V2)

## 1. 背景与问题 (Background)
V1 版本已经实现了 `trace_id` 的全链路传播（从 CRD 传递到 Velero 资源）。
V2 版本引入了新的 CRD 体系 (`DisasterOperation`, `DisasterInstance`, `DataSync`, `ResourceSync`)，目前这些资源之间的操作尚未传递 `trace_id`，导致操作链路无法串联追踪。

## 2. 目标 (Goals)
在 V2 版本的资源操作链中传递 `testudo.softcdata.com/trace-id`。

传播路径如下：
1.  **Server API -> DisasterOperation**: Server 创建 Operation 时设置 `trace_id`。
2.  **DisasterOperation -> Child Operation**: Group Operation 创建 Sub-Operation 时传递 `trace_id`。
3.  **DisasterOperation -> DataSync / ResourceSync**: Operation 触发 Sync 时 (set `trigger.manual`)，将 `trace_id` 传递给 Sync 任务。
    *   方法：更新 Sync CRD 的 Annotation `testudo.softcdata.com/last-trace-id`。
4.  **DisasterOperation -> ConfigMap**: 记录 Replicas 的 ConfigMap 也应携带 `trace_id` (作为 metadata)。

## 3. 设计方案 (Design)

### 3.1 Trace ID 来源
*   约定 Annotation Key: `testudo.softcdata.com/trace-id`
*   如果在 `DisasterOperation` 上发现此 Annotation，则传播。

### 3.2 传播场景

#### A. Group Operation -> Instance Operation
**场景**: Group 级别的 failover/reprotect 等操作会创建子 `DisasterOperation`。
**行为**: 在 `handleGroupOperation` 中创建子 `DisasterOperation` 时，复制父 Operation 的 `trace_id` 到子 Operation 的 Annotations。

#### B. Operation -> DataSync / ResourceSync
**场景**: Failover/Failback 过程中触发的 `FinalSync` 或 `ResumeSchedule`。
**行为**:
1.  在 `handleSync`, `executeFinalSync` 等触发同步的函数中。
2.  读取 Operation 的 `trace_id`。
3.  在更新 `DataSync`/`ResourceSync` 的 `Spec.Trigger.Manual` 之前，先更新其 Annotation:
    ```go
    if ds.Annotations == nil { ds.Annotations = make(map[string]string) }
    ds.Annotations["testudo.softcdata.com/last-trace-id"] = traceID
    ```

#### C. Operation -> ConfigMap (Failover Record Replicas)
**场景**: Failover 开始时记录源集群副本数。
**行为**:
在 `recordReplicasBeforeScaleDown` 创建/更新 ConfigMap 时，设置 Annotations `testudo.softcdata.com/trace-id`。

## 4. 任务分解 (Tasks)
1.  **Server API**: 确保 `ExecuteAction` (Server) 在创建 `DisasterOperation` 时生成并注入 `trace_id`（如果 Request Header 中有则透传）。
2.  **Controller (Group)**: 修改 `handleGroupOperation`，支持父子传播。
3.  **Controller (Sync)**: 修改 `executeFinalSync`, `handleSync`, `executeResumeSchedules` 等触发点，支持写入 Sync Annotation。
4.  **Controller (ConfigMap)**: 修改 `recordReplicasBeforeScaleDown`，支持 ConfigMap 注入。

## 5. 常量定义
使用已有的常量（如果存在），或定义新的：
```go
const AnnotationTraceID = "testudo.softcdata.com/trace-id"
const AnnotationLastTraceID = "testudo.softcdata.com/last-trace-id"
```
