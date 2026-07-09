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

package resourcesync

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	ctrlpkg "github.com/softcdata/testudo-operator/internal/controller"
	"github.com/softcdata/testudo-operator/internal/controller/imagemapping"
	restorebuilder "github.com/softcdata/testudo-operator/internal/controller/restore"
	runtimecfg "github.com/softcdata/testudo-operator/internal/controller/runtimeconfig"
	"github.com/softcdata/testudo-operator/internal/controller/scheduler"
	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	"github.com/softcdata/testudo-operator/pkg/helper"
	"github.com/softcdata/testudo-operator/pkg/metadata"
)

const (
	resourceSyncFinalizer = "testudo.softcdata.com/resourcesync-finalizer"
	AnnotationTraceID     = metadata.AnnotationTraceID
	TraceIDKey            = metadata.TraceIDKey

	resourceSyncReasonBackupFailed           = "BackupFailed"
	resourceSyncReasonBuildRestoreSpecFailed = "BuildRestoreSpecFailed"
	resourceSyncReasonRestoreFailed          = "RestoreFailed"
	resourceSyncReasonDependencyFailed       = "DependencyFailed"
	resourceSyncReasonStorageUnavailable     = "StorageUnavailable"
)

// ResourceSyncReconciler 负责调谐 ResourceSync 对象
type ResourceSyncReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	Log           logr.Logger
	Recorder      record.EventRecorder
	Scheduler     *scheduler.SyncScheduler
	ClientFactory func(config *rest.Config, options client.Options) (client.Client, error)
}

func resourceSyncAuditContext(rs *disasterv1.ResourceSync) (user, traceID string) {
	if rs == nil {
		return "system", "-"
	}
	user = rs.Annotations[metadata.AnnotationUser]
	if user == "" {
		user = "system"
	}
	traceID = rs.Annotations[metadata.AnnotationLastTraceID]
	if traceID == "" {
		traceID = rs.Annotations[metadata.AnnotationTraceID]
	}
	if traceID == "" {
		traceID = "-"
	}
	return user, traceID
}

func resourceSyncTaskName(rs *disasterv1.ResourceSync) string {
	if rs == nil {
		return "执行资源同步"
	}
	return fmt.Sprintf("执行资源同步 %s", rs.Name)
}

func backupStartedForResourceSyncRun(start, lastSync *metav1.Time) bool {
	if start == nil {
		return false
	}
	if lastSync != nil {
		return start.Time.After(lastSync.Time)
	}
	return false
}

func (r *ResourceSyncReconciler) reportResourceSyncStarted(ctx context.Context, rs *disasterv1.ResourceSync, cluster, msg string) {
	user, traceID := resourceSyncAuditContext(rs)
	helper.ReportTaskStartedWithClient(ctx, r.Client, r.Scheme, rs, resourceSyncTaskName(rs), cluster, user, traceID, msg)
}

func (r *ResourceSyncReconciler) reportResourceSyncProgress(ctx context.Context, rs *disasterv1.ResourceSync, cluster, msg string) {
	user, traceID := resourceSyncAuditContext(rs)
	helper.ReportTaskProgressWithClient(ctx, r.Client, r.Scheme, rs, resourceSyncTaskName(rs), cluster, user, traceID, msg)
}

func (r *ResourceSyncReconciler) reportResourceSyncFinished(ctx context.Context, rs *disasterv1.ResourceSync, cluster, status, msg string, errorCode ...string) {
	user, traceID := resourceSyncAuditContext(rs)
	now := metav1.Now()
	helper.ReportTaskFinishedWithClient(ctx, r.Client, r.Scheme, rs, resourceSyncTaskName(rs), cluster, status, nil, &now, user, traceID, msg, errorCode...)
}

func (r *ResourceSyncReconciler) failResourceSync(ctx context.Context, rs *disasterv1.ResourceSync, clusterPair, reason, msg string) (ctrl.Result, error) {
	if clusterPair == "" {
		clusterPair = "-"
	}
	if err := r.updateResourceSyncStatusWithRetry(ctx, rs, func(latest *disasterv1.ResourceSync) bool {
		now := metav1.Now()
		latest.Status.State = disasterv1.ResourceSyncStateFailed
		latest.Status.LastSyncTime = &now
		helper.SetStatusError(&latest.Status, reason, msg)
		helper.SetConditionError(&latest.Status.Conditions, "SyncFailed", reason, msg)
		return true
	}); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.syncStatistics(ctx, rs); err != nil {
		r.Log.Error(err, "Failed to sync statistics (Failed)")
	}
	r.reportResourceSyncFinished(ctx, rs, clusterPair, helper.TaskStatusFailed, msg, reason)
	return ctrl.Result{}, nil
}

func (r *ResourceSyncReconciler) ensureStorageRepositoryReady(ctx context.Context, namespace, storageName string) error {
	if storageName == "" {
		return nil
	}

	sr := &disasterv1.StorageRepository{}
	managementNamespace := ctrlpkg.ManagementNamespace()
	systemKey := types.NamespacedName{Namespace: managementNamespace, Name: storageName}
	if err := r.Get(ctx, systemKey, sr); err != nil {
		if errors.IsNotFound(err) && namespace != "" && namespace != managementNamespace {
			fallbackKey := types.NamespacedName{Namespace: namespace, Name: storageName}
			if fallbackErr := r.Get(ctx, fallbackKey, sr); fallbackErr != nil {
				return fallbackErr
			}
		} else {
			return err
		}
	}

	if sr.Status.Status != disasterv1.StorageRepositoryStatusAvailable {
		if sr.Status.Message != "" {
			return fmt.Errorf("StorageRepository %s status is %s: %s", storageName, sr.Status.Status, sr.Status.Message)
		}
		return fmt.Errorf("StorageRepository %s status is %s", storageName, sr.Status.Status)
	}
	return nil
}

