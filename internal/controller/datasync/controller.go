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

package datasync

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	ctrlcommon "github.com/softcdata/testudo-operator/internal/controller"
	restorebuilder "github.com/softcdata/testudo-operator/internal/controller/restore"
	"github.com/softcdata/testudo-operator/internal/controller/scheduler"
	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	"github.com/softcdata/testudo-operator/pkg/helper"
	"github.com/softcdata/testudo-operator/pkg/metadata"
)

const (
	dataSyncFinalizer = "testudo.softcdata.com/datasync-finalizer"
	AnnotationTraceID = metadata.AnnotationTraceID
	TraceIDKey        = metadata.TraceIDKey

	dataSyncReasonBackupFailed       = "BackupFailed"
	dataSyncReasonBuildRestoreFailed = "BuildRestoreSpecFailed"
	dataSyncReasonRestoreFailed      = "RestoreFailed"
	dataSyncReasonDependencyFailed   = "DependencyFailed"
	dataSyncReasonStorageUnavailable = "StorageUnavailable"
)

// DataSyncReconciler 负责调谐 DataSync 对象
type DataSyncReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	Log       logr.Logger
	Recorder  record.EventRecorder
	Scheduler *scheduler.SyncScheduler
}

func dataSyncAuditContext(ds *disasterv1.DataSync) (user, traceID string) {
	if ds == nil {
		return "system", "-"
	}
	user = ds.Annotations[metadata.AnnotationUser]
	if user == "" {
		user = "system"
	}
	traceID = ds.Annotations[metadata.AnnotationLastTraceID]
	if traceID == "" {
		traceID = ds.Annotations[metadata.AnnotationTraceID]
	}
	if traceID == "" {
		traceID = "-"
	}
	return user, traceID
}

func dataSyncTaskName(ds *disasterv1.DataSync) string {
	if ds == nil {
		return "执行数据同步"
	}
	return fmt.Sprintf("执行数据同步 %s", ds.Name)
}

func backupStartedForDataSyncRun(start, lastSync *metav1.Time) bool {
	if start == nil {
		return false
	}
	if lastSync != nil {
		return start.Time.After(lastSync.Time)
	}
	return false
}

func (r *DataSyncReconciler) reportDataSyncStarted(ctx context.Context, ds *disasterv1.DataSync, cluster, msg string) {
	user, traceID := dataSyncAuditContext(ds)
	helper.ReportTaskStartedWithClient(ctx, r.Client, r.Scheme, ds, dataSyncTaskName(ds), cluster, user, traceID, msg)
}

func (r *DataSyncReconciler) reportDataSyncProgress(ctx context.Context, ds *disasterv1.DataSync, cluster, msg string) {
	user, traceID := dataSyncAuditContext(ds)
	helper.ReportTaskProgressWithClient(ctx, r.Client, r.Scheme, ds, dataSyncTaskName(ds), cluster, user, traceID, msg)
}

func (r *DataSyncReconciler) reportDataSyncFinished(ctx context.Context, ds *disasterv1.DataSync, cluster, status, msg string, errorCode ...string) {
	user, traceID := dataSyncAuditContext(ds)
	now := metav1.Now()
	helper.ReportTaskFinishedWithClient(ctx, r.Client, r.Scheme, ds, dataSyncTaskName(ds), cluster, status, nil, &now, user, traceID, msg, errorCode...)
}

func (r *DataSyncReconciler) failDataSync(ctx context.Context, ds *disasterv1.DataSync, clusterPair, reason, msg string) (ctrl.Result, error) {
	if clusterPair == "" {
		clusterPair = "-"
	}
	now := metav1.Now()
	ds.Status.State = disasterv1.DataSyncStateFailed
	ds.Status.LastSyncTime = &now
	helper.SetStatusError(&ds.Status, reason, msg)
	helper.SetConditionError(&ds.Status.Conditions, "SyncFailed", reason, msg)
	if err := r.Status().Update(ctx, ds); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.syncStatistics(ctx, ds); err != nil {
		r.Log.Error(err, "Failed to sync statistics (Failed)")
	}
	r.reportDataSyncFinished(ctx, ds, clusterPair, helper.TaskStatusFailed, msg, reason)
	return ctrl.Result{}, nil
}

