package apprestore

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	. "github.com/softcdata/testudo-operator/internal/controller"
	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	"github.com/softcdata/testudo-operator/pkg/helper"
	. "github.com/softcdata/testudo-operator/pkg/metadata"
	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	labelAppRestoreAutoRetryCount = "testudo.softcdata.com/app-restore-auto-retry-count"

	labelAppRestoreRetryProgress        = "testudo.softcdata.com/app-restore-retry-progress"
	labelAppRestoreRetryStartup         = "testudo.softcdata.com/app-restore-retry-startup"
	labelAppRestoreRetryMissing         = "testudo.softcdata.com/app-restore-retry-missing"
	labelAppRestoreRetryEmpty           = "testudo.softcdata.com/app-restore-retry-empty"
	annotationAppRestoreLastProgressAt  = "testudo.softcdata.com/app-restore-last-progress-at"
	annotationAppRestoreLastObservedSig = "testudo.softcdata.com/app-restore-last-observed-signature"
	annotationAppRestoreMissingSince    = "testudo.softcdata.com/app-restore-missing-since"
	restoreStallTypeProgressCompleted   = "progress_completed"
	restoreStallTypeStartupStalled      = "startup_stalled"
	restoreStallTypeStartupTransient    = "startup_transient"
	restoreStallTypeMissingRestore      = "missing_restore"
	restoreStallTypeEmptyStatus         = "empty_status"
	veleroDeploymentDefaultName         = "velero"
	veleroRestartAtAnnotationKey        = "kubectl.kubernetes.io/restartedAt"
)

var veleroRestartCooldown = 2 * time.Minute

// RestoringHandler handles the Restoring phase of AppRestore
type RestoringHandler struct{}