// +kubebuilder:rbac:groups=testudo.softcdata.com,resources=resourcesyncs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=testudo.softcdata.com,resources=resourcesyncs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=testudo.softcdata.com,resources=resourcesyncs/finalizers,verbs=update

// Reconcile 处理 ResourceSync 的调谐循环
func (r *ResourceSyncReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("resourcesync", req.NamespacedName)
	log.V(2).Info("正在调谐 ResourceSync")

	// 获取 ResourceSync
	resourceSync := &disasterv1.ResourceSync{}
	if err := r.Get(ctx, req.NamespacedName, resourceSync); err != nil {
		if errors.IsNotFound(err) {
			// 如果已删除，从调度器移除
			r.Scheduler.Remove(req.Namespace, req.Name)
			log.Info("ResourceSync 未找到，已从调度器移除")
			return ctrl.Result{}, nil
		}
		log.Error(err, "获取 ResourceSync 失败")
		return ctrl.Result{}, err
	}

	// 添加 TraceID 到日志和上下文，遵循全局 TraceID 规范
	// 优先读取 last-trace-id (由 DisasterOperation 触发时设置)，回退到 trace-id
	traceID := resourceSync.Annotations[metadata.AnnotationLastTraceID]
	if traceID == "" {
		traceID = resourceSync.Annotations[AnnotationTraceID]
	}
	if traceID != "" {
		log = log.WithValues(TraceIDKey, traceID)
		ctx = context.WithValue(ctx, TraceIDKey, traceID)
	}

	// 处理删除
	if !resourceSync.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(resourceSync, resourceSyncFinalizer) {
			r.reportResourceSyncStarted(ctx, resourceSync, "-", "开始删除 ResourceSync")
			r.Scheduler.Remove(req.Namespace, req.Name)
			log.Info("ResourceSync 正在删除，已从调度器移除")

			controllerutil.RemoveFinalizer(resourceSync, resourceSyncFinalizer)
			if err := r.Update(ctx, resourceSync); err != nil {
				r.reportResourceSyncFinished(ctx, resourceSync, "-", helper.TaskStatusFailed, fmt.Sprintf("删除 ResourceSync 失败: %v", err))
				return ctrl.Result{}, err
			}
			r.reportResourceSyncFinished(ctx, resourceSync, "-", helper.TaskStatusSuccess, "ResourceSync 删除完成")
		}
		return ctrl.Result{}, nil
	}

	// 添加 Finalizer
	if !controllerutil.ContainsFinalizer(resourceSync, resourceSyncFinalizer) {
		controllerutil.AddFinalizer(resourceSync, resourceSyncFinalizer)
		if err := r.Update(ctx, resourceSync); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Sync dependency labels
	if changed, err := r.syncDependencyLabels(ctx, resourceSync); err != nil {
		log.Error(err, "同步依赖标签失败")
		return ctrl.Result{}, err
	} else if changed {
		if err := r.Update(ctx, resourceSync); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// 初始化状态（如果为空）
	if resourceSync.Status.State == "" {
		if err := r.updateResourceSyncStatusWithRetry(ctx, resourceSync, func(latest *disasterv1.ResourceSync) bool {
			if latest.Status.State != "" {
				return false
			}
			latest.Status.State = disasterv1.ResourceSyncStateReady
			helper.ClearStatusError(&latest.Status)
			return true
		}); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// 注册/更新 cron 调度；如果自动调度未启用，则移除任何残留任务。
	if !resourceSync.Spec.Paused && resourceSync.Spec.Trigger.Schedule != "" {
		callback := func() {
			r.triggerSync(resourceSync.Namespace, resourceSync.Name)
		}
		if err := r.Scheduler.AddOrUpdate(resourceSync.Namespace, resourceSync.Name, resourceSync.Spec.Trigger.Schedule, callback); err != nil {
			log.Error(err, "注册 cron 调度失败", "schedule", resourceSync.Spec.Trigger.Schedule)
			r.Recorder.Eventf(resourceSync, "Warning", "ScheduleError", "注册调度失败: %v", err)
		}
	} else {
		r.Scheduler.Remove(resourceSync.Namespace, resourceSync.Name)
		if resourceSync.Spec.Paused {
			log.V(1).Info("ResourceSync 已暂停，从调度器移除")
		} else {
			log.V(1).Info("ResourceSync 未配置自动调度，从调度器移除")
		}
	}

	// 检查是否触发了手动同步
	if r.shouldSync(resourceSync) {
		return r.executeSync(ctx, log, resourceSync)
	}

	return ctrl.Result{}, nil
}

// triggerSync 由 cron 调度器调用以触发同步
func (r *ResourceSyncReconciler) triggerSync(namespace, name string) {
	log := r.Log.WithValues("resourcesync", namespace+"/"+name)
	log.Info("Cron 触发同步")

	ctx, cancel := context.WithTimeout(context.Background(), runtimecfg.SnapshotCurrent().SyncRuntime.SchedulerUpdateTimeout)
	defer cancel()

	resourceSync := &disasterv1.ResourceSync{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, resourceSync); err != nil {
		log.Error(err, "获取 ResourceSync 失败（cron 触发）")
		return
	}

	if resourceSync.Spec.Paused || resourceSync.Spec.Trigger.Schedule == "" {
		log.Info("ResourceSync 自动调度已关闭，忽略残留 cron 触发",
			"paused", resourceSync.Spec.Paused,
			"schedule", resourceSync.Spec.Trigger.Schedule,
		)
		return
	}

	if resourceSync.Status.State == disasterv1.ResourceSyncStateInProgress {
		log.Info("ResourceSync 已在执行中，跳过本次 cron 触发")
		return
	}

	// 设置手动触发时间为当前时间
	resourceSync.Spec.Trigger.Manual = time.Now().Format(time.RFC3339)
	if err := r.Update(ctx, resourceSync); err != nil {
		log.Error(err, "更新 ResourceSync 触发器失败")
		return
	}

	log.Info("手动触发器已更新，调谐将执行同步")
}

// shouldSync 检查是否应该执行同步
func (r *ResourceSyncReconciler) shouldSync(resourceSync *disasterv1.ResourceSync) bool {
	// 1. 优先检查手动触发 (允许在 Paused 状态下执行手动作业)
	if resourceSync.Spec.Trigger.Manual != "" {
		manualTime, err := time.Parse(time.RFC3339, resourceSync.Spec.Trigger.Manual)
		if err == nil {
			// 检查手动时间是否在上次同步时间之后
			if resourceSync.Status.LastSyncTime == nil || manualTime.After(resourceSync.Status.LastSyncTime.Time) {
				return true
			}
		}
	}

	// 2. 如果已在执行同步中，则无论是否暂停都继续推进状态流转
	if resourceSync.Status.State == disasterv1.ResourceSyncStateInProgress {
		return true
	}

	// 3. 如果已暂停且不在进行中，则不执行任何自动同步
	if resourceSync.Spec.Paused {
		return false
	}

	// 4. 检查是否需要补执行自动同步（首次同步或漏执行）
	if resourceSync.Status.LastSyncTime == nil {
		return true
	}

	return false
}

// executeSync 执行资源同步
func (r *ResourceSyncReconciler) executeSync(ctx context.Context, log logr.Logger, resourceSync *disasterv1.ResourceSync) (ctrl.Result, error) {
	// 1. 获取依赖
	instance := &disasterv1.DisasterInstance{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: resourceSync.Namespace, Name: resourceSync.Spec.Instance}, instance); err != nil {
		msg := fmt.Sprintf("获取 DisasterInstance %s 失败: %v", resourceSync.Spec.Instance, err)
		log.Error(err, "获取 DisasterInstance 失败")
		return r.failResourceSync(ctx, resourceSync, "-", resourceSyncReasonDependencyFailed, msg)
	}

	config := &disasterv1.DisasterConfig{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: instance.Namespace, Name: instance.Spec.Config}, config); err != nil {
		msg := fmt.Sprintf("获取 DisasterConfig %s 失败: %v", instance.Spec.Config, err)
		log.Error(err, "获取 DisasterConfig 失败")
		return r.failResourceSync(ctx, resourceSync, "-", resourceSyncReasonDependencyFailed, msg)
	}
	currentSource, currentTarget := resolveClusters(instance, config)
	clusterPair := fmt.Sprintf("%s->%s", currentSource, currentTarget)
	if err := r.ensureStorageRepositoryReady(ctx, resourceSync.Namespace, config.Spec.StorageRepository); err != nil {
		msg := fmt.Sprintf("StorageRepository %s 不可用: %v", config.Spec.StorageRepository, err)
		log.Error(err, "StorageRepository 不可用", "storageRepository", config.Spec.StorageRepository)
		return r.failResourceSync(ctx, resourceSync, clusterPair, resourceSyncReasonStorageUnavailable, msg)
	}

	// 更新状态为 InProgress
	if resourceSync.Status.State != disasterv1.ResourceSyncStateInProgress {
		// 1.5 记录原始副本数 (Before Backup)
		if err := r.recordReplicasToConfigMap(ctx, instance, config, resourceSync); err != nil {
			log.Error(err, "Failed to record original replicas")
			// Continue? Or fail? Best to fail to ensure data safety.
			// return ctrl.Result{}, err
			// Warning only for now to not block backup if source inaccessible (e.g. disaster already happened)?
			// If source inaccessible, we can't record. We rely on OLD record.
			// So we log error and continue.
		}

		if err := r.updateResourceSyncStatusWithRetry(ctx, resourceSync, func(latest *disasterv1.ResourceSync) bool {
			if latest.Status.State == disasterv1.ResourceSyncStateInProgress {
				return false
			}
			latest.Status.State = disasterv1.ResourceSyncStateInProgress
			latest.Status.LastBackupName = ""
			latest.Status.LastRestoreName = ""
			latest.Status.LastClusterRestoreName = ""
			latest.Status.ClusterRestoreStatus = ""
			latest.Status.LastNamespaceRestoreName = ""
			latest.Status.NamespaceRestoreStatus = ""
			helper.ClearStatusError(&latest.Status)
			return true
		}); err != nil {
			return ctrl.Result{}, err
		}

		// Update Stats to InProgress
		if err := r.syncStatistics(ctx, resourceSync); err != nil {
			log.Error(err, "Failed to sync statistics (InProgress)")
		}
		r.reportResourceSyncStarted(ctx, resourceSync, clusterPair, "资源同步已开始")

		// 立即重新排队
		return ctrl.Result{Requeue: true}, nil
	}

	// 2. 检查或触发 AppBackup
	appBackupName := fmt.Sprintf("rs-%s", resourceSync.Name)
	appBackup := &disasterv1.AppBackup{}
	err := r.Get(ctx, types.NamespacedName{Namespace: resourceSync.Namespace, Name: appBackupName}, appBackup)

	if errors.IsNotFound(err) {
		// 不存在，创建新的长期 AppBackup
		newBackup := &disasterv1.AppBackup{
			ObjectMeta: metav1.ObjectMeta{
				Name:      appBackupName,
				Namespace: resourceSync.Namespace,
				Labels: map[string]string{
					metadata.LabelAppResourceOrigin:    metadata.AppResourceOriginDisasterInstance,
					metadata.LabelAppResourceOwnerKind: metadata.AppResourceOwnerKindResourceSync,
					metadata.LabelAppResourceOwnerName: resourceSync.Name,
				},
			},
			Spec: r.buildAppBackupSpec(instance, config),
		}
		// newBackup.Spec.Schedule = "@manual" // 移除：使用空 Schedule
		newBackup.Spec.Action = &disasterv1.BackupAction{
			Type:      "Backup",
			RequestAt: metav1.Now(),
		}

		if err := controllerutil.SetControllerReference(resourceSync, newBackup, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}

		log.Info("创建 ResourceSync AppBackup", "name", appBackupName)
		if err := r.Create(ctx, newBackup); err != nil {
			return ctrl.Result{}, err
		}
		r.reportResourceSyncProgress(ctx, resourceSync, clusterPair, fmt.Sprintf("已创建 AppBackup %s，等待生成 Velero Backup", appBackupName))
		return ctrl.Result{Requeue: true}, nil
	} else if err != nil {
		msg := fmt.Sprintf("获取 AppBackup %s 失败: %v", appBackupName, err)
		log.Error(err, "获取 AppBackup 失败", "appBackup", appBackupName)
		return r.failResourceSync(ctx, resourceSync, clusterPair, resourceSyncReasonDependencyFailed, msg)
	}

	// AppBackup 存在
	// 关键修复：检查 AppBackup 的 Cluster 是否正确（反向保护场景下，Source 可能已改变）
	newBackupSpec := r.buildAppBackupSpec(instance, config)
	if ctrlpkg.AppBackupSpecNeedsUpdate(appBackup.Spec, newBackupSpec) {
		log.Info("更新 ResourceSync AppBackup 模板", "oldCluster", appBackup.Spec.Cluster, "newCluster", newBackupSpec.Cluster)
		appBackup.Spec.Cluster = newBackupSpec.Cluster
		appBackup.Spec.Template = newBackupSpec.Template
		appBackup.Spec.Timeout = newBackupSpec.Timeout
		if err := r.Update(ctx, appBackup); err != nil {
			return ctrl.Result{}, err
		}
		// 更新后重新排队，确保使用最新配置
		return ctrl.Result{Requeue: true}, nil
	}

	if resourceSync.Status.LastBackupName == "" {
		if backupName, ok := ctrlpkg.CurrentBackupActionVeleroBackupName(appBackupName, appBackup, resourceSync.Status.LastSyncTime); ok {
			if rec, found := ctrlpkg.FindBackupRecordByName(appBackup.Status.History, backupName); found {
				log.Info("找到本次 Action 生成的 Velero Backup", "name", rec.Name)
				backupName := rec.Name
				if err := r.updateResourceSyncStatusWithRetry(ctx, resourceSync, func(latest *disasterv1.ResourceSync) bool {
					if latest.Status.LastBackupName == backupName {
						return false
					}
					latest.Status.LastBackupName = backupName
					return true
				}); err != nil {
					return ctrl.Result{}, err
				}
				return ctrl.Result{Requeue: true}, nil
			}
			if appBackup.Status.Status == "Failed" {
				failMsg := fmt.Sprintf("AppBackup %s 失败: %s", appBackupName, appBackup.Status.Message)
				if appBackup.Status.Message == "" {
					failMsg = fmt.Sprintf("AppBackup %s 失败", appBackupName)
				}
				helper.SetConditionError(&resourceSync.Status.Conditions, "BackupFailed", resourceSyncReasonBackupFailed, failMsg)
				return r.failResourceSync(ctx, resourceSync, clusterPair, resourceSyncReasonBackupFailed, failMsg)
			}
			return ctrl.Result{RequeueAfter: runtimecfg.SnapshotCurrent().SyncRuntime.BackupObserveRequeue}, nil
		}

		// 1. 兼容旧状态：没有可关联的 Backup Action 时，只按上次完成时间之后的历史记录兜底。
		for _, rec := range appBackup.Status.History {
			if backupStartedForResourceSyncRun(rec.StartTimestamp, resourceSync.Status.LastSyncTime) {
				log.Info("找到新生成的 Velero Backup", "name", rec.Name)
				backupName := rec.Name
				if err := r.updateResourceSyncStatusWithRetry(ctx, resourceSync, func(latest *disasterv1.ResourceSync) bool {
					if latest.Status.LastBackupName == backupName {
						return false
					}
					latest.Status.LastBackupName = backupName
					return true
				}); err != nil {
					return ctrl.Result{}, err
				}
				return ctrl.Result{Requeue: true}, nil
			}
		}

		// 2. Check Action
		if appBackup.Status.Status == "Failed" {
			failMsg := fmt.Sprintf("AppBackup %s 失败: %s", appBackupName, appBackup.Status.Message)
			if appBackup.Status.Message == "" {
				failMsg = fmt.Sprintf("AppBackup %s 失败", appBackupName)
			}
			helper.SetConditionError(&resourceSync.Status.Conditions, "BackupFailed", resourceSyncReasonBackupFailed, failMsg)
			return r.failResourceSync(ctx, resourceSync, clusterPair, resourceSyncReasonBackupFailed, failMsg)
		}
		// 3. Trigger Action
		log.Info("触发新备份 Action", "appBackup", appBackupName)
		appBackup.Spec.Template = newBackupSpec.Template
		appBackup.Spec.Cluster = newBackupSpec.Cluster
		appBackup.Spec.Action = &disasterv1.BackupAction{
			Type:      "Backup",
			RequestAt: metav1.Now(),
		}
		if err := r.Update(ctx, appBackup); err != nil {
			return ctrl.Result{}, err
		}
		r.reportResourceSyncProgress(ctx, resourceSync, clusterPair, fmt.Sprintf("已触发 AppBackup %s 备份动作", appBackupName))
		return ctrl.Result{Requeue: true}, nil
	} else {
		// LastBackupName 有值，检查状态
		// 注意：Velero Backup 在源集群，我们不能直接 Get。
		// 必须通过 AppBackup CR 的 Status 来判断。

		// 重新获取最新的 AppBackup
		if err := r.Get(ctx, types.NamespacedName{Namespace: resourceSync.Namespace, Name: appBackupName}, appBackup); err != nil {
			return ctrl.Result{}, err
		}

		backupName := resourceSync.Status.LastBackupName
		backupStatus := "InProgress"

		// 在 History 中查找
		found := false
		var backupRecord disasterv1.BackupRecord
		for _, rec := range appBackup.Status.History {
			if rec.Name == backupName {
				backupRecord = rec
				backupStatus = rec.Phase
				found = true
				break
			}
		}

		if !found {
			return ctrl.Result{RequeueAfter: runtimecfg.SnapshotCurrent().SyncRuntime.HistoryMissingRequeue}, nil
		}

		if backupStatus == string(velerov1.BackupPhaseCompleted) {
			r.reportResourceSyncProgress(ctx, resourceSync, clusterPair, fmt.Sprintf("Velero Backup %s 已完成，开始资源恢复", backupName))
			return r.handleRestore(ctx, log, resourceSync, config, instance, backupName)
		} else if ctrlpkg.BackupRecordFailed(backupRecord) {
			backupStatus = ctrlpkg.BackupRecordFailureStatus(backupRecord)
			log.Info("Velero Backup 失败", "name", backupName, "status", backupStatus)
			failMsg := fmt.Sprintf("Velero Backup %s 失败: %s", backupName, backupStatus)
			if appBackup.Status.Message != "" {
				failMsg = fmt.Sprintf("Velero Backup %s 失败: %s", backupName, appBackup.Status.Message)
			}
			if err := r.updateResourceSyncStatusWithRetry(ctx, resourceSync, func(latest *disasterv1.ResourceSync) bool {
				latest.Status.State = disasterv1.ResourceSyncStateFailed
				now := metav1.Now()
				latest.Status.LastSyncTime = &now
				helper.SetStatusError(&latest.Status, resourceSyncReasonBackupFailed, failMsg)
				helper.SetConditionError(&latest.Status.Conditions, "BackupFailed", resourceSyncReasonBackupFailed, failMsg)
				return true
			}); err != nil {
				return ctrl.Result{}, err
			}
			r.reportResourceSyncFinished(ctx, resourceSync, clusterPair, helper.TaskStatusFailed, failMsg, resourceSyncReasonBackupFailed)
			return ctrl.Result{}, nil
		}

		// InProgress
		return ctrl.Result{RequeueAfter: runtimecfg.SnapshotCurrent().SyncRuntime.BackupInProgressRequeue}, nil
	}

}

