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
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-logr/logr"
	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	ctrlcommon "github.com/softcdata/testudo-operator/internal/controller"
	restorebuilder "github.com/softcdata/testudo-operator/internal/controller/restore"
	runtimecfg "github.com/softcdata/testudo-operator/internal/controller/runtimeconfig"
	"github.com/softcdata/testudo-operator/internal/controller/scheduler"
	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	"github.com/softcdata/testudo-operator/pkg/helper"
	"github.com/softcdata/testudo-operator/pkg/metadata"
)

const (
	dataSyncFinalizer = "testudo.softcdata.com/datasync-finalizer"
	AnnotationTraceID = metadata.AnnotationTraceID
	TraceIDKey        = metadata.TraceIDKey

	appBackupParallelFilesUploadEnv = "APPBACKUP_PARALLEL_FILES_UPLOAD"

	dataSyncReasonBackupFailed       = "BackupFailed"
	dataSyncReasonBuildRestoreFailed = "BuildRestoreSpecFailed"
	dataSyncReasonRestoreFailed      = "RestoreFailed"
	dataSyncReasonDependencyFailed   = "DependencyFailed"
	dataSyncReasonStorageUnavailable = "StorageUnavailable"
	dataSyncReasonNoPVCFound         = "NoPVCFound"

	dataSyncConditionNoDataVolumes = "NoDataVolumes"

	dataSyncHistoryStatusSkipped = "Skipped"
)

// DataSyncReconciler 负责调谐 DataSync 对象
type DataSyncReconciler struct {
	client.Client
	Scheme              *runtime.Scheme
	Log                 logr.Logger
	Recorder            record.EventRecorder
	Scheduler           *scheduler.SyncScheduler
	SourceClientFactory ctrlcommon.ClientFactory
	TargetClientFactory ctrlcommon.ClientFactory
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
	apimeta.RemoveStatusCondition(&ds.Status.Conditions, dataSyncConditionNoDataVolumes)
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

type dataSyncVolumePlan struct {
	PVCs []types.NamespacedName
}

func (r *DataSyncReconciler) shouldRunNoPVCPreflight(ctx context.Context, ds *disasterv1.DataSync, appBackupName string) (bool, error) {
	if ds == nil {
		return false, nil
	}
	if ds.Status.State != disasterv1.DataSyncStateInProgress {
		return true, nil
	}
	if ds.Status.LastBackupName != "" || ds.Status.LastRestoreName != "" {
		return false, nil
	}

	appBackup := &disasterv1.AppBackup{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: ds.Namespace, Name: appBackupName}, appBackup); err != nil {
		if errors.IsNotFound(err) {
			return true, nil
		}
		return false, err
	}
	if _, ok := ctrlcommon.CurrentBackupActionVeleroBackupName(appBackupName, appBackup, ds.Status.LastSyncTime); ok {
		return false, nil
	}
	return true, nil
}

func (r *DataSyncReconciler) getSourceClusterClient(ctx context.Context, sourceCluster string) (client.Client, error) {
	factory := r.SourceClientFactory
	if factory == nil {
		factory = &ctrlcommon.DefaultClientFactory{}
	}
	return factory.GetKubeClient(ctx, r.Client, r.Scheme, sourceCluster)
}

func (r *DataSyncReconciler) getTargetClusterClient(ctx context.Context, targetCluster string) (client.Client, error) {
	factory := r.TargetClientFactory
	if factory == nil {
		factory = &ctrlcommon.DefaultClientFactory{}
	}
	return factory.GetKubeClient(ctx, r.Client, r.Scheme, targetCluster)
}