func (h *RestoringHandler) Handle(ctx context.Context, r *AppRestoreReconciler, appRestore *disasterv1.AppRestore) (disasterv1.AppRestorePhase, ctrl.Result, error) {
	logger := logf.FromContext(ctx)
	cfg := r.restoreRuntimeConfig()
	// Get KubeClient for the target cluster
	cli, err := r.ClientFactory.GetKubeClient(ctx, r.Client, r.Scheme, appRestore.Spec.Cluster)
	if err != nil {
		logger.Error(err, "error creating kube client")
		r.Recorder.Event(appRestore, corev1.EventTypeWarning, "CreateKubeClientFailed", err.Error())
		return disasterv1.PhasePending, ctrl.Result{}, err
	}

	// Use utility function to support backward compatibility
	restore, err := r.getVeleroRestore(ctx, cli, appRestore)
	var restoreName string
	if restore != nil {
		restoreName = restore.Name
	} else {
		restoreName = r.GenRestoreName(appRestore)
	}

	if apierrors.IsNotFound(err) {
		return r.handleRestoreNotFound(ctx, cli, appRestore, restoreName, cfg)
	} else if err != nil {
		r.Recorder.Event(appRestore, corev1.EventTypeWarning, "CreateVeleroRestore", "Get Velero Restore failed")
		logger.Error(err, "error getting Velero Restore")
		if isTransientKubeAPIError(err) {
			return disasterv1.PhaseRestoring, ctrl.Result{RequeueAfter: cfg.RetryBackoff}, nil
		}
		return disasterv1.PhaseFailed, ctrl.Result{}, err
	}
	// Restore exists again, clear missing marker.
	clearRestoreMissingSince(appRestore)
	//检查actio是否更新，若更新，执行action动作
	if phase, res, err := r.processAction(ctx, cli, appRestore, restore); err != nil {
		return phase, res, err
	} else if phase != "" {
		return phase, res, nil
	}

	// 如果存在，继续走下面的逻辑
	logger.Info("Velero Restore already exists, checking status")
	r.Recorder.Event(appRestore, corev1.EventTypeNormal, "CreateVeleroRestore", "Velero Restore already exists")

	oldPhase := appRestore.Status.RestoreStatus.Phase

	// 恢复进度事件：进入 InProgress
	if restore.Status.Phase == velerov1.RestorePhaseInProgress && oldPhase != velerov1.RestorePhaseInProgress {
		user := appRestore.Annotations[AnnotationUser]
		if user == "" {
			user = "system"
		}
		triggeredBy := appRestore.Annotations[AnnotationTraceID]
		taskName := fmt.Sprintf("应用恢复 %s 执行恢复 %s", appRestore.Name, restoreName)
		helper.ReportTaskProgressWithClient(ctx, r.Client, r.Scheme, appRestore, taskName, appRestore.Spec.Cluster, user, triggeredBy, "Velero 恢复正在执行中 (InProgress)...")
	}

	// 只要 Restore 处于运行态，就执行 PVR 异常探测（最佳努力）。
	if isVeleroRestoreRunning(restore.Status.Phase) {
		timeout := resolveRestoreInProgressTimeout(appRestore, cfg)
		reason, msg, detectErr := r.detectPodVolumeRestoreIssue(ctx, cli, restoreName, timeout, cfg.PodVolumeRestorePendingMaxWait)
		if detectErr != nil {
			// PVR 状态检查异常按最佳努力处理，不中断主状态机。
			logger.Error(detectErr, "failed to inspect PodVolumeRestore status", "restore", restoreName)
		} else if reason != "" {
			r.Recorder.Event(appRestore, corev1.EventTypeWarning, reason, msg)
			logger.Info("Detected PodVolumeRestore issue, failing AppRestore", "restore", restoreName, "reason", reason, "message", msg)

			if err := r.forceTerminateRestore(ctx, cli, appRestore, restore); err != nil {
				logger.Error(err, "failed to force terminate restore after PodVolumeRestore issue", "restore", restoreName)
			}
			if err := r.cleanupPendingRestoredResources(ctx, cli, appRestore, restoreName); err != nil {
				logger.Error(err, "failed to cleanup pending resources after PodVolumeRestore issue", "restore", restoreName)
			}

			appRestore.Status.Reason = reason
			appRestore.Status.Message = msg

			user := appRestore.Annotations[AnnotationUser]
			if user == "" {
				user = "system"
			}
			triggeredBy := appRestore.Annotations[AnnotationTraceID]
			taskName := fmt.Sprintf("应用恢复 %s 执行恢复 %s", appRestore.Name, restoreName)
			helper.ReportTaskFinishedWithClient(ctx, r.Client, r.Scheme, appRestore, taskName, appRestore.Spec.Cluster, helper.TaskStatusFailed, restore.Status.StartTimestamp, restore.Status.CompletionTimestamp, user, triggeredBy, msg, reason)

			return disasterv1.PhaseFailed, ctrl.Result{}, nil
		}
	}

	// 软收敛：处理“进度完成但长期 InProgress”和“启动后长期无进度”等异常场景。
	if phase, res, handled, handleErr := r.handleRestoreStall(ctx, cli, appRestore, restoreName, restore, cfg); handleErr != nil {
		return phase, res, handleErr
	} else if handled {
		return phase, res, nil
	}

	appRestore.Status.RestoreStatus = restore.Status
	// Check restore status and determine next phase
	switch restore.Status.Phase {
	case velerov1.RestorePhaseCompleted:
		logger.Info("Velero Restore completed successfully")
		audit, verifyErr := r.verifyStorageClassRuleEffects(ctx, cli, appRestore)
		applyModifierEffectAuditAnnotations(&appRestore.ObjectMeta, audit)

		user := appRestore.Annotations[AnnotationUser]
		if user == "" {
			user = "system"
		}
		triggeredBy := appRestore.Annotations[AnnotationTraceID]
		taskName := fmt.Sprintf("应用恢复 %s 执行恢复 %s", appRestore.Name, restoreName)

		if verifyErr != nil {
			errorCode := modifierEffectReasonVerifyFailed
			failMsg := fmt.Sprintf("modifier effect verification error: %v", verifyErr)
			appRestore.Status.Reason = errorCode
			appRestore.Status.Message = failMsg
			r.Recorder.Event(appRestore, corev1.EventTypeWarning, errorCode, failMsg)
			helper.ReportTaskFinishedWithClient(ctx, r.Client, r.Scheme, appRestore, taskName, appRestore.Spec.Cluster, helper.TaskStatusFailed, restore.Status.StartTimestamp, restore.Status.CompletionTimestamp, user, triggeredBy, failMsg, errorCode)
			return disasterv1.PhaseFailed, ctrl.Result{}, nil
		}
		if audit.NoEffectRuleCount > 0 {
			errorCode := modifierEffectReasonNoEffect
			failMsg := buildModifierNoEffectMessage(audit)
			appRestore.Status.Reason = errorCode
			appRestore.Status.Message = failMsg
			r.Recorder.Event(appRestore, corev1.EventTypeWarning, errorCode, failMsg)
			helper.ReportTaskFinishedWithClient(ctx, r.Client, r.Scheme, appRestore, taskName, appRestore.Spec.Cluster, helper.TaskStatusFailed, restore.Status.StartTimestamp, restore.Status.CompletionTimestamp, user, triggeredBy, failMsg, errorCode)
			return disasterv1.PhaseFailed, ctrl.Result{}, nil
		}

		r.Recorder.Event(appRestore, corev1.EventTypeNormal, "RestoreCompleted", "Velero Restore completed successfully")
		helper.ReportTaskFinishedWithClient(ctx, r.Client, r.Scheme, appRestore, taskName, appRestore.Spec.Cluster, helper.TaskStatusSuccess, restore.Status.StartTimestamp, restore.Status.CompletionTimestamp, user, triggeredBy, "恢复成功")
		return disasterv1.PhaseSucceeded, ctrl.Result{}, nil
	case velerov1.RestorePhaseFailed:
		logger.Info("Velero Restore failed")
		r.Recorder.Event(appRestore, corev1.EventTypeWarning, "RestoreFailed", "Velero Restore failed")
		// 恢复失败事件
		user := appRestore.Annotations[AnnotationUser]
		if user == "" {
			user = "system"
		}
		triggeredBy := appRestore.Annotations[AnnotationTraceID]
		taskName := fmt.Sprintf("应用恢复 %s 执行恢复 %s", appRestore.Name, restoreName)
		errorCode := "RestoreFailed"
		failMsg := buildRestoreFailureMessage(restore, "恢复失败")
		appRestore.Status.Reason = errorCode
		appRestore.Status.Message = failMsg
		helper.ReportTaskFinishedWithClient(ctx, r.Client, r.Scheme, appRestore, taskName, appRestore.Spec.Cluster, helper.TaskStatusFailed, restore.Status.StartTimestamp, restore.Status.CompletionTimestamp, user, triggeredBy, failMsg, errorCode)
		return disasterv1.PhaseFailed, ctrl.Result{}, nil
	case velerov1.RestorePhasePartiallyFailed:
		logger.Info("Velero Restore completed with partial failures")
		r.Recorder.Event(appRestore, corev1.EventTypeWarning, "RestorePartiallyFailed", "Velero Restore completed with partial failures")
		// 部分恢复失败事件
		user := appRestore.Annotations[AnnotationUser]
		if user == "" {
			user = "system"
		}
		triggeredBy := appRestore.Annotations[AnnotationTraceID]
		taskName := fmt.Sprintf("应用恢复 %s 执行恢复 %s", appRestore.Name, restoreName)
		errorCode := "RestorePartiallyFailed"
		failMsg := buildRestoreFailureMessage(restore, "恢复部分失败")
		appRestore.Status.Reason = errorCode
		appRestore.Status.Message = failMsg
		helper.ReportTaskFinishedWithClient(ctx, r.Client, r.Scheme, appRestore, taskName, appRestore.Spec.Cluster, helper.TaskStatusFailed, restore.Status.StartTimestamp, restore.Status.CompletionTimestamp, user, triggeredBy, failMsg, errorCode)
		return disasterv1.PhasePartiallyFailed, ctrl.Result{}, nil
	case velerov1.RestorePhaseInProgress:
		// Check global timeout
		timeout := resolveRestoreInProgressTimeout(appRestore, cfg)
		if time.Since(restore.CreationTimestamp.Time) > timeout {
			timeoutErr := fmt.Errorf("timeout waiting for restore to complete after %s", timeout)
			logger.Error(timeoutErr, "timeout waiting for restore to complete", "timeout", timeout)
			r.Recorder.Event(appRestore, corev1.EventTypeWarning, "RestoreTimeout", fmt.Sprintf("Timed out after %s", timeout))

			// Force terminate
			if err := r.forceTerminateRestore(ctx, cli, appRestore, restore); err != nil {
				logger.Error(err, "failed to force terminate timed out restore")
			}

			// Clean up pending resources
			if err := r.cleanupPendingRestoredResources(ctx, cli, appRestore, restoreName); err != nil {
				logger.Error(err, "failed to cleanup pending restored resources after timeout")
			}

			errorCode := "TimeoutExceeded"
			failMsg := timeoutErr.Error()
			appRestore.Status.Reason = errorCode
			appRestore.Status.Message = failMsg
			user := appRestore.Annotations[AnnotationUser]
			if user == "" {
				user = "system"
			}
			triggeredBy := appRestore.Annotations[AnnotationTraceID]
			taskName := fmt.Sprintf("应用恢复 %s 执行恢复 %s", appRestore.Name, restoreName)
			helper.ReportTaskFinishedWithClient(ctx, r.Client, r.Scheme, appRestore, taskName, appRestore.Spec.Cluster, helper.TaskStatusFailed, restore.Status.StartTimestamp, restore.Status.CompletionTimestamp, user, triggeredBy, failMsg, errorCode)
			return disasterv1.PhaseFailed, ctrl.Result{}, timeoutErr
		}
		logger.Info("Velero Restore in progress")

		// Requeue to check status again
		return disasterv1.PhaseRestoring, ctrl.Result{RequeueAfter: cfg.RestoreInProgressPollInterval}, nil
	default:
		// Check global timeout for unknown state too
		timeout := resolveRestoreUnknownTimeout(appRestore, cfg)
		if time.Since(restore.CreationTimestamp.Time) > timeout {
			timeoutErr := fmt.Errorf("restore in unknown state for too long (timeout %s)", timeout)
			logger.Error(timeoutErr, "restore in unknown state for too long", "timeout", timeout)
			r.Recorder.Event(appRestore, corev1.EventTypeWarning, "RestoreTimeout", "Restore in unknown state for too long")

			// Force terminate
			if err := r.forceTerminateRestore(ctx, cli, appRestore, restore); err != nil {
				logger.Error(err, "failed to force terminate timed out restore")
			}

			// Clean up pending resources
			if err := r.cleanupPendingRestoredResources(ctx, cli, appRestore, restoreName); err != nil {
				logger.Error(err, "failed to cleanup pending restored resources after timeout")
			}

			errorCode := "TimeoutExceeded"
			failMsg := timeoutErr.Error()
			appRestore.Status.Reason = errorCode
			appRestore.Status.Message = failMsg
			user := appRestore.Annotations[AnnotationUser]
			if user == "" {
				user = "system"
			}
			triggeredBy := appRestore.Annotations[AnnotationTraceID]
			taskName := fmt.Sprintf("应用恢复 %s 执行恢复 %s", appRestore.Name, restoreName)
			helper.ReportTaskFinishedWithClient(ctx, r.Client, r.Scheme, appRestore, taskName, appRestore.Spec.Cluster, helper.TaskStatusFailed, restore.Status.StartTimestamp, restore.Status.CompletionTimestamp, user, triggeredBy, failMsg, errorCode)
			return disasterv1.PhaseFailed, ctrl.Result{}, timeoutErr
		}

		logger.Info("Velero Restore phase unknown, requeueing", "phase", restore.Status.Phase)
		return disasterv1.PhaseRestoring, ctrl.Result{RequeueAfter: cfg.RestoreUnknownPollInterval}, nil
	}
}

