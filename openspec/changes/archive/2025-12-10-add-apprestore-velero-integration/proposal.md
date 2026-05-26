# 变更：添加 AppRestore 和 Velero 集成

## 状态
- 状态: 已完成
- 完成日期: 2025-12-10

## 背景
为了实现 AppRestore 和 Velero Restore 资源之间的一对一管理关系，确保应用恢复操作的准确性和可追溯性，需要对系统进行改进。

## 变更内容
- 引入 `AppRestore` CRD，包含管理 Velero Restore 资源的字段。
- 实现一个控制器，用于管理 `AppRestore` 的生命周期，并与 Velero Restore 同步状态。
- 添加对标签（名称、集群、命名空间、备份源、状态、时间）的支持。
- 支持对正在进行的恢复操作进行取消。
- 增加对错误处理的支持，确保在恢复失败时提供详细日志和通知。
- 采用状态机模式管理 `AppRestore` 的生命周期：
  - 定义 `StateHandler` 接口，处理不同阶段的逻辑。
  - 根据 `AppRestore` 的状态选择对应的处理器（如 `PendingHandler`、`RestoringHandler`、`FailedHandler` 等）。
  - 在每个阶段执行特定逻辑，并返回下一个状态。

## 设计

### 状态机模式设计
`AppRestore` 的生命周期管理将采用状态机模式，参考 `AppBackup` 的实现。以下是详细设计：

#### 1. 定义状态枚举
在 `pkg/apis/apprestore/v1/` 中定义 `AppRestorePhase` 枚举，表示 `AppRestore` 的不同状态：
```go
// AppRestorePhase defines the phase of an AppRestore
// +kubebuilder:validation:Enum=Pending;Restoring;InProgress;Succeeded;Failed;Cancelled
// +kubebuilder:default=Pending
type AppRestorePhase string

const (
	PhasePending   AppRestorePhase = "Pending"
	PhaseRestoring AppRestorePhase = "Restoring"
	PhaseSucceeded AppRestorePhase = "Succeeded"
	PhaseFailed    AppRestorePhase = "Failed"
	PhaseCancelled AppRestorePhase = "Cancelled"
	PhaseDeleting  AppRestorePhase = "Deleting"
	PhaseUnknown   AppRestorePhase = "Unknown"
)
```

#### 2. 定义状态处理器接口
在 `internal/controller/apprestore/apprestore_state.go` 中定义 `StateHandler` 接口：
```go
// StateHandler handles a specific phase of the AppRestore lifecycle
type StateHandler interface {
	Handle(ctx context.Context, r *AppRestoreReconciler, appRestore *disasterv1.AppRestore) (disasterv1.AppRestorePhase, ctrl.Result, error)
}
```

#### 3. 实现具体状态处理器
为每个状态实现对应的处理器，例如 `PendingHandler`、`RestoringHandler` 等：
```go
// PendingHandler handles the Pending phase of AppRestore
type PendingHandler struct {}

func (h *PendingHandler) Handle(ctx context.Context, r *AppRestoreReconciler, appRestore *disasterv1.AppRestore) (disasterv1.AppRestorePhase, ctrl.Result, error) {
	// 检查必要字段是否完整
	// 验证集群和存储库
	// 更新状态为 Restoring
	return disasterv1.PhaseRestoring, ctrl.Result{}, nil
}
```
此外，还实现了 `SucceededHandler`, `FailedHandler`, `CancelledHandler`, `DeletingHandler` 来处理各自的状态逻辑，包括重试、取消和资源清理。