func (r *DataSyncReconciler) discoverDataSyncVolumePlan(
	ctx context.Context,
	sourceClient client.Client,
	instance *disasterv1.DisasterInstance,
) (dataSyncVolumePlan, error) {
	if sourceClient == nil {
		return dataSyncVolumePlan{}, fmt.Errorf("source client is nil")
	}
	if instance == nil {
		return dataSyncVolumePlan{}, fmt.Errorf("disaster instance is nil")
	}
	if len(instance.Spec.Namespaces) == 0 {
		return dataSyncVolumePlan{}, fmt.Errorf("instance %s/%s has no namespaces configured", instance.Namespace, instance.Name)
	}

	selector := labels.Everything()
	if instance.Spec.LabelSelector != nil {
		parsed, err := metav1.LabelSelectorAsSelector(instance.Spec.LabelSelector)
		if err != nil {
			return dataSyncVolumePlan{}, fmt.Errorf("invalid instance labelSelector: %w", err)
		}
		selector = parsed
	}

	matchedPVCs := make(map[types.NamespacedName]struct{})
	for _, namespace := range instance.Spec.Namespaces {
		namespace = strings.TrimSpace(namespace)
		if namespace == "" {
			return dataSyncVolumePlan{}, fmt.Errorf("instance %s/%s contains empty namespace", instance.Namespace, instance.Name)
		}

		pvcList := &corev1.PersistentVolumeClaimList{}
		if err := sourceClient.List(ctx, pvcList, client.InNamespace(namespace)); err != nil {
			return dataSyncVolumePlan{}, fmt.Errorf("list pvc in namespace %s: %w", namespace, err)
		}

		pvcNames := make(map[string]struct{}, len(pvcList.Items))
		for i := range pvcList.Items {
			pvc := pvcList.Items[i]
			if !pvc.DeletionTimestamp.IsZero() {
				continue
			}
			pvcNames[pvc.Name] = struct{}{}
			if instance.Spec.LabelSelector == nil || selector.Matches(labels.Set(pvc.Labels)) {
				matchedPVCs[types.NamespacedName{Namespace: pvc.Namespace, Name: pvc.Name}] = struct{}{}
			}
		}

		if instance.Spec.LabelSelector == nil {
			continue
		}

		podList := &corev1.PodList{}
		if err := sourceClient.List(ctx, podList, client.InNamespace(namespace), client.MatchingLabelsSelector{Selector: selector}); err != nil {
			return dataSyncVolumePlan{}, fmt.Errorf("list pods in namespace %s: %w", namespace, err)
		}
		for i := range podList.Items {
			pod := podList.Items[i]
			if !pod.DeletionTimestamp.IsZero() {
				continue
			}
			for _, volume := range pod.Spec.Volumes {
				if volume.PersistentVolumeClaim == nil || volume.PersistentVolumeClaim.ClaimName == "" {
					continue
				}
				if _, exists := pvcNames[volume.PersistentVolumeClaim.ClaimName]; !exists {
					continue
				}
				matchedPVCs[types.NamespacedName{Namespace: pod.Namespace, Name: volume.PersistentVolumeClaim.ClaimName}] = struct{}{}
			}
		}
	}

	plan := dataSyncVolumePlan{PVCs: make([]types.NamespacedName, 0, len(matchedPVCs))}
	for pvc := range matchedPVCs {
		plan.PVCs = append(plan.PVCs, pvc)
	}
	return plan, nil
}