func buildRestoreFailureMessage(restore *velerov1.Restore, fallback string) string {
	if restore == nil {
		return fallback
	}
	if restore.Status.FailureReason != "" {
		return fmt.Sprintf("%s: %s", fallback, restore.Status.FailureReason)
	}
	if restore.Status.Errors > 0 {
		return fmt.Sprintf("%s: errors=%d warnings=%d", fallback, restore.Status.Errors, restore.Status.Warnings)
	}
	return fallback
}

func (r *AppRestoreReconciler) handleRestoreStall(
	ctx context.Context,
	cli client.Client,
	appRestore *disasterv1.AppRestore,
	restoreName string,
	restore *velerov1.Restore,
	cfg RestoreRuntimeConfig,
) (disasterv1.AppRestorePhase, ctrl.Result, bool, error) {
	if restore == nil {
		return "", ctrl.Result{}, false, nil
	}

	if stalled, elapsed := isRestoreProgressCompletedButInProgress(restore, cfg.ProgressCompleteGrace); stalled {
		activePVRs, pvrErr := hasActivePodVolumeRestores(ctx, cli, restoreName)
		if pvrErr != nil {
			return disasterv1.PhaseRestoring, ctrl.Result{}, false, pvrErr
		}
		if activePVRs {
			return "", ctrl.Result{}, false, nil
		}

		progress := restore.Status.Progress
		msg := fmt.Sprintf(
			"Velero Restore %s has remained InProgress with completed progress (%d/%d) for %s",
			restore.Name,
			progress.ItemsRestored,
			progress.TotalItems,
			elapsed.Round(time.Second),
		)
		phase, res, err := r.autoRetryOrFailStalledRestore(
			ctx, cli, appRestore, restoreName, restore,
			restoreStallTypeProgressCompleted, "RestoreProgressStalled", msg, cfg,
		)
		return phase, res, true, err
	}

	if stalled, elapsed := isRestoreStartupStalled(restore, cfg.StartupGrace); stalled {
		msg := fmt.Sprintf(
			"Velero Restore %s has no start/completion status for %s since creation",
			restore.Name,
			elapsed.Round(time.Second),
		)
		phase, res, err := r.autoRetryOrFailStalledRestore(
			ctx, cli, appRestore, restoreName, restore,
			restoreStallTypeStartupStalled, "RestoreStartupStalled", msg, cfg,
		)
		return phase, res, true, err
	}

	if isServerStartingTransientFailure(restore) {
		msg := fmt.Sprintf(
			"Velero Restore %s hit transient server-start failure: %s",
			restore.Name,
			strings.TrimSpace(restore.Status.FailureReason),
		)
		phase, res, err := r.autoRetryOrFailStalledRestore(
			ctx, cli, appRestore, restoreName, restore,
			restoreStallTypeStartupTransient, "RestoreStartupStalled", msg, cfg,
		)
		return phase, res, true, err
	}

	if stalled, elapsed := isRestoreEmptyStatusStalled(appRestore, restore, cfg.EmptyStatusGrace); stalled {
		msg := fmt.Sprintf(
			"Velero Restore %s has empty/unchanged status for %s",
			restore.Name,
			elapsed.Round(time.Second),
		)
		phase, res, err := r.autoRetryOrFailStalledRestore(
			ctx, cli, appRestore, restoreName, restore,
			restoreStallTypeEmptyStatus, "RestoreEmptyStatusDetected", msg, cfg,
		)
		return phase, res, true, err
	}

	return "", ctrl.Result{}, false, nil
}