func (r *DataSyncReconciler) ensureStorageRepositoryReady(ctx context.Context, namespace, storageName string) error {
	if storageName == "" {
		return nil
	}

	sr := &disasterv1.StorageRepository{}
	managementNamespace := ctrlcommon.ManagementNamespace()
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

// +kubebuilder:rbac:groups=testudo.softcdata.com,resources=datasyncs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=testudo.softcdata.com,resources=datasyncs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=testudo.softcdata.com,resources=datasyncs/finalizers,verbs=update
// +kubebuilder:rbac:groups=testudo.softcdata.com,resources=disasterinstances,verbs=get;list;watch
// +kubebuilder:rbac:groups=testudo.softcdata.com,resources=disasterpolicies,verbs=get;list;watch

// Reconcile 处理 DataSync 的调谐循环
func (r *DataSyncReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("datasync", req.NamespacedName)
	log.V(1).Info("正在调谐 DataSync")

	// 获取 DataSync
	dataSync := &disasterv1.DataSync{}
	if err := r.Get(ctx, req.NamespacedName, dataSync); err != nil {
		if errors.IsNotFound(err) {
			// 如果已删除，从调度器移除
			r.Scheduler.Remove(req.Namespace, req.Name)
			log.Info("DataSync 未找到，已从调度器移除")
			return ctrl.Result{}, nil
		}
		log.Error(err, "获取 DataSync 失败")
		return ctrl.Result{}, err
	}

	// 添加 TraceID 到日志和上下文，遵循全局 TraceID 规范
	// 优先读取 last-trace-id (由 DisasterOperation 触发时设置)，回退到 trace-id
	traceID := dataSync.Annotations[metadata.AnnotationLastTraceID]
	if traceID == "" {
		traceID = dataSync.Annotations[AnnotationTraceID]
	}
	if traceID != "" {
		log = log.WithValues(TraceIDKey, traceID)
		ctx = context.WithValue(ctx, TraceIDKey, traceID)
	}

	// 处理删除
	if !dataSync.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(dataSync, dataSyncFinalizer) {
			r.reportDataSyncStarted(ctx, dataSync, "-", "开始删除 DataSync")
			r.Scheduler.Remove(req.Namespace, req.Name)
			log.Info("DataSync 正在删除，已从调度器移除")

			controllerutil.RemoveFinalizer(dataSync, dataSyncFinalizer)
			if err := r.Update(ctx, dataSync); err != nil {
				r.reportDataSyncFinished(ctx, dataSync, "-", helper.TaskStatusFailed, fmt.Sprintf("删除 DataSync 失败: %v", err))
				return ctrl.Result{}, err
			}
			r.reportDataSyncFinished(ctx, dataSync, "-", helper.TaskStatusSuccess, "DataSync 删除完成")
		}
		return ctrl.Result{}, nil
	}

	// 添加 Finalizer
	if !controllerutil.ContainsFinalizer(dataSync, dataSyncFinalizer) {
		controllerutil.AddFinalizer(dataSync, dataSyncFinalizer)
		if err := r.Update(ctx, dataSync); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Sync dependency labels
	if changed, err := r.syncDependencyLabels(ctx, dataSync); err != nil {
		log.Error(err, "同步依赖标签失败")
		return ctrl.Result{}, err
	} else if changed {
		if err := r.Update(ctx, dataSync); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// 初始化状态（如果为空）
	if dataSync.Status.State == "" {
		dataSync.Status.State = disasterv1.DataSyncStateReady
		helper.ClearStatusError(&dataSync.Status)
		if err := r.Status().Update(ctx, dataSync); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// 注册/更新 cron 调度；如果自动调度未启用，则移除任何残留任务。
	if !dataSync.Spec.Paused && dataSync.Spec.Trigger.Schedule != "" {
		callback := func() {
			r.triggerSync(dataSync.Namespace, dataSync.Name)
		}
		if err := r.Scheduler.AddOrUpdate(dataSync.Namespace, dataSync.Name, dataSync.Spec.Trigger.Schedule, callback); err != nil {
			log.Error(err, "注册 cron 调度失败", "schedule", dataSync.Spec.Trigger.Schedule)
			r.Recorder.Eventf(dataSync, "Warning", "ScheduleError", "注册调度失败: %v", err)
		} else {
			log.V(1).Info("cron 调度已注册", "schedule", dataSync.Spec.Trigger.Schedule)
		}
	} else {
		r.Scheduler.Remove(dataSync.Namespace, dataSync.Name)
		if dataSync.Spec.Paused {
			log.V(1).Info("DataSync 已暂停，从调度器移除")
		} else {
			log.V(1).Info("DataSync 未配置自动调度，从调度器移除")
		}
	}

	// 检查是否触发了手动同步
	if r.shouldSync(dataSync) {
		return r.executeSync(ctx, log, dataSync)
	}

	return ctrl.Result{}, nil
}

// triggerSync 由 cron 调度器调用以触发同步
func (r *DataSyncReconciler) triggerSync(namespace, name string) {
	log := r.Log.WithValues("datasync", namespace+"/"+name)
	log.Info("Cron 触发同步")

	// 更新手动触发时间戳以触发调谐
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dataSync := &disasterv1.DataSync{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, dataSync); err != nil {
		log.Error(err, "获取 DataSync 失败（cron 触发）")
		return
	}

	if dataSync.Spec.Paused || dataSync.Spec.Trigger.Schedule == "" {
		log.Info("DataSync 自动调度已关闭，忽略残留 cron 触发",
			"paused", dataSync.Spec.Paused,
			"schedule", dataSync.Spec.Trigger.Schedule,
		)
		return
	}

	if dataSync.Status.State == disasterv1.DataSyncStateInProgress {
		log.Info("DataSync 已在执行中，跳过本次 cron 触发")
		return
	}

	// 设置手动触发时间为当前时间
	dataSync.Spec.Trigger.Manual = time.Now().Format(time.RFC3339)
	if err := r.Update(ctx, dataSync); err != nil {
		log.Error(err, "更新 DataSync 触发器失败")
		return
	}

	log.Info("手动触发器已更新，调谐将执行同步")
}

// shouldSync 检查是否应该执行同步
func (r *DataSyncReconciler) shouldSync(dataSync *disasterv1.DataSync) bool {
	// 1. 优先检查手动触发 (允许在 Paused 状态下执行手动作业)
	if dataSync.Spec.Trigger.Manual != "" {
		manualTime, err := time.Parse(time.RFC3339, dataSync.Spec.Trigger.Manual)
		if err == nil {
			// 检查手动时间是否在上次同步时间之后
			if dataSync.Status.LastSyncTime == nil || manualTime.After(dataSync.Status.LastSyncTime.Time) {
				return true
			}
		}
	}

	// 2. 如果已在执行同步中，则无论是否暂停都继续推进状态流转
	if dataSync.Status.State == disasterv1.DataSyncStateInProgress {
		return true
	}

	// 3. 如果已暂停且不在进行中，则不执行任何自动同步
	if dataSync.Spec.Paused {
		return false
	}

	// 4. 检查是否需要补执行自动同步（首次同步或漏执行）
	if dataSync.Status.LastSyncTime == nil {
		return true
	}

	return false
}

// executeSync 执行数据同步
func (r *DataSyncReconciler) executeSync(ctx context.Context, log logr.Logger, dataSync *disasterv1.DataSync) (ctrl.Result, error) {
	// 1. 获取依赖
	instance := &disasterv1.DisasterInstance{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: dataSync.Namespace, Name: dataSync.Spec.Instance}, instance); err != nil {
		msg := fmt.Sprintf("获取 DisasterInstance %s 失败: %v", dataSync.Spec.Instance, err)
		log.Error(err, "获取 DisasterInstance 失败")
		return r.failDataSync(ctx, dataSync, "-", dataSyncReasonDependencyFailed, msg)
	}

	config := &disasterv1.DisasterConfig{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: instance.Namespace, Name: instance.Spec.Config}, config); err != nil {
		msg := fmt.Sprintf("获取 DisasterConfig %s 失败: %v", instance.Spec.Config, err)
		log.Error(err, "获取 DisasterConfig 失败")
		return r.failDataSync(ctx, dataSync, "-", dataSyncReasonDependencyFailed, msg)
	}
	sourceCluster, targetCluster := resolveClusters(instance, config)
	clusterPair := fmt.Sprintf("%s->%s", sourceCluster, targetCluster)
	if err := r.ensureStorageRepositoryReady(ctx, dataSync.Namespace, config.Spec.StorageRepository); err != nil {
		msg := fmt.Sprintf("StorageRepository %s 不可用: %v", config.Spec.StorageRepository, err)
		log.Error(err, "StorageRepository 不可用", "storageRepository", config.Spec.StorageRepository)
		return r.failDataSync(ctx, dataSync, clusterPair, dataSyncReasonStorageUnavailable, msg)
	}

	// 更新状态为 InProgress
	if dataSync.Status.State != disasterv1.DataSyncStateInProgress {
		dataSync.Status.State = disasterv1.DataSyncStateInProgress
		dataSync.Status.LastBackupName = ""
		dataSync.Status.LastRestoreName = ""
		helper.ClearStatusError(&dataSync.Status)
		if err := r.Status().Update(ctx, dataSync); err != nil {
			return ctrl.Result{}, err
		}

		if err := r.syncStatistics(ctx, dataSync); err != nil {
			log.Error(err, "Failed to sync statistics (InProgress)")
		}
		r.reportDataSyncStarted(ctx, dataSync, clusterPair, "数据同步已开始")

		// 立即重新排队以进行下一步
		return ctrl.Result{Requeue: true}, nil
	}

	// 2. 检查或触发 AppBackup
	appBackupName := fmt.Sprintf("ds-%s", dataSync.Name)
	appBackup := &disasterv1.AppBackup{}
	err := r.Get(ctx, types.NamespacedName{Namespace: dataSync.Namespace, Name: appBackupName}, appBackup)

	if errors.IsNotFound(err) {
		// 不存在，创建新的长期 AppBackup
		newBackup := &disasterv1.AppBackup{
			ObjectMeta: metav1.ObjectMeta{
				Name:      appBackupName,
				Namespace: dataSync.Namespace,
				Labels: map[string]string{
					metadata.LabelAppResourceOrigin:    metadata.AppResourceOriginDisasterInstance,
					metadata.LabelAppResourceOwnerKind: metadata.AppResourceOwnerKindDataSync,
					metadata.LabelAppResourceOwnerName: dataSync.Name,
				},
			},
			Spec: r.buildAppBackupSpec(instance, config),
		}

		// 传播 TraceID 到子资源
		if traceID := dataSync.Annotations[AnnotationTraceID]; traceID != "" {
			newBackup.Annotations = map[string]string{AnnotationTraceID: traceID}
		}

		// newBackup.Spec.Schedule = "@manual" // 移除：使用空 Schedule (One-off 模式)
		newBackup.Spec.Action = &disasterv1.BackupAction{
			Type:      "Backup",
			RequestAt: metav1.Now(),
		}

		if err := controllerutil.SetControllerReference(dataSync, newBackup, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}

		log.Info("创建 DataSync AppBackup", "name", appBackupName)
		if err := r.Create(ctx, newBackup); err != nil {
			return ctrl.Result{}, err
		}
		r.reportDataSyncProgress(ctx, dataSync, clusterPair, fmt.Sprintf("已创建 AppBackup %s，等待生成 Velero Backup", appBackupName))
		// 刚创建，等待下一轮处理
		return ctrl.Result{Requeue: true}, nil
	} else if err != nil {
		msg := fmt.Sprintf("获取 AppBackup %s 失败: %v", appBackupName, err)
		log.Error(err, "获取 AppBackup 失败", "appBackup", appBackupName)
		return r.failDataSync(ctx, dataSync, clusterPair, dataSyncReasonDependencyFailed, msg)
	}

	// AppBackup 存在
	// 关键修复：检查 AppBackup 的 Cluster 是否正确（反向保护场景下，Source 可能已改变）
	// 这解决了用户报告的“数据同步反向保护后应用备份集群不对”的问题
	if appBackup.Spec.Cluster != sourceCluster {
		log.Info("更新 AppBackup SourceCluster (反向保护 detected)", "old", appBackup.Spec.Cluster, "new", sourceCluster)
		appBackup.Spec.Cluster = sourceCluster
		if err := r.Update(ctx, appBackup); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// 如果 LastBackupName 为空，说明需要确定或等待本次备份
	if dataSync.Status.LastBackupName == "" {
		if backupName, ok := ctrlcommon.CurrentBackupActionVeleroBackupName(appBackupName, appBackup, dataSync.Status.LastSyncTime); ok {
			if rec, found := ctrlcommon.FindBackupRecordByName(appBackup.Status.History, backupName); found {
				log.Info("找到本次 Action 生成的 Velero Backup", "name", rec.Name)
				dataSync.Status.LastBackupName = rec.Name
				if err := r.Status().Update(ctx, dataSync); err != nil {
					return ctrl.Result{}, err
				}
				return ctrl.Result{Requeue: true}, nil
			}
			if appBackup.Status.Status == "Failed" {
				failMsg := fmt.Sprintf("AppBackup %s 失败: %s", appBackupName, appBackup.Status.Message)
				if appBackup.Status.Message == "" {
					failMsg = fmt.Sprintf("AppBackup %s 失败", appBackupName)
				}
				helper.SetConditionError(&dataSync.Status.Conditions, "BackupFailed", dataSyncReasonBackupFailed, failMsg)
				return r.failDataSync(ctx, dataSync, clusterPair, dataSyncReasonBackupFailed, failMsg)
			}
			return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
		}

		// 1. 兼容旧状态：没有可关联的 Backup Action 时，只按上次完成时间之后的历史记录兜底。
		for _, rec := range appBackup.Status.History {
			if backupStartedForDataSyncRun(rec.StartTimestamp, dataSync.Status.LastSyncTime) {
				log.Info("找到新生成的 Velero Backup", "name", rec.Name)
				dataSync.Status.LastBackupName = rec.Name
				if err := r.Status().Update(ctx, dataSync); err != nil {
					return ctrl.Result{}, err
				}
				return ctrl.Result{Requeue: true}, nil
			}
		}

		// 2. 还没生成，检查 Action 是否已发送
		if appBackup.Status.Status == "Failed" {
			failMsg := fmt.Sprintf("AppBackup %s 失败: %s", appBackupName, appBackup.Status.Message)
			if appBackup.Status.Message == "" {
				failMsg = fmt.Sprintf("AppBackup %s 失败", appBackupName)
			}
			helper.SetConditionError(&dataSync.Status.Conditions, "BackupFailed", dataSyncReasonBackupFailed, failMsg)
			return r.failDataSync(ctx, dataSync, clusterPair, dataSyncReasonBackupFailed, failMsg)
		}
		// 3. Action 没发送或已是上次同步之前的旧 Action，发送新 Action
		log.Info("触发新备份 Action", "appBackup", appBackupName)

		// 关键修复：更新 AppBackup 的 Cluster 以支持 Reprotect 后方向变化
		// Reprotect 后 Primary/Secondary 互换，备份应该在新的 Primary 执行
		newSpec := r.buildAppBackupSpec(instance, config)
		if appBackup.Spec.Cluster != newSpec.Cluster {
			log.Info("检测到集群方向变化，更新 AppBackup", "oldCluster", appBackup.Spec.Cluster, "newCluster", newSpec.Cluster)
			appBackup.Spec.Cluster = newSpec.Cluster
			appBackup.Spec.Template = newSpec.Template
		}

		appBackup.Spec.Action = &disasterv1.BackupAction{
			Type:      "Backup",
			RequestAt: metav1.Now(),
		}
		if err := r.Update(ctx, appBackup); err != nil {
			return ctrl.Result{}, err
		}
		r.reportDataSyncProgress(ctx, dataSync, clusterPair, fmt.Sprintf("已触发 AppBackup %s 备份动作", appBackupName))
		return ctrl.Result{Requeue: true}, nil
	} else {
		// LastBackupName 有值，检查状态
		// 注意：Velero Backup 在源集群，我们不能直接 Get。
		// 必须通过 AppBackup CR 的 Status 来判断。

		// 重新获取最新的 AppBackup
		if err := r.Get(ctx, types.NamespacedName{Namespace: dataSync.Namespace, Name: appBackupName}, appBackup); err != nil {
			return ctrl.Result{}, err
		}

		backupName := dataSync.Status.LastBackupName
		backupStatus := "InProgress" // Default/Unknown

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
			// 可能 AppBackup 还没同步到 History
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}

		if backupStatus == string(velerov1.BackupPhaseCompleted) {
			r.reportDataSyncProgress(ctx, dataSync, clusterPair, fmt.Sprintf("Velero Backup %s 已完成，开始数据恢复", backupName))
			return r.handleRestore(ctx, log, dataSync, config, instance, backupName)
		} else if ctrlcommon.BackupRecordFailed(backupRecord) {
			backupStatus = ctrlcommon.BackupRecordFailureStatus(backupRecord)
			log.Info("Velero Backup 失败", "name", backupName, "status", backupStatus)
			now := metav1.Now()
			dataSync.Status.State = disasterv1.DataSyncStateFailed
			// 记录失败完成时间，避免 shouldSync 在 LastSyncTime 为空时无限重试
			dataSync.Status.LastSyncTime = &now
			failMsg := fmt.Sprintf("Velero Backup %s 失败: %s", backupName, backupStatus)
			if appBackup.Status.Message != "" {
				failMsg = fmt.Sprintf("Velero Backup %s 失败: %s", backupName, appBackup.Status.Message)
			}
			helper.SetStatusError(&dataSync.Status, dataSyncReasonBackupFailed, failMsg)
			helper.SetConditionError(&dataSync.Status.Conditions, "BackupFailed", dataSyncReasonBackupFailed, failMsg)
			if err := r.Status().Update(ctx, dataSync); err != nil {
				return ctrl.Result{}, err
			}
			r.reportDataSyncFinished(ctx, dataSync, clusterPair, helper.TaskStatusFailed, failMsg, dataSyncReasonBackupFailed)
			return ctrl.Result{}, nil
		}

		// InProgress
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

}

func (r *DataSyncReconciler) syncDependencyLabels(ctx context.Context, dataSync *disasterv1.DataSync) (bool, error) {
	if dataSync.Labels == nil {
		dataSync.Labels = make(map[string]string)
	}
	_, _, tokenChanged := metadata.EnsureDependencyTokenLabel(dataSync.Labels, string(dataSync.UID))
	edges := make([]metadata.DependencyEdge, 0, 1)

	if dataSync.Spec.Instance != "" {
		instance := &disasterv1.DisasterInstance{}
		if err := r.Get(ctx, client.ObjectKey{Namespace: dataSync.Namespace, Name: dataSync.Spec.Instance}, instance); err == nil {
			edges = append(edges, metadata.DependencyEdge{
				TargetToken:  metadata.BuildDependencyToken(string(instance.UID)),
				RelationCode: "spec.instance",
			})
		}
	}

	_, depChanged := metadata.RebuildDependencyToLabels(dataSync.Labels, edges)
	return tokenChanged || depChanged, nil
}

// handleRestore 处理恢复阶段
func (r *DataSyncReconciler) handleRestore(ctx context.Context, log logr.Logger, dataSync *disasterv1.DataSync, config *disasterv1.DisasterConfig, instance *disasterv1.DisasterInstance, backupName string) (ctrl.Result, error) {
	// Restore 命名: rec-ds-<dsName[:20]>-<backupHash[:6]>
	dsName := dataSync.Name
	if len(dsName) > 20 {
		dsName = dsName[:20]
	}
	backupHash := fmt.Sprintf("%x", md5.Sum([]byte(backupName)))[:6]
	restoreName := fmt.Sprintf("rec-ds-%s-%s", dsName, backupHash)

	restore := &disasterv1.AppRestore{}
	sourceCluster, targetCluster := resolveClusters(instance, config)
	clusterPair := fmt.Sprintf("%s->%s", sourceCluster, targetCluster)
	if err := r.Get(ctx, types.NamespacedName{Namespace: dataSync.Namespace, Name: restoreName}, restore); err != nil {
		if errors.IsNotFound(err) {
			// 在创建 Restore 之前，清理目标集群上可能残留的 Trafficless Pod
			// 这确保 Velero 能重新创建 Pod 并触发 Data Restore（否则若 Pod 存在且 Policy=None，Velero 会跳过）
			if err := r.cleanupTrafficlessPods(ctx, log, dataSync, config, instance); err != nil {
				log.Error(err, "清理残留 Pod 失败")
				return ctrl.Result{}, err
			}

			restoreSpec, policySummary, specErr := r.buildAppRestoreSpec(ctx, dataSync, config, instance, backupName)
			if specErr != nil {
				msg := fmt.Sprintf("构建 DataSync AppRestore 失败: %v", specErr)
				log.Error(specErr, "构建 AppRestore 规格失败", "restore", restoreName)
				r.Recorder.Event(dataSync, "Warning", "BuildRestoreSpecFailed", msg)
				now := metav1.Now()
				dataSync.Status.State = disasterv1.DataSyncStateFailed
				dataSync.Status.LastSyncTime = &now
				helper.SetStatusError(&dataSync.Status, dataSyncReasonBuildRestoreFailed, msg)
				helper.SetConditionError(&dataSync.Status.Conditions, "BuildRestoreSpecFailed", dataSyncReasonBuildRestoreFailed, msg)

				// BuildRestoreSpecFailed 发生在 AppRestore 创建前，也要落一条失败历史，避免失败计数漏统。
				var backupItems int
				var startTime *metav1.Time
				appBackup := &disasterv1.AppBackup{}
				if err := r.Get(ctx, types.NamespacedName{Namespace: dataSync.Namespace, Name: fmt.Sprintf("ds-%s", dataSync.Name)}, appBackup); err == nil {
					for _, rec := range appBackup.Status.History {
						if rec.Name == backupName {
							backupItems = rec.VeleroStatus.Progress.ItemsBackedUp
							startTime = rec.StartTimestamp
							break
						}
					}
				}
				failedRecord := disasterv1.SyncHistoryRecord{
					StartTime:            startTime,
					CompletionTime:       &now,
					BackupName:           backupName,
					RestoreName:          restoreName,
					BackupResourceCount:  backupItems,
					RestoreResourceCount: 0,
					Status:               string(disasterv1.PhaseFailed),
				}
				if startTime != nil {
					failedRecord.Duration = now.Sub(startTime.Time).String()
				}
				dataSync.Status.History = append(dataSync.Status.History, failedRecord)
				if len(dataSync.Status.History) > 20 {
					dataSync.Status.History = dataSync.Status.History[len(dataSync.Status.History)-20:]
				}

				if err := r.Status().Update(ctx, dataSync); err != nil {
					return ctrl.Result{}, err
				}
				if err := r.syncStatistics(ctx, dataSync); err != nil {
					log.Error(err, "Failed to sync statistics (BuildRestoreSpecFailed)")
				}
				r.reportDataSyncFinished(ctx, dataSync, clusterPair, helper.TaskStatusFailed, msg, dataSyncReasonBuildRestoreFailed)
				return ctrl.Result{}, nil
			}

			// 创建新的 AppRestore
			newRestore := &disasterv1.AppRestore{
				ObjectMeta: metav1.ObjectMeta{
					Name:      restoreName,
					Namespace: dataSync.Namespace,
					Labels: map[string]string{
						metadata.LabelAppResourceOrigin:    metadata.AppResourceOriginDisasterInstance,
						metadata.LabelAppResourceOwnerKind: metadata.AppResourceOwnerKindDataSync,
						metadata.LabelAppResourceOwnerName: dataSync.Name,
					},
				},
				Spec: restoreSpec,
			}
			restorebuilder.ApplyPolicySummaryAnnotations(&newRestore.ObjectMeta, policySummary)

			// 绑定 OwnerReference
			if err := controllerutil.SetControllerReference(dataSync, newRestore, r.Scheme); err != nil {
				return ctrl.Result{}, err
			}

			log.Info("创建新的 AppRestore", "name", restoreName)
			if err := r.Create(ctx, newRestore); err != nil {
				return ctrl.Result{}, err
			}
			r.reportDataSyncProgress(ctx, dataSync, clusterPair, restorebuilder.ModifierAuditMessage(policySummary))
			r.reportDataSyncProgress(ctx, dataSync, clusterPair, fmt.Sprintf("已创建 AppRestore %s，等待恢复完成", restoreName))

			dataSync.Status.LastRestoreName = restoreName
			if err := r.Status().Update(ctx, dataSync); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, err
	}

	// 检查 Restore 状态
	// AppRestore 的 Status.Status 是 Enum
	status := restore.Status.Status
	if status == disasterv1.PhaseSucceeded || status == disasterv1.PhaseFailed {
		msg := "数据同步成功完成"
		finalState := disasterv1.DataSyncStateReady
		if status == disasterv1.PhaseFailed {
			msg = fmt.Sprintf("数据恢复失败: %s", restoreName)
			finalState = disasterv1.DataSyncStateFailed
			helper.SetStatusError(&dataSync.Status, dataSyncReasonRestoreFailed, msg)
			helper.SetConditionError(&dataSync.Status.Conditions, "RestoreFailed", dataSyncReasonRestoreFailed, msg)
			log.Info("AppRestore 失败", "name", restoreName)
		} else {
			log.Info("AppRestore 成功", "name", restoreName)
			helper.ClearStatusError(&dataSync.Status)

			// 清理目标集群上的临时 Pod (trafficless=true) - 只在成功时清理
			if err := r.cleanupTrafficlessPods(ctx, log, dataSync, config, instance); err != nil {
				log.Error(err, "清理临时 Pod 失败，但不影响同步状态")
				r.Recorder.Eventf(dataSync, "Warning", "CleanupFailed", "清理临时 Pod 失败: %v", err)
			} else {
				log.Info("临时 Pod 已清理")
			}
		}

		dataSync.Status.State = finalState
		now := metav1.Now()
		dataSync.Status.LastSyncTime = &now

		// --- 记录历史 ---
		appBackup := &disasterv1.AppBackup{}
		if err := r.Get(ctx, types.NamespacedName{Namespace: dataSync.Namespace, Name: fmt.Sprintf("ds-%s", dataSync.Name)}, appBackup); err == nil {
			var backupItems int
			var startTime *metav1.Time
			for _, rec := range appBackup.Status.History {
				if rec.Name == backupName {
					backupItems = rec.VeleroStatus.Progress.ItemsBackedUp
					startTime = rec.StartTimestamp
					break
				}
			}

			restoreItems := 0
			if restore.Status.RestoreStatus.Progress != nil {
				restoreItems = restore.Status.RestoreStatus.Progress.ItemsRestored
			}

			record := disasterv1.SyncHistoryRecord{
				StartTime:            startTime,
				CompletionTime:       &now,
				BackupName:           backupName,
				RestoreName:          restoreName,
				BackupResourceCount:  backupItems,
				RestoreResourceCount: restoreItems,
				Status:               string(status),
			}
			if startTime != nil {
				record.Duration = now.Sub(startTime.Time).String()
			}

			dataSync.Status.History = append(dataSync.Status.History, record)
			if len(dataSync.Status.History) > 20 {
				dataSync.Status.History = dataSync.Status.History[len(dataSync.Status.History)-20:]
			}
		}

		// --- 同步统计 ---
		if err := r.syncStatistics(ctx, dataSync); err != nil {
			log.Error(err, "Failed to sync statistics")
		}

		r.Recorder.Event(dataSync, "Normal", "SyncCompleted", msg)
		if finalState == disasterv1.DataSyncStateReady {
			r.reportDataSyncFinished(ctx, dataSync, clusterPair, helper.TaskStatusSuccess, msg)
		} else {
			r.reportDataSyncFinished(ctx, dataSync, clusterPair, helper.TaskStatusFailed, msg, dataSync.Status.Reason)
		}

		if err := r.Status().Update(ctx, dataSync); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
}

// syncStatistics 更新关联的 BackupRestoreStatistics CR
func (r *DataSyncReconciler) syncStatistics(ctx context.Context, ds *disasterv1.DataSync) error {
	var total, completed, failed, inProgress int32
	if ds.Status.State == disasterv1.DataSyncStateInProgress {
		inProgress = 1
	}

	for _, h := range ds.Status.History {
		total++
		if h.Status == string(disasterv1.PhaseSucceeded) || h.Status == "Completed" {
			completed++
		} else {
			failed++
		}
	}

	statsName := fmt.Sprintf("ds-%s-stats", ds.Name)
	stats := &disasterv1.BackupRestoreStatistics{
		ObjectMeta: metav1.ObjectMeta{
			Name:      statsName,
			Namespace: ds.Namespace,
		},
	}

	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, stats, func() error {
		stats.Labels = map[string]string{
			"testudo.softcdata.com/owner-kind": "DataSync",
			"disaster.io/scope-uid":            string(ds.UID),
		}
		stats.Spec.ScopeType = disasterv1.ScopeTypeApp // Unified to App
		stats.Spec.ScopeRef = disasterv1.ScopeReference{
			Kind:      "DataSync",
			Name:      ds.Name,
			Namespace: ds.Namespace,
			UID:       ds.UID,
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

// buildAppBackupSpec 构建 AppBackup 的 Spec
func (r *DataSyncReconciler) buildAppBackupSpec(instance *disasterv1.DisasterInstance, config *disasterv1.DisasterConfig) disasterv1.AppBackupSpec {
	source, _ := resolveClusters(instance, config)
	falseVar := false
	trueVar := true
	return disasterv1.AppBackupSpec{
		Cluster: source,
		// DisasterPolicy: config.Spec.DataSyncPolicy, // V2 does not use V1 DisasterPolicy
		Template: velerov1.BackupSpec{
			IncludedNamespaces:       instance.Spec.Namespaces,
			ExcludedNamespaces:       []string{"velero", "kube-system"}, // 排除系统命名空间防止自毁
			IncludedResources:        []string{"pods", "persistentvolumeclaims", "persistentvolumes"},
			LabelSelector:            instance.Spec.LabelSelector,
			StorageLocation:          config.Spec.StorageRepository,
			SnapshotVolumes:          &falseVar,
			DefaultVolumesToFsBackup: &trueVar,
		},
	}
}

// buildAppRestoreSpec 构建 AppRestore 的 Spec (包含 Trafficless 配置)
func (r *DataSyncReconciler) buildAppRestoreSpec(
	ctx context.Context,
	dataSync *disasterv1.DataSync,
	config *disasterv1.DisasterConfig,
	instance *disasterv1.DisasterInstance,
	backupName string,
) (disasterv1.AppRestoreSpec, restorebuilder.PolicySummary, error) {
	appBackupName := fmt.Sprintf("ds-%s", dataSync.Name)
	source, target := resolveClusters(instance, config)
	shouldCleanupPVCVolumeName := shouldInjectInitialPVCVolumeNameCleanup(dataSync, instance)
	cleanupPVCVolumeRule := restorebuilder.MakePVCVolumeNameCleanupRule(instance.Spec.Namespaces)

	dataModifiers := r.makeTrafficlessModifiers(dataSync)
	// When restorePolicy is absent, ApplyInstanceRestorePolicy returns early.
	// Keep first-time PVC cleanup effective by appending it directly in this branch.
	if shouldCleanupPVCVolumeName && instance.Spec.RestorePolicy == nil {
		dataModifiers = append(dataModifiers, cleanupPVCVolumeRule)
	}

	spec := restorebuilder.BuildAppRestoreSpec(restorebuilder.BuilderConfig{
		RestoreType:               restorebuilder.RestoreTypeData,
		BackupSource:              appBackupName,
		BackupName:                backupName,
		TargetCluster:             target,
		SourceCluster:             source,
		StorageRepository:         config.Spec.StorageRepository,
		IncludedNamespaces:        instance.Spec.Namespaces,
		IsForDrill:                false,
		DataResourceModifierRules: dataModifiers,
	})

	var targetClient client.Client
	if restorebuilder.RequiresTargetClassValidation(instance) {
		c, err := ctrlcommon.GetKubeClientSet(ctx, r.Client, r.Scheme, target)
		if err != nil {
			return disasterv1.AppRestoreSpec{}, restorebuilder.PolicySummary{}, fmt.Errorf("build target cluster client for restore policy: %w", err)
		}
		targetClient = c
	}

	applyOpts := []restorebuilder.ApplyInstanceRestorePolicyOption{
		restorebuilder.WithBaselineClusters(config.Spec.SourceCluster, config.Spec.TargetCluster),
		restorebuilder.WithApplyTarget(disasterv1.RestoreModifierApplyDataSync),
	}
	// With restorePolicy present, inject as system-protect so user rules cannot override this safety patch.
	if shouldCleanupPVCVolumeName && instance.Spec.RestorePolicy != nil {
		applyOpts = append(applyOpts, restorebuilder.WithSystemProtectRules([]disasterv1.ResourceModifierRule{cleanupPVCVolumeRule}))
	}

	summary, err := restorebuilder.ApplyInstanceRestorePolicy(ctx, &spec, instance, targetClient, applyOpts...)
	if err != nil {
		return disasterv1.AppRestoreSpec{}, restorebuilder.PolicySummary{}, err
	}
	return spec, summary, nil
}

// shouldInjectInitialPVCVolumeNameCleanup returns true only for the first data restore during instance initialization.
func shouldInjectInitialPVCVolumeNameCleanup(
	dataSync *disasterv1.DataSync,
	instance *disasterv1.DisasterInstance,
) bool {
	if dataSync == nil || instance == nil {
		return false
	}
	if dataSync.Status.LastSyncTime != nil {
		return false
	}
	return instance.Status.FsmState == disasterv1.FsmStateInitializing
}

// makeTrafficlessModifiers 生成 Trafficless Restore 规则
// 根据 V2 设计文档：恢复时 ResourceModifier 替换 Image 为 busybox，移除所有 Labels，确保 Service 不导流
// 注意：Velero 不支持修改 metadata.name，所以我们只能通过移除 labels 和 ownerReferences 来避免 Pod 被控制器管理
func (r *DataSyncReconciler) makeTrafficlessModifiers(ds *disasterv1.DataSync) []disasterv1.ResourceModifierRule {
	trafficlessImage := r.resolveTrafficlessImage(ds)
	trafficlessCommand := r.resolveTrafficlessCommand(ds)
	commandJSON, err := json.Marshal(trafficlessCommand)
	if err != nil {
		// fallback to safe default command
		commandJSON = []byte(`["sleep","3600"]`)
	}

	// 针对 Pods 直接修改 (因为 IncludedResources只包含 Pods)
	// 注意：JSON Patch 操作顺序很重要！
	// 1. 清除原有 labels（防止 Service 导流 + 防止 STS/Deployment 识别）
	// 2. 将 ownerReferences 置空（防止 GC）
	// 3. 替换容器配置
	podPatches := []disasterv1.JSONPatch{
		// 清除所有原有标签 - 替换为只包含 trafficless 的 map
		// 这样 StatefulSet 的 selector 就无法匹配到这个 Pod
		// 使用 add 操作覆盖整个 map，Velero 支持这种行为
		{
			Operation: "add",
			Path:      "/metadata/labels",
			Value:     `{"trafficless": "true"}`,
		},
		// 将 ownerReferences 置空，避免临时 Pod 被控制器/GC 回收。
		// 使用 add + [] 保持幂等：字段不存在时不会触发 "expected one matching path ... got 0"。
		{
			Operation: "add",
			Path:      "/metadata/ownerReferences",
			Value:     "[]",
		},
		// 替换容器镜像为 busybox
		{
			Operation: "replace",
			Path:      "/spec/containers/0/image",
			Value:     trafficlessImage,
		},
		// 注入显式 command，避免不同基础镜像入口行为差异导致 Pod 不可控退出
		{
			Operation: "add",
			Path:      "/spec/containers/0/command",
			Value:     string(commandJSON),
		},
	}

	return []disasterv1.ResourceModifierRule{
		{
			Conditions: disasterv1.Conditions{
				GroupResource: "pods",
			},
			Patches: podPatches,
		},
	}
}

// SetupWithManager 将控制器注册到 Manager
func (r *DataSyncReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&disasterv1.DataSync{}).
		Complete(r)
}

// cleanupTrafficlessPods 清理目标集群上的临时 Pod (trafficless=true)
func (r *DataSyncReconciler) cleanupTrafficlessPods(ctx context.Context, log logr.Logger, dataSync *disasterv1.DataSync, config *disasterv1.DisasterConfig, instance *disasterv1.DisasterInstance) error {
	_, target := resolveClusters(instance, config)

	// 获取目标集群的 client
	targetClient, err := ctrlcommon.GetKubeClientSet(ctx, r.Client, r.Scheme, target)
	if err != nil {
		return fmt.Errorf("failed to get target cluster client: %w", err)
	}

	// 在每个命名空间中查找并删除标记为 trafficless=true 的 Pod
	for _, ns := range instance.Spec.Namespaces {
		podList := &corev1.PodList{}
		listOpts := []client.ListOption{
			client.InNamespace(ns),
		}

		if err := targetClient.List(ctx, podList, listOpts...); err != nil {
			log.Error(err, "列出 Pod 失败", "namespace", ns)
			continue
		}

		for _, pod := range podList.Items {
			isTrafficless := pod.Labels["trafficless"] == "true"
			if !isTrafficless {
				// 兜底方案：检查镜像是否为当前配置的 trafficless 镜像
				expectedImage := r.resolveTrafficlessImage(dataSync)
				for _, container := range pod.Spec.Containers {
					if container.Image == expectedImage {
						isTrafficless = true
						break
					}
				}
			}

			if isTrafficless {
				log.Info("删除临时 Pod", "namespace", ns, "pod", pod.Name)
				if err := targetClient.Delete(ctx, &pod); err != nil && !errors.IsNotFound(err) {
					log.Error(err, "删除 Pod 失败", "namespace", ns, "pod", pod.Name)
				}
			}
		}
	}

	return nil
}

func (r *DataSyncReconciler) resolveTrafficlessImage(ds *disasterv1.DataSync) string {
	if ds != nil && ds.Spec.TrafficlessConfig != nil && ds.Spec.TrafficlessConfig.Image != "" {
		return ds.Spec.TrafficlessConfig.Image
	}
	return "busybox:1.36"
}

func (r *DataSyncReconciler) resolveTrafficlessCommand(ds *disasterv1.DataSync) []string {
	if ds != nil && ds.Spec.TrafficlessConfig != nil && len(ds.Spec.TrafficlessConfig.Command) > 0 {
		return ds.Spec.TrafficlessConfig.Command
	}
	return []string{"sleep", "3600"}
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
