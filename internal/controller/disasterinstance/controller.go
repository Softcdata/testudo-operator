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

package disasterinstance

import (
	"context"
	stderrors "errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	runtimecfg "github.com/softcdata/testudo-operator/internal/controller/runtimeconfig"
	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	"github.com/softcdata/testudo-operator/pkg/helper"
	"github.com/softcdata/testudo-operator/pkg/metadata"
)

const (
	finalizerName     = "testudo.softcdata.com/disasterinstance-finalizer"
	AnnotationTraceID = metadata.AnnotationTraceID
	TraceIDKey        = metadata.TraceIDKey

	instanceReasonDataSyncFailed     = "DataSyncFailed"
	instanceReasonResourceSyncFailed = "ResourceSyncFailed"
	instanceReasonInitializationFail = "InitializationFailed"
	instanceReasonInstanceFailed     = "InstanceFailed"
	instanceReasonFailoverMissing    = "FailoverOperationMissing"
	instanceReasonFailoverFailed     = "FailoverOperationFailed"
	instanceReasonFailbackMissing    = "FailbackOperationMissing"
	instanceReasonFailbackFailed     = "FailbackOperationFailed"
	instanceReasonConfigNotFound     = "ConfigNotFound"
	instanceReasonConfigNotReady     = "ConfigNotReady"
	instanceReasonConfigError        = "ConfigError"
	instanceReasonStableStateMissing = "LastStableStateMissing"

	defaultTransitionWatchdogTimeout = 2 * time.Minute
	minTransitionWatchdogTimeout     = 30 * time.Second
)

func instanceRuntime() runtimecfg.InstanceRuntime {
	return runtimecfg.SnapshotCurrent().InstanceRuntime
}

func instanceInitializingRequeue() time.Duration {
	return instanceRuntime().InitializingRequeue
}

func instanceSteadyRequeue() time.Duration {
	return instanceRuntime().SteadyRequeue
}

func instanceFailedRequeue() time.Duration {
	return instanceRuntime().FailedRequeue
}

// DisasterInstanceReconciler 负责调谐 DisasterInstance 对象
type DisasterInstanceReconciler struct {
	client.Client
	Scheme           *runtime.Scheme
	Log              logr.Logger
	Recorder         record.EventRecorder
	KubeClientGetter func(ctx context.Context, cli client.Client, scheme *runtime.Scheme, clusterName string) (client.Client, error)
}

func instanceAuditContext(instance *disasterv1.DisasterInstance) (user, traceID string) {
	if instance == nil {
		return "system", "-"
	}
	user = instance.Annotations[metadata.AnnotationUser]
	if user == "" {
		user = "system"
	}
	traceID = instance.Annotations[metadata.AnnotationLastTraceID]
	if traceID == "" {
		traceID = instance.Annotations[metadata.AnnotationTraceID]
	}
	if traceID == "" {
		traceID = "-"
	}
	return user, traceID
}

func (r *DisasterInstanceReconciler) reportStructuredTaskStarted(ctx context.Context, instance *disasterv1.DisasterInstance, taskName, cluster, msg string) {
	user, traceID := instanceAuditContext(instance)
	helper.ReportTaskStartedWithClient(ctx, r.Client, r.Scheme, instance, taskName, cluster, user, traceID, msg)
}

func (r *DisasterInstanceReconciler) reportStructuredTaskProgress(ctx context.Context, instance *disasterv1.DisasterInstance, taskName, cluster, msg string) {
	user, traceID := instanceAuditContext(instance)
	helper.ReportTaskProgressWithClient(ctx, r.Client, r.Scheme, instance, taskName, cluster, user, traceID, msg)
}

func (r *DisasterInstanceReconciler) reportStructuredTaskFinished(ctx context.Context, instance *disasterv1.DisasterInstance, taskName, cluster, status, msg string) {
	user, traceID := instanceAuditContext(instance)
	now := metav1.Now()
	if status == helper.TaskStatusFailed {
		errorCode := instance.Status.Reason
		if errorCode == "" {
			errorCode = instanceReasonInstanceFailed
		}
		helper.ReportTaskFinishedWithClient(ctx, r.Client, r.Scheme, instance, taskName, cluster, status, nil, &now, user, traceID, msg, errorCode)
		return
	}
	helper.ReportTaskFinishedWithClient(ctx, r.Client, r.Scheme, instance, taskName, cluster, status, nil, &now, user, traceID, msg)
}

// +kubebuilder:rbac:groups=testudo.softcdata.com,resources=disasterinstances,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=testudo.softcdata.com,resources=disasterinstances/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=testudo.softcdata.com,resources=disasterinstances/finalizers,verbs=update
// +kubebuilder:rbac:groups=testudo.softcdata.com,resources=disasterconfigs,verbs=get;list;watch
// +kubebuilder:rbac:groups=testudo.softcdata.com,resources=disasterpolicies,verbs=get;list;watch
// +kubebuilder:rbac:groups=testudo.softcdata.com,resources=datasyncs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=testudo.softcdata.com,resources=resourcesyncs,verbs=get;list;watch;create;update;patch;delete

