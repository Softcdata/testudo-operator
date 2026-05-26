package apprestore

import (
	"context"
	"fmt"
	"time"

	. "github.com/softcdata/testudo-operator/internal/controller"
	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	"github.com/softcdata/testudo-operator/pkg/helper"
	. "github.com/softcdata/testudo-operator/pkg/metadata"
	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// StateHandler handles a specific phase of the AppRestore lifecycle
type StateHandler interface {
	Handle(ctx context.Context, r *AppRestoreReconciler, appRestore *disasterv1.AppRestore) (disasterv1.AppRestorePhase, ctrl.Result, error)
}

// PendingHandler handles the Pending phase of AppRestore
type PendingHandler struct{}

func (h *PendingHandler) Handle(ctx context.Context, r *AppRestoreReconciler, appRestore *disasterv1.AppRestore) (disasterv1.AppRestorePhase, ctrl.Result, error) {
	logger := logf.FromContext(ctx)

	// 1. Ensure Finalizer
	if !controllerutil.ContainsFinalizer(appRestore, LabelAppRestoreFinalizer) {
		controllerutil.AddFinalizer(appRestore, LabelAppRestoreFinalizer)
		r.Recorder.Event(appRestore, corev1.EventTypeNormal, "FinalizerAdded", "Added finalizer to AppRestore")

		// 创建应用恢复 Started+Finished 事件
		user := appRestore.Annotations["testudo.softcdata.com/user"]
		if user == "" {
			user = "system"
		}
		traceID := appRestore.Annotations[AnnotationTraceID]
		taskName := fmt.Sprintf("创建应用恢复 %s", appRestore.Name)
		now := metav1.Now()
		helper.ReportTaskStartedWithClient(ctx, r.Client, r.Scheme, appRestore, taskName, appRestore.Spec.Cluster, user, traceID, "开始创建应用恢复")
		helper.ReportTaskFinishedWithClient(ctx, r.Client, r.Scheme, appRestore, taskName, appRestore.Spec.Cluster, helper.TaskStatusSuccess, &now, &now, user, traceID, "应用恢复创建完成")

		// Don't requeue immediately. The Update in the main loop will trigger a new reconciliation event
		// once the cache is updated.
		return disasterv1.PhasePending, ctrl.Result{}, nil
	}

	// 2. Validate Cluster
	if appRestore.Spec.Cluster == "" {
		err := fmt.Errorf("cluster is invalid")
		r.Recorder.Event(appRestore, corev1.EventTypeWarning, "ConfigError", err.Error())
		return disasterv1.PhaseFailed, ctrl.Result{}, nil
	}

	// 3. Get KubeClient
	cli, err := r.ClientFactory.GetKubeClient(ctx, r.Client, r.Scheme, appRestore.Spec.Cluster)
	if err != nil {
		logger.Error(err, "error creating kube client")
		r.Recorder.Event(appRestore, corev1.EventTypeWarning, "CreateKubeClientFailed", err.Error())
		return disasterv1.PhasePending, ctrl.Result{}, err
	}

	// 4. BSL Pre-loading (Cross-cluster Restore)
	if appRestore.Spec.SourceCluster != "" && appRestore.Spec.SourceCluster != appRestore.Spec.Cluster {
		if appRestore.Spec.StorageRepository == "" {
			err := fmt.Errorf("StorageRepository is required for cross-cluster restore")
			logger.Error(err, "StorageRepository is missing")
			r.Recorder.Event(appRestore, corev1.EventTypeWarning, "ConfigError", err.Error())
			return disasterv1.PhaseFailed, ctrl.Result{}, nil
		}

		// Get StorageRepository
		sr := &disasterv1.StorageRepository{}
		if err := r.Get(ctx, types.NamespacedName{Name: appRestore.Spec.StorageRepository, Namespace: ManagementNamespace()}, sr); err != nil {
			logger.Error(err, "error getting storage repository")
			r.Recorder.Event(appRestore, corev1.EventTypeWarning, "GetStorageRepositoryFailed", err.Error())
			return disasterv1.PhasePending, ctrl.Result{RequeueAfter: 5 * time.Second}, err
		}

		// Apply BSL to target cluster
		// BSL Name = {StorageRepository}-{SourceCluster}
		// Prefix = {SourceCluster}
		bslName := sr.Name + "-" + appRestore.Spec.SourceCluster
		var defaultBSL DefaultBSL
		err = defaultBSL.ApplyStorageRepository(ctx, r.Client, cli, sr, bslName, appRestore.Spec.SourceCluster)
		if err != nil {
			logger.Error(err, "error applying storage repository")
			r.Recorder.Event(appRestore, corev1.EventTypeWarning, "ApplyStorageRepositoryFailed", err.Error())
			return disasterv1.PhasePending, ctrl.Result{RequeueAfter: 5 * time.Second}, err
		}
		logger.Info("BSL pre-loaded for cross-cluster restore", "bslName", bslName)
	}

	// 5. Get Backup Source Info
	backupSource, err := r.GetBackupSourceInfo(ctx, cli, appRestore)
	if err != nil {
		// Timeout check for Backup Sync
		if apierrors.IsNotFound(err) {
			// Check timeout (e.g., 5 minutes)
			if time.Since(appRestore.CreationTimestamp.Time) > 5*time.Minute {
				logger.Error(err, "timeout waiting for backup to sync")
				r.Recorder.Event(appRestore, corev1.EventTypeWarning, "BackupSyncTimeout", "Timed out waiting for backup to sync")
				return disasterv1.PhaseFailed, ctrl.Result{}, fmt.Errorf("timeout waiting for backup sync")
			}

			logger.Info("Backup not found, waiting for Velero sync...", "backup", appRestore.Spec.Template.BackupName)
			r.Recorder.Event(appRestore, corev1.EventTypeNormal, "WaitingForBackupSync", "Waiting for Velero to sync backup metadata...")
			return disasterv1.PhasePending, ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}

		logger.Error(err, "error getting backup source info")
		r.Recorder.Event(appRestore, corev1.EventTypeWarning, "GetBackupSourceFailed", err.Error())
		return disasterv1.PhaseFailed, ctrl.Result{}, err
	}

	logger.Info("Backup source info retrieved", "backupSource", backupSource.Name)
	if backupSource.Labels != nil && backupSource.Labels[LabelAppBackupIncludeNamespace] != "" {
		appRestore.Labels[LabelAppBackupIncludeNamespace] = backupSource.Labels[LabelAppBackupIncludeNamespace]
	}

	// Update TargetNamespaces in Status
	// Logic: If IncludedNamespaces is set in backupSource, use it.
	// Otherwise (Velero defaults to all namespaces if empty), stick to what we know or what user specified in AppRestore.Spec if applicable?
	// The user request says: "create app backup... write target namespace... query app backup... write to status".
	// The backupSource (Velero Backup) retrieved here corresponds to AppRestore.Spec.Template.BackupName.
	// We should inspect backupSource.Spec.IncludedNamespaces.

	// 1. 确定本地包含的命名空间（来自 Spec 或 AppBackup/Velero Backup）
	localIncluded := appRestore.Spec.Template.IncludedNamespaces
	if len(localIncluded) == 0 {
		// 尝试使用源 Velero Backup 中的命名空间
		if len(backupSource.Spec.IncludedNamespaces) > 0 {
			localIncluded = backupSource.Spec.IncludedNamespaces
		}
	}

	// 2. 确定并应用 ExcludedNamespaces
	// 合并 AppRestore Spec 和 Backup Source 中的排除项
	excludedMap := make(map[string]bool)
	for _, ns := range appRestore.Spec.Template.ExcludedNamespaces {
		excludedMap[ns] = true
	}
	// 注意：Velero 在包含项之上应用排除项。如果备份有排除项，如果我们继承其范围，我们也应该遵守这些排除项。
	for _, ns := range backupSource.Spec.ExcludedNamespaces {
		excludedMap[ns] = true
	}

	// 通过 excludedMap 过滤 localIncluded
	filteredIncluded := make([]string, 0, len(localIncluded))
	for _, ns := range localIncluded {
		if !excludedMap[ns] {
			filteredIncluded = append(filteredIncluded, ns)
		}
	}

	// 3. 应用映射
	mapping := appRestore.Spec.Template.NamespaceMapping
	var finalTargets []string
	if len(mapping) > 0 {
		finalTargets = make([]string, 0, len(filteredIncluded))
		for _, ns := range filteredIncluded {
			if target, ok := mapping[ns]; ok {
				finalTargets = append(finalTargets, target)
			} else {
				finalTargets = append(finalTargets, ns)
			}
		}
	} else {
		finalTargets = filteredIncluded
	}

	appRestore.Status.TargetNamespaces = finalTargets

	// Propagate Backup Type Label (Manual vs Schedule)
	// 用户需求: 用相同的方法，给恢复打上标签(备份源:自动备份，手动备份)，也是从应用备份的标签拿过来
	// Fix: Fetch AppBackup directly as it is the source of truth and guaranteed to have the label (unlike Velero Backup which might be old)
	appBackup := &disasterv1.AppBackup{}
	if err := r.Get(ctx, types.NamespacedName{Name: appRestore.Spec.BackupSource, Namespace: appRestore.Namespace}, appBackup); err == nil {
		if appBackup.Labels != nil && appBackup.Labels[LabelAppBackupType] != "" {
			if appRestore.Labels == nil {
				appRestore.Labels = make(map[string]string)
			}
			appRestore.Labels[LabelAppRestoreSourceType] = appBackup.Labels[LabelAppBackupType]
		}
	} else {
		// Fallback to Velero Backup if AppBackup not found
		if backupSource.Labels != nil && backupSource.Labels[LabelAppBackupType] != "" {
			if appRestore.Labels == nil {
				appRestore.Labels = make(map[string]string)
			}
			appRestore.Labels[LabelAppRestoreSourceType] = backupSource.Labels[LabelAppBackupType]
		}
	}

	// All checks passed, transition to Restoring phase
	return disasterv1.PhaseRestoring, ctrl.Result{}, nil
}

// SucceededHandler handles the Succeeded phase of AppRestore
type SucceededHandler struct{}

func (h *SucceededHandler) Handle(ctx context.Context, r *AppRestoreReconciler, appRestore *disasterv1.AppRestore) (disasterv1.AppRestorePhase, ctrl.Result, error) {
	// 成功状态，无需进一步操作
	return disasterv1.PhaseSucceeded, ctrl.Result{}, nil
}

// FailedHandler handles the Failed phase of AppRestore
type FailedHandler struct{}

func (h *FailedHandler) Handle(ctx context.Context, r *AppRestoreReconciler, appRestore *disasterv1.AppRestore) (disasterv1.AppRestorePhase, ctrl.Result, error) {
	// Failed state: allow retry or cancel, ensure zombie Velero resources are cleaned up
	logger := logf.FromContext(ctx)
	cli, err := r.ClientFactory.GetKubeClient(ctx, r.Client, r.Scheme, appRestore.Spec.Cluster)
	if err != nil {
		logger.Error(err, "error creating kube client")
		r.Recorder.Event(appRestore, corev1.EventTypeWarning, "CreateKubeClientFailed", err.Error())
		return disasterv1.PhasePending, ctrl.Result{}, err
	}

	restore := &velerov1.Restore{}
	restoreExists := false
	// Use utility function to support backward compatibility
	restore, err = r.getVeleroRestore(ctx, cli, appRestore)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			logger.Error(err, "error getting Velero Restore")
			r.Recorder.Event(appRestore, corev1.EventTypeWarning, "GetVeleroRestoreFailed", err.Error())
			return disasterv1.PhaseFailed, ctrl.Result{}, err
		}
		// NotFound is OK - restore is already cleaned up
	} else {
		restoreExists = true
	}

	// Zombie state handling: If AppRestore is Failed but Velero Restore is still running,
	// force terminate it to ensure consistency between AppRestore and Velero states.
	if restoreExists && isVeleroRestoreRunning(restore.Status.Phase) {
		logger.Info("Detected zombie Velero Restore still running while AppRestore is Failed, force terminating",
			"restoreName", restore.Name, "veleroPhase", restore.Status.Phase)
		r.Recorder.Event(appRestore, corev1.EventTypeWarning, "ZombieRestoreDetected",
			"Velero Restore still running while AppRestore is Failed, force terminating")

		if err := r.forceTerminateRestore(ctx, cli, appRestore, restore); err != nil {
			logger.Error(err, "failed to force terminate zombie Velero Restore")
			// Continue anyway - we'll retry on next reconcile
		} else {
			r.Recorder.Event(appRestore, corev1.EventTypeNormal, "ZombieRestoreCleaned",
				"Zombie Velero Restore has been terminated")
		}
	}

	// Process any manual actions (retry, cancel, etc.)
	if phase, res, err := r.processAction(ctx, cli, appRestore, restore); err != nil {
		return phase, res, err
	} else if phase != "" {
		return phase, res, nil
	}
	return disasterv1.PhaseFailed, ctrl.Result{}, nil
}