func (r *DataSyncReconciler) completeDataSyncNoPVC(ctx context.Context, ds *disasterv1.DataSync, clusterPair string) (ctrl.Result, error) {
	if clusterPair == "" {
		clusterPair = "-"
	}
	now := metav1.Now()
	message := "本次保护范围内未发现 PVC，已跳过数据同步"

	ds.Status.State = disasterv1.DataSyncStateReady
	ds.Status.LastSyncTime = &now
	helper.ClearStatusError(&ds.Status)
	clearDataSyncFailureConditions(&ds.Status.Conditions)
	apimeta.SetStatusCondition(&ds.Status.Conditions, metav1.Condition{
		Type:               dataSyncConditionNoDataVolumes,
		Status:             metav1.ConditionTrue,
		LastTransitionTime: now,
		Reason:             dataSyncReasonNoPVCFound,
		Message:            message,
	})
	ds.Status.History = append(ds.Status.History, disasterv1.SyncHistoryRecord{
		StartTime:            &now,
		CompletionTime:       &now,
		Duration:             "0s",
		BackupName:           "",
		RestoreName:          "",
		BackupResourceCount:  0,
		RestoreResourceCount: 0,
		Status:               dataSyncHistoryStatusSkipped,
	})
	if retention := runtimecfg.SnapshotCurrent().SyncRuntime.HistoryRetention; len(ds.Status.History) > retention {
		ds.Status.History = ds.Status.History[len(ds.Status.History)-retention:]
	}

	if err := r.Status().Update(ctx, ds); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.syncStatistics(ctx, ds); err != nil {
		r.Log.Error(err, "Failed to sync statistics (NoPVCSkipped)")
	}
	r.Recorder.Event(ds, "Normal", "SyncSkipped", message)
	r.reportDataSyncFinished(ctx, ds, clusterPair, helper.TaskStatusSuccess, message)
	return ctrl.Result{}, nil
}