// Reconcile 处理 DisasterInstance 的调谐循环
func (r *DisasterInstanceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("disasterinstance", req.NamespacedName)
	log.V(1).Info("正在调谐 DisasterInstance")

	// 获取 DisasterInstance
	instance := &disasterv1.DisasterInstance{}
	if err := r.Get(ctx, req.NamespacedName, instance); err != nil {
		if errors.IsNotFound(err) {
			log.Info("DisasterInstance 未找到，可能已被删除")
			return ctrl.Result{}, nil
		}
		log.Error(err, "获取 DisasterInstance 失败")
		return ctrl.Result{}, err
	}

	// 添加 TraceID 到日志和上下文，遵循全局 TraceID 规范
	if traceID := instance.Annotations[AnnotationTraceID]; traceID != "" {
		log = log.WithValues(TraceIDKey, traceID)
		ctx = context.WithValue(ctx, TraceIDKey, traceID)
	}

	// 处理删除
	if !instance.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, log, instance)
	}

	// 如果没有 Finalizer，添加它
	if !controllerutil.ContainsFinalizer(instance, finalizerName) {
		controllerutil.AddFinalizer(instance, finalizerName)
		if err := r.Update(ctx, instance); err != nil {
			log.Error(err, "添加 Finalizer 失败")
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Sync dependency labels
	if changed, err := r.syncDependencyLabels(ctx, instance); err != nil {
		log.Error(err, "同步依赖标签失败")
		return ctrl.Result{}, err
	} else if changed {
		if err := r.Update(ctx, instance); err != nil {
			log.Error(err, "更新依赖标签失败")
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// 初始化 FsmState（如果为空）
	if instance.Status.FsmState == "" {
		instance.Status.FsmState = disasterv1.FsmStatePending
		if err := r.Status().Update(ctx, instance); err != nil {
			log.Error(err, "初始化 FsmState 失败")
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	handled, result, err := r.guardByConfigHealth(ctx, log, instance)
	if err != nil {
		log.Error(err, "配置健康守卫执行失败")
		return ctrl.Result{}, err
	}
	if handled {
		return result, nil
	}

	// 状态机路由
	switch instance.Status.FsmState {
	case disasterv1.FsmStatePending:
		return r.handlePending(ctx, log, instance)
	case disasterv1.FsmStateInitializing:
		return r.handleInitializing(ctx, log, instance)
	case disasterv1.FsmStateProtected:
		return r.handleProtected(ctx, log, instance)
	case disasterv1.FsmStatePaused:
		return r.handlePaused(ctx, log, instance)
	case disasterv1.FsmStateFailingOver:
		return r.handleFailingOver(ctx, log, instance)
	case disasterv1.FsmStateActive:
		return r.handleActive(ctx, log, instance)
	case disasterv1.FsmStateFailingBack:
		return r.handleFailingBack(ctx, log, instance)
	case disasterv1.FsmStateConfigError:
		return r.handleConfigError(ctx, log, instance)
	case disasterv1.FsmStateFailed:
		return r.handleFailed(ctx, log, instance)
	default:
		log.Info("未知的 FsmState，重置为 Pending", "fsmState", instance.Status.FsmState)
		instance.Status.FsmState = disasterv1.FsmStatePending
		if err := r.Status().Update(ctx, instance); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}
}

func (r *DisasterInstanceReconciler) isConfigGuardManagedState(state string) bool {
	switch state {
	case disasterv1.FsmStatePending,
		disasterv1.FsmStateInitializing,
		disasterv1.FsmStateProtected,
		disasterv1.FsmStatePaused,
		disasterv1.FsmStateActive,
		disasterv1.FsmStateConfigError:
		return true
	default:
		return false
	}
}

func (r *DisasterInstanceReconciler) evaluateConfigHealth(ctx context.Context, instance *disasterv1.DisasterInstance) (healthy bool, reason string, message string, err error) {
	if instance == nil || instance.Spec.Config == "" {
		return true, "", "", nil
	}

	config := &disasterv1.DisasterConfig{}
	if err := r.Get(ctx, client.ObjectKey{Name: instance.Spec.Config}, config); err != nil {
		if errors.IsNotFound(err) {
			return false, instanceReasonConfigNotFound, fmt.Sprintf("DisasterConfig %s not found", instance.Spec.Config), nil
		}
		return false, "", "", err
	}

	if config.Status.Status == disasterv1.DisasterConfigStatusError {
		reason := config.Status.Reason
		if reason == "" {
			reason = instanceReasonConfigError
		}
		message := config.Status.Message
		if message == "" {
			message = fmt.Sprintf("DisasterConfig %s status=%s", instance.Spec.Config, config.Status.Status)
		}
		return false, reason, message, nil
	}

	if config.Status.Status == disasterv1.DisasterConfigStatusNotReady {
		reason := config.Status.Reason
		if reason == "" {
			reason = instanceReasonConfigNotReady
		}
		message := config.Status.Message
		if message == "" {
			message = fmt.Sprintf("DisasterConfig %s status=%s", instance.Spec.Config, config.Status.Status)
		}
		return false, reason, message, nil
	}

	return true, "", "", nil
}

func (r *DisasterInstanceReconciler) guardByConfigHealth(ctx context.Context, log logr.Logger, instance *disasterv1.DisasterInstance) (handled bool, result ctrl.Result, err error) {
	if !r.isConfigGuardManagedState(instance.Status.FsmState) {
		return false, ctrl.Result{}, nil
	}

	healthy, reason, message, err := r.evaluateConfigHealth(ctx, instance)
	if err != nil {
		return false, ctrl.Result{}, err
	}

	// ConfigError state is fully managed by the guard to enforce deterministic recovery.
	if instance.Status.FsmState == disasterv1.FsmStateConfigError {
		if healthy {
			if instance.Status.LastStableFsmState == "" {
				instance.Status.AvailableOperations = []string{}
				helper.SetStatusError(
					&instance.Status,
					instanceReasonStableStateMissing,
					"lastStableFsmState is empty, cannot recover from ConfigError",
				)
				if err := r.Status().Update(ctx, instance); err != nil {
					return true, ctrl.Result{}, err
				}
				log.Info("配置恢复但缺少原状态记忆，保持 ConfigError", "instance", instance.Name)
				return true, ctrl.Result{RequeueAfter: 30 * time.Second}, nil
			}

			recoverState := instance.Status.LastStableFsmState
			instance.Status.FsmState = recoverState
			instance.Status.LastStableFsmState = ""
			helper.ClearStatusError(&instance.Status)
			if err := r.Status().Update(ctx, instance); err != nil {
				return true, ctrl.Result{}, err
			}
			log.Info("配置已恢复，实例回放到原状态", "instance", instance.Name, "state", recoverState)
			return true, ctrl.Result{Requeue: true}, nil
		}

		instance.Status.AvailableOperations = []string{}
		helper.SetStatusError(&instance.Status, reason, message)
		if err := r.Status().Update(ctx, instance); err != nil {
			return true, ctrl.Result{}, err
		}
		return true, ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	if healthy {
		return false, ctrl.Result{}, nil
	}

	instance.Status.LastStableFsmState = instance.Status.FsmState
	instance.Status.FsmState = disasterv1.FsmStateConfigError
	instance.Status.AvailableOperations = []string{}
	helper.SetStatusError(&instance.Status, reason, message)
	if err := r.Status().Update(ctx, instance); err != nil {
		return true, ctrl.Result{}, err
	}
	log.Info("配置异常，实例进入 ConfigError", "instance", instance.Name, "from", instance.Status.LastStableFsmState, "reason", reason)
	return true, ctrl.Result{RequeueAfter: 5 * time.Second}, nil
}

// handlePending 处理 Pending 状态
// - 获取 DisasterConfig
// - 创建带 OwnerReference 的 DataSync 和 ResourceSync
// - 转换到 Initializing 状态
func (r *DisasterInstanceReconciler) handlePending(ctx context.Context, log logr.Logger, instance *disasterv1.DisasterInstance) (ctrl.Result, error) {
	log.Info("处理 Pending 状态")

	// 获取 DisasterConfig
	config := &disasterv1.DisasterConfig{}
	if err := r.Get(ctx, client.ObjectKey{Name: instance.Spec.Config}, config); err != nil {
		if errors.IsNotFound(err) {
			log.Error(err, "DisasterConfig 未找到", "config", instance.Spec.Config)
			r.Recorder.Eventf(instance, "Warning", "ConfigNotFound", "DisasterConfig %s 未找到", instance.Spec.Config)
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}
		return ctrl.Result{}, err
	}

	// 创建或更新 DataSync
	dataSyncName := fmt.Sprintf("dr-ds-%s", instance.Name)
	if err := r.ensureDataSync(ctx, log, instance, config, dataSyncName); err != nil {
		var pnr *policyNotReadyError
		if stderrors.As(err, &pnr) {
			log.Info("策略暂时不可用，等待重试", "requeue", pnr.requeueAfter)
			r.Recorder.Eventf(instance, "Warning", "PolicyNotReady", "引用的 DisasterPolicy 暂时不可用，将在 %v 后重试", pnr.requeueAfter)
			return ctrl.Result{RequeueAfter: pnr.requeueAfter}, nil
		}
		return ctrl.Result{}, err
	}
	instance.Status.DataSyncName = dataSyncName

	// 创建或更新 ResourceSync
	resourceSyncName := fmt.Sprintf("dr-rs-%s", instance.Name)
	if err := r.ensureResourceSync(ctx, log, instance, config, resourceSyncName); err != nil {
		var pnr *policyNotReadyError
		if stderrors.As(err, &pnr) {
			log.Info("策略暂时不可用，等待重试", "requeue", pnr.requeueAfter)
			r.Recorder.Eventf(instance, "Warning", "PolicyNotReady", "引用的 DisasterPolicy 暂时不可用，将在 %v 后重试", pnr.requeueAfter)
			return ctrl.Result{RequeueAfter: pnr.requeueAfter}, nil
		}
		return ctrl.Result{}, err
	}
	instance.Status.ResourceSyncName = resourceSyncName

	// 从配置更新主/备集群
	instance.Status.PrimaryCluster = config.Spec.SourceCluster
	instance.Status.SecondaryCluster = config.Spec.TargetCluster
	instanceTaskName := fmt.Sprintf("创建容灾实例 %s", instance.Name)
	r.reportStructuredTaskStarted(ctx, instance, instanceTaskName, fmt.Sprintf("%s->%s", config.Spec.SourceCluster, config.Spec.TargetCluster), "容灾实例初始化已开始")
	r.reportStructuredTaskProgress(ctx, instance, instanceTaskName, fmt.Sprintf("%s->%s", config.Spec.SourceCluster, config.Spec.TargetCluster), fmt.Sprintf("已创建 DataSync=%s 与 ResourceSync=%s", dataSyncName, resourceSyncName))

	// 转换到 Initializing 状态
	instance.Status.FsmState = disasterv1.FsmStateInitializing
	if err := r.Status().Update(ctx, instance); err != nil {
		log.Error(err, "更新状态到 Initializing 失败")
		return ctrl.Result{}, err
	}

	r.Recorder.Event(instance, "Normal", "Initializing", "DataSync 和 ResourceSync 已创建，正在初始化首次同步")
	log.Info("已转换到 Initializing 状态")
	return ctrl.Result{Requeue: true}, nil
}

// handleInitializing 处理 Initializing 状态
// - 检查 DataSync 和 ResourceSync 状态
// - 当两者都 Ready 时转换到 Protected 状态
func (r *DisasterInstanceReconciler) handleInitializing(ctx context.Context, log logr.Logger, instance *disasterv1.DisasterInstance) (ctrl.Result, error) {
	log.Info("处理 Initializing 状态")
	instanceTaskName := fmt.Sprintf("创建容灾实例 %s", instance.Name)
	clusterPair := fmt.Sprintf("%s->%s", instance.Status.PrimaryCluster, instance.Status.SecondaryCluster)

	// 检查 DataSync 状态
	dataSync := &disasterv1.DataSync{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: instance.Namespace, Name: instance.Status.DataSyncName}, dataSync); err != nil {
		if errors.IsNotFound(err) {
			log.Info("DataSync 未找到，重新入队")
			return ctrl.Result{RequeueAfter: instanceInitializingRequeue()}, nil
		}
		return ctrl.Result{}, err
	}

	// 检查 ResourceSync 状态
	resourceSync := &disasterv1.ResourceSync{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: instance.Namespace, Name: instance.Status.ResourceSyncName}, resourceSync); err != nil {
		if errors.IsNotFound(err) {
			log.Info("ResourceSync 未找到，重新入队")
			return ctrl.Result{RequeueAfter: instanceInitializingRequeue()}, nil
		}
		return ctrl.Result{}, err
	}

	// 检查两者是否都就绪（状态为 Ready 且已完成过至少一次同步）
	dataSyncReady := dataSync.Status.State == disasterv1.DataSyncStateReady && dataSync.Status.LastSyncTime != nil
	resourceSyncReady := resourceSync.Status.State == disasterv1.ResourceSyncStateReady && resourceSync.Status.LastSyncTime != nil

	log.V(1).Info("同步状态", "dataSyncReady", dataSyncReady, "resourceSyncReady", resourceSyncReady,
		"dataSyncLastSync", dataSync.Status.LastSyncTime, "resourceSyncLastSync", resourceSync.Status.LastSyncTime)

	// 任一子资源进入 Failed，实例应结束初始化并进入 Failed，避免长期停留在 Initializing
	if dataSync.Status.State == disasterv1.DataSyncStateFailed || resourceSync.Status.State == disasterv1.ResourceSyncStateFailed {
		reason, message := deriveInstanceInitializationFailure(dataSync, resourceSync)
		instance.Status.FsmState = disasterv1.FsmStateFailed
		helper.SetStatusError(&instance.Status, reason, message)
		instance.Status.AvailableOperations = syncFailureAvailableOperations(reason, dataSync, resourceSync)
		instance.Status.LastDataSyncTime = dataSync.Status.LastSyncTime
		instance.Status.LastResourceSyncTime = resourceSync.Status.LastSyncTime
		if err := r.Status().Update(ctx, instance); err != nil {
			return ctrl.Result{}, err
		}
		r.Recorder.Event(instance, "Warning", "InitializationFailed", "初始化同步失败，实例已进入 Failed 状态")
		r.reportStructuredTaskFinished(ctx, instance, instanceTaskName, clusterPair, helper.TaskStatusFailed, message)
		log.Info("初始化失败，实例转换为 Failed",
			"dataSyncState", dataSync.Status.State, "resourceSyncState", resourceSync.Status.State)
		return ctrl.Result{}, nil
	}

	if dataSyncReady && resourceSyncReady {
		// 转换到 Protected 状态
		instance.Status.FsmState = disasterv1.FsmStateProtected
		helper.ClearStatusError(&instance.Status)
		instance.Status.AvailableOperations = []string{"failover", "pause", "synconce", "syncdata", "syncresource"}
		instance.Status.LastDataSyncTime = dataSync.Status.LastSyncTime
		instance.Status.LastResourceSyncTime = resourceSync.Status.LastSyncTime

		if err := r.Status().Update(ctx, instance); err != nil {
			return ctrl.Result{}, err
		}

		r.Recorder.Event(instance, "Normal", "Protected", "初始同步完成，现在处于保护状态")
		r.reportStructuredTaskFinished(ctx, instance, instanceTaskName, clusterPair, helper.TaskStatusSuccess, "初始同步完成，实例进入 Protected 状态")
		log.Info("已转换到 Protected 状态")
		return ctrl.Result{}, nil
	}

	r.reportStructuredTaskProgress(
		ctx,
		instance,
		instanceTaskName,
		clusterPair,
		fmt.Sprintf("等待首次同步完成 (DataSync=%s, ResourceSync=%s)", dataSync.Status.State, resourceSync.Status.State),
	)

	// 仍在初始化，重新入队
	return ctrl.Result{RequeueAfter: instanceInitializingRequeue()}, nil
}

// reconcileSyncSchedules keeps DataSync/ResourceSync schedule in sync with latest referenced policies
// for already-initialized instances (e.g. Protected/Active).
func (r *DisasterInstanceReconciler) reconcileSyncSchedules(ctx context.Context, log logr.Logger, instance *disasterv1.DisasterInstance) (time.Duration, error) {
	config := &disasterv1.DisasterConfig{}
	if err := r.Get(ctx, client.ObjectKey{Name: instance.Spec.Config}, config); err != nil {
		if errors.IsNotFound(err) {
			// 稳态场景下按最佳努力对齐调度，不阻断主状态机。
			log.Info("DisasterConfig 未找到，跳过本轮调度对齐", "config", instance.Spec.Config)
			return 0, nil
		}
		return 0, err
	}

	dataSyncName := instance.Status.DataSyncName
	if dataSyncName == "" {
		dataSyncName = fmt.Sprintf("dr-ds-%s", instance.Name)
	}
	if err := r.ensureDataSync(ctx, log, instance, config, dataSyncName); err != nil {
		var pnr *policyNotReadyError
		if stderrors.As(err, &pnr) {
			log.Info("DataSync 策略暂未就绪，跳过本轮调度对齐", "requeueAfter", pnr.requeueAfter)
			return 0, nil
		}
		return 0, err
	}
	instance.Status.DataSyncName = dataSyncName

	resourceSyncName := instance.Status.ResourceSyncName
	if resourceSyncName == "" {
		resourceSyncName = fmt.Sprintf("dr-rs-%s", instance.Name)
	}
	if err := r.ensureResourceSync(ctx, log, instance, config, resourceSyncName); err != nil {
		var pnr *policyNotReadyError
		if stderrors.As(err, &pnr) {
			log.Info("ResourceSync 策略暂未就绪，跳过本轮调度对齐", "requeueAfter", pnr.requeueAfter)
			return 0, nil
		}
		return 0, err
	}
	instance.Status.ResourceSyncName = resourceSyncName

	return 0, nil
}

func effectiveDataSyncPolicyName(instance *disasterv1.DisasterInstance, config *disasterv1.DisasterConfig) string {
	if instance != nil && strings.TrimSpace(instance.Spec.DataSyncPolicy) != "" {
		return strings.TrimSpace(instance.Spec.DataSyncPolicy)
	}
	if config == nil {
		return ""
	}
	return strings.TrimSpace(config.Spec.DataSyncPolicy)
}

func effectiveResourceSyncPolicyName(instance *disasterv1.DisasterInstance, config *disasterv1.DisasterConfig) string {
	if instance != nil && strings.TrimSpace(instance.Spec.ResourceSyncPolicy) != "" {
		return strings.TrimSpace(instance.Spec.ResourceSyncPolicy)
	}
	if config == nil {
		return ""
	}
	return strings.TrimSpace(config.Spec.ResourceSyncPolicy)
}

// handleProtected 处理 Protected 状态（正常运行状态）
func (r *DisasterInstanceReconciler) handleProtected(ctx context.Context, log logr.Logger, instance *disasterv1.DisasterInstance) (ctrl.Result, error) {
	log.V(1).Info("处理 Protected 状态")

	// 在稳态周期中持续对齐策略调度，确保策略更新可传播到既有 DataSync/ResourceSync。
	if requeueAfter, err := r.reconcileSyncSchedules(ctx, log, instance); err != nil {
		return ctrl.Result{}, err
	} else if requeueAfter > 0 {
		return ctrl.Result{RequeueAfter: requeueAfter}, nil
	}

	hasFailure, reason, message, lastDataSyncTime, lastResourceSyncTime, err := r.evaluateSteadySyncHealth(ctx, instance)
	if err != nil {
		return ctrl.Result{}, err
	}
	if hasFailure {
		instance.Status.FsmState = disasterv1.FsmStateFailed
		helper.SetStatusError(&instance.Status, reason, message)
		instance.Status.AvailableOperations = r.currentSyncFailureAvailableOperations(ctx, instance, reason)
		instance.Status.LastDataSyncTime = lastDataSyncTime
		instance.Status.LastResourceSyncTime = lastResourceSyncTime
		if err := r.Status().Update(ctx, instance); err != nil {
			return ctrl.Result{}, err
		}
		r.Recorder.Eventf(instance, "Warning", "SteadySyncFailed", "稳态同步失败，实例进入 Failed: %s", message)
		return ctrl.Result{}, nil
	}

	if handled, err := r.guardByRoleDrift(ctx, log, instance); err != nil {
		return ctrl.Result{}, err
	} else if handled {
		return ctrl.Result{RequeueAfter: instanceFailedRequeue()}, nil
	}

	// 更新可用操作
	helper.ClearStatusError(&instance.Status)
	instance.Status.AvailableOperations = []string{"failover", "pause", "synconce", "syncdata", "syncresource"}
	instance.Status.LastDataSyncTime = lastDataSyncTime
	instance.Status.LastResourceSyncTime = lastResourceSyncTime

	if err := r.Status().Update(ctx, instance); err != nil {
		return ctrl.Result{}, err
	}

	// 在 Protected 状态下，定期检查子资源状态
	return ctrl.Result{RequeueAfter: instanceSteadyRequeue()}, nil
}

// handlePaused 处理 Paused 状态
func (r *DisasterInstanceReconciler) handlePaused(ctx context.Context, log logr.Logger, instance *disasterv1.DisasterInstance) (ctrl.Result, error) {
	log.V(1).Info("处理 Paused 状态")

	helper.ClearStatusError(&instance.Status)
	instance.Status.AvailableOperations = []string{"resume"}

	if err := r.Status().Update(ctx, instance); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: instanceSteadyRequeue()}, nil
}

// handleFailingOver 处理 FailingOver 状态
func (r *DisasterInstanceReconciler) handleFailingOver(ctx context.Context, log logr.Logger, instance *disasterv1.DisasterInstance) (ctrl.Result, error) {
	log.Info("处理 FailingOver 状态")

	snapshot, err := r.collectOperationWatchdogSnapshot(ctx, instance, disasterv1.OperationTypeFailover)
	if err != nil {
		return ctrl.Result{}, err
	}

	// 正常中间态：仍有进行中的 failover 操作，等待其推进，不覆盖错误上下文。
	if snapshot.runningCount > 0 {
		if len(instance.Status.AvailableOperations) != 0 {
			if err := r.updateInstanceStatusWithRetry(ctx, instance, func(latest *disasterv1.DisasterInstance) bool {
				if latest.Status.FsmState != disasterv1.FsmStateFailingOver {
					return false
				}
				if len(latest.Status.AvailableOperations) == 0 {
					return false
				}
				latest.Status.AvailableOperations = []string{}
				return true
			}); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{RequeueAfter: instanceInitializingRequeue()}, nil
	}

	if snapshot.latestTerminal == nil {
		if !transitionWatchdogExceeded(instance, snapshot.latestObservedTime) {
			return ctrl.Result{RequeueAfter: instanceInitializingRequeue()}, nil
		}
		if err := r.updateInstanceStatusWithRetry(ctx, instance, func(latest *disasterv1.DisasterInstance) bool {
			if latest.Status.FsmState != disasterv1.FsmStateFailingOver {
				return false
			}
			latest.Status.FsmState = disasterv1.FsmStateFailed
			helper.SetStatusError(
				&latest.Status,
				instanceReasonFailoverMissing,
				"FailingOver timeout exceeded without running failover operation",
			)
			latest.Status.AvailableOperations = []string{"reset"}
			return true
		}); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: instanceSteadyRequeue()}, nil
	}

	if failoverRecoveredByAutoCancel(snapshot.latestTerminal) {
		if err := r.updateInstanceStatusWithRetry(ctx, instance, func(latest *disasterv1.DisasterInstance) bool {
			if latest.Status.FsmState != disasterv1.FsmStateFailingOver {
				return false
			}
			latest.Status.FsmState = disasterv1.FsmStateProtected
			if snapshot.latestTerminal.Status.RoleStatus != nil {
				latest.Status.PrimaryCluster = snapshot.latestTerminal.Status.RoleStatus.PrimaryCluster
				latest.Status.SecondaryCluster = snapshot.latestTerminal.Status.RoleStatus.SecondaryCluster
			}
			helper.ClearStatusError(&latest.Status)
			latest.Status.AvailableOperations = []string{"failover", "pause", "synconce", "syncdata", "syncresource"}
			return true
		}); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	switch snapshot.latestTerminal.Status.State {
	case disasterv1.OperationStateCompleted:
		if err := r.updateInstanceStatusWithRetry(ctx, instance, func(latest *disasterv1.DisasterInstance) bool {
			if latest.Status.FsmState != disasterv1.FsmStateFailingOver {
				return false
			}
			latest.Status.FsmState = disasterv1.FsmStateActive
			if snapshot.latestTerminal.Status.RoleStatus != nil {
				latest.Status.PrimaryCluster = snapshot.latestTerminal.Status.RoleStatus.PrimaryCluster
				latest.Status.SecondaryCluster = snapshot.latestTerminal.Status.RoleStatus.SecondaryCluster
			}
			helper.ClearStatusError(&latest.Status)
			latest.Status.AvailableOperations = []string{"reprotect", "undo"}
			return true
		}); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil

	case disasterv1.OperationStateFailed:
		failedStep := failoverFailedStep(snapshot.latestTerminal)
		if err := r.updateInstanceStatusWithRetry(ctx, instance, func(latest *disasterv1.DisasterInstance) bool {
			if latest.Status.FsmState != disasterv1.FsmStateFailingOver {
				return false
			}
			if shouldRollbackFailoverStepToProtected(failedStep) {
				latest.Status.FsmState = disasterv1.FsmStateProtected
				helper.ClearStatusError(&latest.Status)
				latest.Status.AvailableOperations = []string{"failover", "pause", "synconce", "syncdata", "syncresource"}
				return true
			}
			latest.Status.FsmState = disasterv1.FsmStateFailed
			applyFailedStateFromOperation(&latest.Status, snapshot.latestTerminal, instanceReasonFailoverFailed, "failover operation failed")
			// A failed failover may need a manual cancel to finish rolling back source/target state.
			latest.Status.AvailableOperations = []string{"reset", "cancel"}
			return true
		}); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: instanceFailedRequeue()}, nil
	}

	return ctrl.Result{RequeueAfter: instanceInitializingRequeue()}, nil
}

type operationWatchdogSnapshot struct {
	runningCount       int
	latestObservedTime time.Time
	latestTerminal     *disasterv1.DisasterOperation
}

func (r *DisasterInstanceReconciler) collectOperationWatchdogSnapshot(
	ctx context.Context,
	instance *disasterv1.DisasterInstance,
	opType disasterv1.OperationType,
) (operationWatchdogSnapshot, error) {
	snapshot := operationWatchdogSnapshot{}
	operationList := &disasterv1.DisasterOperationList{}
	if err := r.List(ctx, operationList, client.InNamespace(instance.Namespace)); err != nil {
		return snapshot, err
	}

	for i := range operationList.Items {
		op := &operationList.Items[i]
		if op.Spec.InstanceName != instance.Name || op.Spec.OperationType != opType {
			continue
		}
		progressAt := operationProgressTime(op)
		if progressAt.After(snapshot.latestObservedTime) {
			snapshot.latestObservedTime = progressAt
		}
		if isOperationInFlight(op.Status.State) {
			snapshot.runningCount++
			continue
		}
		if op.Status.State != disasterv1.OperationStateCompleted && op.Status.State != disasterv1.OperationStateFailed {
			continue
		}
		if snapshot.latestTerminal == nil || progressAt.After(operationProgressTime(snapshot.latestTerminal)) {
			snapshot.latestTerminal = op
		}
	}

	if snapshot.latestObservedTime.IsZero() {
		snapshot.latestObservedTime = instance.CreationTimestamp.Time
	}
	return snapshot, nil
}

func isOperationInFlight(state disasterv1.OperationState) bool {
	return state == "" || state == disasterv1.OperationStatePending || state == disasterv1.OperationStateRunning
}

func transitionWatchdogTimeout(instance *disasterv1.DisasterInstance) time.Duration {
	instanceCfg := instanceRuntime()
	timeout := instanceCfg.TransitionWatchdogTimeout
	if instance != nil && instance.Spec.OperationTimeoutMinutes > 0 {
		timeout = time.Duration(instance.Spec.OperationTimeoutMinutes) * time.Minute
	}
	if timeout < instanceCfg.MinTransitionWatchdogTimeout {
		timeout = instanceCfg.MinTransitionWatchdogTimeout
	}
	return timeout
}

func transitionWatchdogExceeded(instance *disasterv1.DisasterInstance, latestObserved time.Time) bool {
	if latestObserved.IsZero() {
		return false
	}
	return time.Since(latestObserved) >= transitionWatchdogTimeout(instance)
}

func (r *DisasterInstanceReconciler) updateInstanceStatusWithRetry(
	ctx context.Context,
	instance *disasterv1.DisasterInstance,
	mutate func(*disasterv1.DisasterInstance) bool,
) error {
	if instance == nil {
		return nil
	}
	var updatedStatus disasterv1.DisasterInstanceStatus
	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		latest := &disasterv1.DisasterInstance{}
		if err := r.Get(ctx, client.ObjectKey{Namespace: instance.Namespace, Name: instance.Name}, latest); err != nil {
			return err
		}
		if !mutate(latest) {
			updatedStatus = latest.Status
			return nil
		}
		if err := r.Status().Update(ctx, latest); err != nil {
			return err
		}
		updatedStatus = latest.Status
		return nil
	})
	if err != nil {
		return err
	}
	instance.Status = updatedStatus
	return nil
}

func applyFailedStateFromOperation(
	status *disasterv1.DisasterInstanceStatus,
	op *disasterv1.DisasterOperation,
	defaultReason string,
	defaultMessage string,
) {
	reason := defaultReason
	if op != nil && strings.TrimSpace(op.Status.Reason) != "" {
		reason = strings.TrimSpace(op.Status.Reason)
	}
	message := defaultMessage
	if op != nil && strings.TrimSpace(op.Status.Message) != "" {
		message = strings.TrimSpace(op.Status.Message)
	}
	helper.SetStatusError(status, reason, message)
}

func operationProgressTime(op *disasterv1.DisasterOperation) time.Time {
	if op == nil {
		return time.Time{}
	}
	if op.Status.CompletionTime != nil {
		return op.Status.CompletionTime.Time
	}
	if op.Status.StartTime != nil {
		return op.Status.StartTime.Time
	}
	return op.CreationTimestamp.Time
}

func failoverFailedStep(op *disasterv1.DisasterOperation) string {
	if op == nil {
		return ""
	}
	for _, step := range op.Status.Steps {
		if step.State == "Failed" && step.Name != "" {
			return step.Name
		}
	}
	return op.Status.CurrentStep
}

func failoverRecoveredByAutoCancel(op *disasterv1.DisasterOperation) bool {
	if op == nil {
		return false
	}
	return op.Status.AutoCancelTriggered &&
		op.Status.AutoCancelStatus == disasterv1.OperationAutoCancelStatusSucceeded
}

func shouldRollbackFailoverStepToProtected(stepName string) bool {
	switch disasterv1.FailoverStep(stepName) {
	case disasterv1.FailoverStepPreCheck, disasterv1.FailoverStepPauseSchedules, disasterv1.FailoverStepFinalSync:
		return true
	default:
		return false
	}
}

// handleActive 处理 Active 状态（成功故障切换后）
func (r *DisasterInstanceReconciler) handleActive(ctx context.Context, log logr.Logger, instance *disasterv1.DisasterInstance) (ctrl.Result, error) {
	log.V(1).Info("处理 Active 状态")

	// Active 场景同样需要持续同步策略到 DataSync/ResourceSync。
	if requeueAfter, err := r.reconcileSyncSchedules(ctx, log, instance); err != nil {
		return ctrl.Result{}, err
	} else if requeueAfter > 0 {
		return ctrl.Result{RequeueAfter: requeueAfter}, nil
	}

	hasFailure, reason, message, lastDataSyncTime, lastResourceSyncTime, err := r.evaluateSteadySyncHealth(ctx, instance)
	if err != nil {
		return ctrl.Result{}, err
	}
	if hasFailure {
		instance.Status.FsmState = disasterv1.FsmStateFailed
		helper.SetStatusError(&instance.Status, reason, message)
		instance.Status.AvailableOperations = r.currentSyncFailureAvailableOperations(ctx, instance, reason)
		instance.Status.LastDataSyncTime = lastDataSyncTime
		instance.Status.LastResourceSyncTime = lastResourceSyncTime
		if err := r.Status().Update(ctx, instance); err != nil {
			return ctrl.Result{}, err
		}
		r.Recorder.Eventf(instance, "Warning", "SteadySyncFailed", "稳态同步失败，实例进入 Failed: %s", message)
		return ctrl.Result{}, nil
	}

	if handled, err := r.guardByRoleDrift(ctx, log, instance); err != nil {
		return ctrl.Result{}, err
	} else if handled {
		return ctrl.Result{RequeueAfter: instanceSteadyRequeue()}, nil
	}

	helper.ClearStatusError(&instance.Status)
	instance.Status.AvailableOperations = []string{"reprotect", "undo"}
	instance.Status.LastDataSyncTime = lastDataSyncTime
	instance.Status.LastResourceSyncTime = lastResourceSyncTime

	if err := r.Status().Update(ctx, instance); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: instanceSteadyRequeue()}, nil
}