// isVeleroRestoreRunning checks if the Velero Restore is in a running (non-terminal) state
func isVeleroRestoreRunning(phase velerov1.RestorePhase) bool {
	switch phase {
	case velerov1.RestorePhaseCompleted, velerov1.RestorePhaseFailed, velerov1.RestorePhasePartiallyFailed:
		return false
	case velerov1.RestorePhaseNew, velerov1.RestorePhaseInProgress, velerov1.RestorePhaseWaitingForPluginOperations,
		velerov1.RestorePhaseWaitingForPluginOperationsPartiallyFailed, velerov1.RestorePhaseFinalizing,
		velerov1.RestorePhaseFinalizingPartiallyFailed:
		return true
	default:
		// Unknown phase - treat as potentially running to be safe
		return phase != ""
	}
}

// CancelledHandler handles the Cancelled phase of AppRestore
type CancelledHandler struct{}

func (h *CancelledHandler) Handle(ctx context.Context, r *AppRestoreReconciler, appRestore *disasterv1.AppRestore) (disasterv1.AppRestorePhase, ctrl.Result, error) {
	// 取消状态，无需进一步操作
	logger := logf.FromContext(ctx)
	cli, err := r.ClientFactory.GetKubeClient(ctx, r.Client, r.Scheme, appRestore.Spec.Cluster)
	if err != nil {
		logger.Error(err, "error creating kube client")
		r.Recorder.Event(appRestore, corev1.EventTypeWarning, "CreateKubeClientFailed", err.Error())
		return disasterv1.PhasePending, ctrl.Result{}, err
	}
	restore := &velerov1.Restore{}
	// Use utility function to support backward compatibility
	restore, err = r.getVeleroRestore(ctx, cli, appRestore)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			logger.Error(err, "error getting Velero Restore")
			r.Recorder.Event(appRestore, corev1.EventTypeWarning, "GetVeleroRestoreFailed", err.Error())
			return disasterv1.PhaseFailed, ctrl.Result{}, err
		}
	}
	if phase, res, err := r.processAction(ctx, cli, appRestore, restore); err != nil {
		return phase, res, err
	} else if phase != "" {
		return phase, res, nil
	}
	return disasterv1.PhaseCancelled, ctrl.Result{}, nil
}