func clearDataSyncFailureConditions(conditions *[]metav1.Condition) {
	if conditions == nil {
		return
	}
	for _, conditionType := range []string{"SyncFailed", "BackupFailed", "BuildRestoreSpecFailed", "RestoreFailed"} {
		apimeta.RemoveStatusCondition(conditions, conditionType)
	}
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
	ctx, cancel := context.WithTimeout(context.Background(), runtimecfg.SnapshotCurrent().SyncRuntime.SchedulerUpdateTimeout)
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

	appBackupName := fmt.Sprintf("ds-%s", dataSync.Name)
	shouldPreflight, err := r.shouldRunNoPVCPreflight(ctx, dataSync, appBackupName)
	if err != nil {
		msg := fmt.Sprintf("检查 DataSync 子任务状态失败: %v", err)
		log.Error(err, "检查 DataSync 子任务状态失败")
		return r.failDataSync(ctx, dataSync, clusterPair, dataSyncReasonDependencyFailed, msg)
	}
	if shouldPreflight {
		sourceClient, err := r.getSourceClusterClient(ctx, sourceCluster)
		if err != nil {
			msg := fmt.Sprintf("构建源集群 %s client 失败: %v", sourceCluster, err)
			log.Error(err, "构建源集群 client 失败", "sourceCluster", sourceCluster)
			return r.failDataSync(ctx, dataSync, clusterPair, dataSyncReasonDependencyFailed, msg)
		}
		volumePlan, err := r.discoverDataSyncVolumePlan(ctx, sourceClient, instance)
		if err != nil {
			msg := fmt.Sprintf("发现源集群可恢复 PVC 失败: %v", err)
			log.Error(err, "发现源集群可恢复 PVC 失败", "sourceCluster", sourceCluster)
			return r.failDataSync(ctx, dataSync, clusterPair, dataSyncReasonDependencyFailed, msg)
		}
		if len(volumePlan.PVCs) == 0 {
			log.Info("本次保护范围内未发现 PVC，跳过 DataSync 数据恢复")
			return r.completeDataSyncNoPVC(ctx, dataSync, clusterPair)
		}
		if err := r.ensureDataSyncTrafficlessRuntime(ctx, sourceCluster, "source", sourceClient); err != nil {
			reason, message := trafficlessLifecycleErrorDetails(err, dataSyncReasonSourceVeleroRuntimeNotReady)
			log.Error(err, "源集群 Velero 运行时未就绪", "sourceCluster", sourceCluster)
			return r.failDataSync(ctx, dataSync, clusterPair, reason, message)
		}
		if err := r.ensureDataSyncTrafficlessRuntime(ctx, targetCluster, "target", nil); err != nil {
			reason, message := trafficlessLifecycleErrorDetails(err, dataSyncReasonTargetVeleroRuntimeNotReady)
			log.Error(err, "目标集群 Velero 运行时未就绪", "targetCluster", targetCluster)
			return r.failDataSync(ctx, dataSync, clusterPair, reason, message)
		}
	}

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
		clearDataSyncFailureConditions(&dataSync.Status.Conditions)
		apimeta.RemoveStatusCondition(&dataSync.Status.Conditions, dataSyncConditionNoDataVolumes)
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
	appBackup := &disasterv1.AppBackup{}
	err = r.Get(ctx, types.NamespacedName{Namespace: dataSync.Namespace, Name: appBackupName}, appBackup)

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

	desiredBackupSpec := r.buildAppBackupSpec(instance, config)
	if ctrlcommon.AppBackupSpecNeedsUpdate(appBackup.Spec, desiredBackupSpec) {
		log.Info("更新 DataSync AppBackup 规格", "oldCluster", appBackup.Spec.Cluster, "newCluster", desiredBackupSpec.Cluster)
		appBackup.Spec.Cluster = desiredBackupSpec.Cluster
		appBackup.Spec.Template = desiredBackupSpec.Template
		appBackup.Spec.Timeout = desiredBackupSpec.Timeout
		if err := r.Update(ctx, appBackup); err != nil {
			return ctrl.Result{}, err
		}
		if dataSync.Status.LastBackupName == "" {
			return ctrl.Result{Requeue: true}, nil
		}
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
			return ctrl.Result{RequeueAfter: runtimecfg.SnapshotCurrent().SyncRuntime.BackupObserveRequeue}, nil
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
			return ctrl.Result{RequeueAfter: runtimecfg.SnapshotCurrent().SyncRuntime.HistoryMissingRequeue}, nil
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
		return ctrl.Result{RequeueAfter: runtimecfg.SnapshotCurrent().SyncRuntime.BackupInProgressRequeue}, nil
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
	restoreName := dataSyncRestoreName(dataSync, backupName)

	restore := &disasterv1.AppRestore{}
	sourceCluster, targetCluster := resolveClusters(instance, config)
	clusterPair := fmt.Sprintf("%s->%s", sourceCluster, targetCluster)
	if err := r.Get(ctx, types.NamespacedName{Namespace: dataSync.Namespace, Name: restoreName}, restore); err != nil {
		if errors.IsNotFound(err) {
			if runtimeErr := r.ensureDataSyncTrafficlessRuntime(ctx, targetCluster, "target", nil); runtimeErr != nil {
				reason, message := trafficlessLifecycleErrorDetails(runtimeErr, dataSyncReasonTargetVeleroRuntimeNotReady)
				log.Error(runtimeErr, "目标集群 Velero 运行时未就绪", "targetCluster", targetCluster)
				return r.failDataSync(ctx, dataSync, clusterPair, reason, message)
			}
			// 在创建 Restore 之前，清理目标集群上可能残留的 Trafficless Pod
			// 这确保 Velero 能重新创建 Pod 并触发 Data Restore（否则若 Pod 存在且 Policy=None，Velero 会跳过）
			cleanup, cleanupErr := r.reconcileTrafficlessPodCleanup(ctx, log, dataSync, config, instance, restoreName, trafficlessCleanupBeforeRestore)
			if cleanup.MetadataChanged {
				if err := r.Update(ctx, dataSync); err != nil {
					return ctrl.Result{}, err
				}
			}
			if cleanupErr != nil {
				reason, message := trafficlessLifecycleErrorDetails(cleanupErr, dataSyncReasonTrafficlessCleanupFailed)
				log.Error(cleanupErr, "清理残留 Trafficless Pod 失败")
				return r.failDataSync(ctx, dataSync, clusterPair, reason, message)
			}
			if !cleanup.Done {
				r.reportDataSyncProgress(ctx, dataSync, clusterPair, cleanup.Message)
				return ctrl.Result{RequeueAfter: trafficlessCleanupRequeueAfter()}, nil
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
				var backupHookStatus *disasterv1.SyncHistoryHookStatus
				if err := r.Get(ctx, types.NamespacedName{Namespace: dataSync.Namespace, Name: fmt.Sprintf("ds-%s", dataSync.Name)}, appBackup); err == nil {
					for _, rec := range appBackup.Status.History {
						if rec.Name == backupName {
							if rec.VeleroStatus != nil {
								if rec.VeleroStatus.Progress != nil {
									backupItems = rec.VeleroStatus.Progress.ItemsBackedUp
								}
								backupHookStatus = syncHistoryHookStatus(rec.VeleroStatus.HookStatus)
							}
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
					BackupHookStatus:     backupHookStatus,
					Status:               string(disasterv1.PhaseFailed),
				}
				if startTime != nil {
					failedRecord.Duration = now.Sub(startTime.Time).String()
				}
				dataSync.Status.History = append(dataSync.Status.History, failedRecord)
				if retention := runtimecfg.SnapshotCurrent().SyncRuntime.HistoryRetention; len(dataSync.Status.History) > retention {
					dataSync.Status.History = dataSync.Status.History[len(dataSync.Status.History)-retention:]
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
					Labels:    dataSyncTrafficlessAppRestoreLabels(dataSync, restoreName),
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
	if isDataSyncTrafficlessLifecycleRestore(restore) && status == disasterv1.PhaseSucceeded {
		pvrReadiness, pvrErr := r.verifyDataSyncPodVolumeRestores(ctx, config, instance, restore)
		if pvrErr != nil {
			reason, message := trafficlessLifecycleErrorDetails(pvrErr, "PodVolumeRestoreFailed")
			restore.Status.Reason = reason
			restore.Status.Message = message
			status = disasterv1.PhaseFailed
		} else if !pvrReadiness.Ready {
			r.reportDataSyncProgress(ctx, dataSync, clusterPair, pvrReadiness.Message)
			return ctrl.Result{RequeueAfter: trafficlessCleanupRequeueAfter()}, nil
		}
	}
	if isDataSyncTrafficlessLifecycleRestore(restore) && status == disasterv1.PhaseSucceeded {
		pvcReadiness, pvcErr := r.verifyDataSyncTargetPVCsReady(ctx, config, instance)
		if pvcErr != nil {
			reason, message := trafficlessLifecycleErrorDetails(pvcErr, dataSyncReasonTargetPVCNotReady)
			restore.Status.Reason = reason
			restore.Status.Message = message
			status = disasterv1.PhaseFailed
		} else if !pvcReadiness.Ready {
			timeout := dataSyncRestoreTimeout(restore, instance)
			if !restore.CreationTimestamp.IsZero() && time.Since(restore.CreationTimestamp.Time) > timeout {
				restore.Status.Reason = dataSyncReasonTargetPVCNotReady
				restore.Status.Message = fmt.Sprintf("%s after %s", pvcReadiness.Message, timeout.Round(time.Second))
				status = disasterv1.PhaseFailed
			} else {
				r.reportDataSyncProgress(ctx, dataSync, clusterPair, pvcReadiness.Message)
				return ctrl.Result{RequeueAfter: trafficlessCleanupRequeueAfter()}, nil
			}
		}
	}
	if isDataSyncTrafficlessLifecycleRestore(restore) && (status == disasterv1.PhaseSucceeded || disasterv1.IsFailedAppRestorePhase(status)) {
		cleanup, cleanupErr := r.reconcileTrafficlessPodCleanup(ctx, log, dataSync, config, instance, restoreName, trafficlessCleanupAfterRestore)
		if cleanup.MetadataChanged {
			if err := r.Update(ctx, dataSync); err != nil {
				return ctrl.Result{}, err
			}
		}
		if cleanupErr != nil {
			reason, message := trafficlessLifecycleErrorDetails(cleanupErr, dataSyncReasonTrafficlessCleanupFailed)
			if restore.Status.Reason != "" || restore.Status.Message != "" {
				message = fmt.Sprintf("%s; AppRestore failure context reason=%s message=%s", message, restore.Status.Reason, restore.Status.Message)
			}
			restore.Status.Reason = reason
			restore.Status.Message = message
			status = disasterv1.PhaseFailed
		} else if !cleanup.Done {
			r.reportDataSyncProgress(ctx, dataSync, clusterPair, cleanup.Message)
			return ctrl.Result{RequeueAfter: trafficlessCleanupRequeueAfter()}, nil
		}
	}
	if status == disasterv1.PhaseSucceeded || disasterv1.IsFailedAppRestorePhase(status) {
		msg := "数据同步成功完成"
		finalState := disasterv1.DataSyncStateReady
		if disasterv1.IsFailedAppRestorePhase(status) {
			reason := restore.Status.Reason
			if reason == "" {
				reason = dataSyncReasonRestoreFailed
			}
			msg = fmt.Sprintf("数据恢复失败: %s status=%s", restoreName, status)
			if restore.Status.Message != "" {
				msg = fmt.Sprintf("%s: %s", msg, restore.Status.Message)
			}
			finalState = disasterv1.DataSyncStateFailed
			helper.SetStatusError(&dataSync.Status, reason, msg)
			helper.SetConditionError(&dataSync.Status.Conditions, "RestoreFailed", reason, msg)
			log.Info("AppRestore 失败", "name", restoreName, "status", status)
		} else {
			log.Info("AppRestore 成功", "name", restoreName)
			helper.ClearStatusError(&dataSync.Status)
		}

		dataSync.Status.State = finalState
		now := metav1.Now()
		dataSync.Status.LastSyncTime = &now

		// --- 记录历史 ---
		appBackup := &disasterv1.AppBackup{}
		if err := r.Get(ctx, types.NamespacedName{Namespace: dataSync.Namespace, Name: fmt.Sprintf("ds-%s", dataSync.Name)}, appBackup); err == nil {
			var backupItems int
			var startTime *metav1.Time
			var backupHookStatus *disasterv1.SyncHistoryHookStatus
			for _, rec := range appBackup.Status.History {
				if rec.Name == backupName {
					if rec.VeleroStatus != nil {
						if rec.VeleroStatus.Progress != nil {
							backupItems = rec.VeleroStatus.Progress.ItemsBackedUp
						}
						backupHookStatus = syncHistoryHookStatus(rec.VeleroStatus.HookStatus)
					}
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
				BackupHookStatus:     backupHookStatus,
				RestoreHookStatus:    syncHistoryHookStatus(restore.Status.RestoreStatus.HookStatus),
				Status:               string(status),
			}
			if startTime != nil {
				record.Duration = now.Sub(startTime.Time).String()
			}

			dataSync.Status.History = append(dataSync.Status.History, record)
			if retention := runtimecfg.SnapshotCurrent().SyncRuntime.HistoryRetention; len(dataSync.Status.History) > retention {
				dataSync.Status.History = dataSync.Status.History[len(dataSync.Status.History)-retention:]
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

	return ctrl.Result{RequeueAfter: runtimecfg.SnapshotCurrent().SyncRuntime.RestoreObserveRequeue}, nil
}

// syncStatistics 更新关联的 BackupRestoreStatistics CR
func (r *DataSyncReconciler) syncStatistics(ctx context.Context, ds *disasterv1.DataSync) error {
	var total, completed, failed, inProgress int32
	if ds.Status.State == disasterv1.DataSyncStateInProgress {
		inProgress = 1
	}

	for _, h := range ds.Status.History {
		total++
		if h.Status == string(disasterv1.PhaseSucceeded) || h.Status == "Completed" || h.Status == dataSyncHistoryStatusSkipped {
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
	spec := disasterv1.AppBackupSpec{
		Cluster: source,
		Timeout: ctrlcommon.ResolveAppBackupTimeout(instance),
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
	if uploaderConfig := resolveDataSyncBackupUploaderConfig(); uploaderConfig != nil {
		spec.Template.UploaderConfig = uploaderConfig
	}
	if hooks := dataBackupHooks(instance); hooks != nil {
		spec.Template.Hooks = *hooks
	}
	return spec
}

func resolveDataSyncBackupUploaderConfig() *velerov1.UploaderConfigForBackup {
	raw := strings.TrimSpace(os.Getenv(appBackupParallelFilesUploadEnv))
	if raw == "" {
		return nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return nil
	}
	return &velerov1.UploaderConfigForBackup{ParallelFilesUpload: value}
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

	trafficlessRuntime, trafficlessCluster, err := ctrlcommon.ResolveTrafficlessRuntime(ctx, r.Client, target, dataSync.Spec.TrafficlessConfig)
	if err != nil {
		return disasterv1.AppRestoreSpec{}, restorebuilder.PolicySummary{}, err
	}

	var targetClient client.Client
	if trafficlessRuntime.PullSecretName != "" {
		c, err := r.getTargetClusterClient(ctx, target)
		if err != nil {
			return disasterv1.AppRestoreSpec{}, restorebuilder.PolicySummary{}, fmt.Errorf("build target cluster client for trafficless registry secret: %w", err)
		}
		targetClient = c
		if _, err := ctrlcommon.SyncTrafficlessRegistryPullSecret(ctx, r.Client, targetClient, trafficlessCluster, instance.Spec.Namespaces); err != nil {
			return disasterv1.AppRestoreSpec{}, restorebuilder.PolicySummary{}, fmt.Errorf("sync trafficless registry pull secret: %w", err)
		}
	}

	preparedDataRestoreHooks, hookMarkerRules := restorebuilder.PrepareTrafficlessDataRestoreHooks(
		dataRestoreHooks(instance),
		instance.Spec.Namespaces,
		nil,
	)
	dataModifiers := r.makeTrafficlessModifiers(dataSync, trafficlessRuntime, dataSyncRestoreName(dataSync, backupName))
	if len(hookMarkerRules) > 0 && instance.Spec.RestorePolicy == nil {
		dataModifiers = append(dataModifiers, hookMarkerRules...)
	}
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
		DataRestoreHooks:          preparedDataRestoreHooks,
	})
	spec.Timeout = ctrlcommon.ResolveAppBackupTimeout(instance)

	if restorebuilder.RequiresTargetClassValidation(instance) {
		if targetClient == nil {
			c, err := r.getTargetClusterClient(ctx, target)
			if err != nil {
				return disasterv1.AppRestoreSpec{}, restorebuilder.PolicySummary{}, fmt.Errorf("build target cluster client for restore policy: %w", err)
			}
			targetClient = c
		}
	}

	applyOpts := []restorebuilder.ApplyInstanceRestorePolicyOption{
		restorebuilder.WithBaselineClusters(config.Spec.SourceCluster, config.Spec.TargetCluster),
		restorebuilder.WithApplyTarget(disasterv1.RestoreModifierApplyDataSync),
	}
	systemProtectRules := make([]disasterv1.ResourceModifierRule, 0, 1+len(hookMarkerRules))
	// With restorePolicy present, inject as system-protect so user rules cannot override this safety patch.
	if shouldCleanupPVCVolumeName && instance.Spec.RestorePolicy != nil {
		systemProtectRules = append(systemProtectRules, cleanupPVCVolumeRule)
	}
	if len(hookMarkerRules) > 0 && instance.Spec.RestorePolicy != nil {
		systemProtectRules = append(systemProtectRules, hookMarkerRules...)
	}
	if len(systemProtectRules) > 0 {
		applyOpts = append(applyOpts, restorebuilder.WithSystemProtectRules(systemProtectRules))
	}

	summary, err := restorebuilder.ApplyInstanceRestorePolicy(ctx, &spec, instance, targetClient, applyOpts...)
	if err != nil {
		return disasterv1.AppRestoreSpec{}, restorebuilder.PolicySummary{}, err
	}
	return spec, summary, nil
}

func dataBackupHooks(instance *disasterv1.DisasterInstance) *velerov1.BackupHooks {
	if instance == nil || instance.Spec.VeleroHooks == nil {
		return nil
	}
	return instance.Spec.VeleroHooks.DataBackup
}

func dataRestoreHooks(instance *disasterv1.DisasterInstance) *velerov1.RestoreHooks {
	if instance == nil || instance.Spec.VeleroHooks == nil {
		return nil
	}
	return instance.Spec.VeleroHooks.DataRestore
}

func syncHistoryHookStatus(status *velerov1.HookStatus) *disasterv1.SyncHistoryHookStatus {
	if status == nil {
		return nil
	}
	return &disasterv1.SyncHistoryHookStatus{
		HooksAttempted: status.HooksAttempted,
		HooksFailed:    status.HooksFailed,
	}
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
func (r *DataSyncReconciler) makeTrafficlessModifiers(ds *disasterv1.DataSync, runtime ctrlcommon.TrafficlessRuntime, restoreName string) []disasterv1.ResourceModifierRule {
	trafficlessImage := runtime.Image
	if trafficlessImage == "" {
		trafficlessImage = r.resolveTrafficlessImage(ds)
	}
	trafficlessCommand := runtime.Command
	if len(trafficlessCommand) == 0 {
		trafficlessCommand = r.resolveTrafficlessCommand(ds)
	}
	commandJSON, err := json.Marshal(trafficlessCommand)
	if err != nil {
		// fallback to safe default command
		commandJSON = []byte(`["sleep","3600"]`)
	}

	trafficlessLabels, err := json.Marshal(dataSyncTrafficlessLabels(ds, restoreName))
	if err != nil {
		trafficlessLabels = []byte(`{"trafficless":"true"}`)
	}

	// 针对 Pods 直接修改 (因为 IncludedResources只包含 Pods)
	// 注意：JSON Patch 操作顺序很重要！
	// 1. 清除原有 labels（防止 Service 导流 + 防止 STS/Deployment 识别）
	// 2. 将 ownerReferences 置空（防止 GC）
	// 3. 清理源集群调度约束，避免临时恢复 Pod 在目标集群 Pending
	// 4. 替换容器配置
	podPatches := []disasterv1.JSONPatch{
		// 清除所有原有标签 - 替换为只包含 trafficless 的 map
		// 这样 StatefulSet 的 selector 就无法匹配到这个 Pod
		// 使用 add 操作覆盖整个 map，Velero 支持这种行为
		{
			Operation: "add",
			Path:      "/metadata/labels",
			Value:     string(trafficlessLabels),
		},
		// 将 ownerReferences 置空，避免临时 Pod 被控制器/GC 回收。
		// 使用 add + [] 保持幂等：字段不存在时不会触发 "expected one matching path ... got 0"。
		{
			Operation: "add",
			Path:      "/metadata/ownerReferences",
			Value:     "[]",
		},
		// 清理源集群节点绑定和选择器；trafficless Pod 只需要能调度并写入 PVC。
		{
			Operation: "add",
			Path:      "/spec/nodeName",
			Value:     "",
		},
		{
			Operation: "add",
			Path:      "/spec/nodeSelector",
			Value:     "{}",
		},
		{
			Operation: "add",
			Path:      "/spec/affinity",
			Value:     "{}",
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
		// command 已改为 trafficless 启动命令，必须同步清空原始 args。
		// 否则 busybox sleep 会把原 workload args 当作参数并退出，post restore exec hook 无法稳定进入容器。
		{
			Operation: "add",
			Path:      "/spec/containers/0/args",
			Value:     "[]",
		},
	}
	if pullSecretPatch, ok := ctrlcommon.TrafficlessImagePullSecretsPatch(runtime.PullSecretName); ok {
		podPatches = append(podPatches, pullSecretPatch)
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