// handleFailingBack 处理 FailingBack 状态
func (r *DisasterInstanceReconciler) handleFailingBack(ctx context.Context, log logr.Logger, instance *disasterv1.DisasterInstance) (ctrl.Result, error) {
	log.Info("处理 FailingBack 状态")

	snapshot, err := r.collectOperationWatchdogSnapshot(ctx, instance, disasterv1.OperationTypeReprotect)
	if err != nil {
		return ctrl.Result{}, err
	}

	if snapshot.runningCount > 0 {
		if len(instance.Status.AvailableOperations) != 0 {
			if err := r.updateInstanceStatusWithRetry(ctx, instance, func(latest *disasterv1.DisasterInstance) bool {
				if latest.Status.FsmState != disasterv1.FsmStateFailingBack {
					return false
				}
				if len(latest.Status.AvailableOperations) == 0 {
					return false
				}
				latest.Status.AvailableOperations = []string{}
				return true
			}); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{RequeueAfter: instanceInitializingRequeue()}, nil
	}

	if snapshot.latestTerminal == nil {
		if !transitionWatchdogExceeded(instance, snapshot.latestObservedTime) {
			return ctrl.Result{RequeueAfter: instanceInitializingRequeue()}, nil
		}
		if err := r.updateInstanceStatusWithRetry(ctx, instance, func(latest *disasterv1.DisasterInstance) bool {
			if latest.Status.FsmState != disasterv1.FsmStateFailingBack {
				return false
			}
			latest.Status.FsmState = disasterv1.FsmStateFailed
			helper.SetStatusError(
				&latest.Status,
				instanceReasonFailbackMissing,
				"FailingBack timeout exceeded without running reprotect operation",
			)
			latest.Status.AvailableOperations = []string{"reset"}
			return true
		}); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: instanceFailedRequeue()}, nil
	}

	switch snapshot.latestTerminal.Status.State {
	case disasterv1.OperationStateCompleted:
		if err := r.updateInstanceStatusWithRetry(ctx, instance, func(latest *disasterv1.DisasterInstance) bool {
			if latest.Status.FsmState != disasterv1.FsmStateFailingBack {
				return false
			}
			latest.Status.FsmState = disasterv1.FsmStateProtected
			if snapshot.latestTerminal.Status.RoleStatus != nil {
				latest.Status.PrimaryCluster = snapshot.latestTerminal.Status.RoleStatus.PrimaryCluster
				latest.Status.SecondaryCluster = snapshot.latestTerminal.Status.RoleStatus.SecondaryCluster
			}
			helper.ClearStatusError(&latest.Status)
			latest.Status.AvailableOperations = []string{"failover", "pause", "synconce", "syncdata", "syncresource"}
			return true
		}); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	case disasterv1.OperationStateFailed:
		if err := r.updateInstanceStatusWithRetry(ctx, instance, func(latest *disasterv1.DisasterInstance) bool {
			if latest.Status.FsmState != disasterv1.FsmStateFailingBack {
				return false
			}
			latest.Status.FsmState = disasterv1.FsmStateFailed
			applyFailedStateFromOperation(&latest.Status, snapshot.latestTerminal, instanceReasonFailbackFailed, "reprotect operation failed")
			latest.Status.AvailableOperations = []string{"reset"}
			return true
		}); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: instanceFailedRequeue()}, nil
	}

	return ctrl.Result{RequeueAfter: instanceInitializingRequeue()}, nil
}

