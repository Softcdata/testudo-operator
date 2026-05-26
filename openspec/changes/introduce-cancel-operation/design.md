# Design: Cancel Operation

## Workflow

### 1. State Transition
DisasterInstance State Machine:
- `FailingOver` --(Cancel)--> `Protected`

### 2. Execution Flow
当接收到 `Cancel` 请求时，Operator 执行以下步骤：

1.  **ScaleDown Target**:
    - 显式读取 `Spec.Config.TargetCluster`。
    - 将 Target Cluster 中相关 Namespace 下的 Deployment/StatefulSet 副本数置为 0。
    - *不* 记录副本数（因为 Target 是失败/中间态）。

2.  **ScaleUp Source**:
    - 显式读取 `Spec.Config.SourceCluster`。
    - 从 ConfigMap (`replicas-dr-rs-<name>`) 读取原始副本数。
    - 将 Source Cluster 中相关 workload 扩容回原始副本数。
    - 等待 Pod Ready。

3.  **Resume Schedules**:
    - 恢复 DataSync 和 ResourceSync 的暂停状态 (`Spec.Paused = false`)。

4.  **Finalize**:
    - 将 Instance 状态更新为 `Protected`。
    - 标记 Operation Completed。

### 3. API Changes
- **DisasterServer**:
    - `POST /groups/:name/actions` 和 `POST /instances/:name/actions`: 支持 `{"operation": "cancel"}`。
- **DisasterOperator**:
    - `DisasterOperation` CRD: Enum `OperationType` 增加 `Cancel`。
    - Controller 逻辑: 新增 `handleCancel`，以及底层的 `doScaleDown(cluster)` 和 `doScaleUp(cluster)` 方法。

## CRD Updates
```go
// pkg/apis/disaster/v1/disasteroperation_types.go

const (
    OperationTypeCancel OperationType = "Cancel"
)

const (
    CancelStepScaleDownTarget StepName = "ScaleDownTarget"
    CancelStepScaleUpSource   StepName = "ScaleUpSource"
    CancelStepResumeSchedules StepName = "ResumeSchedules"
)
```
