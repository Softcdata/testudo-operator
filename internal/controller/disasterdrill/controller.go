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

package disasterdrill

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	"github.com/softcdata/testudo-operator/pkg/helper"
	dmeta "github.com/softcdata/testudo-operator/pkg/metadata"
)

const (
	drillFinalizer = "testudo.softcdata.com/disasterdrill-finalizer"

	drillReasonValidationFailed = "ValidationFailed"
	drillReasonTopologyChanged  = "TopologyChanged"
	drillReasonOperationMissing = "OperationNotFound"
	drillReasonCleanupFailed    = "CleanupFailed"
	drillReasonOperationFailed  = "OperationFailed"
	drillReasonInternalError    = "InternalError"
	drillReasonDrillFailed      = "DrillFailed"
)

// DisasterDrillReconciler reconciles a DisasterDrill object
type DisasterDrillReconciler struct {
	client.Client
	Log      logr.Logger
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

func drillAuditContext(drill *disasterv1.DisasterDrill) (user, traceID string) {
	if drill == nil {
		return "system", "-"
	}
	user = drill.Annotations[dmeta.AnnotationUser]
	if user == "" {
		user = "system"
	}
	traceID = drill.Annotations[dmeta.AnnotationTraceID]
	if traceID == "" {
		traceID = "-"
	}
	return user, traceID
}

func (r *DisasterDrillReconciler) reportDrillStarted(ctx context.Context, drill *disasterv1.DisasterDrill, taskName, cluster, msg string) {
	user, traceID := drillAuditContext(drill)
	helper.ReportTaskStartedWithClient(ctx, r.Client, r.Scheme, drill, taskName, cluster, user, traceID, msg)
}

func (r *DisasterDrillReconciler) reportDrillProgress(ctx context.Context, drill *disasterv1.DisasterDrill, taskName, cluster, msg string) {
	user, traceID := drillAuditContext(drill)
	helper.ReportTaskProgressWithClient(ctx, r.Client, r.Scheme, drill, taskName, cluster, user, traceID, msg)
}

func (r *DisasterDrillReconciler) reportDrillFinished(ctx context.Context, drill *disasterv1.DisasterDrill, taskName, cluster, status, msg string) {
	user, traceID := drillAuditContext(drill)
	now := metav1.Now()
	if status == helper.TaskStatusFailed {
		errorCode := drill.Status.Reason
		if errorCode == "" {
			errorCode = deriveDrillFailureReason(drill)
		}
		helper.ReportTaskFinishedWithClient(ctx, r.Client, r.Scheme, drill, taskName, cluster, status, nil, &now, user, traceID, msg, errorCode)
		return
	}
	helper.ReportTaskFinishedWithClient(ctx, r.Client, r.Scheme, drill, taskName, cluster, status, nil, &now, user, traceID, msg)
}

func normalizeDrillStatusError(drill *disasterv1.DisasterDrill) bool {
	if drill == nil {
		return false
	}
	if drill.Status.State == disasterv1.DrillStateFailed {
		if drill.Status.Reason == "" {
			drill.Status.Reason = deriveDrillFailureReason(drill)
			return drill.Status.Reason != ""
		}
		return false
	}
	if drill.Status.Reason != "" {
		drill.Status.Reason = ""
		return true
	}
	return false
}

func deriveDrillFailureReason(drill *disasterv1.DisasterDrill) string {
	if drill == nil {
		return drillReasonDrillFailed
	}
	if reason := mapDrillFailureMessageToReason(drill.Status.Message); reason != "" {
		return reason
	}
	for i := len(drill.Status.Steps) - 1; i >= 0; i-- {
		step := drill.Status.Steps[i]
		if step.State != "Failed" {
			continue
		}
		if reason := mapDrillFailureMessageToReason(step.Message); reason != "" {
			return reason
		}
		return drillReasonOperationFailed
	}
	return drillReasonDrillFailed
}

func mapDrillFailureMessageToReason(message string) string {
	switch {
	case message == "":
		return ""
	case containsAny(message, "不能同时指定", "必须指定", "状态不是 Ready", "校验失败"):
		return drillReasonValidationFailed
	case containsAny(message, "危险操作拦截", "拓扑"):
		return drillReasonTopologyChanged
	case containsAny(message, "内部错误"):
		return drillReasonInternalError
	case containsAny(message, "Operation", "operation") && containsAny(message, "未找到", "缺少"):
		return drillReasonOperationMissing
	case containsAny(message, "清理") && containsAny(message, "失败"):
		return drillReasonCleanupFailed
	case containsAny(message, "演练失败", "失败"):
		return drillReasonOperationFailed
	default:
		return drillReasonDrillFailed
	}
}

func containsAny(message string, parts ...string) bool {
	for _, part := range parts {
		if part != "" && strings.Contains(message, part) {
			return true
		}
	}
	return false
}

// +kubebuilder:rbac:groups=testudo.softcdata.com,resources=disasterdrills,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=testudo.softcdata.com,resources=disasterdrills/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=testudo.softcdata.com,resources=disasterdrills/finalizers,verbs=update
// +kubebuilder:rbac:groups=testudo.softcdata.com,resources=disasteroperations,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=testudo.softcdata.com,resources=disasterinstances,verbs=get;list;watch
// +kubebuilder:rbac:groups=testudo.softcdata.com,resources=clusters,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop
func (r *DisasterDrillReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("disasterdrill", req.NamespacedName)

	// Fetch the DisasterDrill instance
	drill := &disasterv1.DisasterDrill{}
	if err := r.Get(ctx, req.NamespacedName, drill); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	log.V(1).Info("正在调谐 DisasterDrill")

	// Handle deletion
	if !drill.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, log, drill)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(drill, drillFinalizer) {
		controllerutil.AddFinalizer(drill, drillFinalizer)
		if err := r.Update(ctx, drill); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Sync dependency labels
	if changed, err := r.syncDependencyLabels(ctx, drill); err != nil {
		log.Error(err, "同步依赖标签失败")
		return ctrl.Result{}, err
	} else if changed {
		if err := r.Update(ctx, drill); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	if normalizeDrillStatusError(drill) {
		if err := r.Status().Update(ctx, drill); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Initialize status if empty
	if drill.Status.State == "" {
		drill.Status.State = disasterv1.DrillStatePending
		drill.Status.StartTime = &metav1.Time{Time: time.Now()}
		drill.Status.Message = "演练已创建，正在校验..."
		r.reportDrillStarted(ctx, drill, fmt.Sprintf("创建演练 %s", drill.Name), drill.Spec.TargetCluster, "演练创建成功，开始执行前置校验")
		if err := r.Status().Update(ctx, drill); err != nil {
			return ctrl.Result{}, err
		}
		r.Recorder.Event(drill, "Normal", "Created", "演练已创建")
		return ctrl.Result{Requeue: true}, nil
	}

	// State machine
	switch drill.Status.State {
	case disasterv1.DrillStatePending:
		return r.handlePending(ctx, log, drill)
	case disasterv1.DrillStateReady:
		return r.handleReady(ctx, log, drill)
	case disasterv1.DrillStateExecuting:
		return r.handleExecuting(ctx, log, drill)

	case disasterv1.DrillStateCompleted, disasterv1.DrillStateFailed:
		if drill.Spec.CleanUp {
			return r.triggerCleanup(ctx, log, drill)
		}
		// Terminal states logic handled above
		// Double check to be safe
		if shouldRestart(drill) {
			return r.resetDrill(ctx, log, drill)
		}
		return ctrl.Result{}, nil
	case disasterv1.DrillStateCleaningUp:
		return r.handleCleaningUp(ctx, log, drill)
	case disasterv1.DrillStateCleanedUp:
		// 如果在 CleanedUp 之后用户再次请求重跑演练（如通过 AnnotationRestartTimestamp），重置状态。不过 CleanUp 标志需要先置为 false 才能重跑。
		if !drill.Spec.CleanUp && shouldRestart(drill) {
			return r.resetDrill(ctx, log, drill)
		}
		return ctrl.Result{}, nil
	default:
		log.Info("未知状态", "state", drill.Status.State)
		return ctrl.Result{}, nil
	}
}

// handlePending 处理 Pending 状态：执行自检
func (r *DisasterDrillReconciler) handlePending(ctx context.Context, log logr.Logger, drill *disasterv1.DisasterDrill) (ctrl.Result, error) {
	log.Info("处理 Pending 状态：执行自检")

	// 3. 校验互斥参数
	if drill.Spec.InstanceName != "" && drill.Spec.GroupName != "" {
		drill.Status.State = disasterv1.DrillStateFailed
		drill.Status.Message = "InstanceName 和 GroupName 不能同时指定"
		r.Recorder.Event(drill, "Warning", "ValidationFailed", drill.Status.Message)
		r.reportDrillFinished(ctx, drill, fmt.Sprintf("创建演练 %s", drill.Name), drill.Spec.TargetCluster, helper.TaskStatusFailed, drill.Status.Message)
		// 不需要 Requeue，等待用户修改
		return ctrl.Result{}, r.Status().Update(ctx, drill)
	}
	if drill.Spec.InstanceName == "" && drill.Spec.GroupName == "" {
		drill.Status.State = disasterv1.DrillStateFailed
		drill.Status.Message = "必须指定 InstanceName 或 GroupName"
		r.Recorder.Event(drill, "Warning", "ValidationFailed", drill.Status.Message)
		r.reportDrillFinished(ctx, drill, fmt.Sprintf("创建演练 %s", drill.Name), drill.Spec.TargetCluster, helper.TaskStatusFailed, drill.Status.Message)
		return ctrl.Result{}, r.Status().Update(ctx, drill)
	}

	// 4. 目标集群确定与安全校验
	var targetCluster string
	var validationErr error

	if drill.Spec.InstanceName != "" {
		// --- 实例演练逻辑 ---
		targetCluster, validationErr = r.validateInstanceDrill(ctx, drill)
	} else {
		// --- 容灾组演练逻辑 ---
		targetCluster, validationErr = r.validateGroupDrill(ctx, drill)
	}

	if validationErr != nil {
		drill.Status.State = disasterv1.DrillStateFailed
		drill.Status.Message = validationErr.Error()
		drill.Status.ValidationResults.ClusterReachable = false // 简单标记
		r.Recorder.Event(drill, "Warning", "ValidationFailed", drill.Status.Message)
		r.reportDrillFinished(ctx, drill, fmt.Sprintf("创建演练 %s", drill.Name), drill.Spec.TargetCluster, helper.TaskStatusFailed, drill.Status.Message)
		return ctrl.Result{}, r.Status().Update(ctx, drill)
	}

	drill.Status.TargetCluster = targetCluster
	drill.Status.ValidationResults.ClusterReachable = true
	drill.Status.ValidationResults.BackupAvailable = true // 简化假设，实际应检查所有实例

	// 5. 校验目标集群可达性 (仅当指定了明确的单集群时)
	if targetCluster != "" && targetCluster != "(Auto)" {
		cluster := &disasterv1.Cluster{}
		if err := r.Get(ctx, client.ObjectKey{Name: targetCluster}, cluster); err != nil {
			if errors.IsNotFound(err) {
				drill.Status.State = disasterv1.DrillStateFailed
				drill.Status.Message = fmt.Sprintf("目标集群 %s 未找到", targetCluster)
				drill.Status.ValidationResults.ClusterReachable = false
				r.Recorder.Event(drill, "Warning", "ValidationFailed", drill.Status.Message)
				return ctrl.Result{}, r.Status().Update(ctx, drill)
			}
			return ctrl.Result{}, err
		}
		if cluster.Status.Status != "Ready" {
			if !drill.Spec.SkipValidation {
				drill.Status.State = disasterv1.DrillStateFailed
				drill.Status.Message = fmt.Sprintf("目标集群 %s 状态不是 Ready，当前: %s", targetCluster, cluster.Status.Status)
				drill.Status.ValidationResults.ClusterReachable = false
				r.Recorder.Event(drill, "Warning", "ValidationFailed", drill.Status.Message)
				return ctrl.Result{}, r.Status().Update(ctx, drill)
			}
		}
	}

	// 6. 演练始终使用完整恢复模式
	drill.Status.RestoreMode = disasterv1.RestoreModeFullRestore

	// 7. 校验通过，进入 Ready 状态
	drill.Status.State = disasterv1.DrillStateReady
	drill.Status.ReadyTime = &metav1.Time{Time: time.Now()}
	drill.Status.Message = "演练已就绪，请设置 spec.confirmed=true 开始执行"
	r.Recorder.Event(drill, "Normal", "Ready", "校验通过，等待用户确认")
	r.reportDrillFinished(ctx, drill, fmt.Sprintf("创建演练 %s", drill.Name), targetCluster, helper.TaskStatusSuccess, "前置校验完成，演练进入 Ready")

	return ctrl.Result{}, r.Status().Update(ctx, drill)
}

// handleReady 处理 Ready 状态：等待用户确认
func (r *DisasterDrillReconciler) handleReady(ctx context.Context, log logr.Logger, drill *disasterv1.DisasterDrill) (ctrl.Result, error) {
	// 等待用户设置 confirmed=true
	if !drill.Spec.Confirmed {
		log.V(1).Info("等待用户确认执行演练")
		return ctrl.Result{}, nil
	}

	log.Info("用户已确认，开始执行演练")
	r.Recorder.Event(drill, "Normal", "UserConfirmed", "用户已确认执行")
	r.reportDrillStarted(ctx, drill, fmt.Sprintf("执行演练 %s", drill.Name), drill.Status.TargetCluster, "用户确认执行，演练开始")

	// Double Check: 在执行前再次校验拓扑结构，防止 Pending 期间发生主备切换
	if drill.Spec.InstanceName != "" {
		instance := &disasterv1.DisasterInstance{}
		if err := r.Get(ctx, client.ObjectKey{Namespace: drill.Namespace, Name: drill.Spec.InstanceName}, instance); err != nil {
			// 如果找不到实例，无法继续
			drill.Status.State = disasterv1.DrillStateFailed
			drill.Status.Message = fmt.Sprintf("无法获取关联实例 %s: %v", drill.Spec.InstanceName, err)
			return ctrl.Result{}, r.Status().Update(ctx, drill)
		}

		// 核心校验：目标集群不能是当前的主集群
		// 防止场景：创建演练(Target=B) -> 发生切换(B变为主) -> 用户确认 -> 在主集群执行
		if drill.Status.TargetCluster == instance.Status.PrimaryCluster {
			drill.Status.State = disasterv1.DrillStateFailed
			drill.Status.Message = fmt.Sprintf("危险操作拦截：目标集群 %s 已变更为实例为主集群（可能发生主备切换），请删除并重建演练", drill.Status.TargetCluster)
			r.Recorder.Event(drill, "Warning", "TopologyChanged", drill.Status.Message)
			r.reportDrillFinished(ctx, drill, fmt.Sprintf("执行演练 %s", drill.Name), drill.Status.TargetCluster, helper.TaskStatusFailed, drill.Status.Message)
			return ctrl.Result{}, r.Status().Update(ctx, drill)
		}
	}

	// 防止重复创建 (通过 Label 查找已存在的 Operation)
	var op *disasterv1.DisasterOperation
	opList := &disasterv1.DisasterOperationList{}
	if err := r.List(ctx, opList, client.InNamespace(drill.Namespace), client.MatchingLabels{"testudo.softcdata.com/drill": drill.Name}); err != nil {
		return ctrl.Result{}, err
	}

	if len(opList.Items) > 0 {
		// 查找非终态的 Operation 进行复用 (针对 crash 恢复)
		// 如果是 Completed/Failed 的，说明是历史记录，不复用
		for i := range opList.Items {
			o := &opList.Items[i]
			if o.Status.State != disasterv1.OperationStateCompleted && o.Status.State != disasterv1.OperationStateFailed {
				op = o
				log.Info("发现正在运行的 DisasterOperation，复用之", "name", op.Name)
				break
			}
		}
	}

	if op == nil {
		// 创建 DisasterOperation (使用带时间戳的名称避免重复)
		opName := fmt.Sprintf("drill-%s-%d", drill.Name, time.Now().Unix())

		op = &disasterv1.DisasterOperation{
			ObjectMeta: metav1.ObjectMeta{
				Name:      opName,
				Namespace: drill.Namespace,
				Labels: map[string]string{
					"testudo.softcdata.com/drill": drill.Name,
				},
				OwnerReferences: []metav1.OwnerReference{
					*metav1.NewControllerRef(drill, disasterv1.GroupVersion.WithKind("DisasterDrill")),
				},
			},
			Spec: disasterv1.DisasterOperationSpec{
				InstanceName:  drill.Spec.InstanceName, // 空 if Group
				GroupName:     drill.Spec.GroupName,    // 空 if Instance
				OperationType: disasterv1.OperationTypeDrill,
				DrillConfig: &disasterv1.DrillConfig{
					TargetCluster: func() string {
						if drill.Status.TargetCluster == "(Auto)" {
							return ""
						}
						return drill.Status.TargetCluster
					}(),
					NamespaceMapping: drill.Spec.NamespaceMapping,
					SkipValidation:   drill.Spec.SkipValidation,
					RestorePolicy:    drill.Spec.RestorePolicy,
				},
				Directive: &disasterv1.OperationDirective{
					Confirmed: true,
				},
				SkipPodReadyCheck: func() *bool {
					v := !drill.Spec.WaitUntilReady
					return &v
				}(),
				WaitUntilReady: drill.Spec.WaitUntilReady,
			},
		}

		if err := r.Create(ctx, op); err != nil {
			if !errors.IsAlreadyExists(err) {
				return ctrl.Result{}, err
			}
		}
	}

	// 更新状态
	drill.Status.State = disasterv1.DrillStateExecuting
	drill.Status.OperationName = op.Name
	drill.Status.ExecutionTime = &metav1.Time{Time: time.Now()}
	drill.Status.Message = "演练执行中..."
	r.Recorder.Event(drill, "Normal", "Executing", "演练开始执行")
	r.reportDrillProgress(ctx, drill, fmt.Sprintf("执行演练 %s", drill.Name), drill.Status.TargetCluster, "已创建演练 Operation，等待步骤推进")

	return ctrl.Result{}, r.Status().Update(ctx, drill)
}

func (r *DisasterDrillReconciler) syncDependencyLabels(ctx context.Context, drill *disasterv1.DisasterDrill) (bool, error) {
	if drill.Labels == nil {
		drill.Labels = make(map[string]string)
	}
	_, _, tokenChanged := dmeta.EnsureDependencyTokenLabel(drill.Labels, string(drill.UID))
	edges := make([]dmeta.DependencyEdge, 0, 1)

	if drill.Spec.InstanceName != "" {
		instance := &disasterv1.DisasterInstance{}
		if err := r.Get(ctx, client.ObjectKey{Namespace: drill.Namespace, Name: drill.Spec.InstanceName}, instance); err == nil {
			edges = append(edges, dmeta.DependencyEdge{
				TargetToken:  dmeta.BuildDependencyToken(string(instance.UID)),
				RelationCode: "spec.instanceName",
			})
		}
	}
	if drill.Spec.GroupName != "" {
		group := &disasterv1.DisasterGroup{}
		if err := r.Get(ctx, client.ObjectKey{Namespace: drill.Namespace, Name: drill.Spec.GroupName}, group); err == nil {
			edges = append(edges, dmeta.DependencyEdge{
				TargetToken:  dmeta.BuildDependencyToken(string(group.UID)),
				RelationCode: "spec.groupName",
			})
		}
	}

	_, depChanged := dmeta.RebuildDependencyToLabels(drill.Labels, edges)
	return tokenChanged || depChanged, nil
}

// handleExecuting 处理 Executing 状态：同步 DisasterOperation 状态
func (r *DisasterDrillReconciler) handleExecuting(ctx context.Context, log logr.Logger, drill *disasterv1.DisasterDrill) (ctrl.Result, error) {
	// 获取关联的 DisasterOperation
	if drill.Status.OperationName == "" {
		drill.Status.State = disasterv1.DrillStateFailed
		drill.Status.Message = "内部错误：缺少 operationName"
		return ctrl.Result{}, r.Status().Update(ctx, drill)
	}

	op := &disasterv1.DisasterOperation{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: drill.Namespace, Name: drill.Status.OperationName}, op); err != nil {
		if errors.IsNotFound(err) {
			drill.Status.State = disasterv1.DrillStateFailed
			drill.Status.Message = fmt.Sprintf("DisasterOperation %s 未找到", drill.Status.OperationName)
			return ctrl.Result{}, r.Status().Update(ctx, drill)
		}
		return ctrl.Result{}, err
	}

	// 同步 Operation 状态
	drill.Status.CurrentStep = op.Status.CurrentStep
	drill.Status.Steps = op.Status.Steps
	drill.Status.GroupProgress = op.Status.GroupStatus // 同步 Group 进度

	switch op.Status.State {
	case disasterv1.OperationStateCompleted:
		drill.Status.State = disasterv1.DrillStateCompleted
		drill.Status.CompletionTime = &metav1.Time{Time: time.Now()}
		drill.Status.Message = "演练完成"
		r.Recorder.Event(drill, "Normal", "Completed", "演练已完成")
		r.reportDrillFinished(ctx, drill, fmt.Sprintf("执行演练 %s", drill.Name), drill.Status.TargetCluster, helper.TaskStatusSuccess, "演练执行完成")
		return ctrl.Result{}, r.Status().Update(ctx, drill)

	case disasterv1.OperationStateFailed:
		drill.Status.State = disasterv1.DrillStateFailed
		drill.Status.Reason = op.Status.Reason
		drill.Status.CompletionTime = &metav1.Time{Time: time.Now()}
		drill.Status.Message = fmt.Sprintf("演练失败: %s", op.Status.Message)
		r.Recorder.Event(drill, "Warning", "Failed", drill.Status.Message)
		r.reportDrillFinished(ctx, drill, fmt.Sprintf("执行演练 %s", drill.Name), drill.Status.TargetCluster, helper.TaskStatusFailed, drill.Status.Message)
		return ctrl.Result{}, r.Status().Update(ctx, drill)

	default:
		// 仍在执行中，继续等待
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}
}

// triggerCleanup 触发清理逻辑
func (r *DisasterDrillReconciler) triggerCleanup(ctx context.Context, log logr.Logger, drill *disasterv1.DisasterDrill) (ctrl.Result, error) {
	log.Info("开始演练清理流程")

	var op *disasterv1.DisasterOperation
	var terminalOp *disasterv1.DisasterOperation
	opList := &disasterv1.DisasterOperationList{}
	if err := r.List(ctx, opList, client.InNamespace(drill.Namespace), client.MatchingLabels{"testudo.softcdata.com/drill": drill.Name}); err != nil {
		return ctrl.Result{}, err
	}

	for i := range opList.Items {
		o := &opList.Items[i]
		if o.Spec.OperationType != disasterv1.OperationTypeDrillCleanup {
			continue
		}
		if o.Status.State == disasterv1.OperationStateCompleted || o.Status.State == disasterv1.OperationStateFailed {
			terminalOp = newestCleanupOperation(terminalOp, o)
			continue
		}
		op = newestCleanupOperation(op, o)
	}

	if op != nil {
		log.Info("发现正在运行的 Drill Cleanup Operation，复用之", "name", op.Name)
	} else if terminalOp != nil {
		log.Info("发现已结束的 Drill Cleanup Operation，不再重复创建", "name", terminalOp.Name, "state", terminalOp.Status.State)
		return r.syncCleanupOperationStatus(ctx, drill, terminalOp)
	}

	if op == nil {
		if drill.Status.OperationName != "" {
			existingOp := &disasterv1.DisasterOperation{}
			if err := r.Get(ctx, client.ObjectKey{Namespace: drill.Namespace, Name: drill.Status.OperationName}, existingOp); err != nil {
				if !errors.IsNotFound(err) {
					return ctrl.Result{}, err
				}
			} else if existingOp.Spec.OperationType == disasterv1.OperationTypeDrillCleanup &&
				(existingOp.Status.State == disasterv1.OperationStateCompleted || existingOp.Status.State == disasterv1.OperationStateFailed) {
				log.Info("当前清理 Operation 已结束，不再重复创建", "name", existingOp.Name, "state", existingOp.Status.State)
				return r.syncCleanupOperationStatus(ctx, drill, existingOp)
			}
		}
	}

	if op == nil {
		targetCluster := drill.Status.TargetCluster
		if targetCluster == "(Auto)" {
			targetCluster = ""
		}
		opName := fmt.Sprintf("drill-cln-%s-%d", drill.Name, time.Now().Unix())

		op = &disasterv1.DisasterOperation{
			ObjectMeta: metav1.ObjectMeta{
				Name:      opName,
				Namespace: drill.Namespace,
				Labels: map[string]string{
					"testudo.softcdata.com/drill": drill.Name,
				},
				OwnerReferences: []metav1.OwnerReference{
					*metav1.NewControllerRef(drill, disasterv1.GroupVersion.WithKind("DisasterDrill")),
				},
			},
			Spec: disasterv1.DisasterOperationSpec{
				InstanceName:  drill.Spec.InstanceName,
				GroupName:     drill.Spec.GroupName,
				OperationType: disasterv1.OperationTypeDrillCleanup,
				DrillConfig: &disasterv1.DrillConfig{
					TargetCluster:    targetCluster,
					NamespaceMapping: drill.Spec.NamespaceMapping,
					SkipValidation:   drill.Spec.SkipValidation,
					RestorePolicy:    drill.Spec.RestorePolicy,
				},
			},
		}

		if err := r.Create(ctx, op); err != nil {
			if !errors.IsAlreadyExists(err) {
				return ctrl.Result{}, err
			}
		}
	}

	drill.Status.State = disasterv1.DrillStateCleaningUp
	drill.Status.OperationName = op.Name
	drill.Status.Message = "正在清理演练资源..."
	r.Recorder.Event(drill, "Normal", "CleaningUp", "开始清理演练资源")
	r.reportDrillStarted(ctx, drill, fmt.Sprintf("清理演练 %s", drill.Name), drill.Status.TargetCluster, "已触发演练清理操作")

	return ctrl.Result{}, r.Status().Update(ctx, drill)
}

func newestCleanupOperation(current, candidate *disasterv1.DisasterOperation) *disasterv1.DisasterOperation {
	if candidate == nil {
		return current
	}
	if current == nil {
		return candidate
	}
	candidateTime := candidate.CreationTimestamp.Time
	currentTime := current.CreationTimestamp.Time
	if candidateTime.After(currentTime) {
		return candidate
	}
	if candidateTime.Equal(currentTime) && candidate.Name > current.Name {
		return candidate
	}
	return current
}

// handleCleaningUp 轮询清理操作状态
func (r *DisasterDrillReconciler) handleCleaningUp(ctx context.Context, log logr.Logger, drill *disasterv1.DisasterDrill) (ctrl.Result, error) {
	if drill.Status.OperationName == "" {
		drill.Status.State = disasterv1.DrillStateFailed
		drill.Status.Message = "内部错误：缺少清理 operationName"
		return ctrl.Result{}, r.Status().Update(ctx, drill)
	}

	op := &disasterv1.DisasterOperation{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: drill.Namespace, Name: drill.Status.OperationName}, op); err != nil {
		if errors.IsNotFound(err) {
			drill.Status.State = disasterv1.DrillStateFailed
			drill.Status.Message = fmt.Sprintf("Drill Cleanup Operation %s 未找到", drill.Status.OperationName)
			return ctrl.Result{}, r.Status().Update(ctx, drill)
		}
		return ctrl.Result{}, err
	}

	return r.syncCleanupOperationStatus(ctx, drill, op)
}

func (r *DisasterDrillReconciler) syncCleanupOperationStatus(ctx context.Context, drill *disasterv1.DisasterDrill, op *disasterv1.DisasterOperation) (ctrl.Result, error) {
	switch op.Status.State {
	case disasterv1.OperationStateCompleted:
		message := "演练资源清理完成"
		if drill.Status.State == disasterv1.DrillStateCleanedUp &&
			drill.Status.OperationName == op.Name &&
			drill.Status.Reason == "" &&
			drill.Status.Message == message {
			return ctrl.Result{}, nil
		}
		drill.Status.State = disasterv1.DrillStateCleanedUp
		drill.Status.OperationName = op.Name
		drill.Status.Reason = ""
		drill.Status.CompletionTime = &metav1.Time{Time: time.Now()}
		drill.Status.Message = message
		r.Recorder.Event(drill, "Normal", "CleanedUp", "演练资源清理已完成")
		r.reportDrillFinished(ctx, drill, fmt.Sprintf("清理演练 %s", drill.Name), drill.Status.TargetCluster, helper.TaskStatusSuccess, "演练资源清理完成")
		return ctrl.Result{}, r.Status().Update(ctx, drill)

	case disasterv1.OperationStateFailed:
		reason := op.Status.Reason
		if reason == "" {
			reason = drillReasonCleanupFailed
		}
		message := fmt.Sprintf("演练资源清理失败: %s", op.Status.Message)
		if drill.Status.State == disasterv1.DrillStateFailed &&
			drill.Status.OperationName == op.Name &&
			drill.Status.Reason == reason &&
			drill.Status.Message == message {
			return ctrl.Result{}, nil
		}
		drill.Status.State = disasterv1.DrillStateFailed
		drill.Status.OperationName = op.Name
		drill.Status.Reason = reason
		drill.Status.Message = message
		r.Recorder.Event(drill, "Warning", "CleanupFailed", drill.Status.Message)
		r.reportDrillFinished(ctx, drill, fmt.Sprintf("清理演练 %s", drill.Name), drill.Status.TargetCluster, helper.TaskStatusFailed, drill.Status.Message)
		return ctrl.Result{}, r.Status().Update(ctx, drill)

	default:
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}
}

// handleDeletion 处理删除
func (r *DisasterDrillReconciler) handleDeletion(ctx context.Context, log logr.Logger, drill *disasterv1.DisasterDrill) (ctrl.Result, error) {
	log.Info("处理 DisasterDrill 删除")
	r.reportDrillStarted(ctx, drill, fmt.Sprintf("删除演练 %s", drill.Name), drill.Status.TargetCluster, "开始删除演练")

	// DisasterOperation 通过 OwnerReferences 自动级联删除
	// 移除 finalizer
	controllerutil.RemoveFinalizer(drill, drillFinalizer)
	if err := r.Update(ctx, drill); err != nil {
		r.reportDrillFinished(ctx, drill, fmt.Sprintf("删除演练 %s", drill.Name), drill.Status.TargetCluster, helper.TaskStatusFailed, fmt.Sprintf("删除演练失败: %v", err))
		return ctrl.Result{}, err
	}

	r.reportDrillFinished(ctx, drill, fmt.Sprintf("删除演练 %s", drill.Name), drill.Status.TargetCluster, helper.TaskStatusSuccess, "演练删除完成")
	r.Recorder.Event(drill, "Normal", "Deleted", "演练已删除")
	return ctrl.Result{}, nil
}

// validateInstanceDrill 校验实例演练
func (r *DisasterDrillReconciler) validateInstanceDrill(ctx context.Context, drill *disasterv1.DisasterDrill) (string, error) {
	instance := &disasterv1.DisasterInstance{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: drill.Namespace, Name: drill.Spec.InstanceName}, instance); err != nil {
		if errors.IsNotFound(err) {
			return "", fmt.Errorf("DisasterInstance %s 未找到", drill.Spec.InstanceName)
		}
		return "", err
	}

	drill.Status.ValidationResults = &disasterv1.DrillValidationResults{}

	// 校验状态
	if instance.Status.FsmState != disasterv1.FsmStateProtected && instance.Status.FsmState != disasterv1.FsmStateActive {
		if !drill.Spec.SkipValidation {
			drill.Status.ValidationResults.InstanceValid = false
			return "", fmt.Errorf("实例 %s 状态必须是 Protected 或 Active", instance.Name)
		}
	}
	drill.Status.ValidationResults.InstanceValid = true
	drill.Status.ValidationResults.LastDataSyncTime = instance.Status.LastDataSyncTime
	drill.Status.ValidationResults.LastResourceSyncTime = instance.Status.LastResourceSyncTime

	// 确定目标集群
	targetCluster := drill.Spec.TargetCluster
	if targetCluster == "" {
		targetCluster = instance.Status.SecondaryCluster
	}

	// 安全提示：目标集群为生产备集群且未配置映射时，记录警告事件但不拦截
	if targetCluster == instance.Status.SecondaryCluster && len(drill.Spec.NamespaceMapping) == 0 {
		warnMsg := fmt.Sprintf("注意：演练目标集群为生产备用集群 (%s) 且未配置 NamespaceMapping，演练恢复将覆盖备用环境", targetCluster)
		r.Recorder.Event(drill, "Warning", "NoNamespaceMapping", warnMsg)
	}

	return targetCluster, nil
}