func hasActivePodVolumeRestores(ctx context.Context, cli client.Client, restoreName string) (bool, error) {
	if cli == nil || restoreName == "" {
		return false, nil
	}

	pvrList := &velerov1.PodVolumeRestoreList{}
	if err := cli.List(ctx, pvrList,
		client.InNamespace(VeleroNamespace),
		client.MatchingLabels{velerov1.RestoreNameLabel: restoreName},
	); err != nil {
		return false, err
	}

	for i := range pvrList.Items {
		if !isPodVolumeRestoreTerminalPhase(pvrList.Items[i].Status.Phase) {
			return true, nil
		}
	}
	return false, nil
}

func isPodVolumeRestoreTerminalPhase(phase velerov1.PodVolumeRestorePhase) bool {
	switch phase {
	case velerov1.PodVolumeRestorePhaseCompleted,
		velerov1.PodVolumeRestorePhaseCanceled,
		velerov1.PodVolumeRestorePhaseFailed:
		return true
	default:
		return false
	}
}

func isTransientKubeAPIError(err error) bool {
	if err == nil {
		return false
	}
	if apierrors.IsTimeout(err) ||
		apierrors.IsServerTimeout(err) ||
		apierrors.IsTooManyRequests(err) ||
		apierrors.IsServiceUnavailable(err) ||
		apierrors.IsInternalError(err) {
		return true
	}

	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "server was unable to return a response in the time allotted") ||
		strings.Contains(msg, "context deadline exceeded") ||
		strings.Contains(msg, "i/o timeout") ||
		strings.Contains(msg, "tls handshake timeout") ||
		strings.Contains(msg, "client.timeout exceeded") ||
		strings.Contains(msg, "timeout awaiting response headers") ||
		strings.Contains(msg, "http2: client connection lost") ||
		strings.Contains(msg, "connection reset by peer") ||
		strings.Contains(msg, "server closed idle connection") ||
		strings.Contains(msg, "unexpected eof") ||
		strings.Contains(msg, "connection refused")
}

func (r *AppRestoreReconciler) autoRetryOrFailStalledRestore(
	ctx context.Context,
	cli client.Client,
	appRestore *disasterv1.AppRestore,
	restoreName string,
	restore *velerov1.Restore,
	stallType string,
	stallReason string,
	stallMessage string,
	cfg RestoreRuntimeConfig,
) (disasterv1.AppRestorePhase, ctrl.Result, error) {
	logger := logf.FromContext(ctx)
	r.Recorder.Event(appRestore, corev1.EventTypeWarning, stallReason, stallMessage)

	retryLimit := resolveRestoreRetryLimit(stallType, cfg)
	retryCount := getRestoreRetryCount(appRestore, stallType)
	if retryCount < retryLimit {
		if restore != nil {
			if err := cli.Delete(ctx, restore); err != nil && !apierrors.IsNotFound(err) {
				logger.Error(err, "failed to delete stalled restore for auto-retry", "restore", restoreName, "stallType", stallType)
				return disasterv1.PhaseFailed, ctrl.Result{}, err
			}
		}
		setRestoreRetryCount(appRestore, stallType, retryCount+1)
		clearRestoreConvergenceTracking(appRestore)
		appRestore.Status.RestoreStatus = velerov1.RestoreStatus{}

		retryMessage := fmt.Sprintf(
			"%s; auto-retry triggered (stallType=%s %d/%d)",
			stallMessage, stallType, retryCount+1, retryLimit,
		)
		r.Recorder.Event(appRestore, corev1.EventTypeNormal, "RestoreAutoRetryTriggered", retryMessage)
		logger.Info(
			"auto-retry triggered for stalled restore",
			"restore", restoreName,
			"stallType", stallType,
			"retryCount", retryCount+1,
			"retryLimit", retryLimit,
		)
		return disasterv1.PhaseRestoring, ctrl.Result{RequeueAfter: cfg.RetryBackoff}, nil
	}

	failReason := "RestoreStalledAfterRetry"
	failMsg := fmt.Sprintf(
		"%s; stallType=%s autoRetry=%d/%d exhausted",
		stallMessage, stallType, retryCount, retryLimit,
	)
	r.Recorder.Event(appRestore, corev1.EventTypeWarning, failReason, failMsg)
	r.tryRestartVeleroAfterStall(ctx, cli, appRestore, stallType, restoreName)

	if restore != nil {
		if err := r.forceTerminateRestore(ctx, cli, appRestore, restore); err != nil {
			logger.Error(err, "failed to force terminate restore after stall retry exhausted", "restore", restoreName, "stallType", stallType)
		}
	}
	if err := r.cleanupPendingRestoredResources(ctx, cli, appRestore, restoreName); err != nil {
		logger.Error(err, "failed to cleanup pending restored resources after stall retry exhausted", "restore", restoreName, "stallType", stallType)
	}

	appRestore.Status.Reason = failReason
	appRestore.Status.Message = failMsg
	user := appRestore.Annotations[AnnotationUser]
	if user == "" {
		user = "system"
	}
	triggeredBy := appRestore.Annotations[AnnotationTraceID]
	taskName := fmt.Sprintf("应用恢复 %s 执行恢复 %s", appRestore.Name, restoreName)
	var startTimestamp, completionTimestamp *metav1.Time
	if restore != nil {
		startTimestamp = restore.Status.StartTimestamp
		completionTimestamp = restore.Status.CompletionTimestamp
	}
	helper.ReportTaskFinishedWithClient(
		ctx, r.Client, r.Scheme, appRestore, taskName, appRestore.Spec.Cluster, helper.TaskStatusFailed,
		startTimestamp, completionTimestamp, user, triggeredBy, failMsg, failReason,
	)
	return disasterv1.PhaseFailed, ctrl.Result{}, nil
}

