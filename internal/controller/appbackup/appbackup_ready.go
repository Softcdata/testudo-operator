/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package appbackup

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"time"

	. "github.com/softcdata/testudo-operator/internal/controller"
	. "github.com/softcdata/testudo-operator/pkg/metadata"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	"github.com/softcdata/testudo-operator/pkg/helper"
	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type ReadyHandler struct{}

func (h *ReadyHandler) Handle(ctx context.Context, r *AppBackupReconciler, appBackup *disasterv1.AppBackup) (AppBackupPhase, ctrl.Result, error) {
	logger := logf.FromContext(ctx)

	// 1. Get KubeClient
	cli, err := r.ClientFactory.GetKubeClient(ctx, r.Client, r.Scheme, appBackup.Spec.Cluster)
	if err != nil {
		logger.Error(err, "error creating kube client")
		// If we can't connect to the cluster, we can't do anything.
		// Maybe go back to Pending to retry connection checks?
		return PhasePending, ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	// 2. Provisioning (Schedule vs One-off)
	if appBackup.Spec.Schedule != "" || appBackup.Spec.DisasterPolicy != "" {
		// Handle Schedule
		if appBackup.Spec.DisasterPolicy != "" {
			//获取策略配置
			policy, err := r.GetDisasterPolicy(ctx, r.Client, appBackup.Namespace, appBackup.Spec.DisasterPolicy)
			if err != nil {
				logger.Error(err, "error getting DisasterPolicy")
				r.Recorder.Event(appBackup, corev1.EventTypeWarning, "GetDisasterPolicyFailed", err.Error())
				return PhaseFailed, ctrl.Result{}, err
			}
			if policy.Spec.Type == disasterv1.PolicyTypeAutoBackup {
				applyAutoBackupPolicy(appBackup, policy)
			}

		}
		if appBackup.Spec.Schedule == "" {
			logger.Error(errors.New("no schedule found for scheduled AppBackup"), "no schedule found for scheduled AppBackup")
			return PhaseFailed, ctrl.Result{}, nil
		}
		// Ensure BSL exists
		bslName, err := h.ensureBSL(ctx, r, cli, appBackup)
		if err != nil {
			logger.Error(err, "failed to ensure BSL for schedule")
			return PhaseFailed, ctrl.Result{}, err
		}
		schedule, created, err := r.CreateVeleroSchedule(ctx, cli, appBackup, bslName)
		if err != nil {
			logger.Error(err, "error ensuring Velero Schedule")
			return PhaseFailed, ctrl.Result{}, err
		}
		if created {
			// Schedule created, wait for it to do its thing
			return PhaseReady, ctrl.Result{RequeueAfter: time.Minute}, nil
		}

		// Check for updates to Schedule
		needUpdate := false
		expectedSchedule := veleroScheduleExpression(appBackup.Spec.Schedule)
		if schedule.Spec.Schedule != expectedSchedule {
			schedule.Spec.Schedule = expectedSchedule
			needUpdate = true
		}
		if schedule.Spec.Paused != appBackup.Spec.Paused {
			schedule.Spec.Paused = appBackup.Spec.Paused
			needUpdate = true
		}
		if !reflect.DeepEqual(schedule.Spec.UseOwnerReferencesInBackup, appBackup.Spec.UseOwnerReferencesInBackup) {
			schedule.Spec.UseOwnerReferencesInBackup = appBackup.Spec.UseOwnerReferencesInBackup
			needUpdate = true
		}

		// FIX: Ensure Schedule Template receives the correct StorageLocation with cluster suffix
		expectedTemplate := *appBackup.Spec.Template.DeepCopy()
		expectedTemplate.StorageLocation = bslName

		if !reflect.DeepEqual(schedule.Spec.Template, expectedTemplate) {
			schedule.Spec.Template = expectedTemplate
			needUpdate = true
		}

		if needUpdate {
			if schedule.Annotations == nil {
				schedule.Annotations = make(map[string]string)
				schedule.Annotations[AnnotationTraceID] = appBackup.Annotations[AnnotationTraceID]
			}
			if err := cli.Update(ctx, schedule); err != nil {
				logger.Error(err, "failed to update Velero Schedule")
				r.Recorder.Event(appBackup, corev1.EventTypeWarning, "UpdateScheduleFailed", "Failed to update Velero Schedule")
				return PhaseReady, ctrl.Result{}, err
			}
			r.Recorder.Event(appBackup, corev1.EventTypeNormal, "ScheduleUpdated", "Velero Schedule updated")
		}
		appBackup.Status.ScheduleStatus = schedule.Status

	} else {
		// Handle One-off Backup (Initial creation if needed)
		// If no backups exist and we haven't run one yet, create one.
		// But wait, the "Action" logic handles manual runs.
		// The original logic had: if schedule is empty, create a backup immediately if none exists.

		backups, _, err := r.ListAppBackups(ctx, cli, appBackup)
		if err != nil {
			return PhaseReady, ctrl.Result{}, err
		}

		if !appBackup.Status.HasRunInitialBackup && len(backups) == 0 && len(appBackup.Status.History) == 0 && (appBackup.Spec.SkipImmediately == nil || !*appBackup.Spec.SkipImmediately) {
			// Create initial backup
			// Ensure BSL exists
			bslName, err := h.ensureBSL(ctx, r, cli, appBackup)
			if err != nil {
				logger.Error(err, "failed to ensure BSL for initial backup")
				return PhaseFailed, ctrl.Result{}, err
			}
			backup, _, err := r.CreateVeleroBackup(ctx, cli, appBackup, bslName, "")
			if err != nil {
				logger.Error(err, "error creating initial Velero Backup")
				return PhaseFailed, ctrl.Result{}, err
			}
			// 执行备份 Started 事件
			user := appBackup.Annotations[AnnotationUser]
			if user == "" {
				user = "system"
			}
			triggeredBy := appBackup.Annotations[AnnotationTraceID]
			taskName := fmt.Sprintf("应用备份 %s 执行备份 %s", appBackup.Name, backup.Name)
			helper.ReportTaskStartedWithClient(ctx, r.Client, r.Scheme, appBackup, taskName, appBackup.Spec.Cluster, user, triggeredBy, "首次备份开始")
			helper.ReportTaskProgressWithClient(ctx, r.Client, r.Scheme, appBackup, taskName, appBackup.Spec.Cluster, user, triggeredBy, fmt.Sprintf("已创建 Velero Backup 资源 %s，等待执行...", backup.Name))

			// Mark as run
			appBackup.Status.HasRunInitialBackup = true
			// Immediate Feedback: Set InProgress (Handled inside CreateVeleroBackup)
			return PhaseReady, ctrl.Result{Requeue: true}, nil
		}
	}

	// 3. Observation (List Backups)
	backups, _, err := r.ListAppBackups(ctx, cli, appBackup)
	if err != nil {
		logger.Error(err, "failed to list backups")
		return PhaseReady, ctrl.Result{RequeueAfter: 10 * time.Second}, err
	}

	var latestBackup *velerov1.Backup
	if len(backups) > 0 {
		latestBackup = &backups[0]
	}

	// 4. Action Handling
	if appBackup.Spec.Action != nil {
		shouldRun := false
		if appBackup.Status.LastAction == nil {
			shouldRun = true
		} else if appBackup.Spec.Action.RequestAt.Time.After(appBackup.Status.LastAction.RequestAt.Time) {
			shouldRun = true
		}

		if shouldRun {
			logger.Info("Processing manual action", "type", appBackup.Spec.Action.Type)
			switch appBackup.Spec.Action.Type {
			case "Backup":
				// Ensure BSL exists
				bslName, err := h.ensureBSL(ctx, r, cli, appBackup)
				if err != nil {
					logger.Error(err, "failed to ensure BSL for manual backup")
					return PhaseReady, ctrl.Result{RequeueAfter: 5 * time.Second}, nil
				}
				backup, _, err := r.CreateVeleroBackup(ctx, cli, appBackup, bslName, "")
				if err != nil {
					return PhaseReady, ctrl.Result{}, err
				}
				// 执行备份 Started 事件
				user := appBackup.Annotations[AnnotationUser]
				if user == "" {
					user = "system"
				}
				triggeredBy := appBackup.Annotations[AnnotationTraceID]
				taskName := fmt.Sprintf("应用备份 %s 执行备份 %s", appBackup.Name, backup.Name)
				helper.ReportTaskStartedWithClient(ctx, r.Client, r.Scheme, appBackup, taskName, appBackup.Spec.Cluster, user, triggeredBy, "手动备份开始")
				helper.ReportTaskProgressWithClient(ctx, r.Client, r.Scheme, appBackup, taskName, appBackup.Spec.Cluster, user, triggeredBy, fmt.Sprintf("已创建 Velero Backup 资源 %s，等待执行...", backup.Name))
				// Immediate Feedback (Handled inside CreateVeleroBackup)

			case "Retry":
				// Retry logic: Delete target (or latest) if exists, then create new
				targetName := appBackup.Spec.Action.TargetBackup
				isRetrySpecific := targetName != ""

				// Note: RetryBackup needs modification to support specific target,
				// OR we implement specific logic here.
				// Currently RetryBackup (in controller) likely targets "latest".
				// For now, let's keep using RetryBackup if no target, or improve it.
				// To avoid changing controller interface too much in this step, let's look at RetryBackup implementation.
				// If we can't change RetryBackup signature easily, we can replicate logic here.

				// Replicating/Enhancing logic inline for specificity:
				// Find target backup
				var targetBackup *velerov1.Backup
				if isRetrySpecific {
					for _, b := range backups {
						if b.Name == targetName {
							targetBackup = &b
							break
						}
					}
				} else {
					targetBackup = latestBackup
				}

				if targetBackup != nil {
					// Delete existing
					if err := cli.Delete(ctx, targetBackup); err != nil && !k8serrors.IsNotFound(err) {
						return PhaseReady, ctrl.Result{}, err
					}
					// Wait for deletion? Velero deletes are generic k8s deletes.
				}

				// Create new
				// Ensure BSL exists
				bslName, err := h.ensureBSL(ctx, r, cli, appBackup)
				if err != nil {
					logger.Error(err, "failed to ensure BSL for retry backup")
					return PhaseReady, ctrl.Result{RequeueAfter: 5 * time.Second}, nil
				}
				// Use same name if we are retrying a specific one?
				// Usually Retry implies "try again", maybe new name or same name.
				// Spec says "recreate same name backup".
				nameToCreate := ""
				if isRetrySpecific {
					nameToCreate = targetName
				} else if latestBackup != nil {
					nameToCreate = latestBackup.Name
				}

				// If we deleted it, we can recreate it with same name.
				backup, _, err := r.CreateVeleroBackup(ctx, cli, appBackup, bslName, nameToCreate)
				if err != nil {
					return PhaseReady, ctrl.Result{}, err
				}
				// 重试备份 Started 事件
				user := appBackup.Annotations[AnnotationUser]
				if user == "" {
					user = "system"
				}
				triggeredBy := appBackup.Annotations[AnnotationTraceID]
				taskName := fmt.Sprintf("应用备份 %s 重试备份 %s", appBackup.Name, backup.Name)
				helper.ReportTaskStartedWithClient(ctx, r.Client, r.Scheme, appBackup, taskName, appBackup.Spec.Cluster, user, triggeredBy, "重试备份开始")
				helper.ReportTaskProgressWithClient(ctx, r.Client, r.Scheme, appBackup, taskName, appBackup.Spec.Cluster, user, triggeredBy, fmt.Sprintf("已创建 Velero Backup 资源 %s，等待执行...", backup.Name))

			case "Cancel":
				// Cancel logic: Delete running backup
				var targetBackup *velerov1.Backup
				if appBackup.Spec.Action.TargetBackup != "" {
					for _, b := range backups {
						if b.Name == appBackup.Spec.Action.TargetBackup {
							targetBackup = &b
							break
						}
					}
					if targetBackup == nil {
						logger.Info("Target backup for cancel not found", "backupName", appBackup.Spec.Action.TargetBackup)
						// Already gone, consider done
					}
				} else {
					targetBackup = latestBackup
				}

				if targetBackup != nil && (targetBackup.Status.Phase == velerov1.BackupPhaseInProgress || targetBackup.Status.Phase == velerov1.BackupPhaseNew || targetBackup.Status.Phase == "") {
					// 取消备份 Started 事件
					user := appBackup.Annotations[AnnotationUser]
					if user == "" {
						user = "system"
					}
					traceID := appBackup.Annotations[AnnotationTraceID]
					taskName := fmt.Sprintf("应用备份 %s 取消备份 %s", appBackup.Name, targetBackup.Name)
					helper.ReportTaskStartedWithClient(ctx, r.Client, r.Scheme, appBackup, taskName, appBackup.Spec.Cluster, user, traceID, "取消备份开始")

					logger.Info("Cancelling backup", "backupName", targetBackup.Name)
					if err := cli.Delete(ctx, targetBackup); err != nil {
						logger.Error(err, "failed to delete backup for cancel")
						// 取消备份失败事件
						now := metav1.Now()
						errorCode := appBackup.Status.Reason
						if errorCode == "" {
							errorCode = "BackupActionFailed"
						}
						helper.ReportTaskFinishedWithClient(ctx, r.Client, r.Scheme, appBackup, taskName, appBackup.Spec.Cluster, helper.TaskStatusFailed, &now, &now, user, traceID, "取消备份失败: "+err.Error(), errorCode)
						return PhaseReady, ctrl.Result{}, err
					}
					r.Recorder.Event(appBackup, corev1.EventTypeNormal, "BackupCanceled", "Backup canceled by user")

					// 取消备份成功事件
					now := metav1.Now()
					helper.ReportTaskFinishedWithClient(ctx, r.Client, r.Scheme, appBackup, taskName, appBackup.Spec.Cluster, helper.TaskStatusSuccess, &now, &now, user, traceID, "取消备份完成")

					// Update History immediately
					foundInHistory := false
					for i, rec := range appBackup.Status.History {
						if rec.Name == targetBackup.Name {
							appBackup.Status.History[i].ManagedStatus = disasterv1.LastBackupStatusCanceled
							foundInHistory = true
							break
						}
					}
					if !foundInHistory {
						appBackup.Status.History = append(appBackup.Status.History, disasterv1.BackupRecord{
							Name:          targetBackup.Name,
							ManagedStatus: disasterv1.LastBackupStatusCanceled,
						})
					}
					appBackup.Status.LatestBackupStatus = disasterv1.LastBackupStatusCanceled
				}

			case "Delete":
				if appBackup.Spec.Action.TargetBackup == "" {
					r.Recorder.Event(appBackup, corev1.EventTypeWarning, "DeleteActionFailed", "TargetBackup is required for Delete action")
					// Mark as handled to avoid infinite loop
					appBackup.Status.LastAction = appBackup.Spec.Action
					return PhaseReady, ctrl.Result{}, nil
				}
				targetName := appBackup.Spec.Action.TargetBackup
				logger.Info("Deleting backup", "backupName", targetName)

				// 删除备份历史 Started 事件
				user := appBackup.Annotations[AnnotationUser]
				if user == "" {
					user = "system"
				}
				traceID := appBackup.Annotations[AnnotationTraceID]
				taskName := fmt.Sprintf("应用备份 %s 删除备份 %s", appBackup.Name, targetName)
				helper.ReportTaskStartedWithClient(ctx, r.Client, r.Scheme, appBackup, taskName, appBackup.Spec.Cluster, user, traceID, "删除备份历史开始")

				// Find backup object to ensure we get namespace correct
				var targetBackup *velerov1.Backup
				for _, b := range backups {
					if b.Name == targetName {
						targetBackup = &b
						break
					}
				}

				shouldCleanHistory := false
				if targetBackup == nil {
					r.Recorder.Event(appBackup, corev1.EventTypeWarning, "BackupNotFound", "Backup not found for deletion, cleaning up history")
					shouldCleanHistory = true
				} else {
					// Check phase
					if targetBackup.Status.Phase == velerov1.BackupPhaseInProgress || targetBackup.Status.Phase == velerov1.BackupPhaseNew {
						r.Recorder.Event(appBackup, corev1.EventTypeWarning, "DeleteActionBlocked", "Cannot delete backup in progress, please Cancel first")
						// 删除备份失败事件
						now := metav1.Now()
						errorCode := appBackup.Status.Reason
						if errorCode == "" {
							errorCode = "BackupInProgress"
						}
						helper.ReportTaskFinishedWithClient(ctx, r.Client, r.Scheme, appBackup, taskName, appBackup.Spec.Cluster, helper.TaskStatusFailed, &now, &now, user, traceID, "备份正在进行中，请先取消", errorCode)
					} else {
						// Execute Delete via DeleteBackupRequest
						deleteReq := &velerov1.DeleteBackupRequest{
							ObjectMeta: metav1.ObjectMeta{
								Name:      targetBackup.Name,
								Namespace: VeleroNamespace,
							},
							Spec: velerov1.DeleteBackupRequestSpec{
								BackupName: targetBackup.Name,
							},
						}
						if err := cli.Create(ctx, deleteReq); err != nil {
							if !k8serrors.IsAlreadyExists(err) {
								logger.Error(err, "failed to create DeleteBackupRequest")
								// 删除备份失败事件
								now := metav1.Now()
								errorCode := appBackup.Status.Reason
								if errorCode == "" {
									errorCode = "BackupActionFailed"
								}
								helper.ReportTaskFinishedWithClient(ctx, r.Client, r.Scheme, appBackup, taskName, appBackup.Spec.Cluster, helper.TaskStatusFailed, &now, &now, user, traceID, "删除备份失败: "+err.Error(), errorCode)
								return PhaseReady, ctrl.Result{}, err
							}
						}
						r.Recorder.Event(appBackup, corev1.EventTypeNormal, "BackupDeleted", "Backup deletion requested via DeleteBackupRequest")
						shouldCleanHistory = true
					}
				}

				if shouldCleanHistory {
					// Remove from history
					newHistory := []disasterv1.BackupRecord{}
					for _, rec := range appBackup.Status.History {
						if rec.Name != targetName {
							newHistory = append(newHistory, rec)
						}
					}
					appBackup.Status.History = newHistory
					appBackup.Status.TotalBackups = len(newHistory)
					appBackup.Status.HasRunInitialBackup = true

					// 删除备份完成事件
					now := metav1.Now()
					helper.ReportTaskFinishedWithClient(ctx, r.Client, r.Scheme, appBackup, taskName, appBackup.Spec.Cluster, helper.TaskStatusSuccess, &now, &now, user, traceID, "删除备份完成")
				}
			}

			// Update LastAction
			appBackup.Status.LastAction = appBackup.Spec.Action
			return PhaseReady, ctrl.Result{Requeue: true}, nil
		}
	}

	// 5. Timeout Check for In-Progress or not-yet-started Backups
	// Check if any non-terminal backup has exceeded the timeout threshold.
	runningTimeout := BackupPhaseInProgressMaxWait
	unknownTimeout := BackupPhaseUnknownMaxWait
	if appBackup.Spec.Timeout != nil {
		runningTimeout = appBackup.Spec.Timeout.Duration
		unknownTimeout = appBackup.Spec.Timeout.Duration
	}

	for i := range backups {
		backup := &backups[i]
		if isVeleroBackupRunning(backup.Status.Phase) {
			timeout := runningTimeout
			if backup.Status.Phase == "" {
				timeout = unknownTimeout
			}
			startTime := backup.CreationTimestamp.Time
			if normalizedStart := normalizedBackupStartTimestamp(backup); normalizedStart != nil {
				startTime = normalizedStart.Time
			}
			if startTime.IsZero() {
				continue
			}

			if time.Since(startTime) > timeout {
				timeoutMessage := backupTimeoutMessage(backup, timeout)
				logger.Error(nil, "Backup timeout exceeded, force terminating",
					"backupName", backup.Name, "timeout", timeout, "elapsed", time.Since(startTime))
				r.Recorder.Event(appBackup, corev1.EventTypeWarning, "BackupTimeout",
					fmt.Sprintf("Backup %s timed out after %s, force terminating", backup.Name, timeout))

				// Force terminate the backup
				if err := r.forceTerminateBackup(ctx, cli, appBackup, backup); err != nil {
					logger.Error(err, "failed to force terminate timed out backup", "backupName", backup.Name)
					// Continue - we'll retry on next reconcile
				} else {
					r.Recorder.Event(appBackup, corev1.EventTypeNormal, "BackupTimeoutCleaned",
						fmt.Sprintf("Timed out backup %s has been terminated", backup.Name))
				}

				// Update the record to reflect timeout failure. syncStatus will preserve this
				// managed failure even if Velero keeps reporting an empty/running phase.
				markBackupTimeoutFailed(appBackup, backup, timeoutMessage)
			}
		}
	}

	// 6. Status Sync
	h.syncStatus(ctx, r, appBackup, backups, latestBackup)
	if appBackup.Status.LatestBackupStatus == disasterv1.LastBackupStatusInProgress {
		return PhaseReady, ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}
	return PhaseReady, ctrl.Result{RequeueAfter: time.Minute}, nil
}

func (h *ReadyHandler) syncStatus(ctx context.Context, r *AppBackupReconciler, appBackup *disasterv1.AppBackup, backups []velerov1.Backup, latestBackup *velerov1.Backup) {
	// Merge Strategy: Combine History and current Backups
	// Use a map to deduplicate and update records
	recordMap := make(map[string]disasterv1.BackupRecord)

	// 1. Load existing history
	if appBackup.Status.History == nil {
		appBackup.Status.History = []disasterv1.BackupRecord{}
	}
	for _, rec := range appBackup.Status.History {
		recordMap[rec.Name] = rec
	}

	// 2. Update or add from current observations
	observedNames := make(map[string]bool)
	for _, b := range backups {
		observedNames[b.Name] = true
		rec, exists := recordMap[b.Name]
		if !exists {
			// New backup found (e.g. from Schedule)
			rec = disasterv1.BackupRecord{
				Name: b.Name,
			}
		}
		if rec.ManagedStatus == disasterv1.LastBackupStatusFailed &&
			rec.Phase == BackupPhaseTimeoutFailed &&
			isVeleroBackupRunning(b.Status.Phase) {
			recordMap[b.Name] = rec
			continue
		}

		// Check for terminal phase transition
		oldPhase := rec.Phase
		newPhase := string(b.Status.Phase)

		// Update fields from observation
		normalizedStart := normalizedBackupStartTimestamp(&b)
		rec.Phase = newPhase
		rec.StartTimestamp = normalizedStart
		rec.CompletionTimestamp = b.Status.CompletionTimestamp
		rec.Errors = b.Status.Errors
		rec.Warnings = b.Status.Warnings
		rec.Expiration = b.Status.Expiration
		rec.VeleroStatus = b.Status.DeepCopy() // Populate detailed status

		// Update ManagedStatus if not explicitly Canceled
		if rec.ManagedStatus != disasterv1.LastBackupStatusCanceled {
			rec.ManagedStatus = getManagedStatus(b.Status.Phase)
		}

		// 备份进度事件：进入 InProgress
		if newPhase == string(velerov1.BackupPhaseInProgress) && oldPhase != string(velerov1.BackupPhaseInProgress) {
			user := appBackup.Annotations[AnnotationUser]
			if user == "" {
				user = "system"
			}
			triggeredBy := appBackup.Annotations[AnnotationTraceID]
			taskName := fmt.Sprintf("应用备份 %s 执行备份 %s", appBackup.Name, b.Name)
			helper.ReportTaskProgressWithClient(ctx, r.Client, r.Scheme, appBackup, taskName, appBackup.Spec.Cluster, user, triggeredBy, "Velero 备份正在执行中 (InProgress)...")
		}

		// 备份完成事件（当达到终态时发射）
		if helper.IsTerminalPhase(newPhase) && !helper.IsTerminalPhase(oldPhase) {
			user := appBackup.Annotations[AnnotationUser]
			if user == "" {
				user = "system"
			}
			triggeredBy := appBackup.Annotations[AnnotationTraceID]
			taskName := fmt.Sprintf("应用备份 %s 执行备份 %s", appBackup.Name, b.Name)
			status := helper.TaskStatusSuccess
			extraMsg := "备份成功"
			errorCode := ""
			if newPhase == string(velerov1.BackupPhaseFailed) || newPhase == string(velerov1.BackupPhasePartiallyFailed) {
				status = helper.TaskStatusFailed
				if newPhase == string(velerov1.BackupPhasePartiallyFailed) {
					errorCode = appBackupReasonBackupPartiallyFailed
				} else {
					errorCode = appBackupReasonBackupFailed
				}
				extraMsg = buildBackupFailureMessage(&b)
				appBackup.Status.Reason = errorCode
				appBackup.Status.Message = extraMsg
			} else if appBackup.Status.Reason == appBackupReasonBackupFailed ||
				appBackup.Status.Reason == appBackupReasonBackupPartiallyFailed ||
				appBackup.Status.Reason == appBackupReasonTimeoutExceeded {
				// Latest terminal run is successful, clear stale backup-level error details.
				appBackup.Status.Reason = ""
				appBackup.Status.Message = ""
			}
			if status == helper.TaskStatusFailed {
				helper.ReportTaskFinishedWithClient(ctx, r.Client, r.Scheme, appBackup, taskName, appBackup.Spec.Cluster, status, normalizedStart, b.Status.CompletionTimestamp, user, triggeredBy, extraMsg, errorCode)
			} else {
				helper.ReportTaskFinishedWithClient(ctx, r.Client, r.Scheme, appBackup, taskName, appBackup.Spec.Cluster, status, normalizedStart, b.Status.CompletionTimestamp, user, triggeredBy, extraMsg)
			}
		}

		recordMap[b.Name] = rec
	}

	// 3. Handle missing records (Backups deleted from Velero)
	// If a record exists in history but not in the observed list, it means the backup has been deleted.
	// We should retain it ONLY if it was explicitly marked as Canceled by us (since we want to show cancel history).
	// Otherwise, if it's gone from the cluster, it should be removed from our history to reflect the current state.
	for name, rec := range recordMap {
		if !observedNames[name] {
			// Record exists in history but not in cluster
			if rec.ManagedStatus != disasterv1.LastBackupStatusCanceled &&
				rec.ManagedStatus != disasterv1.LastBackupStatusFailed {
				// Backup is gone and wasn't canceled/failed -> Remove it from history
				delete(recordMap, name)
			}
		}
	}

	// 4. Convert map to slice
	newHistory := make([]disasterv1.BackupRecord, 0, len(recordMap))
	for _, rec := range recordMap {
		newHistory = append(newHistory, rec)
	}

	// 5. Sort by StartTimestamp descending
	sort.Slice(newHistory, func(i, j int) bool {
		t1 := newHistory[i].StartTimestamp
		t2 := newHistory[j].StartTimestamp
		if t1 == nil {
			return false
		}
		if t2 == nil {
			return true
		}
		return t1.Time.After(t2.Time)
	})

	appBackup.Status.History = newHistory
	appBackup.Status.TotalBackups = len(newHistory)

	// 6. Determine LatestBackupStatus from History
	if len(appBackup.Status.History) > 0 {
		appBackup.Status.LatestBackupStatus = appBackup.Status.History[0].ManagedStatus
	} else {
		appBackup.Status.LatestBackupStatus = ""
	}

	if latestBackup != nil {
		appBackup.Status.BackupStatus = latestBackup.Status
	} else {
		appBackup.Status.BackupStatus = velerov1.BackupStatus{}
	}

	// Migration: specific logic to ensure existing resources have HasRunInitialBackup=true
	if (len(backups) > 0 || len(appBackup.Status.History) > 0) && !appBackup.Status.HasRunInitialBackup {
		appBackup.Status.HasRunInitialBackup = true
	}
}

func getManagedStatus(phase velerov1.BackupPhase) string {
	switch phase {
	case "":
		return disasterv1.LastBackupStatusInProgress
	case velerov1.BackupPhaseNew, velerov1.BackupPhaseInProgress, velerov1.BackupPhaseWaitingForPluginOperations, velerov1.BackupPhaseFinalizing:
		return disasterv1.LastBackupStatusInProgress
	case velerov1.BackupPhaseCompleted:
		return disasterv1.LastBackupStatusCompleted
	case velerov1.BackupPhaseFailed, velerov1.BackupPhasePartiallyFailed, velerov1.BackupPhaseFailedValidation:
		return disasterv1.LastBackupStatusFailed
	case velerov1.BackupPhaseDeleting:
		return disasterv1.LastBackupStatusCanceled
	default:
		return string(phase)
	}
}

func buildBackupFailureMessage(backup *velerov1.Backup) string {
	if backup == nil {
		return "备份失败"
	}
	if backup.Status.FailureReason != "" {
		return fmt.Sprintf("备份失败: %s", backup.Status.FailureReason)
	}
	if backup.Status.Errors > 0 {
		return fmt.Sprintf("备份失败: errors=%d warnings=%d", backup.Status.Errors, backup.Status.Warnings)
	}
	return "备份失败"
}

func backupTimeoutMessage(backup *velerov1.Backup, timeout time.Duration) string {
	if backup != nil && backup.Status.Phase == "" {
		return fmt.Sprintf("Backup %s did not report a Velero phase within %s", backup.Name, timeout)
	}
	if backup != nil {
		return fmt.Sprintf("Backup %s exceeded timeout of %s", backup.Name, timeout)
	}
	return fmt.Sprintf("Backup exceeded timeout of %s", timeout)
}

func markBackupTimeoutFailed(appBackup *disasterv1.AppBackup, backup *velerov1.Backup, message string) {
	if appBackup == nil || backup == nil {
		return
	}
	now := metav1.Now()
	start := normalizedBackupStartTimestamp(backup)
	found := false
	for i := range appBackup.Status.History {
		if appBackup.Status.History[i].Name == backup.Name {
			appBackup.Status.History[i].ManagedStatus = disasterv1.LastBackupStatusFailed
			appBackup.Status.History[i].Phase = BackupPhaseTimeoutFailed
			appBackup.Status.History[i].CompletionTimestamp = &now
			appBackup.Status.History[i].VeleroStatus = backup.Status.DeepCopy()
			if appBackup.Status.History[i].StartTimestamp == nil {
				appBackup.Status.History[i].StartTimestamp = start
			}
			found = true
			break
		}
	}
	if !found {
		appBackup.Status.History = append([]disasterv1.BackupRecord{{
			Name:                backup.Name,
			Phase:               BackupPhaseTimeoutFailed,
			ManagedStatus:       disasterv1.LastBackupStatusFailed,
			StartTimestamp:      start,
			CompletionTimestamp: &now,
			VeleroStatus:        backup.Status.DeepCopy(),
		}}, appBackup.Status.History...)
	}
	appBackup.Status.LatestBackupStatus = disasterv1.LastBackupStatusFailed
	appBackup.Status.Reason = appBackupReasonTimeoutExceeded
	appBackup.Status.Message = message
	appBackup.Status.BackupStatus = backup.Status
}

func applyAutoBackupPolicy(appBackup *disasterv1.AppBackup, policy *disasterv1.DisasterPolicy) {
	if appBackup == nil || policy == nil || policy.Spec.Type != disasterv1.PolicyTypeAutoBackup {
		return
	}
	appBackup.Spec.Schedule = policy.Spec.Schedule //使用策略配置的调度表达式
	if policy.Spec.TTL != nil {
		appBackup.Spec.Template.TTL = *policy.Spec.TTL
	}
	manualPaused, hasManualPausedAnnotation := appBackupManualPaused(appBackup, policy)
	policyPaused := policy.Spec.State == disasterv1.PolicyStateDisabled
	if !hasManualPausedAnnotation {
		if manualPaused {
			setAppBackupManualPausedAnnotation(appBackup, true)
		} else if policyPaused {
			setAppBackupManualPausedAnnotation(appBackup, false)
		}
	}
	appBackup.Spec.Paused = manualPaused || policyPaused
	if appBackup.Labels == nil {
		appBackup.Labels = make(map[string]string)
	}
	appBackup.Labels[LabelDisasterPolicyUID] = string(policy.UID)
}

func appBackupManualPaused(appBackup *disasterv1.AppBackup, policy *disasterv1.DisasterPolicy) (bool, bool) {
	if appBackup == nil {
		return false, false
	}
	if appBackup.Annotations != nil {
		if raw, ok := appBackup.Annotations[AnnotationAppBackupManualPaused]; ok {
			paused, err := strconv.ParseBool(raw)
			if err == nil {
				return paused, true
			}
		}
	}
	if policy != nil && policy.Spec.State == disasterv1.PolicyStateDisabled {
		return false, false
	}
	return appBackup.Spec.Paused, false
}

func setAppBackupManualPausedAnnotation(appBackup *disasterv1.AppBackup, paused bool) {
	if appBackup.Annotations == nil {
		appBackup.Annotations = make(map[string]string)
	}
	appBackup.Annotations[AnnotationAppBackupManualPaused] = strconv.FormatBool(paused)
}

// ensureBSL ensures that the BackupStorageLocation exists on the target cluster
func (h *ReadyHandler) ensureBSL(ctx context.Context, r *AppBackupReconciler, cli client.Client, appBackup *disasterv1.AppBackup) (string, error) {
	logger := logf.FromContext(ctx)
	sr := &disasterv1.StorageRepository{}
	// StorageRepository is always in disaster-system
	err := r.Get(ctx, types.NamespacedName{Name: appBackup.Spec.Template.StorageLocation, Namespace: ManagementNamespace()}, sr)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			r.Recorder.Event(appBackup, corev1.EventTypeWarning, "StorageRepositoryNotFound", "StorageRepository not found")
			return "", err
		}
		return "", err
	}

	var defaultBSL DefaultBSL
	bslName := sr.Name + "-" + appBackup.Spec.Cluster
	err = defaultBSL.ApplyStorageRepository(ctx, r.Client, cli, sr, bslName, appBackup.Spec.Cluster)
	if err != nil {
		if err.Error() == fmt.Sprintf("BackupStorageLocation %s is in Unavailable status", bslName) {
			logger.Info("BackupStorageLocation is unavailable", "bslName", bslName)
			r.Recorder.Event(appBackup, corev1.EventTypeWarning, "BSLUnavailable", "BackupStorageLocation is unavailable")
		}
		// If BSL is unavailable, we return error so the controller requeues and retries
		return "", err
	}
	return bslName, nil
}