// handleConfigError is intentionally conservative: recovery is handled by guardByConfigHealth.
func (r *DisasterInstanceReconciler) handleConfigError(ctx context.Context, log logr.Logger, instance *disasterv1.DisasterInstance) (ctrl.Result, error) {
	log.Info("处理 ConfigError 状态")
	instance.Status.AvailableOperations = []string{}
	if err := r.Status().Update(ctx, instance); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

// handleFailed 处理 Failed 状态
func (r *DisasterInstanceReconciler) handleFailed(ctx context.Context, log logr.Logger, instance *disasterv1.DisasterInstance) (ctrl.Result, error) {
	log.Info("处理 Failed 状态")

	if strings.TrimSpace(instance.Status.Reason) == instanceReasonRoleDriftDetected {
		evaluation := r.evaluateRoleDrift(ctx, instance)
		applyRoleDriftCondition(instance, evaluation)
		if evaluation.ConditionStatus == metav1.ConditionFalse {
			recoveredState, recoveredOps := r.resolveRecoveredSteadyState(ctx, instance)
			instance.Status.FsmState = recoveredState
			helper.ClearStatusError(&instance.Status)
			instance.Status.AvailableOperations = recoveredOps
			if err := r.Status().Update(ctx, instance); err != nil {
				return ctrl.Result{}, err
			}
			r.Recorder.Eventf(instance, "Normal", "RoleDriftRecovered", "真实主备关系已恢复，实例自动收敛到 %s", recoveredState)
			return ctrl.Result{Requeue: true}, nil
		}
		if evaluation.HardFailure || evaluation.ConditionStatus == metav1.ConditionUnknown {
			helper.SetStatusError(&instance.Status, instanceReasonRoleDriftDetected, evaluation.Message)
			instance.Status.AvailableOperations = []string{}
			if err := r.Status().Update(ctx, instance); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{RequeueAfter: instanceFailedRequeue()}, nil
		}
	}

	hasFailure, syncReason, syncMessage, lastDataSyncTime, lastResourceSyncTime, err := r.evaluateSteadySyncHealth(ctx, instance)
	if err != nil {
		return ctrl.Result{}, err
	}
	instance.Status.LastDataSyncTime = lastDataSyncTime
	instance.Status.LastResourceSyncTime = lastResourceSyncTime

	// 仅对同步链路导致的失败执行自动收敛：
	// 当 DataSync/ResourceSync 已恢复且都有有效 LastSyncTime 时，自动清错并回到稳定态。
	if isRecoverableSyncFailureReason(instance.Status.Reason) &&
		!hasFailure &&
		lastDataSyncTime != nil &&
		lastResourceSyncTime != nil {
		recoveredState, recoveredOps := r.resolveRecoveredSteadyState(ctx, instance)
		instance.Status.FsmState = recoveredState
		helper.ClearStatusError(&instance.Status)
		instance.Status.AvailableOperations = recoveredOps
		if err := r.Status().Update(ctx, instance); err != nil {
			return ctrl.Result{}, err
		}
		r.Recorder.Eventf(instance, "Normal", "SteadySyncRecovered", "同步已恢复，实例自动收敛到 %s", recoveredState)
		return ctrl.Result{Requeue: true}, nil
	}

	// 同步仍失败时，持续回写最新的同步错误上下文，避免停留旧错误信息。
	if isRecoverableSyncFailureReason(instance.Status.Reason) && hasFailure {
		helper.SetStatusError(&instance.Status, syncReason, syncMessage)
	}

	if instance.Status.Reason == "" || instance.Status.Message == "" {
		reason := instance.Status.Reason
		message := instance.Status.Message
		if reason == "" {
			reason = instanceReasonInstanceFailed
		}
		if message == "" {
			message = "instance entered Failed state"
		}
		helper.SetStatusError(&instance.Status, reason, message)
	}
	if isRecoverableSyncFailureReason(instance.Status.Reason) {
		instance.Status.AvailableOperations = r.currentSyncFailureAvailableOperations(ctx, instance, instance.Status.Reason)
	} else {
		preserveCancel := containsOperation(instance.Status.AvailableOperations, "cancel")
		instance.Status.AvailableOperations = []string{"reset"}
		if preserveCancel {
			instance.Status.AvailableOperations = appendOperation(instance.Status.AvailableOperations, "cancel")
		}
	}

	if err := r.Status().Update(ctx, instance); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: instanceFailedRequeue()}, nil
}