type DeletingHandler struct{}

func (h *DeletingHandler) Handle(ctx context.Context, r *AppRestoreReconciler, appRestore *disasterv1.AppRestore) (disasterv1.AppRestorePhase, ctrl.Result, error) {
	logger := logf.FromContext(ctx)

	if controllerutil.ContainsFinalizer(appRestore, LabelAppRestoreFinalizer) {
		r.Recorder.Event(appRestore, corev1.EventTypeNormal, "Deleting", "Starting to delete external resources")

		// 删除应用恢复 Started 事件
		user := appRestore.Annotations["testudo.softcdata.com/user"]
		if user == "" {
			user = "system"
		}
		traceID := appRestore.Annotations[AnnotationTraceID]
		taskName := fmt.Sprintf("删除应用恢复 %s", appRestore.Name)
		helper.ReportTaskStartedWithClient(ctx, r.Client, r.Scheme, appRestore, taskName, appRestore.Spec.Cluster, user, traceID, "开始删除应用恢复")

		// Get KubeClient
		cli, err := r.ClientFactory.GetKubeClient(ctx, r.Client, r.Scheme, appRestore.Spec.Cluster)
		if err != nil {
			if apierrors.IsNotFound(err) {
				logger.Info("Cluster not found, skipping external resource deletion", "cluster", appRestore.Spec.Cluster)
				r.Recorder.Event(appRestore, corev1.EventTypeWarning, "ClusterNotFound", "Cluster not found, skipping external resource deletion")
			} else {
				logger.Error(err, "error creating kube client for deletion")
				return disasterv1.PhaseDeleting, ctrl.Result{}, err
			}
		} else {
			// Execute cleanup logic
			if err := r.deleteExternalResources(ctx, cli, appRestore); err != nil {
				r.Recorder.Event(appRestore, corev1.EventTypeWarning, "DeleteExternalResourcesFailed", err.Error())
				logger.Error(err, "failed to delete external resources")
				return disasterv1.PhaseDeleting, ctrl.Result{}, err
			}
			r.Recorder.Event(appRestore, corev1.EventTypeNormal, "Deleted", "External resources deleted successfully")
		}

		// 删除应用恢复 Finished 事件（必须在移除 Finalizer 之前！）
		now := metav1.Now()
		helper.ReportTaskFinishedWithClient(ctx, r.Client, r.Scheme, appRestore, taskName, appRestore.Spec.Cluster, helper.TaskStatusSuccess, appRestore.DeletionTimestamp, &now, user, traceID, "应用恢复删除完成")

		// Finalizer removal is handled in the main Reconcile loop to ensure thread safety
	}

	// Stop reconciliation
	return disasterv1.PhaseDeleting, ctrl.Result{}, nil
}

type InitiatingHandler struct{}

func (h *InitiatingHandler) Handle(ctx context.Context, r *AppRestoreReconciler, appRestore *disasterv1.AppRestore) (disasterv1.AppRestorePhase, ctrl.Result, error) {
	// Initiating phase is a transient phase to update status
	return disasterv1.PhaseRestoring, ctrl.Result{RequeueAfter: 1 * time.Second}, nil
}