#### 4. 在控制器中集成状态机
在 `internal/controller/apprestore/apprestore_controller.go` 中，根据 `AppRestore` 的当前状态选择对应的处理器：
```go
func (r *AppRestoreReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// ... 获取 AppRestore 实例 ...

	// 根据状态选择处理器
	var handler StateHandler
	switch phase {
	case disasterv1.PhasePending:
		handler = &PendingHandler{}
	case disasterv1.PhaseRestoring:
		handler = &RestoringHandler{}
	case disasterv1.PhaseSucceeded:
		handler = &SucceededHandler{}
	case disasterv1.PhaseFailed:
		handler = &FailedHandler{}
	case disasterv1.PhaseCancelled:
		handler = &CancelledHandler{}
	case disasterv1.PhaseDeleting:
		handler = &DeletingHandler{}
	default:
		// ... 错误处理 ...
	}

	// 执行处理器逻辑
	nextPhase, result, handlerErr := handler.Handle(ctx, r, &appRestore)
	// ... 状态更新和错误处理 ...
}
```
	// 更新状态
	appRestore.Status.Phase = nextPhase
	if updateErr := r.Status().Update(ctx, &appRestore); updateErr != nil {
		return ctrl.Result{}, updateErr
	}

	return result, err
}
```

#### 5. 测试
为每个状态处理器编写单元测试，确保逻辑正确性。

### PendingHandler 逻辑设计

`AppRestore` 的 `PendingHandler` 逻辑将参考 `AppBackup` 的实现，确保在进入恢复阶段前完成必要的检查和配置。具体设计如下：

1. **确保 Finalizer 存在**：
   - 检查 `AppRestore` 是否包含 `AppRestoreFinalizer`，如果没有，则添加 Finalizer 并记录事件。

2. **验证集群**：
   - 检查 `AppRestore.Spec.Cluster` 是否为空。
   - 如果为空，记录配置错误事件，并将状态设置为 `PhaseFailed`。

3. **获取 KubeClient**：
   - 使用 `ClientFactory` 获取目标集群的 KubeClient。
   - 如果获取失败，记录错误并保持状态为 `PhasePending`。

4. **检查存储库**：
   - 获取 `StorageRepository` 对象。
   - 如果存储库不存在，记录事件并将状态设置为 `PhaseFailed`。
   - 如果获取存储库时发生其他错误，记录错误并保持状态为 `PhasePending`。

5. **应用存储库配置**：
   - 调用 `ApplyStorageRepository` 方法，将存储库配置应用到目标集群。
   - 如果应用失败，记录错误并将状态设置为 `PhaseFailed`。

6. **状态更新**：
   - 如果所有检查通过，将状态更新为 `PhaseReady`，并重新排队。

#### 示例代码

以下是 `PendingHandler` 的示例代码：

```go
type PendingHandler struct{}