func (r *ResourceSyncReconciler) syncDependencyLabels(ctx context.Context, resourceSync *disasterv1.ResourceSync) (bool, error) {
	if resourceSync.Labels == nil {
		resourceSync.Labels = make(map[string]string)
	}
	_, _, tokenChanged := metadata.EnsureDependencyTokenLabel(resourceSync.Labels, string(resourceSync.UID))
	edges := make([]metadata.DependencyEdge, 0, 1)

	if resourceSync.Spec.Instance != "" {
		instance := &disasterv1.DisasterInstance{}
		if err := r.Get(ctx, client.ObjectKey{Namespace: resourceSync.Namespace, Name: resourceSync.Spec.Instance}, instance); err == nil {
			edges = append(edges, metadata.DependencyEdge{
				TargetToken:  metadata.BuildDependencyToken(string(instance.UID)),
				RelationCode: "spec.instance",
			})
		}
	}

	_, depChanged := metadata.RebuildDependencyToLabels(resourceSync.Labels, edges)
	return tokenChanged || depChanged, nil
}

// handleRestore 处理恢复阶段
func (r *ResourceSyncReconciler) handleRestore(ctx context.Context, log logr.Logger, resourceSync *disasterv1.ResourceSync, config *disasterv1.DisasterConfig, instance *disasterv1.DisasterInstance, backupName string) (ctrl.Result, error) {
	sourceCluster, targetCluster := resolveClusters(instance, config)
	clusterPair := fmt.Sprintf("%s->%s", sourceCluster, targetCluster)
	phases := planResourceSyncRestorePhases(instance)
	if len(phases) == 0 {
		return r.finalizeRestoreResult(ctx, log, resourceSync, clusterPair, backupName, "", disasterv1.ResourceSyncStateReady, disasterv1.PhaseSucceeded, 0)
	}

	totalRestoreItems := 0
	for _, phase := range phases {
		restoreName := resourceSyncRestoreName(resourceSync.Name, backupName, phase)
		restore := &disasterv1.AppRestore{}
		if err := r.Get(ctx, types.NamespacedName{Namespace: resourceSync.Namespace, Name: restoreName}, restore); err != nil {
			if !errors.IsNotFound(err) {
				return ctrl.Result{}, err
			}

			var (
				restoreSpec    disasterv1.AppRestoreSpec
				policySummary  restorebuilder.PolicySummary
				specErr        error
				progressStatus disasterv1.AppRestorePhase
			)
			switch phase {
			case resourceSyncRestorePhaseCluster:
				restoreSpec, policySummary, specErr = r.buildClusterAppRestoreSpec(ctx, resourceSync, config, instance, backupName)
				progressStatus = disasterv1.PhasePending
			case resourceSyncRestorePhaseNamespace:
				restoreSpec, policySummary, specErr = r.buildAppRestoreSpec(ctx, resourceSync, config, instance, backupName)
				progressStatus = disasterv1.PhasePending
			default:
				restoreSpec, policySummary, specErr = r.buildAppRestoreSpec(ctx, resourceSync, config, instance, backupName)
				progressStatus = disasterv1.PhasePending
			}
			if specErr != nil {
				return r.failRestoreBuild(ctx, log, resourceSync, clusterPair, backupName, restoreName, specErr, totalRestoreItems)
			}

			newRestore := &disasterv1.AppRestore{
				ObjectMeta: metav1.ObjectMeta{
					Name:      restoreName,
					Namespace: resourceSync.Namespace,
					Labels: map[string]string{
						metadata.LabelAppResourceOrigin:    metadata.AppResourceOriginDisasterInstance,
						metadata.LabelAppResourceOwnerKind: metadata.AppResourceOwnerKindResourceSync,
						metadata.LabelAppResourceOwnerName: resourceSync.Name,
					},
				},
				Spec: restoreSpec,
			}
			restorebuilder.ApplyPolicySummaryAnnotations(&newRestore.ObjectMeta, policySummary)
			if err := controllerutil.SetControllerReference(resourceSync, newRestore, r.Scheme); err != nil {
				return ctrl.Result{}, err
			}

			log.Info("创建新的 AppRestore", "phase", phase, "name", restoreName)
			if err := r.Create(ctx, newRestore); err != nil {
				return ctrl.Result{}, err
			}
			r.reportResourceSyncProgress(ctx, resourceSync, clusterPair, restorebuilder.ModifierAuditMessage(policySummary))
			r.reportResourceSyncProgress(ctx, resourceSync, clusterPair, fmt.Sprintf("已创建 AppRestore %s，等待恢复完成", restoreName))

			if err := r.updateResourceSyncStatusWithRetry(ctx, resourceSync, func(latest *disasterv1.ResourceSync) bool {
				return updateResourceSyncRestorePhaseStatus(latest, phase, restoreName, progressStatus)
			}); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		}

		status := restore.Status.Status
		phaseChanged := updateResourceSyncRestorePhaseStatus(resourceSync, phase, restoreName, status)
		if status != disasterv1.PhaseSucceeded && !disasterv1.IsFailedAppRestorePhase(status) {
			if phaseChanged {
				if err := r.updateResourceSyncStatusWithRetry(ctx, resourceSync, func(latest *disasterv1.ResourceSync) bool {
					return updateResourceSyncRestorePhaseStatus(latest, phase, restoreName, status)
				}); err != nil {
					return ctrl.Result{}, err
				}
			}
			return ctrl.Result{RequeueAfter: runtimecfg.SnapshotCurrent().SyncRuntime.RestoreObserveRequeue}, nil
		}

		totalRestoreItems += lookupResourceSyncRestoreItems(restore)
		if disasterv1.IsFailedAppRestorePhase(status) {
			if phaseChanged {
				log.Info("AppRestore 失败", "phase", phase, "name", restoreName, "status", status)
			}
			return r.finalizeRestoreResult(ctx, log, resourceSync, clusterPair, backupName, restoreName, disasterv1.ResourceSyncStateFailed, status, totalRestoreItems)
		}

		if phaseChanged {
			log.Info("AppRestore 成功", "phase", phase, "name", restoreName)
			if err := r.updateResourceSyncStatusWithRetry(ctx, resourceSync, func(latest *disasterv1.ResourceSync) bool {
				return updateResourceSyncRestorePhaseStatus(latest, phase, restoreName, status)
			}); err != nil {
				return ctrl.Result{}, err
			}
		}
	}

	return r.finalizeRestoreResult(ctx, log, resourceSync, clusterPair, backupName, resourceSyncRestoreName(resourceSync.Name, backupName, phases[len(phases)-1]), disasterv1.ResourceSyncStateReady, disasterv1.PhaseSucceeded, totalRestoreItems)
}