// validateGroupDrill 校验容灾组演练
func (r *DisasterDrillReconciler) validateGroupDrill(ctx context.Context, drill *disasterv1.DisasterDrill) (string, error) {
	group := &disasterv1.DisasterGroup{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: drill.Namespace, Name: drill.Spec.GroupName}, group); err != nil {
		if errors.IsNotFound(err) {
			return "", fmt.Errorf("DisasterGroup %s 未找到", drill.Spec.GroupName)
		}
		return "", err
	}

	drill.Status.ValidationResults = &disasterv1.DrillValidationResults{
		InstanceValid: true, // Group 模式下默认为 true，各实例单独校验在 Operation 中
	}

	targetCluster := drill.Spec.TargetCluster
	noMapping := len(drill.Spec.NamespaceMapping) == 0

	// 遍历组内所有实例进行安全校验
	for _, level := range group.Spec.Levels {
		for _, instanceName := range level {
			instance := &disasterv1.DisasterInstance{}
			if err := r.Get(ctx, client.ObjectKey{Namespace: drill.Namespace, Name: instanceName}, instance); err != nil {
				if errors.IsNotFound(err) {
					// 演练开始前实例就被删了，视为校验失败
					return "", fmt.Errorf("容灾组内实例 %s 未找到", instanceName)
				}
				return "", err
			}

			// 确定该实例的实际演练目标
			instTarget := targetCluster
			if instTarget == "" {
				instTarget = instance.Status.SecondaryCluster
			}

			// 安全提示：如果未配置映射且目标指向了该实例的生产备集群，记录警告但不拦截
			if noMapping && instTarget == instance.Status.SecondaryCluster {
				warnMsg := fmt.Sprintf("注意：实例 %s 的演练环境与生产备环境 (%s) 重合且未配置映射，演练恢复将覆盖备用环境", instanceName, instTarget)
				r.Recorder.Event(drill, "Warning", "NoNamespaceMapping", warnMsg)
			}
		}
	}

	// 如果指定了统一目标集群，返回该集群。否则返回 (Auto) 表示跟随实例各自配置
	if targetCluster == "" {
		return "(Auto)", nil
	}

	return targetCluster, nil
}