func isRecoverableSyncFailureReason(reason string) bool {
	switch strings.TrimSpace(reason) {
	case instanceReasonDataSyncFailed,
		instanceReasonResourceSyncFailed,
		instanceReasonInitializationFail,
		"BackupFailed",
		"BuildRestoreSpecFailed",
		"RestoreFailed",
		"DependencyFailed",
		"StorageUnavailable":
		return true
	default:
		return false
	}
}

func syncFailureAvailableOperations(reason string, dataSync *disasterv1.DataSync, resourceSync *disasterv1.ResourceSync) []string {
	ops := []string{"reset"}
	if !isRecoverableSyncFailureReason(reason) {
		return ops
	}

	dataFailed := dataSync != nil && dataSync.Status.State == disasterv1.DataSyncStateFailed
	resourceFailed := resourceSync != nil && resourceSync.Status.State == disasterv1.ResourceSyncStateFailed

	if dataFailed {
		ops = appendOperation(ops, "syncdata")
	}
	if resourceFailed {
		ops = appendOperation(ops, "syncresource")
	}
	if len(ops) > 1 {
		return ops
	}

	switch strings.TrimSpace(reason) {
	case instanceReasonDataSyncFailed:
		return appendOperation(ops, "syncdata")
	case instanceReasonResourceSyncFailed:
		return appendOperation(ops, "syncresource")
	default:
		ops = appendOperation(ops, "syncdata")
		return appendOperation(ops, "syncresource")
	}
}