// syncStatistics 更新关联的 BackupRestoreStatistics CR
func (r *ResourceSyncReconciler) syncStatistics(ctx context.Context, rs *disasterv1.ResourceSync) error {
	// 统计 History
	var total, completed, failed, inProgress int32
	if rs.Status.State == disasterv1.ResourceSyncStateInProgress {
		inProgress = 1
	}

	for _, h := range rs.Status.History {
		total++
		if h.Status == string(disasterv1.PhaseSucceeded) || h.Status == "Completed" {
			completed++
		} else {
			failed++
		}
	}

	// 查找或创建 Statistics CR
	// 使用与 AppBackup 相同的 ScopeUID 逻辑?
	// 不，用户说 "与 AppBackup 一样"，意味着 RS 应该作为 Owner。
	// 但是 AppBackup 的 Stats controller 是基于 AppBackup 自动创建的。
	// 这里我们手动维护一个 "app-rs-<name>-stats" ?
	// 简单起见，我们查找/创建名为 "rs-<name>-stats" 的对象。
	statsName := fmt.Sprintf("rs-%s-stats", rs.Name)
	stats := &disasterv1.BackupRestoreStatistics{
		ObjectMeta: metav1.ObjectMeta{
			Name:      statsName,
			Namespace: rs.Namespace,
		},
	}

	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, stats, func() error {
		stats.Labels = map[string]string{
			"testudo.softcdata.com/owner-kind": "ResourceSync",
			"disaster.io/scope-uid":            string(rs.UID),
		}
		stats.Spec.ScopeType = disasterv1.ScopeTypeApp // Unified to App
		stats.Spec.ScopeRef = disasterv1.ScopeReference{
			Kind:      "ResourceSync",
			Name:      rs.Name,
			Namespace: rs.Namespace,
			UID:       rs.UID,
		}
		return nil
	}); err != nil {
		return err
	}

	// Explicitly Update Status
	stats.Status.Statistics.Total = total
	stats.Status.Statistics.Completed = completed
	stats.Status.Statistics.Failed = failed
	stats.Status.Statistics.InProgress = inProgress
	now := metav1.Now()
	stats.Status.LastUpdateTime = &now

	if err := r.Status().Update(ctx, stats); err != nil {
		return err
	}
	return nil
}