func (r *AppRestoreReconciler) tryRestartVeleroAfterStall(
	ctx context.Context,
	cli client.Client,
	appRestore *disasterv1.AppRestore,
	stallType string,
	restoreName string,
) {
	if cli == nil {
		return
	}
	logger := logf.FromContext(ctx)
	if stallType == restoreStallTypeStartupTransient || stallType == restoreStallTypeEmptyStatus {
		msg := fmt.Sprintf("skip velero rollout restart for %s because restarting Velero may interrupt running backup/restore data paths", stallType)
		r.Recorder.Event(appRestore, corev1.EventTypeNormal, "VeleroRestartSkipped", msg)
		logger.Info("skip velero restart for non-fatal restore stall", "stallType", stallType, "restore", restoreName)
		return
	}

	running, err := hasRunningVeleroOperations(ctx, cli)
	if err != nil {
		logger.Error(err, "failed to inspect running velero operations before restart", "stallType", stallType, "restore", restoreName)
		r.Recorder.Event(appRestore, corev1.EventTypeWarning, "VeleroRestartSkipped", err.Error())
		return
	}
	if running {
		msg := fmt.Sprintf("skip velero rollout restart because running Velero operations still exist (stallType=%s)", stallType)
		r.Recorder.Event(appRestore, corev1.EventTypeNormal, "VeleroRestartSkipped", msg)
		logger.Info("skip velero restart while velero operations are running", "stallType", stallType, "restore", restoreName)
		return
	}

	restarted, err := restartVeleroDeployment(ctx, cli)
	if err != nil {
		logger.Error(err, "failed to restart velero after restore stall", "stallType", stallType, "restore", restoreName)
		r.Recorder.Event(appRestore, corev1.EventTypeWarning, "VeleroRestartFailed", err.Error())
		return
	}
	if !restarted {
		logger.Info("skip velero restart due to cooldown", "stallType", stallType, "restore", restoreName, "cooldown", veleroRestartCooldown)
		return
	}

	msg := fmt.Sprintf("detected stalled restore (stallType=%s), triggered velero rollout restart", stallType)
	r.Recorder.Event(appRestore, corev1.EventTypeNormal, "VeleroRestartTriggered", msg)
	logger.Info("triggered velero rollout restart after stalled restore", "stallType", stallType, "restore", restoreName)
}

func hasRunningVeleroOperations(ctx context.Context, cli client.Client) (bool, error) {
	if cli == nil {
		return false, nil
	}

	backupList := &velerov1.BackupList{}
	if err := cli.List(ctx, backupList, client.InNamespace(VeleroNamespace)); err != nil {
		return false, err
	}
	for i := range backupList.Items {
		if isVeleroBackupRunning(backupList.Items[i].Status.Phase) {
			return true, nil
		}
	}

	restoreList := &velerov1.RestoreList{}
	if err := cli.List(ctx, restoreList, client.InNamespace(VeleroNamespace)); err != nil {
		return false, err
	}
	for i := range restoreList.Items {
		if isVeleroRestoreRunning(restoreList.Items[i].Status.Phase) {
			return true, nil
		}
	}

	pvbList := &velerov1.PodVolumeBackupList{}
	if err := cli.List(ctx, pvbList, client.InNamespace(VeleroNamespace)); err != nil {
		return false, err
	}
	for i := range pvbList.Items {
		if !isPodVolumeBackupTerminalPhase(pvbList.Items[i].Status.Phase) {
			return true, nil
		}
	}

	pvrList := &velerov1.PodVolumeRestoreList{}
	if err := cli.List(ctx, pvrList, client.InNamespace(VeleroNamespace)); err != nil {
		return false, err
	}
	for i := range pvrList.Items {
		if !isPodVolumeRestoreTerminalPhase(pvrList.Items[i].Status.Phase) {
			return true, nil
		}
	}

	return false, nil
}

func isVeleroBackupRunning(phase velerov1.BackupPhase) bool {
	switch phase {
	case velerov1.BackupPhaseNew,
		velerov1.BackupPhaseInProgress,
		velerov1.BackupPhaseWaitingForPluginOperations,
		velerov1.BackupPhaseWaitingForPluginOperationsPartiallyFailed,
		velerov1.BackupPhaseFinalizing,
		velerov1.BackupPhaseFinalizingPartiallyFailed,
		velerov1.BackupPhaseDeleting:
		return true
	default:
		return false
	}
}

func isPodVolumeBackupTerminalPhase(phase velerov1.PodVolumeBackupPhase) bool {
	switch phase {
	case velerov1.PodVolumeBackupPhaseCompleted,
		velerov1.PodVolumeBackupPhaseCanceled,
		velerov1.PodVolumeBackupPhaseFailed:
		return true
	default:
		return false
	}
}