func (h *PendingHandler) Handle(ctx context.Context, r *AppRestoreReconciler, appRestore *disasterv1.AppRestore) (AppRestorePhase, ctrl.Result, error) {
    logger := logf.FromContext(ctx)

    // 1. Ensure Finalizer
    if !controllerutil.ContainsFinalizer(appRestore, AppRestoreFinalizer) {
        controllerutil.AddFinalizer(appRestore, AppRestoreFinalizer)
        r.Recorder.Event(appRestore, corev1.EventTypeNormal, "FinalizerAdded", "Added finalizer to AppRestore")
    }

    // 2. Validate Cluster
    if appRestore.Spec.Cluster == "" {
        err := fmt.Errorf("cluster is invalid")
        r.Recorder.Event(appRestore, corev1.EventTypeWarning, "ConfigError", err.Error())
        return PhaseFailed, ctrl.Result{}, nil
    }

    // 3. Get KubeClient
    cli, err := r.ClientFactory.GetKubeClient(ctx, r.Client, r.Scheme, appRestore.Spec.Cluster)
    if err != nil {
        logger.Error(err, "error creating kube client")
        r.Recorder.Event(appRestore, corev1.EventTypeWarning, "CreateKubeClientFailed", err.Error())
        return PhasePending, ctrl.Result{}, err
    }

    // 4. Check StorageRepository
    sr := &disasterv1.StorageRepository{}
    err = r.Get(ctx, types.NamespacedName{Name: appRestore.Spec.Template.StorageLocation, Namespace: "disaster-system"}, sr)
    if err != nil {
        if apierrors.IsNotFound(err) {
            logger.Info("storage repository not found")
            r.Recorder.Event(appRestore, corev1.EventTypeWarning, "StorageRepositoryNotFound", "StorageRepository not found")
            return PhaseFailed, ctrl.Result{}, nil
        }
        logger.Error(err, "error getting storage repository")
        return PhasePending, ctrl.Result{}, err
    }

    // 5. Apply StorageRepository
    err = r.ApplyStorageRepository(ctx, cli, sr, appRestore.Spec.Cluster)
    if err != nil {
        logger.Error(err, "error applying storage repository")
        r.Recorder.Event(appRestore, corev1.EventTypeWarning, "ApplyStorageRepositoryFailed", err.Error())
        return PhaseFailed, ctrl.Result{}, nil
    }

    // All checks passed, transition to Restoring phase
    return disasterv1.PhaseRestoring, ctrl.Result{Requeue: true}, nil
}
```

### 标签同步设计

在 `AppRestore` 的生命周期中，标签需要与其状态保持同步，以下是具体设计：

#### 1. 标签字段
`AppRestore` 的标签将包括以下字段：
- `apprestore/name`：唯一标识 `AppRestore` 的名称。
- `apprestore/cluster`：标识 `AppRestore` 所属的集群。
- `apprestore/namespace`：标识 `AppRestore` 所属的命名空间。
- `apprestore/backup-source`：标识 `AppRestore` 恢复的备份来源。
- `apprestore/status`：标识 `AppRestore` 的当前状态。
- `apprestore/last-updated`：记录 `AppRestore` 的最后更新时间。

#### 2. 标签同步逻辑
在每次状态变更时，更新 `AppRestore` 的标签：
- **状态变更**：
  - 当 `AppRestore` 的状态更新时，自动更新 `apprestore/status` 标签。
- **时间更新**：
  - 每次状态变更时，更新 `apprestore/last-updated` 标签为当前时间。

#### 3. 控制器实现
在 `internal/controller/apprestore_controller.go` 中，添加标签同步逻辑：
```go
func (r *AppRestoreReconciler) syncLabels(ctx context.Context, appRestore *disasterv1.AppRestore) error {
	labels := appRestore.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}

	// 更新标签
	labels["apprestore/name"] = appRestore.Name
	labels["apprestore/cluster"] = appRestore.Spec.Cluster
	labels["apprestore/namespace"] = appRestore.Namespace
	labels["apprestore/backup-source"] = appRestore.Spec.BackupSource
	labels["apprestore/status"] = string(appRestore.Status.Phase)
	labels["apprestore/last-updated"] = time.Now().Format(time.RFC3339)

	appRestore.SetLabels(labels)
	return r.Update(ctx, appRestore)
}
```

#### 4. 调用同步逻辑
在状态处理器中调用 `syncLabels` 方法：
```go
nextPhase, result, err := handler.Handle(ctx, r, &appRestore)
if err != nil {
	r.Recorder.Event(&appRestore, corev1.EventTypeWarning, "ReconcileError", err.Error())
	appRestore.Status.Reason = "ReconcileError"
}

// 同步标签
if syncErr := r.syncLabels(ctx, &appRestore); syncErr != nil {
	return ctrl.Result{}, syncErr
}

// 更新状态
appRestore.Status.Phase = nextPhase
if updateErr := r.Status().Update(ctx, &appRestore); updateErr != nil {
	return ctrl.Result{}, updateErr
}

