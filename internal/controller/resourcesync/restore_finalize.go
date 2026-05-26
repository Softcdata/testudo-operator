package resourcesync

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	"github.com/softcdata/testudo-operator/pkg/helper"
)

func (r *ResourceSyncReconciler) failRestoreBuild(
	ctx context.Context,
	log logr.Logger,
	resourceSync *disasterv1.ResourceSync,
	clusterPair string,
	backupName string,
	restoreName string,
	buildErr error,
	restoreItems int,
) (ctrl.Result, error) {
	msg := fmt.Sprintf("构建 ResourceSync AppRestore 失败: %v", buildErr)
	log.Error(buildErr, "构建 AppRestore 规格失败", "restore", restoreName)
	r.Recorder.Event(resourceSync, "Warning", "BuildRestoreSpecFailed", msg)

	now := metav1.Now()
	backupItems, startTime := r.lookupRestoreHistorySource(ctx, resourceSync, backupName)
	if err := r.updateResourceSyncStatusWithRetry(ctx, resourceSync, func(latest *disasterv1.ResourceSync) bool {
		latest.Status.State = disasterv1.ResourceSyncStateFailed
		latest.Status.LastSyncTime = &now
		helper.SetStatusError(&latest.Status, resourceSyncReasonBuildRestoreSpecFailed, msg)
		helper.SetConditionError(&latest.Status.Conditions, "BuildRestoreSpecFailed", resourceSyncReasonBuildRestoreSpecFailed, msg)
		appendResourceSyncHistory(latest, backupName, restoreName, backupItems, restoreItems, startTime, now, disasterv1.PhaseFailed)
		return true
	}); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.syncStatistics(ctx, resourceSync); err != nil {
		log.Error(err, "Failed to sync statistics (BuildRestoreSpecFailed)")
	}
	r.reportResourceSyncFinished(ctx, resourceSync, clusterPair, helper.TaskStatusFailed, msg, resourceSyncReasonBuildRestoreSpecFailed)
	return ctrl.Result{}, nil
}

func (r *ResourceSyncReconciler) finalizeRestoreResult(
	ctx context.Context,
	log logr.Logger,
	resourceSync *disasterv1.ResourceSync,
	clusterPair string,
	backupName string,
	restoreName string,
	finalState string,
	restoreStatus disasterv1.AppRestorePhase,
	restoreItems int,
) (ctrl.Result, error) {
	msg := "资源同步成功完成"
	if finalState == disasterv1.ResourceSyncStateFailed {
		msg = fmt.Sprintf("资源恢复失败: %s", restoreName)
	}

	now := metav1.Now()

	backupItems, startTime := r.lookupRestoreHistorySource(ctx, resourceSync, backupName)
	if err := r.updateResourceSyncStatusWithRetry(ctx, resourceSync, func(latest *disasterv1.ResourceSync) bool {
		if finalState == disasterv1.ResourceSyncStateFailed {
			helper.SetStatusError(&latest.Status, resourceSyncReasonRestoreFailed, msg)
			helper.SetConditionError(&latest.Status.Conditions, "RestoreFailed", resourceSyncReasonRestoreFailed, msg)
		} else {
			helper.ClearStatusError(&latest.Status)
		}
		latest.Status.State = finalState
		latest.Status.LastSyncTime = &now
		appendResourceSyncHistory(latest, backupName, restoreName, backupItems, restoreItems, startTime, now, restoreStatus)
		return true
	}); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.syncStatistics(ctx, resourceSync); err != nil {
		log.Error(err, "Failed to sync statistics")
	}

	r.Recorder.Event(resourceSync, "Normal", "SyncCompleted", msg)
	if finalState == disasterv1.ResourceSyncStateReady {
		r.reportResourceSyncFinished(ctx, resourceSync, clusterPair, helper.TaskStatusSuccess, msg)
	} else {
		r.reportResourceSyncFinished(ctx, resourceSync, clusterPair, helper.TaskStatusFailed, msg, resourceSync.Status.Reason)
	}
	return ctrl.Result{}, nil
}

func (r *ResourceSyncReconciler) lookupRestoreHistorySource(
	ctx context.Context,
	resourceSync *disasterv1.ResourceSync,
	backupName string,
) (int, *metav1.Time) {
	appBackup := &disasterv1.AppBackup{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: resourceSync.Namespace, Name: fmt.Sprintf("rs-%s", resourceSync.Name)}, appBackup); err != nil {
		return 0, nil
	}
	return lookupResourceSyncBackupCycle(appBackup, backupName)
}

func appendResourceSyncHistory(
	resourceSync *disasterv1.ResourceSync,
	backupName string,
	restoreName string,
	backupItems int,
	restoreItems int,
	startTime *metav1.Time,
	completion metav1.Time,
	status disasterv1.AppRestorePhase,
) {
	if resourceSync == nil {
		return
	}

	record := disasterv1.SyncHistoryRecord{
		StartTime:            startTime,
		CompletionTime:       &completion,
		BackupName:           backupName,
		RestoreName:          restoreName,
		BackupResourceCount:  backupItems,
		RestoreResourceCount: restoreItems,
		Status:               string(status),
	}
	if startTime != nil {
		record.Duration = completion.Sub(startTime.Time).Round(time.Second).String()
	}

	for i := range resourceSync.Status.History {
		existing := resourceSync.Status.History[i]
		if existing.BackupName == backupName && existing.RestoreName == restoreName {
			resourceSync.Status.History[i] = record
			return
		}
	}

	resourceSync.Status.History = append(resourceSync.Status.History, record)
	if len(resourceSync.Status.History) > 20 {
		resourceSync.Status.History = resourceSync.Status.History[len(resourceSync.Status.History)-20:]
	}
}