func appendOperation(ops []string, operation string) []string {
	if containsOperation(ops, operation) {
		return ops
	}
	return append(ops, operation)
}

func containsOperation(ops []string, operation string) bool {
	for _, existing := range ops {
		if existing == operation {
			return true
		}
	}
	return false
}

func (r *DisasterInstanceReconciler) currentSyncFailureAvailableOperations(
	ctx context.Context,
	instance *disasterv1.DisasterInstance,
	reason string,
) []string {
	if !isRecoverableSyncFailureReason(reason) {
		return []string{"reset"}
	}

	var dataSync *disasterv1.DataSync
	if strings.TrimSpace(instance.Status.DataSyncName) != "" {
		ds := &disasterv1.DataSync{}
		if err := r.Get(ctx, client.ObjectKey{Namespace: instance.Namespace, Name: instance.Status.DataSyncName}, ds); err == nil {
			dataSync = ds
		}
	}

	var resourceSync *disasterv1.ResourceSync
	if strings.TrimSpace(instance.Status.ResourceSyncName) != "" {
		rs := &disasterv1.ResourceSync{}
		if err := r.Get(ctx, client.ObjectKey{Namespace: instance.Namespace, Name: instance.Status.ResourceSyncName}, rs); err == nil {
			resourceSync = rs
		}
	}

	return syncFailureAvailableOperations(reason, dataSync, resourceSync)
}