func restartVeleroDeployment(ctx context.Context, cli client.Client) (bool, error) {
	deploy := &appsv1.Deployment{}
	key := types.NamespacedName{Name: veleroDeploymentDefaultName, Namespace: VeleroNamespace}
	if err := cli.Get(ctx, key, deploy); err != nil {
		return false, err
	}

	if deploy.Spec.Template.Annotations == nil {
		deploy.Spec.Template.Annotations = map[string]string{}
	}

	now := time.Now().UTC()
	if raw := strings.TrimSpace(deploy.Spec.Template.Annotations[veleroRestartAtAnnotationKey]); raw != "" {
		if lastRestartAt, err := time.Parse(time.RFC3339, raw); err == nil {
			if now.Sub(lastRestartAt) < veleroRestartCooldown {
				return false, nil
			}
		}
	}

	patch := client.MergeFrom(deploy.DeepCopy())
	deploy.Spec.Template.Annotations[veleroRestartAtAnnotationKey] = now.Format(time.RFC3339)
	if err := cli.Patch(ctx, deploy, patch); err != nil {
		return false, err
	}
	return true, nil
}

func resolveRestoreRetryLimit(stallType string, cfg RestoreRuntimeConfig) int {
	switch stallType {
	case restoreStallTypeProgressCompleted:
		return cfg.AutoRetryLimitProgress
	case restoreStallTypeStartupStalled, restoreStallTypeStartupTransient:
		return cfg.AutoRetryLimitStartup
	case restoreStallTypeMissingRestore:
		return cfg.AutoRetryLimitMissing
	case restoreStallTypeEmptyStatus:
		return cfg.AutoRetryLimitEmpty
	default:
		return cfg.AutoRetryLimit
	}
}

func getRestoreRetryCount(appRestore *disasterv1.AppRestore, stallType string) int {
	labelKey := retryCountLabelByType(stallType)
	if labelKey != "" {
		if count, ok := parseLabelInt(appRestore, labelKey); ok {
			return count
		}
	}
	if count, ok := parseLabelInt(appRestore, labelAppRestoreAutoRetryCount); ok {
		return count
	}
	return 0
}

func setRestoreRetryCount(appRestore *disasterv1.AppRestore, stallType string, count int) {
	if appRestore == nil {
		return
	}
	if appRestore.Labels == nil {
		appRestore.Labels = map[string]string{}
	}
	labelKey := retryCountLabelByType(stallType)
	if labelKey == "" {
		labelKey = labelAppRestoreAutoRetryCount
	}
	if count <= 0 {
		delete(appRestore.Labels, labelKey)
		return
	}
	appRestore.Labels[labelKey] = strconv.Itoa(count)
	// Once the typed counter is written, stop relying on the legacy shared counter.
	if labelKey != labelAppRestoreAutoRetryCount {
		delete(appRestore.Labels, labelAppRestoreAutoRetryCount)
	}
}

func retryCountLabelByType(stallType string) string {
	switch stallType {
	case restoreStallTypeProgressCompleted:
		return labelAppRestoreRetryProgress
	case restoreStallTypeStartupStalled, restoreStallTypeStartupTransient:
		return labelAppRestoreRetryStartup
	case restoreStallTypeMissingRestore:
		return labelAppRestoreRetryMissing
	case restoreStallTypeEmptyStatus:
		return labelAppRestoreRetryEmpty
	default:
		return labelAppRestoreAutoRetryCount
	}
}

func parseLabelInt(appRestore *disasterv1.AppRestore, labelKey string) (int, bool) {
	if appRestore == nil || appRestore.Labels == nil {
		return 0, false
	}
	raw := strings.TrimSpace(appRestore.Labels[labelKey])
	if raw == "" {
		return 0, false
	}
	count, err := strconv.Atoi(raw)
	if err != nil || count < 0 {
		return 0, false
	}
	return count, true
}

func clearRestoreConvergenceTracking(appRestore *disasterv1.AppRestore) {
	if appRestore == nil || appRestore.Annotations == nil {
		return
	}
	delete(appRestore.Annotations, annotationAppRestoreMissingSince)
	delete(appRestore.Annotations, annotationAppRestoreLastObservedSig)
	delete(appRestore.Annotations, annotationAppRestoreLastProgressAt)
}

func clearRestoreMissingSince(appRestore *disasterv1.AppRestore) {
	if appRestore == nil || appRestore.Annotations == nil {
		return
	}
	delete(appRestore.Annotations, annotationAppRestoreMissingSince)
}