// buildAppBackupSpec 构建 AppBackup Spec
func (r *ResourceSyncReconciler) buildAppBackupSpec(instance *disasterv1.DisasterInstance, config *disasterv1.DisasterConfig) disasterv1.AppBackupSpec {
	source, _ := resolveClusters(instance, config)
	falseVar := false
	spec := disasterv1.AppBackupSpec{
		Cluster: source,
		Timeout: ctrlpkg.ResolveAppBackupTimeout(instance),
		// DisasterPolicy: config.Spec.ResourceSyncPolicy, // V2 does not use V1 DisasterPolicy
		Template: velerov1.BackupSpec{
			IncludedNamespaces: instance.Spec.Namespaces,
			ExcludedNamespaces: []string{"velero", "kube-system"},                               // 排除系统命名空间
			ExcludedResources:  []string{"pods", "persistentvolumeclaims", "persistentvolumes"}, // PVC/PV 由 DataSync 负责
			LabelSelector:      instance.Spec.LabelSelector,
			StorageLocation:    config.Spec.StorageRepository,
			SnapshotVolumes:    &falseVar,
		},
	}
	applyScopedSelectionToResourceSyncBackupSpec(&spec.Template, resolveResourceSyncSelectionPlan(instance))
	return spec
}

func (r *ResourceSyncReconciler) buildImageRewriteRules(
	ctx context.Context,
	config *disasterv1.DisasterConfig,
	instance *disasterv1.DisasterInstance,
	sourceClusterName string,
	targetClusterName string,
) ([]disasterv1.ResourceModifierRule, error) {
	sourceCluster := &disasterv1.Cluster{}
	if err := r.Get(ctx, types.NamespacedName{Name: sourceClusterName}, sourceCluster); err != nil {
		return nil, fmt.Errorf("get source cluster %s: %w", sourceClusterName, err)
	}

	targetCluster := &disasterv1.Cluster{}
	if err := r.Get(ctx, types.NamespacedName{Name: targetClusterName}, targetCluster); err != nil {
		return nil, fmt.Errorf("get target cluster %s: %w", targetClusterName, err)
	}

	mappings, policy, enabled, err := imagemapping.ResolveRegistryMappings(
		sourceCluster,
		targetCluster,
		config.Spec.ImageRewrite,
		disasterv1.ImageRewriteApplyResourceSync,
	)
	if err != nil {
		return nil, err
	}
	if !enabled {
		return nil, nil
	}

	sourceClient, err := ctrlpkg.GetKubeClientSet(ctx, r.Client, r.Scheme, sourceClusterName)
	if err != nil {
		return nil, fmt.Errorf("build source cluster client for image rewrite: %w", err)
	}

	deployments, statefulSets, err := imagemapping.CollectWorkloads(
		ctx,
		sourceClient,
		instance.Spec.Namespaces,
		instance.Spec.LabelSelector,
	)
	if err != nil {
		return nil, err
	}

	rules, unmatched := imagemapping.BuildRulesFromWorkloads(deployments, statefulSets, mappings, policy)
	if policy == disasterv1.ImageRewriteUnmatchedPolicyFail && len(unmatched) > 0 {
		const limit = 5
		if len(unmatched) > limit {
			return nil, fmt.Errorf("image rewrite unmatched (%d): %s ...", len(unmatched), strings.Join(unmatched[:limit], "; "))
		}
		return nil, fmt.Errorf("image rewrite unmatched (%d): %s", len(unmatched), strings.Join(unmatched, "; "))
	}

	return rules, nil
}