func (r *DisasterInstanceReconciler) resolveRecoveredSteadyState(
	ctx context.Context,
	instance *disasterv1.DisasterInstance,
) (string, []string) {
	protectedOps := []string{"failover", "pause", "synconce", "syncdata", "syncresource"}
	activeOps := []string{"reprotect", "undo"}

	if instance == nil || strings.TrimSpace(instance.Spec.Config) == "" {
		return disasterv1.FsmStateProtected, protectedOps
	}

	config := &disasterv1.DisasterConfig{}
	if err := r.Get(ctx, client.ObjectKey{Name: instance.Spec.Config}, config); err != nil {
		return disasterv1.FsmStateProtected, protectedOps
	}

	if strings.TrimSpace(instance.Status.PrimaryCluster) == strings.TrimSpace(config.Spec.TargetCluster) &&
		strings.TrimSpace(instance.Status.SecondaryCluster) == strings.TrimSpace(config.Spec.SourceCluster) {
		return disasterv1.FsmStateActive, activeOps
	}

	return disasterv1.FsmStateProtected, protectedOps
}

func (r *DisasterInstanceReconciler) evaluateSteadySyncHealth(
	ctx context.Context,
	instance *disasterv1.DisasterInstance,
) (hasFailure bool, reason, message string, lastDataSyncTime, lastResourceSyncTime *metav1.Time, err error) {
	var dataSync *disasterv1.DataSync
	var resourceSync *disasterv1.ResourceSync

	if instance.Status.DataSyncName != "" {
		ds := &disasterv1.DataSync{}
		getErr := r.Get(ctx, client.ObjectKey{Namespace: instance.Namespace, Name: instance.Status.DataSyncName}, ds)
		if getErr != nil {
			if !errors.IsNotFound(getErr) {
				return false, "", "", nil, nil, getErr
			}
		} else {
			dataSync = ds
			lastDataSyncTime = ds.Status.LastSyncTime
		}
	}

	if instance.Status.ResourceSyncName != "" {
		rs := &disasterv1.ResourceSync{}
		getErr := r.Get(ctx, client.ObjectKey{Namespace: instance.Namespace, Name: instance.Status.ResourceSyncName}, rs)
		if getErr != nil {
			if !errors.IsNotFound(getErr) {
				return false, "", "", nil, nil, getErr
			}
		} else {
			resourceSync = rs
			lastResourceSyncTime = rs.Status.LastSyncTime
		}
	}

	reason, message = deriveInstanceSteadyFailure(dataSync, resourceSync)
	return reason != "", reason, message, lastDataSyncTime, lastResourceSyncTime, nil
}

func deriveInstanceInitializationFailure(dataSync *disasterv1.DataSync, resourceSync *disasterv1.ResourceSync) (reason, message string) {
	if dataSync != nil && dataSync.Status.State == disasterv1.DataSyncStateFailed {
		reason = dataSync.Status.Reason
		if reason == "" {
			reason = instanceReasonDataSyncFailed
		}
		message = dataSync.Status.Message
		if message == "" {
			message = "initialization failed: data sync failed"
		}
		return reason, message
	}
	if resourceSync != nil && resourceSync.Status.State == disasterv1.ResourceSyncStateFailed {
		reason = resourceSync.Status.Reason
		if reason == "" {
			reason = instanceReasonResourceSyncFailed
		}
		message = resourceSync.Status.Message
		if message == "" {
			message = "initialization failed: resource sync failed"
		}
		return reason, message
	}
	return instanceReasonInitializationFail, "initialization failed"
}

func deriveInstanceSteadyFailure(dataSync *disasterv1.DataSync, resourceSync *disasterv1.ResourceSync) (reason, message string) {
	if dataSync != nil && dataSync.Status.State == disasterv1.DataSyncStateFailed {
		reason = strings.TrimSpace(dataSync.Status.Reason)
		if reason == "" {
			reason = instanceReasonDataSyncFailed
		}
		message = strings.TrimSpace(dataSync.Status.Message)
		if message == "" {
			message = "data sync failed"
		}
		return reason, message
	}
	if resourceSync != nil && resourceSync.Status.State == disasterv1.ResourceSyncStateFailed {
		reason = strings.TrimSpace(resourceSync.Status.Reason)
		if reason == "" {
			reason = instanceReasonResourceSyncFailed
		}
		message = strings.TrimSpace(resourceSync.Status.Message)
		if message == "" {
			message = "resource sync failed"
		}
		return reason, message
	}
	return "", ""
}

// handleDeletion 处理 DisasterInstance 被删除时的清理工作
func (r *DisasterInstanceReconciler) handleDeletion(ctx context.Context, log logr.Logger, instance *disasterv1.DisasterInstance) (ctrl.Result, error) {
	log.Info("处理删除")
	deleteTaskName := fmt.Sprintf("删除容灾实例 %s", instance.Name)
	clusterPair := fmt.Sprintf("%s->%s", instance.Status.PrimaryCluster, instance.Status.SecondaryCluster)
	r.reportStructuredTaskStarted(ctx, instance, deleteTaskName, clusterPair, "开始删除容灾实例")

	// 检查是否强制删除
	forceDelete := false
	if val, ok := instance.Annotations["testudo.softcdata.com/force-delete"]; ok && val == "true" {
		forceDelete = true
	}
	_ = forceDelete

	// Legacy finalizer deletion protection is temporarily disabled.
	//
	// The old behavior blocked finalizer removal when DisasterInstance stayed in
	// protected/active states or was still referenced by DisasterGroup. We are
	// temporarily bypassing that legacy protection so deletion can proceed, and
	// will re-introduce the new case-based deletion rules separately.
	/*
		if !forceDelete {
			protectedStates := map[string]bool{
				disasterv1.FsmStateProtected:   true,
				disasterv1.FsmStateActive:      true,
				disasterv1.FsmStateFailingOver: true,
				disasterv1.FsmStateFailingBack: true,
			}

			if protectedStates[instance.Status.FsmState] {
				log.Info("阻止删除：实例处于活动状态", "state", instance.Status.FsmState)
				r.Recorder.Eventf(instance, "Warning", "DeletionBlocked",
					"无法删除处于 %s 状态的实例。请先暂停/重置，或添加 testudo.softcdata.com/force-delete=true 注解强制删除。",
					instance.Status.FsmState)

				return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
			}

			var groups disasterv1.DisasterGroupList
			if err := r.List(ctx, &groups, client.InNamespace(instance.Namespace)); err != nil {
				log.Error(err, "获取 DisasterGroups 失败")
				return ctrl.Result{}, err
			}

			for _, group := range groups.Items {
				for levelIdx, level := range group.Spec.Levels {
					for _, instanceName := range level {
						if instanceName == instance.Name {
							log.Info("阻止删除：当前实例正被容灾组引用", "group", group.Name, "level", levelIdx+1)
							r.Recorder.Eventf(instance, "Warning", "DeletionBlocked",
								"无法删除实例 %s：该实例正被容灾组 %s (位于 Level %d) 引用于保护。请先将其从此容灾组中移除，或添加 testudo.softcdata.com/force-delete=true 强制删除。",
								instance.Name, group.Name, levelIdx+1)

							return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
						}
					}
				}
			}
		}
	*/

	// 子资源（DataSync, ResourceSync）将通过 OwnerReference 级联删除自动删除

	// 遵循 Safe Deletion Pattern: 重新获取最新对象并使用 Patch 移除 Finalizer
	latest := &disasterv1.DisasterInstance{}
	if err := r.Get(ctx, client.ObjectKeyFromObject(instance), latest); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	original := latest.DeepCopy()
	controllerutil.RemoveFinalizer(latest, finalizerName)
	if err := r.Patch(ctx, latest, client.MergeFrom(original)); err != nil {
		log.Error(err, "移除 Finalizer 失败")
		r.reportStructuredTaskFinished(ctx, instance, deleteTaskName, clusterPair, helper.TaskStatusFailed, fmt.Sprintf("移除 Finalizer 失败: %v", err))
		return ctrl.Result{}, err
	}

	r.reportStructuredTaskFinished(ctx, instance, deleteTaskName, clusterPair, helper.TaskStatusSuccess, "容灾实例删除完成")
	r.Recorder.Event(instance, "Normal", "Deleted", "DisasterInstance 已删除")
	log.Info("Finalizer 已移除，删除完成")
	return ctrl.Result{}, nil
}