// shouldRestart 检查是否需要重跑
func shouldRestart(drill *disasterv1.DisasterDrill) bool {
	val, ok := drill.Annotations[dmeta.AnnotationRestartTimestamp]
	if !ok {
		return false
	}

	restartTs, err := time.Parse(time.RFC3339, val)
	if err != nil {
		// 解析失败忽略
		return false
	}

	// 如果没有完成时间（理论上 Completed/Failed必有），或者重跑时间晚于完成时间
	if drill.Status.CompletionTime == nil || restartTs.After(drill.Status.CompletionTime.Time) {
		return true
	}

	return false
}

// resetDrill 重置演练状态
func (r *DisasterDrillReconciler) resetDrill(ctx context.Context, log logr.Logger, drill *disasterv1.DisasterDrill) (ctrl.Result, error) {
	// 1. 重置 Spec.Confirmed = false (必须强制重置)
	if drill.Spec.Confirmed {
		drill.Spec.Confirmed = false
		if err := r.Update(ctx, drill); err != nil {
			return ctrl.Result{}, err
		}
		// Update 后会触发新的 Reconcile，此时 Spec 已更新但 Status 未变 (仍为 Completed)
		// 但由于 Requeue，下次循环中 Spec.Confirmed 为 false
		// 我们在这里继续重置 Status，还是等待下次？
		// 建议一次性处理完全，或者利用 Update 触发。
		// 由于 Status 是子资源，分开更新较安全。
		return ctrl.Result{Requeue: true}, nil
	}

	// 2. 重置 Status
	drill.Status = disasterv1.DisasterDrillStatus{}
	drill.Status.State = disasterv1.DrillStatePending // 重置为 Pending 重新校验
	drill.Status.StartTime = &metav1.Time{Time: time.Now()}
	drill.Status.Message = "演练已重置，重新开始校验..."

	if err := r.Status().Update(ctx, drill); err != nil {
		return ctrl.Result{}, err
	}

	r.Recorder.Event(drill, "Normal", "Restarted", "演练已重置并重新开始")
	return ctrl.Result{Requeue: true}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *DisasterDrillReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&disasterv1.DisasterDrill{}).
		Owns(&disasterv1.DisasterOperation{}).
		Complete(r)
}