func getRestoreMissingSince(appRestore *disasterv1.AppRestore) (time.Time, bool) {
	if appRestore == nil || appRestore.Annotations == nil {
		return time.Time{}, false
	}
	raw := strings.TrimSpace(appRestore.Annotations[annotationAppRestoreMissingSince])
	if raw == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

func markRestoreMissingSince(appRestore *disasterv1.AppRestore, ts time.Time) {
	if appRestore == nil {
		return
	}
	if appRestore.Annotations == nil {
		appRestore.Annotations = map[string]string{}
	}
	appRestore.Annotations[annotationAppRestoreMissingSince] = ts.UTC().Format(time.RFC3339)
}

func isRestoreEmptyStatusStalled(appRestore *disasterv1.AppRestore, restore *velerov1.Restore, grace time.Duration) (bool, time.Duration) {
	if appRestore == nil || restore == nil || grace <= 0 {
		return false, 0
	}
	if !isRestoreEmptyStatusCandidate(restore) {
		return false, 0
	}
	if appRestore.Annotations == nil {
		appRestore.Annotations = map[string]string{}
	}

	signature := buildRestoreObservationSignature(restore)
	lastSignature := strings.TrimSpace(appRestore.Annotations[annotationAppRestoreLastObservedSig])
	lastProgressAt, hasLastProgressAt := parseLabelTime(appRestore.Annotations[annotationAppRestoreLastProgressAt])
	now := time.Now().UTC()

	if lastSignature == "" || signature == "" || signature != lastSignature || !hasLastProgressAt {
		appRestore.Annotations[annotationAppRestoreLastObservedSig] = signature
		appRestore.Annotations[annotationAppRestoreLastProgressAt] = now.Format(time.RFC3339)
		return false, 0
	}
	elapsed := now.Sub(lastProgressAt)
	return elapsed > grace, elapsed
}

func parseLabelTime(raw string) (time.Time, bool) {
	if strings.TrimSpace(raw) == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(raw))
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

func buildRestoreObservationSignature(restore *velerov1.Restore) string {
	if restore == nil {
		return ""
	}
	progress := restore.Status.Progress
	itemsRestored := 0
	totalItems := 0
	if progress != nil {
		itemsRestored = progress.ItemsRestored
		totalItems = progress.TotalItems
	}
	return fmt.Sprintf(
		"%s|%d|%d|%d|%d|%s|%s",
		restore.Status.Phase,
		itemsRestored,
		totalItems,
		restore.Status.RestoreItemOperationsAttempted,
		restore.Status.RestoreItemOperationsCompleted,
		strings.TrimSpace(restore.Status.FailureReason),
		restore.ResourceVersion,
	)
}

func isRestoreEmptyStatusCandidate(restore *velerov1.Restore) bool {
	if restore == nil {
		return false
	}
	if restore.Status.CompletionTimestamp != nil {
		return false
	}
	switch restore.Status.Phase {
	case velerov1.RestorePhaseCompleted, velerov1.RestorePhaseFailed, velerov1.RestorePhasePartiallyFailed:
		return false
	}
	if restore.Status.StartTimestamp != nil {
		return false
	}
	if restore.Status.Progress == nil {
		return true
	}
	if restore.Status.Progress.TotalItems > 0 || restore.Status.Progress.ItemsRestored > 0 {
		return false
	}
	if restore.Status.RestoreItemOperationsAttempted > 0 || restore.Status.RestoreItemOperationsCompleted > 0 {
		return false
	}
	return strings.TrimSpace(restore.Status.FailureReason) == ""
}

func shouldCreateRestoreImmediately(appRestore *disasterv1.AppRestore) bool {
	if appRestore == nil {
		return false
	}
	if appRestore.Status.RestoreStatus.Phase != "" {
		return false
	}
	if appRestore.Status.RestoreStatus.StartTimestamp != nil || appRestore.Status.RestoreStatus.CompletionTimestamp != nil {
		return false
	}
	if appRestore.Status.RestoreStatus.Progress != nil {
		if appRestore.Status.RestoreStatus.Progress.TotalItems > 0 ||
			appRestore.Status.RestoreStatus.Progress.ItemsRestored > 0 {
			return false
		}
	}
	if appRestore.Annotations == nil {
		return true
	}
	return strings.TrimSpace(appRestore.Annotations[annotationAppRestoreLastObservedSig]) == ""
}

func (r *AppRestoreReconciler) handleRestoreNotFound(
	ctx context.Context,
	cli client.Client,
	appRestore *disasterv1.AppRestore,
	restoreName string,
	cfg RestoreRuntimeConfig,
) (disasterv1.AppRestorePhase, ctrl.Result, error) {
	logger := logf.FromContext(ctx)

	if shouldCreateRestoreImmediately(appRestore) {
		clearRestoreConvergenceTracking(appRestore)
		logger.Info("Velero Restore not found, creating", "restore", restoreName)
		if err := r.createVeleroRestore(ctx, cli, appRestore); err != nil {
			logger.Error(err, "failed to create missing restore", "restore", restoreName)
			return disasterv1.PhaseFailed, ctrl.Result{}, err
		}
		user := appRestore.Annotations[AnnotationUser]
		if user == "" {
			user = "system"
		}
		triggeredBy := appRestore.Annotations[AnnotationTraceID]
		taskName := fmt.Sprintf("应用恢复 %s 执行恢复 %s", appRestore.Name, restoreName)
		helper.ReportTaskStartedWithClient(ctx, r.Client, r.Scheme, appRestore, taskName, appRestore.Spec.Cluster, user, triggeredBy, "恢复开始")
		helper.ReportTaskProgressWithClient(ctx, r.Client, r.Scheme, appRestore, taskName, appRestore.Spec.Cluster, user, triggeredBy, fmt.Sprintf("已创建 Velero Restore 资源 %s，等待执行...", restoreName))
		return disasterv1.PhaseRestoring, ctrl.Result{RequeueAfter: RestorePhaseCreateWaitSeconds}, nil
	}

	missingSince, exists := getRestoreMissingSince(appRestore)
	if !exists {
		markRestoreMissingSince(appRestore, time.Now().UTC())
		msg := fmt.Sprintf("Velero Restore %s is missing, waiting up to %s before convergence action", restoreName, cfg.MissingGrace)
		r.Recorder.Event(appRestore, corev1.EventTypeWarning, "RestoreMissingDetected", msg)
		return disasterv1.PhaseRestoring, ctrl.Result{RequeueAfter: cfg.RetryBackoff}, nil
	}

	elapsed := time.Since(missingSince)
	if elapsed <= cfg.MissingGrace {
		return disasterv1.PhaseRestoring, ctrl.Result{RequeueAfter: cfg.RetryBackoff}, nil
	}

	msg := fmt.Sprintf(
		"Velero Restore %s has been missing for %s (grace=%s)",
		restoreName, elapsed.Round(time.Second), cfg.MissingGrace,
	)
	return r.autoRetryOrFailStalledRestore(
		ctx, cli, appRestore, restoreName, nil,
		restoreStallTypeMissingRestore, "RestoreMissingDetected", msg, cfg,
	)
}

func isRestoreProgressCompletedButInProgress(restore *velerov1.Restore, grace time.Duration) (bool, time.Duration) {
	if restore == nil {
		return false, 0
	}
	if restore.Status.Phase != velerov1.RestorePhaseInProgress || restore.Status.CompletionTimestamp != nil {
		return false, 0
	}
	if restore.Status.Progress == nil {
		return false, 0
	}

	total := restore.Status.Progress.TotalItems
	restored := restore.Status.Progress.ItemsRestored
	if total <= 0 || restored < total {
		return false, 0
	}
	if restore.Status.RestoreItemOperationsAttempted > restore.Status.RestoreItemOperationsCompleted {
		return false, 0
	}

	baseTime := restore.CreationTimestamp.Time
	if restore.Status.StartTimestamp != nil {
		baseTime = restore.Status.StartTimestamp.Time
	}
	if baseTime.IsZero() {
		return false, 0
	}

	elapsed := time.Since(baseTime)
	return elapsed > grace, elapsed
}

func isRestoreStartupStalled(restore *velerov1.Restore, grace time.Duration) (bool, time.Duration) {
	if restore == nil {
		return false, 0
	}
	if restore.Status.StartTimestamp != nil || restore.Status.CompletionTimestamp != nil {
		return false, 0
	}
	if restore.Status.Phase != "" && restore.Status.Phase != velerov1.RestorePhaseNew {
		return false, 0
	}
	if restore.CreationTimestamp.IsZero() {
		return false, 0
	}

	elapsed := time.Since(restore.CreationTimestamp.Time)
	return elapsed > grace, elapsed
}

func isServerStartingTransientFailure(restore *velerov1.Restore) bool {
	if restore == nil {
		return false
	}
	if restore.Status.Phase != velerov1.RestorePhasePartiallyFailed && restore.Status.Phase != velerov1.RestorePhaseFailed {
		return false
	}

	reason := strings.ToLower(strings.TrimSpace(restore.Status.FailureReason))
	if reason == "" {
		return false
	}
	return strings.Contains(reason, "during the server starting") ||
		(strings.Contains(reason, "found a restore with status") && strings.Contains(reason, "\"inprogress\""))
}

func resolveRestoreInProgressTimeout(appRestore *disasterv1.AppRestore, cfg RestoreRuntimeConfig) time.Duration {
	timeout := cfg.RestoreInProgressMaxWaitDefault
	if appRestore != nil && appRestore.Spec.Timeout != nil {
		timeout = appRestore.Spec.Timeout.Duration
	}
	return timeout
}

func resolveRestoreUnknownTimeout(appRestore *disasterv1.AppRestore, cfg RestoreRuntimeConfig) time.Duration {
	timeout := cfg.RestoreUnknownMaxWaitDefault
	if appRestore != nil && appRestore.Spec.Timeout != nil {
		timeout = appRestore.Spec.Timeout.Duration
	}
	return timeout
}

func (r *AppRestoreReconciler) detectPodVolumeRestoreIssue(
	ctx context.Context,
	cli client.Client,
	restoreName string,
	restoreTimeout time.Duration,
	pendingPhaseMaxWait time.Duration,
) (reason, message string, err error) {
	if restoreName == "" {
		return "", "", nil
	}

	pvrList := &velerov1.PodVolumeRestoreList{}
	if err := cli.List(ctx, pvrList,
		client.InNamespace(VeleroNamespace),
		client.MatchingLabels{"velero.io/restore-name": restoreName},
	); err != nil {
		return "", "", err
	}
	if len(pvrList.Items) == 0 {
		return "", "", nil
	}

	now := time.Now()
	pendingStallTimeout := restoreTimeout
	if pendingStallTimeout <= 0 || pendingStallTimeout > pendingPhaseMaxWait {
		pendingStallTimeout = pendingPhaseMaxWait
	}

	var failedCount int
	var failedName string
	var failedMsg string

	var stalledCount int
	var stalledName string
	var stalledPhase velerov1.PodVolumeRestorePhase

	for i := range pvrList.Items {
		pvr := &pvrList.Items[i]

		if pvr.Status.Phase == velerov1.PodVolumeRestorePhaseFailed {
			failedCount++
			if failedName == "" {
				failedName = pvr.Name
				failedMsg = strings.TrimSpace(pvr.Status.Message)
			}
			continue
		}

		if pvr.Status.Phase == velerov1.PodVolumeRestorePhaseCompleted || pvr.Status.Phase == velerov1.PodVolumeRestorePhaseCanceled {
			continue
		}

		if !isPodVolumeRestorePendingPhase(pvr.Status.Phase) {
			continue
		}

		refTime := podVolumeRestoreReferenceTime(pvr)
		if refTime.IsZero() {
			continue
		}
		if now.Sub(refTime) > pendingStallTimeout {
			stalledCount++
			if stalledName == "" {
				stalledName = pvr.Name
				stalledPhase = pvr.Status.Phase
			}
		}
	}

	if failedCount > 0 {
		msg := fmt.Sprintf("PodVolumeRestore failed (%d/%d), first=%s", failedCount, len(pvrList.Items), failedName)
		if failedMsg != "" {
			msg = fmt.Sprintf("%s: %s", msg, failedMsg)
		}
		return "PodVolumeRestoreFailed", msg, nil
	}

	if stalledCount > 0 {
		return "PodVolumeRestoreStalled",
			fmt.Sprintf("PodVolumeRestore stalled in pending phase for over %s (%d/%d), first=%s phase=%s",
				pendingStallTimeout.Round(time.Second), stalledCount, len(pvrList.Items), stalledName, stalledPhase),
			nil
	}

	return "", "", nil
}

func isPodVolumeRestorePendingPhase(phase velerov1.PodVolumeRestorePhase) bool {
	switch phase {
	case "",
		velerov1.PodVolumeRestorePhaseNew,
		velerov1.PodVolumeRestorePhaseAccepted,
		velerov1.PodVolumeRestorePhasePrepared,
		velerov1.PodVolumeRestorePhaseCanceling:
		return true
	default:
		return false
	}
}

func podVolumeRestoreReferenceTime(pvr *velerov1.PodVolumeRestore) time.Time {
	if pvr == nil {
		return time.Time{}
	}
	if pvr.Status.StartTimestamp != nil {
		return pvr.Status.StartTimestamp.Time
	}
	if pvr.Status.AcceptedTimestamp != nil {
		return pvr.Status.AcceptedTimestamp.Time
	}
	return pvr.CreationTimestamp.Time
}