// ensureDataSync 创建或更新 DataSync 资源
func (r *DisasterInstanceReconciler) ensureDataSync(ctx context.Context, log logr.Logger, instance *disasterv1.DisasterInstance, config *disasterv1.DisasterConfig, name string) error {
	dataSync := &disasterv1.DataSync{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: instance.Namespace,
		},
	}

	op, err := controllerutil.CreateOrUpdate(ctx, r.Client, dataSync, func() error {
		// 设置 spec
		dataSync.Spec.Instance = instance.Name

		// 传播 TraceID 到子资源
		if traceID := instance.Annotations[AnnotationTraceID]; traceID != "" {
			if dataSync.Annotations == nil {
				dataSync.Annotations = make(map[string]string)
			}
			dataSync.Annotations[AnnotationTraceID] = traceID
		}

		if dataSync.Labels == nil {
			dataSync.Labels = make(map[string]string)
		}
		dataSync.Labels, _ = metadata.EnsureCleanupLabels(dataSync.Labels, metadata.CleanupDescriptor{
			OwnerUID:     string(instance.UID),
			RelationCode: "ownerReference.dataSync",
			Strategy:     metadata.CleanupStrategyOwnerReference,
		})

		// 设置 owner reference
		return controllerutil.SetControllerReference(instance, dataSync, r.Scheme)
	})

	if err != nil {
		log.Error(err, "确保 DataSync 失败", "name", name)
		return err
	}

	log.Info("DataSync 已确保", "name", name, "operation", op)

	// 策略调度传播：优先使用实例 override，未设置时回退到基础配置。
	// 与 CreateOrUpdate 分离执行，避免 mutate func 中混合 Get 操作。
	desiredSchedule := ""
	if policyName := effectiveDataSyncPolicyName(instance, config); policyName != "" {
		schedule, requeueAfter, policyErr := r.resolveScheduleFromPolicy(ctx, log, instance, policyName)
		if policyErr != nil {
			// 策略不存在或其他错误：记录事件并告知调用方需要重新入队
			return policyErr
		}
		if requeueAfter > 0 {
			// 策略暂时不可用，由 handlePending 负责重新入队
			return &policyNotReadyError{requeueAfter: requeueAfter}
		}
		desiredSchedule = schedule
	}
	if dataSync.Spec.Trigger.Schedule != desiredSchedule {
		dataSync.Spec.Trigger.Schedule = desiredSchedule
		if updateErr := r.Update(ctx, dataSync); updateErr != nil {
			log.Error(updateErr, "更新 DataSync Schedule 失败")
			return updateErr
		}
		log.Info("DataSync Schedule 已更新", "schedule", desiredSchedule)
	}

	return nil
}

// ensureResourceSync 创建或更新 ResourceSync 资源
func (r *DisasterInstanceReconciler) ensureResourceSync(ctx context.Context, log logr.Logger, instance *disasterv1.DisasterInstance, config *disasterv1.DisasterConfig, name string) error {
	resourceSync := &disasterv1.ResourceSync{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: instance.Namespace,
		},
	}

	op, err := controllerutil.CreateOrUpdate(ctx, r.Client, resourceSync, func() error {
		// 设置 spec
		resourceSync.Spec.Instance = instance.Name

		// 传播 TraceID 到子资源
		if traceID := instance.Annotations[AnnotationTraceID]; traceID != "" {
			if resourceSync.Annotations == nil {
				resourceSync.Annotations = make(map[string]string)
			}
			resourceSync.Annotations[AnnotationTraceID] = traceID
		}

		if resourceSync.Labels == nil {
			resourceSync.Labels = make(map[string]string)
		}
		resourceSync.Labels, _ = metadata.EnsureCleanupLabels(resourceSync.Labels, metadata.CleanupDescriptor{
			OwnerUID:     string(instance.UID),
			RelationCode: "ownerReference.resourceSync",
			Strategy:     metadata.CleanupStrategyOwnerReference,
		})

		// 设置 standby modifier 默认值
		if resourceSync.Spec.StandbyModifier == nil {
			resourceSync.Spec.StandbyModifier = &disasterv1.StandbyModifierConfig{
				ScaleToZero:               true,
				OriginalReplicaAnnotation: "testudo.softcdata.com/original-replica-count",
			}
		}

		// 设置 owner reference
		return controllerutil.SetControllerReference(instance, resourceSync, r.Scheme)
	})

	if err != nil {
		log.Error(err, "确保 ResourceSync 失败", "name", name)
		return err
	}

	log.Info("ResourceSync 已确保", "name", name, "operation", op)

	// 策略调度传播：优先使用实例 override，未设置时回退到基础配置。
	desiredSchedule := ""
	if policyName := effectiveResourceSyncPolicyName(instance, config); policyName != "" {
		schedule, requeueAfter, policyErr := r.resolveScheduleFromPolicy(ctx, log, instance, policyName)
		if policyErr != nil {
			return policyErr
		}
		if requeueAfter > 0 {
			return &policyNotReadyError{requeueAfter: requeueAfter}
		}
		desiredSchedule = schedule
	}
	if resourceSync.Spec.Trigger.Schedule != desiredSchedule {
		resourceSync.Spec.Trigger.Schedule = desiredSchedule
		if updateErr := r.Update(ctx, resourceSync); updateErr != nil {
			log.Error(updateErr, "更新 ResourceSync Schedule 失败")
			return updateErr
		}
		log.Info("ResourceSync Schedule 已更新", "schedule", desiredSchedule)
	}

	return nil
}

// SetupWithManager 将控制器注册到 Manager
func (r *DisasterInstanceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&disasterv1.DisasterInstance{}).
		Owns(&disasterv1.DataSync{}).
		Owns(&disasterv1.ResourceSync{}).
		Complete(r)
}

// policyNotReadyError 表示 DisasterPolicy 暂时不可用，需要重新入队等待
type policyNotReadyError struct {
	requeueAfter time.Duration
}

func (e *policyNotReadyError) Error() string {
	return fmt.Sprintf("DisasterPolicy 暂时不可用，将在 %v 后重试", e.requeueAfter)
}

// resolveScheduleFromPolicy 查询 DisasterPolicy 并返回对应的 Cron 调度表达式。
//
// 返回值：
//   - schedule: 要应用的 Cron 表达式（策略 Disabled 时为空字符串）
//   - requeueAfter: 如果 > 0 表示策略不存在，调用方应在此时间后重试
//   - err: 发生了意外错误
func (r *DisasterInstanceReconciler) resolveScheduleFromPolicy(
	ctx context.Context,
	log logr.Logger,
	instance *disasterv1.DisasterInstance,
	policyName string,
) (schedule string, requeueAfter time.Duration, err error) {
	policy := &disasterv1.DisasterPolicy{}
	if getErr := r.Get(ctx, client.ObjectKey{Namespace: instance.Namespace, Name: policyName}, policy); getErr != nil {
		if errors.IsNotFound(getErr) {
			// 策略尚未就绪，记录日志并告知需要重新入队
			log.Info("引用的 DisasterPolicy 不存在", "policy", policyName)
			return "", 30 * time.Second, nil
		}
		// 其他错误（权限等）
		log.Error(getErr, "查询 DisasterPolicy 失败", "policy", policyName)
		return "", 0, getErr
	}

	// 策略禁用时返回空 Cron（与曂起行为一致）
	if policy.Spec.State == disasterv1.PolicyStateDisabled {
		log.Info("策略已禁用，清空 Schedule", "policy", policyName)
		return "", 0, nil
	}

	return policy.Spec.Schedule, 0, nil
}

func (r *DisasterInstanceReconciler) syncDependencyLabels(ctx context.Context, instance *disasterv1.DisasterInstance) (bool, error) {
	if instance.Labels == nil {
		instance.Labels = make(map[string]string)
	}
	_, _, tokenChanged := metadata.EnsureDependencyTokenLabel(instance.Labels, string(instance.UID))
	edges := make([]metadata.DependencyEdge, 0, 1)

	if instance.Spec.Config != "" {
		config := &disasterv1.DisasterConfig{}
		if err := r.Get(ctx, client.ObjectKey{Name: instance.Spec.Config}, config); err == nil {
			edges = append(edges, metadata.DependencyEdge{
				TargetToken:  metadata.BuildDependencyToken(string(config.UID)),
				RelationCode: "spec.config",
			})
		}
	}

	_, depChanged := metadata.RebuildDependencyToLabels(instance.Labels, edges)
	return tokenChanged || depChanged, nil
}