// makeSkeletonModifiers 生成将 replicas 设置为 0 的规则
func (r *ResourceSyncReconciler) makeSkeletonModifiers(rs *disasterv1.ResourceSync) []disasterv1.ResourceModifierRule {
	patches := []disasterv1.JSONPatch{
		{
			Operation: "add", // 使用 add 操作强制覆盖或创建，比 replace 更稳健
			Path:      "/spec/replicas",
			Value:     "0",
		},
	}

	// 针对 Deployments 和 StatefulSets
	return []disasterv1.ResourceModifierRule{
		{
			Conditions: disasterv1.Conditions{GroupResource: "deployments.apps"},
			Patches:    patches,
		},
		{
			Conditions: disasterv1.Conditions{GroupResource: "statefulsets.apps"},
			Patches:    patches,
		},
	}
}

// recordReplicasToConfigMap 扫描源集群副本数并记录到 ConfigMap
func (r *ResourceSyncReconciler) recordReplicasToConfigMap(ctx context.Context, instance *disasterv1.DisasterInstance, config *disasterv1.DisasterConfig, rs *disasterv1.ResourceSync) error {
	sourceName := instance.Status.PrimaryCluster
	if sourceName == "" {
		sourceName = config.Spec.SourceCluster
	}

	remoteClient, err := ctrlpkg.GetKubeClientSet(ctx, r.Client, r.Scheme, sourceName)
	if err != nil {
		// Log warning but allow proceed
		return fmt.Errorf("failed to connect to source cluster %s: %w", sourceName, err)
	}

	replicasMap := make(map[string]int32)

	// Selector
	var selector client.ListOption
	if instance.Spec.LabelSelector != nil {
		ls, err := metav1.LabelSelectorAsSelector(instance.Spec.LabelSelector)
		if err != nil {
			return err
		}
		selector = client.MatchingLabelsSelector{Selector: ls}
	}

	for _, ns := range instance.Spec.Namespaces {
		opts := []client.ListOption{client.InNamespace(ns)}
		if selector != nil {
			opts = append(opts, selector)
		}

		// Deployments
		deployList := &appsv1.DeploymentList{}
		if err := remoteClient.List(ctx, deployList, opts...); err != nil {
			return err
		}
		for _, item := range deployList.Items {
			key := fmt.Sprintf("%s/deployments/%s", ns, item.Name)
			if item.Spec.Replicas != nil {
				replicasMap[key] = *item.Spec.Replicas
			} else {
				replicasMap[key] = 1 // Default
			}
		}

		// StatefulSets
		stsList := &appsv1.StatefulSetList{}
		if err := remoteClient.List(ctx, stsList, opts...); err != nil {
			return err
		}
		for _, item := range stsList.Items {
			key := fmt.Sprintf("%s/statefulsets/%s", ns, item.Name)
			if item.Spec.Replicas != nil {
				replicasMap[key] = *item.Spec.Replicas
			} else {
				replicasMap[key] = 1
			}
		}
	}

	// Serialize
	data, err := json.Marshal(replicasMap)
	if err != nil {
		return err
	}

	// Create/Update ConfigMap
	cmName := fmt.Sprintf("replicas-%s", rs.Name)
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cmName,
			Namespace: rs.Namespace,
		},
	}

	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, cm, func() error {
		if cm.Data == nil {
			cm.Data = make(map[string]string)
		}

		// ===== 关键修复: 保护已有的非零副本数记录 =====
		// 如果当前扫描到的副本数都是0 (Failover 场景)，但 ConfigMap 中有非零记录，
		// 则保留原记录，不覆盖
		existingData := cm.Data["replicas"]
		if existingData != "" {
			// 检查当前扫描的副本数是否全部为 0
			allCurrentZero := true
			for _, v := range replicasMap {
				if v > 0 {
					allCurrentZero = false
					break
				}
			}

			if allCurrentZero {
				// 当前扫描到的副本数都是0，检查是否有已保存的非零记录
				var existingMap map[string]int32
				if err := json.Unmarshal([]byte(existingData), &existingMap); err == nil {
					hasNonZero := false
					for _, v := range existingMap {
						if v > 0 {
							hasNonZero = true
							break
						}
					}
					if hasNonZero {
						// 保留已有的非零记录，不覆盖
						r.Log.Info("Preserving existing non-zero replicas in ConfigMap (current scan found only zeros)",
							"existingCount", len(existingMap), "currentCount", len(replicasMap))
						return nil
					}
				}
			}
		}

		cm.Data["replicas"] = string(data)

		// Set OwnerRef
		if err := controllerutil.SetControllerReference(rs, cm, r.Scheme); err != nil {
			return err
		}
		return nil
	})

	return err
}

// SetupWithManager 将控制器注册到 Manager
func (r *ResourceSyncReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&disasterv1.ResourceSync{}).
		Owns(&disasterv1.AppBackup{}).
		Owns(&disasterv1.AppRestore{}).
		Complete(r)
}

// resolveClusters 确定源集群和目标集群
// 如果 DisasterInstance 状态中定义了 Primary/Secondary，则优先使用（支持故障切换后的反向同步）
// 否则使用 DisasterConfig 中的静态配置
func resolveClusters(instance *disasterv1.DisasterInstance, config *disasterv1.DisasterConfig) (source, target string) {
	if instance.Status.PrimaryCluster != "" {
		return instance.Status.PrimaryCluster, instance.Status.SecondaryCluster
	}
	return config.Spec.SourceCluster, config.Spec.TargetCluster
}