return result, err
```

#### 5. 测试
为标签同步逻辑编写单元测试，确保标签在状态变更时正确更新。

### 标签管理
在 `AppRestore` 的创建和更新过程中，自动生成以下标签：
- 名称：唯一标识 `AppRestore` 的名称。
- 集群：标识 `AppRestore` 的目标集群。
- 命名空间：标识 `AppRestore` 的目标命名空间。
- 备份源：标识 `AppRestore` 恢复的备份来源。
- 状态：标识 `AppRestore` 的当前状态。
- 时间：记录 `AppRestore` 的创建时间和最后更新时间。

### 错误处理
- 在每个状态处理器中，捕获错误并记录详细日志。
- 使用 `r.Recorder.Event` 记录 Kubernetes 事件，提供上下文信息。
- 在状态更新失败时，重试更新操作，确保状态一致性。

### 标签常量设计

参考 `AppBackup` 的标签设计，在 `internal/controller/apprestore_controller.go` 中定义 `AppRestore` 的标签常量：
```go
const (
	AppRestoreFinalizer = "testudo.softcdata.com/finalizer"
	AppRestoreLabel     = "testudo.softcdata.com/app-restore"
	AppRestoreUIDLabel  = "testudo.softcdata.com/app-restore-uid"

	LabelAppRestoreName      = "testudo.softcdata.com/app-restore-name"
	LabelAppRestoreNamespace = "testudo.softcdata.com/app-restore-namespace"
	LabelAppRestoreCluster   = "testudo.softcdata.com/app-restore-cluster"
	LabelAppRestoreStatus    = "testudo.softcdata.com/app-restore-status"
	LabelAppRestoreSource    = "testudo.softcdata.com/app-restore-source"
	LabelAppRestoreUpdated   = "testudo.softcdata.com/app-restore-updated"
)
```

### 标签用途
- **`AppRestoreFinalizer`**：用于确保在删除 `AppRestore` 时完成清理逻辑。
- **`AppRestoreLabel`**：标识 `AppRestore` 资源。
- **`AppRestoreUIDLabel`**：唯一标识 `AppRestore` 的 UID。
- **`LabelAppRestoreName`**：记录 `AppRestore` 的名称。
- **`LabelAppRestoreNamespace`**：记录恢复的目标命名空间（支持多个命名空间，参考 `AppBackup` 的实现）。
- **`LabelAppRestoreCluster`**：记录恢复的目标集群。
- **`LabelAppRestoreStatus`**：记录 `AppRestore` 的当前状态。
- **`LabelAppRestoreSource`**：记录 `AppRestore` 的备份来源。
- **`LabelAppRestoreUpdated`**：记录 `AppRestore` 的最后更新时间。

### 实现步骤
1. 在 `internal/controller/apprestore_controller.go` 中定义上述常量。
2. 在标签同步逻辑中使用这些常量，确保标签的统一性和可维护性。
3. 在单元测试中验证标签的正确性。

### Velero Restore 标签设计

在创建 Velero Restore 时，参考 `CreateVeleroBackup` 的逻辑，为 Restore 资源添加以下标签：

- **`apprestore/name`**：记录 `AppRestore` 的名称。
- **`apprestore/uid`**：记录 `AppRestore` 的唯一标识符（UID）。

#### 实现步骤
1. 在 `internal/controller/apprestore_controller.go` 中，修改 `CreateVeleroRestore` 方法。
2. 在创建 `Velero Restore` 的 `ObjectMeta` 中添加上述标签：

```go
ObjectMeta: metav1.ObjectMeta{
    Name:      restoreName,
    Namespace: "velero",
    Labels: map[string]string{
        "apprestore/name": ar.Name,
        "apprestore/uid":  string(ar.UID),
    },
},
```

3. 确保标签在 `Velero Restore` 创建后正确同步。
4. 编写单元测试验证标签的正确性。

## CRD 字段设计

`AppRestore` 的 CRD 将参考 `AppBackup` 的设计，但需要确保与 Velero Restore 资源的一对一关系。以下是字段设计：

### Spec
- **`BackupSource`** (string, required): 指定恢复操作的备份来源。
- **`Cluster`** (string, required): 指定目标集群的名称。
- **`Template`** (object, required): 包含恢复操作的模板配置。
  - **`StorageLocation`** (string, required): 指定存储库的位置。

### Status
- **`Phase`** (string, optional): 当前恢复操作的状态。
  - 可选值：`Pending`、`Restoring`、`InProgress`、`Succeeded`、`Failed`、`Cancelled`。
- **`Reason`** (string, optional): 描述当前状态的原因。
- **`RestoreStatus`** (object, optional): Velero Restore 的状态。
  - **`Phase`** (string, optional): Velero Restore 的当前阶段。
  - **`Errors`** (array[string], optional): 恢复过程中发生的错误信息。

### 一对一关系设计
- **唯一性保证**：
  - `AppRestore` 的名称将直接映射到 Velero Restore 的名称，确保一对一关系。
  - 在创建 Velero Restore 时，使用 `AppRestore` 的 UID 作为标签，确保唯一性。

- **重试逻辑**：
  - 如果 Velero Restore 存在且状态异常，`RetryRestore` 方法将删除现有的 Velero Restore 并重新创建。
  - 删除操作完成后，立即重新创建 Velero Restore，而无需依赖历史记录。

### 示例 YAML
以下是 `AppRestore` CRD 的示例：
```yaml
apiVersion: testudo.softcdata.com/v1
kind: AppRestore
metadata:
  name: example-restore
  namespace: disaster-system
spec:
  backupSource: example-backup
  cluster: target-cluster
  template:
    storageLocation: example-storage
status:
  phase: Pending
  reason: "Waiting for prerequisites"
  restoreStatus:
    phase: "InProgress"
    errors:
      - "Failed to apply storage configuration"
```