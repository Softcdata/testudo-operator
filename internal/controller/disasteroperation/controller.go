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

package disasteroperation

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"strconv"
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
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/softcdata/testudo-operator/internal/controller/imagemapping"
	"github.com/softcdata/testudo-operator/internal/controller/restore"
	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	"github.com/softcdata/testudo-operator/pkg/helper"
	metadata "github.com/softcdata/testudo-operator/pkg/metadata"
	"github.com/softcdata/testudo-operator/pkg/tools"
)

// DisasterOperationReconciler 负责调谐 DisasterOperation 对象
type DisasterOperationReconciler struct {
	client.Client
	Scheme                      *runtime.Scheme
	Log                         logr.Logger
	Recorder                    record.EventRecorder
	ClientFactory               func(config *rest.Config, options client.Options) (client.Client, error)
	ModifierSubmissionValidator func(ctx context.Context, instance *disasterv1.DisasterInstance, baselineSource, baselineTarget, sourceClusterName string) error
}

const (
	annotationSkipScaleDownSource = "testudo.softcdata.com/skip-scale-down-source"
	annotationTaskStartedEmitted  = "testudo.softcdata.com/task-started-emitted"
	annotationTaskFinishedEmitted = "testudo.softcdata.com/task-finished-emitted"

	operationReasonInvalidOperationType   = "InvalidOperationType"
	operationReasonResourceNotFound       = "ResourceNotFound"
	operationReasonTimeoutExceeded        = "TimeoutExceeded"
	operationReasonSyncFailed             = "SyncFailed"
	operationReasonInvalidState           = "InvalidState"
	operationReasonSuperseded             = "SupersededByNewOperation"
	operationReasonClusterConnectionError = "ClusterConnectionFailed"
	operationReasonStepFailed             = "StepFailed"
	operationReasonOperationFailed        = "OperationFailed"

	eventCodeFailoverFailed      = "FailoverFailed"
	eventCodeAutoCancelTriggered = "AutoCancelTriggered"
	eventCodeAutoCancelSucceeded = "AutoCancelSucceeded"
	eventCodeAutoCancelFailed    = "AutoCancelFailed"

	defaultOperationTimeoutMinutes int32 = 60
)

// resolveWaitUntilReady resolves whether readiness validation should be enabled for this operation.
// Priority:
// 1) Operation-level explicit override: spec.skipPodReadyCheck
// 2) Legacy operation flag: spec.waitUntilReady=true
// 3) Instance-level default: spec.skipPodReadyCheck
// 4) Legacy fallback: spec.waitUntilReady (false by default)
func resolveWaitUntilReady(instance *disasterv1.DisasterInstance, operation *disasterv1.DisasterOperation) bool {
	if operation != nil && operation.Spec.SkipPodReadyCheck != nil {
		return !*operation.Spec.SkipPodReadyCheck
	}
	if operation != nil && operation.Spec.WaitUntilReady {
		return true
	}
	if instance != nil && instance.Spec.SkipPodReadyCheck != nil {
		return !*instance.Spec.SkipPodReadyCheck
	}
	if operation != nil {
		return operation.Spec.WaitUntilReady
	}
	return false
}

func cloneBoolPtr(in *bool) *bool {
	if in == nil {
		return nil
	}
	v := *in
	return &v
}

func operationTypeDisplayName(opType disasterv1.OperationType) string {
	switch opType {
	case disasterv1.OperationTypeFailover:
		return "故障切换"
	case disasterv1.OperationTypeReprotect:
		return "反向保护"
	case disasterv1.OperationTypeUndo:
		return "撤销切换"
	case disasterv1.OperationTypeCancel:
		return "取消切换"
	case disasterv1.OperationTypePause:
		return "暂停同步"
	case disasterv1.OperationTypeResume:
		return "恢复同步"
	case disasterv1.OperationTypeSyncOnce:
		return "立即同步"
	case disasterv1.OperationTypeSyncData:
		return "数据同步"
	case disasterv1.OperationTypeSyncResource:
		return "资源同步"
	case disasterv1.OperationTypeDrill:
		return "演练执行"
	case disasterv1.OperationTypeDrillCleanup:
		return "演练清理"
	default:
		return string(opType)
	}
}

func operationAuditContext(operation *disasterv1.DisasterOperation) (user, traceID string) {
	if operation == nil {
		return "system", "-"
	}
	user = operation.Annotations[metadata.AnnotationUser]
	if user == "" {
		user = "system"
	}
	traceID = operation.Annotations[metadata.AnnotationTraceID]
	if traceID == "" {
		traceID = "-"
	}
	return user, traceID
}

func operationTaskName(operation *disasterv1.DisasterOperation) string {
	if operation == nil {
		return "执行容灾操作"
	}
	action := operationTypeDisplayName(operation.Spec.OperationType)
	if operation.Spec.GroupName != "" {
		return fmt.Sprintf("执行容灾组操作 %s %s", operation.Spec.GroupName, action)
	}
	if operation.Spec.InstanceName != "" {
		return fmt.Sprintf("执行容灾实例操作 %s %s", operation.Spec.InstanceName, action)
	}
	return fmt.Sprintf("执行容灾操作 %s", action)
}

func operationClusterPair(instance *disasterv1.DisasterInstance) string {
	if instance == nil {
		return "-"
	}
	if instance.Status.PrimaryCluster == "" && instance.Status.SecondaryCluster == "" {
		return "-"
	}
	return fmt.Sprintf("%s->%s", instance.Status.PrimaryCluster, instance.Status.SecondaryCluster)
}

func (r *DisasterOperationReconciler) reportOperationStarted(ctx context.Context, operation *disasterv1.DisasterOperation, instance *disasterv1.DisasterInstance, msg string) {
	user, traceID := operationAuditContext(operation)
	helper.ReportTaskStartedWithClient(ctx, r.Client, r.Scheme, operation, operationTaskName(operation), operationClusterPair(instance), user, traceID, msg)
}

func (r *DisasterOperationReconciler) reportOperationProgress(ctx context.Context, operation *disasterv1.DisasterOperation, instance *disasterv1.DisasterInstance, msg string) {
	user, traceID := operationAuditContext(operation)
	helper.ReportTaskProgressWithClient(ctx, r.Client, r.Scheme, operation, operationTaskName(operation), operationClusterPair(instance), user, traceID, msg)
}

func (r *DisasterOperationReconciler) reportOperationProgressWithCode(ctx context.Context, operation *disasterv1.DisasterOperation, instance *disasterv1.DisasterInstance, code, msg string) {
	user, traceID := operationAuditContext(operation)
	helper.ReportTaskProgressWithClient(ctx, r.Client, r.Scheme, operation, operationTaskName(operation), operationClusterPair(instance), user, traceID, msg, code)
}

func (r *DisasterOperationReconciler) reportOperationFinished(ctx context.Context, operation *disasterv1.DisasterOperation, instance *disasterv1.DisasterInstance, status, msg string) {
	user, traceID := operationAuditContext(operation)
	now := metav1.Now()
	start := operation.Status.StartTime
	end := operation.Status.CompletionTime
	if end == nil {
		end = &now
	}
	if status == helper.TaskStatusFailed {
		errorCode := operation.Status.Reason
		if errorCode == "" {
			errorCode = deriveOperationFailureReason(operation)
		}
		helper.ReportTaskFinishedWithClient(ctx, r.Client, r.Scheme, operation, operationTaskName(operation), operationClusterPair(instance), status, start, end, user, traceID, msg, errorCode)
		return
	}
	helper.ReportTaskFinishedWithClient(ctx, r.Client, r.Scheme, operation, operationTaskName(operation), operationClusterPair(instance), status, start, end, user, traceID, msg)
}

func normalizeOperationStatusError(operation *disasterv1.DisasterOperation) bool {
	if operation == nil {
		return false
	}
	if operation.Status.State == disasterv1.OperationStateFailed {
		if operation.Status.Reason == "" {
			operation.Status.Reason = deriveOperationFailureReason(operation)
			return operation.Status.Reason != ""
		}
		return false
	}
	if operation.Status.Reason != "" {
		operation.Status.Reason = ""
		return true
	}
	return false
}

func deriveOperationFailureReason(operation *disasterv1.DisasterOperation) string {
	if operation == nil {
		return operationReasonOperationFailed
	}

	for i := len(operation.Status.Conditions) - 1; i >= 0; i-- {
		cond := operation.Status.Conditions[i]
		if cond.Status == metav1.ConditionTrue && cond.Reason != "" {
			return cond.Reason
		}
	}

	if reason := mapOperationFailureMessageToReason(operation.Status.Message); reason != "" {
		return reason
	}

	for i := len(operation.Status.Steps) - 1; i >= 0; i-- {
		step := operation.Status.Steps[i]
		if step.State != "Failed" {
			continue
		}
		if reason := mapOperationFailureMessageToReason(step.Message); reason != "" {
			return reason
		}
		return operationReasonStepFailed
	}

	return operationReasonOperationFailed
}

func mapOperationFailureMessageToReason(message string) string {
	if message == "" {
		return ""
	}
	lowerMsg := strings.ToLower(message)
	switch {
	case strings.Contains(message, "未知的操作类型"):
		return operationReasonInvalidOperationType
	case strings.Contains(message, "未找到"):
		return operationReasonResourceNotFound
	case strings.Contains(message, "超时"):
		return operationReasonTimeoutExceeded
	case strings.Contains(message, "同步失败"):
		return operationReasonSyncFailed
	case strings.Contains(message, "无法连接"):
		return operationReasonClusterConnectionError
	case strings.Contains(message, "必须在") || strings.Contains(message, "无法执行"):
		return operationReasonInvalidState
	case strings.Contains(message, "失败") || strings.Contains(lowerMsg, "failed"):
		return operationReasonStepFailed
	default:
		return ""
	}
}

func (r *DisasterOperationReconciler) failOperationOnStepError(ctx context.Context, operation *disasterv1.DisasterOperation, step *disasterv1.StepStatus, err error) error {
	if operation == nil || step == nil || err == nil {
		return nil
	}

	now := metav1.Now()
	step.State = "Failed"
	step.Message = fmt.Sprintf("执行失败: %v", err)
	step.CompletionTime = &now

	operation.Status.State = disasterv1.OperationStateFailed
	operation.Status.CompletionTime = &now
	operation.Status.CurrentStep = step.Name
	operation.Status.Message = fmt.Sprintf("步骤 %s 执行失败: %v", step.Name, err)

	return r.Status().Update(ctx, operation)
}

func resolveFailoverAutoCancelMode(stepName string) disasterv1.OperationAutoCancelMode {
	switch disasterv1.FailoverStep(stepName) {
	case disasterv1.FailoverStepPreCheck:
		return disasterv1.OperationAutoCancelModeDirectRollback
	case disasterv1.FailoverStepPauseSchedules,
		disasterv1.FailoverStepFinalSync,
		disasterv1.FailoverStepScaleDownSource,
		disasterv1.FailoverStepScaleUpTarget,
		disasterv1.FailoverStepCheckReplicas:
		return disasterv1.OperationAutoCancelModeCancelPath
	default:
		return disasterv1.OperationAutoCancelModeNoAutoCancel
	}
}

func shouldRollbackFailoverToProtected(stepName string) bool {
	return resolveFailoverAutoCancelMode(stepName) == disasterv1.OperationAutoCancelModeDirectRollback
}

func initializeAutoCancelSteps() []disasterv1.StepStatus {
	return []disasterv1.StepStatus{
		{Name: string(disasterv1.CancelStepScaleDownTarget), State: "Pending"},
		{Name: string(disasterv1.CancelStepScaleUpSource), State: "Pending"},
		{Name: string(disasterv1.CancelStepResumeSchedules), State: "Pending"},
	}
}

func cloneStepStatuses(steps []disasterv1.StepStatus) []disasterv1.StepStatus {
	if len(steps) == 0 {
		return nil
	}
	cloned := make([]disasterv1.StepStatus, len(steps))
	for i := range steps {
		steps[i].DeepCopyInto(&cloned[i])
	}
	return cloned
}

func markStepFailed(step *disasterv1.StepStatus, errMessage string) metav1.Time {
	now := metav1.Now()
	if step == nil {
		return now
	}
	step.State = "Failed"
	step.Message = errMessage
	step.CompletionTime = &now
	return now
}

func (r *DisasterOperationReconciler) markFailoverNoAutoCancel(
	ctx context.Context,
	log logr.Logger,
	operation *disasterv1.DisasterOperation,
	instance *disasterv1.DisasterInstance,
	step *disasterv1.StepStatus,
	errMessage string,
) (ctrl.Result, error) {
	now := markStepFailed(step, errMessage)
	operation.Status.State = disasterv1.OperationStateFailed
	operation.Status.CompletionTime = &now
	operation.Status.CurrentStep = step.Name
	operation.Status.AutoCancelTriggered = false
	operation.Status.AutoCancelStatus = disasterv1.OperationAutoCancelStatusNotTriggered
	operation.Status.AutoCancelMode = disasterv1.OperationAutoCancelModeNoAutoCancel
	operation.Status.AutoCancelReason = errMessage
	operation.Status.AutoCancelTriggerStep = step.Name
	operation.Status.AutoCancelCurrentStep = ""
	operation.Status.AutoCancelSteps = nil
	operation.Status.AutoCancelTriggeredAt = nil
	operation.Status.AutoCancelCompletionTime = nil
	operation.Status.ManualInterventionRequired = true
	operation.Status.Message = fmt.Sprintf("步骤 %s 失败，当前阶段不支持自动补偿，请人工介入: %s", step.Name, errMessage)

	r.settleInstanceStateOnStepFailure(ctx, log, disasterv1.OperationTypeFailover, instance, step.Name, errMessage)
	r.reportOperationProgressWithCode(ctx, operation, instance, eventCodeFailoverFailed, fmt.Sprintf("故障切换在步骤 %s 失败，需人工介入: %s", step.Name, errMessage))
	helper.ReportDiagnosticEventf(r.Recorder, operation, corev1.EventTypeWarning, "FailoverFailedManualIntervention", "步骤 %s 失败，需要人工介入: %s", step.Name, errMessage)

	return ctrl.Result{}, r.Status().Update(ctx, operation)
}

func (r *DisasterOperationReconciler) markFailoverDirectRollbackSuccess(
	ctx context.Context,
	log logr.Logger,
	operation *disasterv1.DisasterOperation,
	instance *disasterv1.DisasterInstance,
	step *disasterv1.StepStatus,
	errMessage string,
) (ctrl.Result, error) {
	now := markStepFailed(step, errMessage)
	operation.Status.State = disasterv1.OperationStateFailed
	operation.Status.CompletionTime = &now
	operation.Status.CurrentStep = step.Name
	operation.Status.AutoCancelTriggered = true
	operation.Status.AutoCancelStatus = disasterv1.OperationAutoCancelStatusSucceeded
	operation.Status.AutoCancelMode = disasterv1.OperationAutoCancelModeDirectRollback
	operation.Status.AutoCancelReason = errMessage
	operation.Status.AutoCancelTriggerStep = step.Name
	operation.Status.AutoCancelCurrentStep = ""
	operation.Status.AutoCancelSteps = nil
	operation.Status.AutoCancelTriggeredAt = &now
	operation.Status.AutoCancelCompletionTime = &now
	operation.Status.ManualInterventionRequired = false
	operation.Status.RoleStatus = &disasterv1.RoleStatus{
		PrimaryCluster:   instance.Status.PrimaryCluster,
		SecondaryCluster: instance.Status.SecondaryCluster,
	}
	operation.Status.Message = fmt.Sprintf("步骤 %s 失败，系统已自动补偿并将实例回置为 Protected: %s", step.Name, errMessage)

	r.settleInstanceStateOnStepFailure(ctx, log, disasterv1.OperationTypeFailover, instance, step.Name, errMessage)
	r.reportOperationProgressWithCode(ctx, operation, instance, eventCodeFailoverFailed, fmt.Sprintf("故障切换在步骤 %s 失败: %s", step.Name, errMessage))
	r.reportOperationProgressWithCode(ctx, operation, instance, eventCodeAutoCancelTriggered, fmt.Sprintf("故障切换失败后触发自动补偿，模式=%s", disasterv1.OperationAutoCancelModeDirectRollback))
	r.reportOperationProgressWithCode(ctx, operation, instance, eventCodeAutoCancelSucceeded, fmt.Sprintf("故障切换失败后已自动补偿，实例已回置为 %s", disasterv1.FsmStateProtected))
	helper.ReportDiagnosticEventf(r.Recorder, operation, corev1.EventTypeNormal, "FailoverFailedAutoCancelSucceeded", "步骤 %s 失败后已自动补偿成功", step.Name)

	return ctrl.Result{}, r.Status().Update(ctx, operation)
}

func (r *DisasterOperationReconciler) startFailoverAutoCancelPath(
	ctx context.Context,
	operation *disasterv1.DisasterOperation,
	instance *disasterv1.DisasterInstance,
	step *disasterv1.StepStatus,
	errMessage string,
) (ctrl.Result, error) {
	now := markStepFailed(step, errMessage)
	operation.Status.State = disasterv1.OperationStateRunning
	operation.Status.CompletionTime = nil
	operation.Status.CurrentStep = step.Name
	operation.Status.AutoCancelTriggered = true
	operation.Status.AutoCancelStatus = disasterv1.OperationAutoCancelStatusRunning
	operation.Status.AutoCancelMode = disasterv1.OperationAutoCancelModeCancelPath
	operation.Status.AutoCancelReason = errMessage
	operation.Status.AutoCancelTriggerStep = step.Name
	operation.Status.AutoCancelCurrentStep = ""
	if len(operation.Status.AutoCancelSteps) == 0 {
		operation.Status.AutoCancelSteps = initializeAutoCancelSteps()
	} else {
		operation.Status.AutoCancelSteps = cloneStepStatuses(operation.Status.AutoCancelSteps)
	}
	operation.Status.AutoCancelTriggeredAt = &now
	operation.Status.AutoCancelCompletionTime = nil
	operation.Status.ManualInterventionRequired = false
	operation.Status.Message = fmt.Sprintf("步骤 %s 失败，已触发自动补偿: %s", step.Name, errMessage)

	r.reportOperationProgressWithCode(ctx, operation, instance, eventCodeFailoverFailed, fmt.Sprintf("故障切换在步骤 %s 失败: %s", step.Name, errMessage))
	r.reportOperationProgressWithCode(ctx, operation, instance, eventCodeAutoCancelTriggered, fmt.Sprintf("故障切换失败后触发自动补偿，模式=%s", disasterv1.OperationAutoCancelModeCancelPath))
	helper.ReportDiagnosticEventf(r.Recorder, operation, corev1.EventTypeNormal, "FailoverFailedAutoCancelStarted", "步骤 %s 失败后开始自动补偿", step.Name)

	return ctrl.Result{Requeue: true}, r.Status().Update(ctx, operation)
}

func (r *DisasterOperationReconciler) handleFailoverStepFailure(
	ctx context.Context,
	log logr.Logger,
	operation *disasterv1.DisasterOperation,
	instance *disasterv1.DisasterInstance,
	step *disasterv1.StepStatus,
	errMessage string,
) (ctrl.Result, error) {
	if step == nil {
		return ctrl.Result{}, nil
	}
	mode := resolveFailoverAutoCancelMode(step.Name)
	switch mode {
	case disasterv1.OperationAutoCancelModeDirectRollback:
		return r.markFailoverDirectRollbackSuccess(ctx, log, operation, instance, step, errMessage)
	case disasterv1.OperationAutoCancelModeCancelPath:
		return r.startFailoverAutoCancelPath(ctx, operation, instance, step, errMessage)
	default:
		return r.markFailoverNoAutoCancel(ctx, log, operation, instance, step, errMessage)
	}
}

func (r *DisasterOperationReconciler) failFailoverAutoCancel(
	ctx context.Context,
	log logr.Logger,
	operation *disasterv1.DisasterOperation,
	instance *disasterv1.DisasterInstance,
	step *disasterv1.StepStatus,
	errMessage string,
) error {
	now := markStepFailed(step, errMessage)
	operation.Status.State = disasterv1.OperationStateFailed
	operation.Status.CompletionTime = &now
	operation.Status.CurrentStep = operation.Status.AutoCancelTriggerStep
	operation.Status.AutoCancelStatus = disasterv1.OperationAutoCancelStatusFailed
	operation.Status.AutoCancelReason = errMessage
	operation.Status.AutoCancelCurrentStep = step.Name
	operation.Status.AutoCancelCompletionTime = &now
	operation.Status.ManualInterventionRequired = true
	operation.Status.Message = fmt.Sprintf("故障切换在步骤 %s 失败后触发的自动补偿失败，补偿步骤 %s 出错: %s", operation.Status.AutoCancelTriggerStep, step.Name, errMessage)

	r.settleInstanceStateOnStepFailure(ctx, log, disasterv1.OperationTypeCancel, instance, step.Name, errMessage)
	r.reportOperationProgressWithCode(ctx, operation, instance, eventCodeAutoCancelFailed, fmt.Sprintf("故障切换失败后的自动补偿在步骤 %s 失败: %s", step.Name, errMessage))
	helper.ReportDiagnosticEventf(r.Recorder, operation, corev1.EventTypeWarning, "FailoverFailedAutoCancelFailed", "自动补偿步骤 %s 失败: %s", step.Name, errMessage)

	return r.Status().Update(ctx, operation)
}

func (r *DisasterOperationReconciler) completeFailoverAutoCancel(
	ctx context.Context,
	operation *disasterv1.DisasterOperation,
	instance *disasterv1.DisasterInstance,
) (ctrl.Result, error) {
	latestInstance := &disasterv1.DisasterInstance{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: instance.Namespace, Name: instance.Name}, latestInstance); err != nil {
		return ctrl.Result{}, err
	}
	latestInstance.Status.FsmState = disasterv1.FsmStateProtected
	helper.ClearStatusError(&latestInstance.Status)
	if err := r.Status().Update(ctx, latestInstance); err != nil {
		return ctrl.Result{}, err
	}
	instance.Status = latestInstance.Status

	now := metav1.Now()
	operation.Status.State = disasterv1.OperationStateFailed
	operation.Status.CompletionTime = &now
	operation.Status.CurrentStep = operation.Status.AutoCancelTriggerStep
	operation.Status.AutoCancelStatus = disasterv1.OperationAutoCancelStatusSucceeded
	operation.Status.AutoCancelCurrentStep = ""
	operation.Status.AutoCancelCompletionTime = &now
	operation.Status.ManualInterventionRequired = false
	operation.Status.RoleStatus = &disasterv1.RoleStatus{
		PrimaryCluster:   instance.Status.PrimaryCluster,
		SecondaryCluster: instance.Status.SecondaryCluster,
	}
	operation.Status.Message = fmt.Sprintf("故障切换在步骤 %s 失败后已自动补偿，实例已恢复为 %s", operation.Status.AutoCancelTriggerStep, disasterv1.FsmStateProtected)

	r.reportOperationProgressWithCode(ctx, operation, instance, eventCodeAutoCancelSucceeded, fmt.Sprintf("故障切换失败后的自动补偿已完成，实例已恢复为 %s", disasterv1.FsmStateProtected))
	helper.ReportDiagnosticEventf(r.Recorder, operation, corev1.EventTypeNormal, "FailoverFailedAutoCancelSucceeded", "自动补偿已完成，实例恢复为 %s", disasterv1.FsmStateProtected)

	if err := r.Status().Update(ctx, operation); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *DisasterOperationReconciler) handleFailoverAutoCancelPath(
	ctx context.Context,
	log logr.Logger,
	operation *disasterv1.DisasterOperation,
	instance *disasterv1.DisasterInstance,
) (ctrl.Result, error) {
	for i := range operation.Status.AutoCancelSteps {
		step := &operation.Status.AutoCancelSteps[i]
		if step.State == "Pending" {
			step.State = "Running"
			step.StartTime = &metav1.Time{Time: time.Now()}
			operation.Status.AutoCancelCurrentStep = step.Name
			operation.Status.Message = fmt.Sprintf("故障切换在步骤 %s 失败后，自动补偿步骤 %s 已开始", operation.Status.AutoCancelTriggerStep, step.Name)
			if err := r.Status().Update(ctx, operation); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
		}

		if step.State == "Running" {
			timeoutMinutes := effectiveOperationTimeoutMinutes(operation, instance)
			if timeoutMinutes > 0 && step.StartTime != nil {
				elapsed := time.Since(step.StartTime.Time)
				timeout := time.Duration(timeoutMinutes) * time.Minute
				if elapsed > timeout {
					errMessage := buildStepTimeoutMessage(step, elapsed, timeoutMinutes)
					return ctrl.Result{}, r.failFailoverAutoCancel(ctx, log, operation, instance, step, errMessage)
				}
			}

			previousStepMessage := step.Message
			completed, err := r.executeCancelStep(ctx, log, step, instance, operation)
			if err != nil {
				return ctrl.Result{}, r.failFailoverAutoCancel(ctx, log, operation, instance, step, err.Error())
			}
			if !completed {
				if step.Message != previousStepMessage {
					operation.Status.Message = fmt.Sprintf("故障切换在步骤 %s 失败后，自动补偿步骤 %s 进行中: %s", operation.Status.AutoCancelTriggerStep, step.Name, step.Message)
					if err := r.Status().Update(ctx, operation); err != nil {
						return ctrl.Result{}, err
					}
				}
				return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
			}

			now := metav1.Now()
			step.State = "Completed"
			step.CompletionTime = &now
			step.Message = "已完成"
			operation.Status.Message = fmt.Sprintf("故障切换在步骤 %s 失败后，自动补偿步骤 %s 已完成", operation.Status.AutoCancelTriggerStep, step.Name)
			if err := r.Status().Update(ctx, operation); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		}
	}

	return r.completeFailoverAutoCancel(ctx, operation, instance)
}

func (r *DisasterOperationReconciler) settleInstanceStateOnStepFailure(
	ctx context.Context,
	log logr.Logger,
	operationType disasterv1.OperationType,
	instance *disasterv1.DisasterInstance,
	stepName string,
	errMessage string,
) {
	if instance == nil || stepName == "" {
		return
	}
	if strings.TrimSpace(errMessage) == "" {
		errMessage = "unknown error"
	}

	var (
		settledStatus disasterv1.DisasterInstanceStatus
		expectState   = ""
	)
	switch operationType {
	case disasterv1.OperationTypeFailover, disasterv1.OperationTypeCancel:
		expectState = disasterv1.FsmStateFailingOver
	case disasterv1.OperationTypeReprotect:
		expectState = disasterv1.FsmStateFailingBack
	}

	if err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		latestInstance := &disasterv1.DisasterInstance{}
		if getErr := r.Get(ctx, client.ObjectKey{Namespace: instance.Namespace, Name: instance.Name}, latestInstance); getErr != nil {
			return getErr
		}

		// 仅在预期中间态时收敛，避免覆盖并发流程已写入的终态。
		if expectState != "" && latestInstance.Status.FsmState != expectState {
			settledStatus = latestInstance.Status
			return nil
		}

		if operationType == disasterv1.OperationTypeFailover && shouldRollbackFailoverToProtected(stepName) {
			latestInstance.Status.FsmState = disasterv1.FsmStateProtected
			helper.ClearStatusError(&latestInstance.Status)
		} else {
			latestInstance.Status.FsmState = disasterv1.FsmStateFailed
			helper.SetStatusError(
				&latestInstance.Status,
				operationReasonStepFailed,
				fmt.Sprintf("%s step %s failed: %s", operationType, stepName, errMessage),
			)
			// 兜底保障：Failed 终态必须携带 reason/message，避免“空错误”。
			if strings.TrimSpace(latestInstance.Status.Reason) == "" || strings.TrimSpace(latestInstance.Status.Message) == "" {
				helper.SetStatusError(
					&latestInstance.Status,
					operationReasonStepFailed,
					fmt.Sprintf("%s step %s failed: %s", operationType, stepName, errMessage),
				)
			}
		}

		if updateErr := r.Status().Update(ctx, latestInstance); updateErr != nil {
			return updateErr
		}
		settledStatus = latestInstance.Status
		return nil
	}); err != nil {
		log.Error(err, "故障切换步骤失败后实例状态收敛失败", "instance", instance.Name, "step", stepName)
		return
	}
	instance.Status = settledStatus
}

func (r *DisasterOperationReconciler) settleFailoverInstanceStateOnStepFailure(
	ctx context.Context,
	log logr.Logger,
	instance *disasterv1.DisasterInstance,
	stepName string,
	errMessage string,
) {
	r.settleInstanceStateOnStepFailure(ctx, log, disasterv1.OperationTypeFailover, instance, stepName, errMessage)
}

func isInFlightOperationState(state disasterv1.OperationState) bool {
	return state == "" || state == disasterv1.OperationStatePending || state == disasterv1.OperationStateRunning
}

func (r *DisasterOperationReconciler) findOtherInFlightOperation(
	ctx context.Context,
	namespace string,
	instanceName string,
	opType disasterv1.OperationType,
	selfName string,
) (*disasterv1.DisasterOperation, error) {
	if namespace == "" || instanceName == "" {
		return nil, nil
	}

	operationList := &disasterv1.DisasterOperationList{}
	if err := r.List(ctx, operationList, client.InNamespace(namespace)); err != nil {
		return nil, err
	}

	for i := range operationList.Items {
		candidate := &operationList.Items[i]
		if candidate.Name == selfName {
			continue
		}
		if candidate.Spec.InstanceName != instanceName || candidate.Spec.OperationType != opType {
			continue
		}
		if !isInFlightOperationState(candidate.Status.State) {
			continue
		}
		return candidate, nil
	}
	return nil, nil
}

func shouldSupersedeOperation(currentOpType, candidateOpType disasterv1.OperationType) bool {
	switch currentOpType {
	case disasterv1.OperationTypeCancel, disasterv1.OperationTypeUndo:
		// cancel/undo 的语义是终止当前 failover 流程并切换到新流程，
		// 旧 failover 不能继续保持 Running。
		return candidateOpType == disasterv1.OperationTypeFailover
	default:
		return false
	}
}

func (r *DisasterOperationReconciler) supersedeInFlightOperations(ctx context.Context, operation *disasterv1.DisasterOperation) error {
	if operation == nil {
		return nil
	}
	if !shouldSupersedeOperation(operation.Spec.OperationType, disasterv1.OperationTypeFailover) {
		return nil
	}

	ops := &disasterv1.DisasterOperationList{}
	if err := r.List(ctx, ops, client.InNamespace(operation.Namespace)); err != nil {
		return err
	}

	for i := range ops.Items {
		candidate := &ops.Items[i]
		if candidate.Name == operation.Name {
			continue
		}
		if !isInFlightOperationState(candidate.Status.State) {
			continue
		}
		if !shouldSupersedeOperation(operation.Spec.OperationType, candidate.Spec.OperationType) {
			continue
		}

		sameInstance := operation.Spec.InstanceName != "" && candidate.Spec.InstanceName == operation.Spec.InstanceName
		sameGroup := operation.Spec.GroupName != "" && candidate.Spec.GroupName == operation.Spec.GroupName
		if !sameInstance && !sameGroup {
			continue
		}

		if err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			latest := &disasterv1.DisasterOperation{}
			if err := r.Get(ctx, client.ObjectKey{Namespace: candidate.Namespace, Name: candidate.Name}, latest); err != nil {
				return client.IgnoreNotFound(err)
			}
			if !isInFlightOperationState(latest.Status.State) {
				return nil
			}

			now := metav1.Now()
			latest.Status.State = disasterv1.OperationStateFailed
			latest.Status.Reason = operationReasonSuperseded
			latest.Status.Message = fmt.Sprintf(
				"操作被 %s(%s) 接管，当前 %s 流程终止",
				operation.Spec.OperationType,
				operation.Name,
				latest.Spec.OperationType,
			)
			latest.Status.CompletionTime = &now
			for idx := range latest.Status.Steps {
				if latest.Status.Steps[idx].State == "Running" {
					latest.Status.Steps[idx].State = "Failed"
					latest.Status.Steps[idx].Message = latest.Status.Message
					latest.Status.Steps[idx].CompletionTime = &now
				}
			}
			return r.Status().Update(ctx, latest)
		}); err != nil {
			return err
		}
	}
	return nil
}

func (r *DisasterOperationReconciler) markOperationTaskEvent(ctx context.Context, operation *disasterv1.DisasterOperation, annotationKey string) {
	if operation == nil {
		return
	}
	if operation.Annotations != nil && operation.Annotations[annotationKey] != "" {
		return
	}
	latest := &disasterv1.DisasterOperation{}
	if err := r.Get(ctx, client.ObjectKeyFromObject(operation), latest); err != nil {
		r.Log.V(1).Info("跳过事件标记：获取最新 Operation 失败", "name", operation.Name, "error", err)
		return
	}
	if latest.Annotations == nil {
		latest.Annotations = make(map[string]string)
	}
	if latest.Annotations[annotationKey] != "" {
		return
	}
	original := latest.DeepCopy()
	latest.Annotations[annotationKey] = time.Now().Format(time.RFC3339Nano)
	if err := r.Patch(ctx, latest, client.MergeFrom(original)); err != nil {
		r.Log.V(1).Info("写入事件标记失败", "name", operation.Name, "key", annotationKey, "error", err)
		return
	}
	refreshed := &disasterv1.DisasterOperation{}
	if err := r.Get(ctx, client.ObjectKeyFromObject(operation), refreshed); err == nil {
		operation.ResourceVersion = refreshed.ResourceVersion
	}
	if operation.Annotations == nil {
		operation.Annotations = make(map[string]string)
	}
	operation.Annotations[annotationKey] = latest.Annotations[annotationKey]
}

func (r *DisasterOperationReconciler) getOperationInstance(ctx context.Context, operation *disasterv1.DisasterOperation) *disasterv1.DisasterInstance {
	if operation == nil || operation.Spec.InstanceName == "" {
		return nil
	}
	instance := &disasterv1.DisasterInstance{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: operation.Namespace, Name: operation.Spec.InstanceName}, instance); err != nil {
		return nil
	}
	return instance
}

// +kubebuilder:rbac:groups=testudo.softcdata.com,resources=disasteroperations,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=testudo.softcdata.com,resources=disasteroperations/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=testudo.softcdata.com,resources=disasteroperations/finalizers,verbs=update
// +kubebuilder:rbac:groups=testudo.softcdata.com,resources=disasterinstances,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=testudo.softcdata.com,resources=datasyncs,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=testudo.softcdata.com,resources=resourcesyncs,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=testudo.softcdata.com,resources=clusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=testudo.softcdata.com,resources=disastergroups,verbs=get;list;watch
// +kubebuilder:rbac:groups=testudo.softcdata.com,resources=backuprestorestatisticses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=testudo.softcdata.com,resources=backuprestorestatisticses/status,verbs=get;update;patch

// Reconcile 处理 DisasterOperation 的调谐循环
func (r *DisasterOperationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("disasteroperation", req.NamespacedName)
	log.V(1).Info("正在调谐 DisasterOperation")

	// 获取 DisasterOperation
	operation := &disasterv1.DisasterOperation{}
	if err := r.Get(ctx, req.NamespacedName, operation); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		log.Error(err, "获取 DisasterOperation 失败")
		return ctrl.Result{}, err
	}

	// 添加 TraceID 到日志和上下文，遵循全局 TraceID 规范
	traceID := operation.Annotations[metadata.AnnotationTraceID]
	if traceID != "" {
		log = log.WithValues(metadata.TraceIDKey, traceID)
		ctx = context.WithValue(ctx, metadata.TraceIDKey, traceID)
	}

	if changed, err := r.ensureOwnerReferences(ctx, operation); err != nil {
		log.Error(err, "同步 OwnerReference 失败")
		return ctrl.Result{}, err
	} else if changed {
		if err := r.Update(ctx, operation); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	if changed, err := r.syncDependencyLabels(ctx, operation); err != nil {
		log.Error(err, "同步依赖标签失败")
		return ctrl.Result{}, err
	} else if changed {
		if err := r.Update(ctx, operation); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	isTerminal := operation.Status.State == disasterv1.OperationStateCompleted ||
		operation.Status.State == disasterv1.OperationStateFailed
	if !isTerminal && normalizeOperationStatusError(operation) {
		if err := r.Status().Update(ctx, operation); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// 如果已完成或失败则跳过（但首次进入终态时需同步统计）
	if isTerminal {
		if normalizeOperationStatusError(operation) {
			if err := r.Status().Update(ctx, operation); err != nil {
				return ctrl.Result{}, err
			}
		}
		if operation.Annotations == nil || operation.Annotations[annotationTaskFinishedEmitted] == "" {
			instance := r.getOperationInstance(ctx, operation)
			finishStatus := helper.TaskStatusSuccess
			if operation.Status.State == disasterv1.OperationStateFailed {
				finishStatus = helper.TaskStatusFailed
			}
			msg := operation.Status.Message
			if msg == "" {
				msg = fmt.Sprintf("操作 %s 已结束", operationTypeDisplayName(operation.Spec.OperationType))
			}
			r.reportOperationFinished(ctx, operation, instance, finishStatus, msg)
			r.markOperationTaskEvent(ctx, operation, annotationTaskFinishedEmitted)
		}
		// 如果统计 CR 还没写入过（LastSyncTime 为空），补充一次统计
		// 正常情况下各 handle* 函数在写入终态前已调用 syncStatistics，
		// 这里作为兜底，确保统计数据最终一致
		if err := r.syncStatistics(ctx, operation); err != nil {
			log.Error(err, "同步操作统计数据失败（非致命）")
		}
		return ctrl.Result{}, nil
	}

	// 初始化状态（如果待执行）
	if operation.Status.State == "" || operation.Status.State == disasterv1.OperationStatePending {
		// 如果未指定超时时间，尝试从 Instance 继承
		if operation.Spec.TimeoutMinutes == 0 && operation.Spec.InstanceName != "" {
			instance := &disasterv1.DisasterInstance{}
			if err := r.Get(ctx, client.ObjectKey{Namespace: operation.Namespace, Name: operation.Spec.InstanceName}, instance); err == nil {
				if instance.Spec.OperationTimeoutMinutes > 0 {
					operation.Spec.TimeoutMinutes = instance.Spec.OperationTimeoutMinutes
					log.Info("继承 Instance 超时配置", "timeout", operation.Spec.TimeoutMinutes)
					if err := r.Update(ctx, operation); err != nil {
						return ctrl.Result{}, err
					}
					return ctrl.Result{Requeue: true}, nil
				}
			}
		}

		operation.Status.State = disasterv1.OperationStateRunning
		operation.Status.StartTime = &metav1.Time{Time: time.Now()}
		if err := r.Status().Update(ctx, operation); err != nil {
			return ctrl.Result{}, err
		}
		helper.ReportDiagnosticEventf(r.Recorder, operation, corev1.EventTypeNormal, "Started", "操作 %s 已开始", operation.Spec.OperationType)
		return ctrl.Result{Requeue: true}, nil
	}

	if operation.Status.State == disasterv1.OperationStateRunning &&
		(operation.Annotations == nil || operation.Annotations[annotationTaskStartedEmitted] == "") {
		instance := r.getOperationInstance(ctx, operation)
		msg := fmt.Sprintf("操作已开始: %s", operationTypeDisplayName(operation.Spec.OperationType))
		if instance != nil {
			msg = fmt.Sprintf("操作已开始: %s，运行期方向 %s", operationTypeDisplayName(operation.Spec.OperationType), operationClusterPair(instance))
		}
		r.reportOperationStarted(ctx, operation, instance, msg)
		r.markOperationTaskEvent(ctx, operation, annotationTaskStartedEmitted)
	}

	// cancel/undo 启动后，需让旧 failover 操作尽快终态，避免“新操作 Completed 但旧操作仍 Running”。
	if operation.Status.State == disasterv1.OperationStateRunning {
		if err := r.supersedeInFlightOperations(ctx, operation); err != nil {
			return ctrl.Result{}, err
		}
	}

	// 检查是 Instance 操作还是 Group 操作
	if operation.Spec.GroupName != "" {
		return r.handleGroupOperation(ctx, log, operation)
	}

	// 根据操作类型路由
	switch operation.Spec.OperationType {
	case disasterv1.OperationTypeFailover:
		return r.handleFailover(ctx, log, operation)
	case disasterv1.OperationTypeReprotect:
		return r.handleReprotect(ctx, log, operation)
	case disasterv1.OperationTypeUndo:
		return r.handleUndo(ctx, log, operation)
	case disasterv1.OperationTypeCancel:
		return r.handleCancel(ctx, log, operation)
	case disasterv1.OperationTypePause:
		return r.handlePause(ctx, log, operation)
	case disasterv1.OperationTypeResume:
		return r.handleResume(ctx, log, operation)
	case disasterv1.OperationTypeSyncOnce:
		return r.handleSync(ctx, log, operation, true, true)
	case disasterv1.OperationTypeSyncData:
		return r.handleSync(ctx, log, operation, true, false)
	case disasterv1.OperationTypeSyncResource:
		return r.handleSync(ctx, log, operation, false, true)
	case disasterv1.OperationTypeDrill:
		return r.handleDrill(ctx, log, operation)
	case disasterv1.OperationTypeDrillCleanup:
		return r.handleDrillCleanup(ctx, log, operation)
	default:
		log.Info("未知的操作类型", "type", operation.Spec.OperationType)
		operation.Status.State = disasterv1.OperationStateFailed
		operation.Status.Message = "未知的操作类型"
		instance := r.getOperationInstance(ctx, operation)
		r.reportOperationFinished(ctx, operation, instance, helper.TaskStatusFailed, operation.Status.Message)
		r.markOperationTaskEvent(ctx, operation, annotationTaskFinishedEmitted)
		return ctrl.Result{}, r.Status().Update(ctx, operation)
	}
}

// handleFailover 处理故障切换操作
func (r *DisasterOperationReconciler) handleFailover(ctx context.Context, log logr.Logger, operation *disasterv1.DisasterOperation) (ctrl.Result, error) {
	// 获取 DisasterInstance
	instance := &disasterv1.DisasterInstance{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: operation.Namespace, Name: operation.Spec.InstanceName}, instance); err != nil {
		if errors.IsNotFound(err) {
			operation.Status.State = disasterv1.OperationStateFailed
			operation.Status.Message = "DisasterInstance 未找到"
			return ctrl.Result{}, r.Status().Update(ctx, operation)
		}
		return ctrl.Result{}, err
	}

	// 防止同一实例并发执行多个 failover 操作，避免状态竞争覆盖。
	if conflictOp, err := r.findOtherInFlightOperation(
		ctx,
		operation.Namespace,
		operation.Spec.InstanceName,
		disasterv1.OperationTypeFailover,
		operation.Name,
	); err != nil {
		return ctrl.Result{}, err
	} else if conflictOp != nil {
		operation.Status.State = disasterv1.OperationStateFailed
		operation.Status.Reason = operationReasonInvalidState
		operation.Status.Message = fmt.Sprintf(
			"实例 %s 已存在进行中的 failover 操作 %s (state=%s)，拒绝并发执行",
			operation.Spec.InstanceName,
			conflictOp.Name,
			conflictOp.Status.State,
		)
		operation.Status.CompletionTime = &metav1.Time{Time: time.Now()}
		return ctrl.Result{}, r.Status().Update(ctx, operation)
	}

	if instance.Status.FsmState != disasterv1.FsmStateProtected && instance.Status.FsmState != disasterv1.FsmStateFailingOver {
		operation.Status.State = disasterv1.OperationStateFailed
		operation.Status.Reason = operationReasonInvalidState
		operation.Status.Message = fmt.Sprintf(
			"实例处于 %s 状态，无法执行故障切换（必须为 Protected 或 FailingOver）",
			instance.Status.FsmState,
		)
		operation.Status.CompletionTime = &metav1.Time{Time: time.Now()}
		return ctrl.Result{}, r.Status().Update(ctx, operation)
	}

	// 0. 更新中间状态 (FailingOver)
	if instance.Status.FsmState == disasterv1.FsmStateProtected {
		instance.Status.FsmState = disasterv1.FsmStateFailingOver
		log.Info("Failover 启动: 更新状态机", "state", instance.Status.FsmState)
		if err := r.Status().Update(ctx, instance); err != nil {
			return ctrl.Result{}, err
		}
	}

	// 定义故障切换步骤
	// 注意: FinalSync 必须在 ScaleDownSource 之前执行！
	// 原因: Velero FSB 备份依赖运行中的 Pod 来读取 PVC 数据，
	//       如果先缩容源集群 (Pod=0)，则 FinalSync 的备份无法创建 PVB，
	//       导致最终同步"成功"但不包含任何 PVC 数据。
	steps := []string{
		string(disasterv1.FailoverStepPreCheck),
		string(disasterv1.FailoverStepPauseSchedules),
		string(disasterv1.FailoverStepFinalSync),       // 先同步（Pod 还在运行，FSB 能读取数据）
		string(disasterv1.FailoverStepScaleDownSource), // 再缩容源集群（避免脑裂）
		string(disasterv1.FailoverStepScaleUpTarget),
		string(disasterv1.FailoverStepCheckReplicas),
		string(disasterv1.FailoverStepSwitchRoles),
	}

	// 初始化步骤（如果为空）
	if len(operation.Status.Steps) == 0 {
		// 先锁定我们预期的主备角色，防止由于断电或多次 Reconcile 竞争导致后续双重翻转（幂等设计的核心）
		if operation.Annotations == nil {
			operation.Annotations = make(map[string]string)
		}
		if operation.Annotations["testudo.softcdata.com/target-primary"] == "" {
			operation.Annotations["testudo.softcdata.com/target-primary"] = instance.Status.SecondaryCluster
			operation.Annotations["testudo.softcdata.com/target-secondary"] = instance.Status.PrimaryCluster
			if err := r.Update(ctx, operation); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		}

		for _, stepName := range steps {
			operation.Status.Steps = append(operation.Status.Steps, disasterv1.StepStatus{
				Name:  stepName,
				State: "Pending",
			})
		}
		operation.Status.CurrentStep = steps[0]
		if err := r.Status().Update(ctx, operation); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	if operation.Status.AutoCancelStatus == disasterv1.OperationAutoCancelStatusRunning && len(operation.Status.AutoCancelSteps) > 0 {
		return r.handleFailoverAutoCancelPath(ctx, log, operation, instance)
	}

	// 处理步骤
	for i := range operation.Status.Steps {
		step := &operation.Status.Steps[i]
		if step.State == "Pending" {
			// 开始步骤
			step.State = "Running"
			step.StartTime = &metav1.Time{Time: time.Now()}
			operation.Status.CurrentStep = step.Name
			log.Info("开始步骤", "step", step.Name)
			helper.ReportDiagnosticEventf(r.Recorder, operation, corev1.EventTypeNormal, "StepStarted", "步骤 %s 已开始", step.Name)
			r.reportOperationProgress(ctx, operation, instance, fmt.Sprintf("步骤 %s 已开始", step.Name))

			if err := r.Status().Update(ctx, operation); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
		}

		if step.State == "Running" {
			// ===== 步骤超时检查 =====
			// 如果设置了超时时间，检查当前步骤是否已超时
			if operation.Spec.TimeoutMinutes > 0 && step.StartTime != nil {
				elapsed := time.Since(step.StartTime.Time)
				timeout := time.Duration(operation.Spec.TimeoutMinutes) * time.Minute
				if elapsed > timeout {
					// 步骤超时，标记操作失败
					log.Info("步骤超时", "step", step.Name, "elapsed", elapsed, "timeout", timeout)
					helper.ReportDiagnosticEventf(r.Recorder, operation, corev1.EventTypeWarning, "StepTimeout", "步骤 %s 超时 (已等待 %v)", step.Name, elapsed.Round(time.Second))
					return r.handleFailoverStepFailure(ctx, log, operation, instance, step, buildStepTimeoutMessage(step, elapsed, operation.Spec.TimeoutMinutes))
				}
			}

			// 执行具体步骤逻辑
			previousStepMessage := step.Message
			completed, err := r.executeFailoverStep(ctx, log, step, instance, operation)
			if err != nil {
				log.Error(err, "执行步骤失败", "step", step.Name)
				helper.ReportDiagnosticEventf(r.Recorder, operation, corev1.EventTypeWarning, "StepFailed", "步骤 %s 失败: %v", step.Name, err)
				return r.handleFailoverStepFailure(ctx, log, operation, instance, step, fmt.Sprintf("执行失败: %v", err))
			}

			if !completed {
				if step.Message != previousStepMessage {
					if updateErr := r.Status().Update(ctx, operation); updateErr != nil {
						return ctrl.Result{}, updateErr
					}
				}
				// 未完成，稍后重试
				return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
			}

			// 完成步骤
			step.State = "Completed"
			step.CompletionTime = &metav1.Time{Time: time.Now()}
			step.Message = "已完成"
			log.Info("步骤已完成", "step", step.Name)
			helper.ReportDiagnosticEventf(r.Recorder, operation, corev1.EventTypeNormal, "StepCompleted", "步骤 %s 已完成", step.Name)
			r.reportOperationProgress(ctx, operation, instance, fmt.Sprintf("步骤 %s 已完成", step.Name))

			if err := r.Status().Update(ctx, operation); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		}
	}

	// 所有步骤完成 - 最终处理
	log.Info("所有故障切换步骤已完成")

	// 必须重新从 Live client/API 拿最新版本
	latestInstance := &disasterv1.DisasterInstance{}
	if getErr := r.Get(ctx, client.ObjectKey{Namespace: instance.Namespace, Name: instance.Name}, latestInstance); getErr != nil {
		return ctrl.Result{}, getErr
	}

	// 幂等角色翻转：依赖外部控制器本身的 Requeue 进行重试，避免死循环重入。
	// 通过 FsmState 的严格状态机控制：只有从 FailingOver 状态过来才进行强行翻转。
	// 如果已经是 Active，说明在之前发生 Operation Update 冲突导致重试时，Instance 上一次已经翻转成功。
	if latestInstance.Status.FsmState == disasterv1.FsmStateFailingOver {
		origP := latestInstance.Status.PrimaryCluster
		origS := latestInstance.Status.SecondaryCluster

		latestInstance.Status.FsmState = disasterv1.FsmStateActive
		latestInstance.Status.PrimaryCluster = origS
		latestInstance.Status.SecondaryCluster = origP

		if updateErr := r.Status().Update(ctx, latestInstance); updateErr != nil {
			return ctrl.Result{}, updateErr // 抛出冲突错误，触发 Controller 标准外层 Requeue
		}
		// 同步更新本地指针供后续 RoleStatus 使用
		instance.Status = latestInstance.Status
	} else if latestInstance.Status.FsmState == disasterv1.FsmStateActive {
		// 已经在此前的重试环路中翻转并激活成功，直接同步到内存即可
		instance.Status = latestInstance.Status
	} else {
		return ctrl.Result{}, fmt.Errorf("unexpected FsmState at end of Failover: %s", latestInstance.Status.FsmState)
	}

	// 完成操作
	operation.Status.State = disasterv1.OperationStateCompleted
	operation.Status.CompletionTime = &metav1.Time{Time: time.Now()}
	operation.Status.CurrentStep = ""
	operation.Status.RoleStatus = &disasterv1.RoleStatus{
		PrimaryCluster:   instance.Status.PrimaryCluster,
		SecondaryCluster: instance.Status.SecondaryCluster,
	}
	operation.Status.Message = "故障切换成功完成"

	if err := r.Status().Update(ctx, operation); err != nil {
		return ctrl.Result{}, err
	}

	helper.ReportDiagnosticEvent(r.Recorder, operation, corev1.EventTypeNormal, "Completed", "故障切换已完成")
	helper.ReportDiagnosticEvent(r.Recorder, instance, corev1.EventTypeNormal, "FailoverCompleted", "故障切换已完成，现在在备集群上激活")

	return ctrl.Result{}, nil
}

func (r *DisasterOperationReconciler) ensureOwnerReferences(ctx context.Context, operation *disasterv1.DisasterOperation) (bool, error) {
	if operation == nil {
		return false, nil
	}
	if operation.Spec.OperationType == disasterv1.OperationTypeDrill ||
		operation.Spec.OperationType == disasterv1.OperationTypeDrillCleanup {
		return false, nil
	}
	if hasOwnerReference(operation.OwnerReferences, "DisasterDrill") {
		return false, nil
	}

	var ownerKind string
	var ownerName string
	var ownerUID types.UID

	if operation.Spec.InstanceName != "" {
		instance := &disasterv1.DisasterInstance{}
		if err := r.Get(ctx, client.ObjectKey{Namespace: operation.Namespace, Name: operation.Spec.InstanceName}, instance); err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		ownerKind = "DisasterInstance"
		ownerName = instance.Name
		ownerUID = instance.UID
	} else if operation.Spec.GroupName != "" {
		group := &disasterv1.DisasterGroup{}
		if err := r.Get(ctx, client.ObjectKey{Namespace: operation.Namespace, Name: operation.Spec.GroupName}, group); err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		ownerKind = "DisasterGroup"
		ownerName = group.Name
		ownerUID = group.UID
	}

	if ownerKind == "" || ownerName == "" || ownerUID == "" {
		return false, nil
	}
	if hasOwnerReference(operation.OwnerReferences, ownerKind, ownerName) {
		return false, nil
	}

	operation.OwnerReferences = append(operation.OwnerReferences, metav1.OwnerReference{
		APIVersion: disasterv1.GroupVersion.String(),
		Kind:       ownerKind,
		Name:       ownerName,
		UID:        ownerUID,
	})
	return true, nil
}

func hasOwnerReference(refs []metav1.OwnerReference, kind string, name ...string) bool {
	for _, ref := range refs {
		if ref.Kind != kind {
			continue
		}
		if len(name) == 0 || ref.Name == name[0] {
			return true
		}
	}
	return false
}

func (r *DisasterOperationReconciler) syncDependencyLabels(ctx context.Context, operation *disasterv1.DisasterOperation) (bool, error) {
	if operation.Labels == nil {
		operation.Labels = make(map[string]string)
	}
	_, _, tokenChanged := metadata.EnsureDependencyTokenLabel(operation.Labels, string(operation.UID))
	edges := make([]metadata.DependencyEdge, 0, 2)

	if operation.Spec.InstanceName != "" {
		instance := &disasterv1.DisasterInstance{}
		if err := r.Get(ctx, client.ObjectKey{Namespace: operation.Namespace, Name: operation.Spec.InstanceName}, instance); err == nil {
			edges = append(edges, metadata.DependencyEdge{
				TargetToken:  metadata.BuildDependencyToken(string(instance.UID)),
				RelationCode: "spec.instanceName",
			})
		}
	}
	if operation.Spec.GroupName != "" {
		group := &disasterv1.DisasterGroup{}
		if err := r.Get(ctx, client.ObjectKey{Namespace: operation.Namespace, Name: operation.Spec.GroupName}, group); err == nil {
			edges = append(edges, metadata.DependencyEdge{
				TargetToken:  metadata.BuildDependencyToken(string(group.UID)),
				RelationCode: "spec.groupName",
			})
		}
	}

	_, depChanged := metadata.RebuildDependencyToLabels(operation.Labels, edges)
	return tokenChanged || depChanged, nil
}

// handleReprotect 处理反向保护操作 (原 Failback)
// 确认故障切换，将原备集群提升为新主集群，原主集群降级为新备集群，并建立反向同步
// 此时 Cluster-B (Primary) 正在运行，Cluster-A (Secondary) 需要被拉起作为备用
func (r *DisasterOperationReconciler) handleReprotect(ctx context.Context, log logr.Logger, operation *disasterv1.DisasterOperation) (ctrl.Result, error) {
	// 获取 DisasterInstance
	instance := &disasterv1.DisasterInstance{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: operation.Namespace, Name: operation.Spec.InstanceName}, instance); err != nil {
		if errors.IsNotFound(err) {
			operation.Status.State = disasterv1.OperationStateFailed
			operation.Status.Message = "DisasterInstance 未找到"
			return ctrl.Result{}, r.Status().Update(ctx, operation)
		}
		return ctrl.Result{}, err
	}

	// 检查实例状态（只能从 Active 回切）
	if instance.Status.FsmState != disasterv1.FsmStateActive {
		if instance.Status.FsmState != disasterv1.FsmStateFailingBack {
			operation.Status.State = disasterv1.OperationStateFailed
			operation.Status.Message = fmt.Sprintf("实例处于 %s 状态，无法执行回切（必须为 Active）", instance.Status.FsmState)
			return ctrl.Result{}, r.Status().Update(ctx, operation)
		}
	} else {
		// 更新状态为 FailingBack
		instance.Status.FsmState = disasterv1.FsmStateFailingBack
		if err := r.Status().Update(ctx, instance); err != nil {
			return ctrl.Result{}, err
		}
	}

	// 定义 Reprotect 步骤
	// 先执行 PreCheck（含 RestorePolicy dry-run fail-closed），
	// 再暂停调度（重置状态），后续 Resume 操作将启动 B->A 的反向同步。
	steps := []string{
		string(disasterv1.FailoverStepPreCheck),
		string(disasterv1.FailoverStepPauseSchedules),
	}

	// 初始化步骤
	if len(operation.Status.Steps) == 0 {
		for _, stepName := range steps {
			operation.Status.Steps = append(operation.Status.Steps, disasterv1.StepStatus{
				Name:  stepName,
				State: "Pending",
			})
		}
		operation.Status.CurrentStep = steps[0]
		if err := r.Status().Update(ctx, operation); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// 处理步骤
	for i := range operation.Status.Steps {
		step := &operation.Status.Steps[i]
		if step.State == "Pending" {
			step.State = "Running"
			step.StartTime = &metav1.Time{Time: time.Now()}
			operation.Status.CurrentStep = step.Name
			log.Info("开始 Reprotect 步骤", "step", step.Name)
			helper.ReportDiagnosticEventf(r.Recorder, operation, corev1.EventTypeNormal, "StepStarted", "步骤 %s 已开始", step.Name)
			r.reportOperationProgress(ctx, operation, instance, fmt.Sprintf("步骤 %s 已开始", step.Name))

			if err := r.Status().Update(ctx, operation); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
		}

		if step.State == "Running" {
			// ===== 步骤超时检查 =====
			if operation.Spec.TimeoutMinutes > 0 && step.StartTime != nil {
				elapsed := time.Since(step.StartTime.Time)
				timeout := time.Duration(operation.Spec.TimeoutMinutes) * time.Minute
				if elapsed > timeout {
					step.State = "Failed"
					step.Message = buildStepTimeoutMessage(step, elapsed, operation.Spec.TimeoutMinutes)
					step.CompletionTime = &metav1.Time{Time: time.Now()}

					operation.Status.State = disasterv1.OperationStateFailed
					operation.Status.CompletionTime = &metav1.Time{Time: time.Now()}
					operation.Status.Message = fmt.Sprintf("步骤 %s 超时: %s", step.Name, step.Message)

					log.Info("步骤超时", "step", step.Name, "elapsed", elapsed, "timeout", timeout)
					helper.ReportDiagnosticEventf(r.Recorder, operation, corev1.EventTypeWarning, "StepTimeout", "步骤 %s 超时 (已等待 %v)", step.Name, elapsed.Round(time.Second))
					r.settleInstanceStateOnStepFailure(ctx, log, disasterv1.OperationTypeReprotect, instance, step.Name, step.Message)

					return ctrl.Result{}, r.Status().Update(ctx, operation)
				}
			}

			completed, err := r.executeFailoverStep(ctx, log, step, instance, operation)
			if err != nil {
				log.Error(err, "执行步骤失败", "step", step.Name)
				r.settleInstanceStateOnStepFailure(ctx, log, disasterv1.OperationTypeReprotect, instance, step.Name, err.Error())
				if updateErr := r.failOperationOnStepError(ctx, operation, step, err); updateErr != nil {
					return ctrl.Result{}, updateErr
				}
				helper.ReportDiagnosticEventf(r.Recorder, operation, corev1.EventTypeWarning, "StepFailed", "步骤 %s 失败: %v", step.Name, err)
				r.reportOperationProgress(ctx, operation, instance, fmt.Sprintf("步骤 %s 失败: %v", step.Name, err))
				return ctrl.Result{}, nil
			}

			if !completed {
				return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
			}

			step.State = "Completed"
			step.CompletionTime = &metav1.Time{Time: time.Now()}
			step.Message = "已完成"
			log.Info("回切步骤已完成", "step", step.Name)
			helper.ReportDiagnosticEventf(r.Recorder, operation, corev1.EventTypeNormal, "StepCompleted", "步骤 %s 已完成", step.Name)
			r.reportOperationProgress(ctx, operation, instance, fmt.Sprintf("步骤 %s 已完成", step.Name))

			if err := r.Status().Update(ctx, operation); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		}
	}

	// 所有步骤完成 - 最终处理
	log.Info("所有反向保护步骤已完成，正在恢复同步")

	// 恢复调度 (启用 B -> A 同步)
	if _, err := r.executeResumeSchedules(ctx, instance); err != nil {
		log.Error(err, "恢复调度失败")
		return ctrl.Result{}, err
	}

	// 触发立即同步，确保保护立即生效（如果未跳过最终同步）
	if !operation.Spec.SkipFinalSync {
		now := time.Now().Format(time.RFC3339)
		if instance.Status.DataSyncName != "" {
			ds := &disasterv1.DataSync{}
			if err := r.Get(ctx, client.ObjectKey{Namespace: instance.Namespace, Name: instance.Status.DataSyncName}, ds); err == nil {
				ds.Spec.Trigger.Manual = now
				if err := r.Update(ctx, ds); err != nil {
					log.Error(err, "Reprotect: 触发 DataSync 失败")
				}
			}
		}
		if instance.Status.ResourceSyncName != "" {
			rs := &disasterv1.ResourceSync{}
			if err := r.Get(ctx, client.ObjectKey{Namespace: instance.Namespace, Name: instance.Status.ResourceSyncName}, rs); err == nil {
				rs.Spec.Trigger.Manual = now
				if err := r.Update(ctx, rs); err != nil {
					log.Error(err, "Reprotect: 触发 ResourceSync 失败")
				}
			}
		}
	} else {
		log.Info("Skipping manual sync trigger due to SkipFinalSync configuration")
	}

	// 更新 DisasterInstance 状态 -> Protected
	instance.Status.FsmState = disasterv1.FsmStateProtected
	// 保持当前角色 (B Primary, A Secondary)
	if err := r.Status().Update(ctx, instance); err != nil {
		return ctrl.Result{}, err
	}

	// 完成操作
	operation.Status.State = disasterv1.OperationStateCompleted
	operation.Status.CompletionTime = &metav1.Time{Time: time.Now()}
	operation.Status.CurrentStep = ""
	operation.Status.RoleStatus = &disasterv1.RoleStatus{
		PrimaryCluster:   instance.Status.PrimaryCluster,
		SecondaryCluster: instance.Status.SecondaryCluster,
	}
	operation.Status.Message = "反向保护成功完成 (反向同步已建立)"

	if err := r.Status().Update(ctx, operation); err != nil {
		return ctrl.Result{}, err
	}

	helper.ReportDiagnosticEvent(r.Recorder, operation, "Normal", "Completed", "反向保护已完成")
	helper.ReportDiagnosticEvent(r.Recorder, instance, "Normal", "ReprotectCompleted", "反向保护已完成，反向同步已建立")

	return ctrl.Result{}, nil
}

// handlePause 处理暂停操作
func (r *DisasterOperationReconciler) handlePause(ctx context.Context, log logr.Logger, operation *disasterv1.DisasterOperation) (ctrl.Result, error) {
	log.Info("handlePause - 正在暂停同步")

	// 获取 DisasterInstance
	instance := &disasterv1.DisasterInstance{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: operation.Namespace, Name: operation.Spec.InstanceName}, instance); err != nil {
		operation.Status.State = disasterv1.OperationStateFailed
		operation.Status.Message = "DisasterInstance 未找到"
		return ctrl.Result{}, r.Status().Update(ctx, operation)
	}

	// 暂停 DataSync
	if instance.Status.DataSyncName != "" {
		dataSync := &disasterv1.DataSync{}
		if err := r.Get(ctx, client.ObjectKey{Namespace: instance.Namespace, Name: instance.Status.DataSyncName}, dataSync); err == nil {
			dataSync.Spec.Paused = true
			if err := r.Update(ctx, dataSync); err != nil {
				log.Error(err, "暂停 DataSync 失败")
			} else {
				log.Info("DataSync 已暂停", "name", dataSync.Name)
			}
		}
	}

	// 暂停 ResourceSync
	if instance.Status.ResourceSyncName != "" {
		resourceSync := &disasterv1.ResourceSync{}
		if err := r.Get(ctx, client.ObjectKey{Namespace: instance.Namespace, Name: instance.Status.ResourceSyncName}, resourceSync); err == nil {
			resourceSync.Spec.Paused = true
			if err := r.Update(ctx, resourceSync); err != nil {
				log.Error(err, "暂停 ResourceSync 失败")
			} else {
				log.Info("ResourceSync 已暂停", "name", resourceSync.Name)
			}
		}
	}

	// 更新 DisasterInstance 状态
	instance.Status.FsmState = disasterv1.FsmStatePaused
	if err := r.Status().Update(ctx, instance); err != nil {
		return ctrl.Result{}, err
	}

	// 完成操作
	operation.Status.State = disasterv1.OperationStateCompleted
	operation.Status.CompletionTime = &metav1.Time{Time: time.Now()}
	operation.Status.Message = "同步已暂停"

	helper.ReportDiagnosticEvent(r.Recorder, instance, "Normal", "Paused", "同步已暂停")
	return ctrl.Result{}, r.Status().Update(ctx, operation)
}

// handleResume 处理恢复操作
func (r *DisasterOperationReconciler) handleResume(ctx context.Context, log logr.Logger, operation *disasterv1.DisasterOperation) (ctrl.Result, error) {
	log.Info("handleResume - 正在恢复同步")

	// 获取 DisasterInstance
	instance := &disasterv1.DisasterInstance{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: operation.Namespace, Name: operation.Spec.InstanceName}, instance); err != nil {
		operation.Status.State = disasterv1.OperationStateFailed
		operation.Status.Message = "DisasterInstance 未找到"
		return ctrl.Result{}, r.Status().Update(ctx, operation)
	}

	if _, err := r.executeResumeSchedules(ctx, instance); err != nil {
		log.Error(err, "恢复调度失败")
		return ctrl.Result{}, err
	}

	// 更新 DisasterInstance 状态
	instance.Status.FsmState = disasterv1.FsmStateProtected
	if err := r.Status().Update(ctx, instance); err != nil {
		return ctrl.Result{}, err
	}

	// 完成操作
	operation.Status.State = disasterv1.OperationStateCompleted
	operation.Status.CompletionTime = &metav1.Time{Time: time.Now()}
	operation.Status.Message = "同步已恢复"

	helper.ReportDiagnosticEvent(r.Recorder, instance, "Normal", "Resumed", "同步已恢复")
	return ctrl.Result{}, r.Status().Update(ctx, operation)
}

// handleSync 处理同步操作 (通用)
// 触发同步并等待完成，Operation 在 DataSync/ResourceSync 状态变为 Ready 后才标记 Completed
func (r *DisasterOperationReconciler) handleSync(ctx context.Context, log logr.Logger, operation *disasterv1.DisasterOperation, syncData bool, syncResource bool) (ctrl.Result, error) {
	// 获取 DisasterInstance
	instance := &disasterv1.DisasterInstance{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: operation.Namespace, Name: operation.Spec.InstanceName}, instance); err != nil {
		operation.Status.State = disasterv1.OperationStateFailed
		operation.Status.Message = "DisasterInstance 未找到"
		return ctrl.Result{}, r.Status().Update(ctx, operation)
	}

	now := time.Now()
	if remaining, waiting := r.syncRetryWaitRemaining(operation, now); waiting {
		log.Info("sync retry wait still active, skip immediate retrigger", "remaining", remaining, "nextRetryTime", operation.Status.NextRetryTime)
		return ctrl.Result{RequeueAfter: remaining}, nil
	}

	// 1. 触发同步（仅在首次或尚未触发时执行）
	// 使用 annotation 记录触发时间，避免重复触发
	triggerKey := "testudo.softcdata.com/sync-triggered-at"
	triggeredAt := ""
	if operation.Annotations != nil {
		triggeredAt = operation.Annotations[triggerKey]
	}

	if triggeredAt == "" {
		// 首次执行，触发同步
		triggerAt := now.Format(time.RFC3339)
		log.Info("handleSync - 首次触发同步", "data", syncData, "resource", syncResource)
		traceID := operation.Annotations[metadata.AnnotationTraceID]
		triggerErrors := make([]string, 0, 2)

		if syncData && instance.Status.DataSyncName != "" {
			if err := r.triggerDataSyncManualAtWithRetry(ctx, instance.Namespace, instance.Status.DataSyncName, triggerAt, traceID); err != nil {
				log.Error(err, "触发 DataSync 失败", "name", instance.Status.DataSyncName)
				triggerErrors = append(triggerErrors, fmt.Sprintf("DataSync(%s): %v", instance.Status.DataSyncName, err))
			} else {
				log.Info("DataSync 已触发", "name", instance.Status.DataSyncName)
			}
		}

		if syncResource && instance.Status.ResourceSyncName != "" {
			if err := r.triggerResourceSyncManualAtWithRetry(ctx, instance.Namespace, instance.Status.ResourceSyncName, triggerAt, traceID); err != nil {
				log.Error(err, "触发 ResourceSync 失败", "name", instance.Status.ResourceSyncName)
				triggerErrors = append(triggerErrors, fmt.Sprintf("ResourceSync(%s): %v", instance.Status.ResourceSyncName, err))
			} else {
				log.Info("ResourceSync 已触发", "name", instance.Status.ResourceSyncName)
			}
		}

		if len(triggerErrors) > 0 {
			operation.Status.Message = fmt.Sprintf("同步触发失败，等待重试: %s", strings.Join(triggerErrors, "; "))
			helper.ReportDiagnosticEvent(r.Recorder, operation, "Warning", "SyncTriggerFailed", operation.Status.Message)
			r.reportOperationProgress(ctx, operation, instance, operation.Status.Message)
			if err := r.Status().Update(ctx, operation); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{RequeueAfter: 3 * time.Second}, nil
		}

		// 记录触发时间
		if operation.Annotations == nil {
			operation.Annotations = make(map[string]string)
		}
		operation.Annotations[triggerKey] = triggerAt
		operation.Status.NextRetryTime = nil
		operation.Status.Message = fmt.Sprintf("同步已于 %s 触发，等待完成...", triggerAt)
		if err := r.updateOperationMetadataWithRetry(ctx, operation.Namespace, operation.Name, func(latest *disasterv1.DisasterOperation) {
			if latest.Annotations == nil {
				latest.Annotations = make(map[string]string)
			}
			latest.Annotations[triggerKey] = triggerAt
		}); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.updateOperationStatusWithRetry(ctx, operation.Namespace, operation.Name, func(latest *disasterv1.DisasterOperation) {
			latest.Status.NextRetryTime = nil
			latest.Status.Message = fmt.Sprintf("同步已于 %s 触发，等待完成...", triggerAt)
		}); err != nil {
			return ctrl.Result{}, err
		}
		helper.ReportDiagnosticEvent(r.Recorder, instance, "Normal", "SyncTriggered", fmt.Sprintf("同步已触发 (Data=%v, Res=%v)", syncData, syncResource))
		r.reportOperationProgress(ctx, operation, instance, fmt.Sprintf("已触发同步 (data=%v, resource=%v)，等待完成", syncData, syncResource))
		return ctrl.Result{RequeueAfter: 3 * time.Second}, nil
	}

	// 2. 检查同步状态
	allReady := true
	var pendingInfo []string
	var failedError error

	// 解析触发时间
	var triggeredAtTime time.Time
	if triggeredAt != "" {
		triggeredAtTime, _ = time.Parse(time.RFC3339, triggeredAt)
	}

	if syncData && instance.Status.DataSyncName != "" {
		dataSync := &disasterv1.DataSync{}
		if err := r.Get(ctx, client.ObjectKey{Namespace: instance.Namespace, Name: instance.Status.DataSyncName}, dataSync); err == nil {
			// 检查是否是本次触发的同步结果 (LastSyncTime >= TriggeredAt)
			isCurrent := false
			if dataSync.Status.LastSyncTime != nil && !dataSync.Status.LastSyncTime.Time.Before(triggeredAtTime) {
				isCurrent = true
			}

			if !isCurrent {
				allReady = false
				pendingInfo = append(pendingInfo, fmt.Sprintf("DataSync(%s)=WaitingStart", dataSync.Name))
			} else {
				if dataSync.Status.State == disasterv1.DataSyncStateFailed {
					failedError = fmt.Errorf("DataSync %s is Failed", dataSync.Name)
				} else if dataSync.Status.State != disasterv1.DataSyncStateReady {
					allReady = false
					pendingInfo = append(pendingInfo, fmt.Sprintf("DataSync(%s)=%s", dataSync.Name, dataSync.Status.State))
				}
			}
		} else {
			allReady = false
			pendingInfo = append(pendingInfo, fmt.Sprintf("DataSync(%s)=GetError", instance.Status.DataSyncName))
			log.Error(err, "读取 DataSync 状态失败", "name", instance.Status.DataSyncName)
		}
	}

	if syncResource && instance.Status.ResourceSyncName != "" && failedError == nil {
		resourceSync := &disasterv1.ResourceSync{}
		if err := r.Get(ctx, client.ObjectKey{Namespace: instance.Namespace, Name: instance.Status.ResourceSyncName}, resourceSync); err == nil {
			if failed, reason := hasResourceSyncFailureSince(resourceSync, triggeredAtTime); failed {
				failedError = fmt.Errorf("ResourceSync %s is Failed: %s", resourceSync.Name, reason)
			} else {
				// 检查是否是本次触发的同步结果
				isCurrent := false
				if resourceSync.Status.LastSyncTime != nil && !resourceSync.Status.LastSyncTime.Time.Before(triggeredAtTime) {
					isCurrent = true
				}

				if !isCurrent {
					allReady = false
					pendingInfo = append(pendingInfo, fmt.Sprintf("ResourceSync(%s)=WaitingStart", resourceSync.Name))
				} else {
					if resourceSync.Status.State == disasterv1.ResourceSyncStateFailed {
						failedError = fmt.Errorf("ResourceSync %s is Failed", resourceSync.Name)
					} else if resourceSync.Status.State != disasterv1.ResourceSyncStateReady {
						allReady = false
						pendingInfo = append(pendingInfo, fmt.Sprintf("ResourceSync(%s)=%s", resourceSync.Name, resourceSync.Status.State))
					}
				}
			}
		} else {
			allReady = false
			pendingInfo = append(pendingInfo, fmt.Sprintf("ResourceSync(%s)=GetError", instance.Status.ResourceSyncName))
			log.Error(err, "读取 ResourceSync 状态失败", "name", instance.Status.ResourceSyncName)
		}
	}

	// 如果底层同步失败，检查重试策略
	if failedError != nil {
		log.Info("底层同步失败，检查重试策略", "error", failedError)

		// 检查重试条件
		if r.shouldRetry(operation) {
			log.Info("触发重试", "retryCount", operation.Status.RetryCount, "maxRetries", operation.Spec.RetryPolicy.MaxRetries)

			// 增加重试计数
			operation.Status.RetryCount++

			// 清除触发标记，以便下次 Requeue 时重新触发
			if operation.Annotations != nil {
				delete(operation.Annotations, triggerKey)
			}

			wait := r.retryWaitDuration(operation)
			nextRetryAt := metav1.NewTime(time.Now().Add(wait))
			operation.Status.NextRetryTime = &nextRetryAt

			msg := fmt.Sprintf("同步失败 (%v)，将在 %v 后重试 (%d/%d)", failedError, wait, operation.Status.RetryCount, operation.Spec.RetryPolicy.MaxRetries)
			operation.Status.Message = msg
			helper.ReportDiagnosticEvent(r.Recorder, operation, "Warning", "Retrying", msg)
			r.reportOperationProgress(ctx, operation, instance, msg)

			if err := r.updateOperationMetadataWithRetry(ctx, operation.Namespace, operation.Name, func(latest *disasterv1.DisasterOperation) {
				if latest.Annotations != nil {
					delete(latest.Annotations, triggerKey)
				}
			}); err != nil {
				return ctrl.Result{}, err
			}

			if err := r.updateOperationStatusWithRetry(ctx, operation.Namespace, operation.Name, func(latest *disasterv1.DisasterOperation) {
				latest.Status.RetryCount = operation.Status.RetryCount
				latest.Status.NextRetryTime = operation.Status.NextRetryTime
				latest.Status.Message = msg
			}); err != nil {
				return ctrl.Result{}, err
			}

			return ctrl.Result{RequeueAfter: wait}, nil
		}

		operation.Status.State = disasterv1.OperationStateFailed
		operation.Status.NextRetryTime = nil
		operation.Status.Message = fmt.Sprintf("同步失败: %v", failedError)
		helper.ReportDiagnosticEvent(r.Recorder, operation, "Warning", "SyncFailed", operation.Status.Message)
		r.reportOperationProgress(ctx, operation, instance, operation.Status.Message)
		return ctrl.Result{}, r.Status().Update(ctx, operation)
	}

	// 3. 超时检查
	if operation.Spec.TimeoutMinutes > 0 && operation.Status.StartTime != nil {
		elapsed := time.Since(operation.Status.StartTime.Time)
		timeout := time.Duration(operation.Spec.TimeoutMinutes) * time.Minute
		if elapsed > timeout {
			operation.Status.State = disasterv1.OperationStateFailed
			operation.Status.CompletionTime = &metav1.Time{Time: time.Now()}
			operation.Status.NextRetryTime = nil
			operation.Status.Message = fmt.Sprintf("同步超时 (已等待 %v，超时设置 %d 分钟)", elapsed.Round(time.Second), operation.Spec.TimeoutMinutes)
			helper.ReportDiagnosticEvent(r.Recorder, operation, "Warning", "SyncTimeout", operation.Status.Message)
			r.reportOperationProgress(ctx, operation, instance, operation.Status.Message)
			return ctrl.Result{}, r.Status().Update(ctx, operation)
		}
	}

	// 4. 等待或完成
	if !allReady {
		log.V(1).Info("等待同步完成", "pending", strings.Join(pendingInfo, ", "))
		operation.Status.Message = fmt.Sprintf("等待同步完成: %s", strings.Join(pendingInfo, ", "))
		r.reportOperationProgress(ctx, operation, instance, operation.Status.Message)
		r.Status().Update(ctx, operation)
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	// 所有同步已完成
	log.Info("同步已完成", "instance", instance.Name)
	operation.Status.State = disasterv1.OperationStateCompleted
	operation.Status.CompletionTime = &metav1.Time{Time: time.Now()}
	operation.Status.NextRetryTime = nil
	operation.Status.Message = "同步已完成"
	helper.ReportDiagnosticEvent(r.Recorder, instance, "Normal", "SyncCompleted", "同步已完成")
	return ctrl.Result{}, r.Status().Update(ctx, operation)
}

func hasDataSyncFailureSince(dataSync *disasterv1.DataSync, triggeredAt time.Time) (bool, string) {
	if dataSync == nil {
		return false, ""
	}

	if triggeredAt.IsZero() {
		if dataSync.Status.State == disasterv1.DataSyncStateFailed {
			return true, dataSyncFailureDetail(dataSync, "state=Failed")
		}
	} else if dataSync.Status.State == disasterv1.DataSyncStateFailed &&
		dataSync.Status.LastSyncTime != nil &&
		!dataSync.Status.LastSyncTime.Time.Before(triggeredAt) {
		return true, dataSyncFailureDetail(dataSync, "state=Failed")
	}

	for i := len(dataSync.Status.Conditions) - 1; i >= 0; i-- {
		cond := dataSync.Status.Conditions[i]
		if cond.Status != metav1.ConditionTrue {
			continue
		}
		if cond.Type != "SyncFailed" && cond.Reason != "SyncFailed" &&
			cond.Type != "BackupFailed" && cond.Reason != "BackupFailed" &&
			cond.Type != "RestoreFailed" && cond.Reason != "RestoreFailed" {
			continue
		}
		if !triggeredAt.IsZero() && cond.LastTransitionTime.Time.Before(triggeredAt) {
			continue
		}
		if cond.Message != "" {
			return true, cond.Message
		}
		if cond.Reason != "" {
			return true, cond.Reason
		}
		return true, cond.Type
	}

	return false, ""
}

func dataSyncFailureDetail(dataSync *disasterv1.DataSync, fallback string) string {
	if dataSync == nil {
		return fallback
	}
	if dataSync.Status.Message != "" {
		return dataSync.Status.Message
	}
	if dataSync.Status.Reason != "" {
		return dataSync.Status.Reason
	}
	return fallback
}

func resourceSyncFailureDetail(resourceSync *disasterv1.ResourceSync, fallback string) string {
	if resourceSync == nil {
		return fallback
	}
	if resourceSync.Status.Message != "" {
		return resourceSync.Status.Message
	}
	if resourceSync.Status.Reason != "" {
		return resourceSync.Status.Reason
	}
	return fallback
}

// hasResourceSyncFailureSince reports whether ResourceSync has a failure signal that belongs to current trigger window.
func hasResourceSyncFailureSince(resourceSync *disasterv1.ResourceSync, triggeredAt time.Time) (bool, string) {
	if resourceSync == nil {
		return false, ""
	}

	if triggeredAt.IsZero() {
		if resourceSync.Status.State == disasterv1.ResourceSyncStateFailed {
			return true, resourceSyncFailureDetail(resourceSync, "state=Failed")
		}
	} else if resourceSync.Status.State == disasterv1.ResourceSyncStateFailed &&
		resourceSync.Status.LastSyncTime != nil &&
		!resourceSync.Status.LastSyncTime.Time.Before(triggeredAt) {
		return true, resourceSyncFailureDetail(resourceSync, "state=Failed")
	}

	for i := len(resourceSync.Status.Conditions) - 1; i >= 0; i-- {
		cond := resourceSync.Status.Conditions[i]
		if cond.Status != metav1.ConditionTrue {
			continue
		}
		if cond.Type != "BuildRestoreSpecFailed" && cond.Reason != "BuildRestoreSpecFailed" &&
			cond.Type != "BackupFailed" && cond.Reason != "BackupFailed" &&
			cond.Type != "SyncFailed" && cond.Reason != "SyncFailed" &&
			cond.Type != "RestoreFailed" && cond.Reason != "RestoreFailed" {
			continue
		}
		if !triggeredAt.IsZero() && cond.LastTransitionTime.Time.Before(triggeredAt) {
			continue
		}
		if cond.Message != "" {
			return true, cond.Message
		}
		if cond.Reason != "" {
			return true, cond.Reason
		}
		return true, cond.Type
	}

	return false, ""
}

func (r *DisasterOperationReconciler) getClusterRESTConfig(ctx context.Context, clusterName string) (*rest.Config, error) {
	cluster := &disasterv1.Cluster{}
	if err := r.Get(ctx, types.NamespacedName{Name: clusterName}, cluster); err != nil {
		return nil, fmt.Errorf("failed to get cluster %s: %w", clusterName, err)
	}

	if len(cluster.Spec.KubeConfig) > 0 {
		restConfig, err := tools.GetRestConfig(cluster.Spec.KubeConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to build rest config for cluster %s: %w", clusterName, err)
		}
		return restConfig, nil
	}
	if cluster.Spec.Token != "" && cluster.Spec.Endpoint != "" {
		restConfig, err := tools.GetRestConfigFromToken(cluster.Spec.Endpoint, cluster.Spec.Token)
		if err != nil {
			return nil, fmt.Errorf("failed to build rest config for cluster %s: %w", clusterName, err)
		}
		return restConfig, nil
	}

	return nil, fmt.Errorf("cluster %s has no valid access credentials (kubeconfig or token)", clusterName)
}

// getClusterClient 获取指定 Cluster 名称的 Client
// 这里直接从 Cluster CR 读取 KubeConfig 构建 Client
func (r *DisasterOperationReconciler) getClusterClient(ctx context.Context, clusterName string) (client.Client, error) {
	restConfig, err := r.getClusterRESTConfig(ctx, clusterName)
	if err != nil {
		return nil, err
	}

	clientFactory := r.ClientFactory
	if clientFactory == nil {
		clientFactory = client.New
	}
	return clientFactory(restConfig, client.Options{Scheme: r.Scheme})
}

func (r *DisasterOperationReconciler) buildImageRewriteRulesForDrill(
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
		disasterv1.ImageRewriteApplyDrill,
	)
	if err != nil {
		return nil, err
	}
	if !enabled {
		return nil, nil
	}

	sourceClient, err := r.getClusterClient(ctx, sourceClusterName)
	if err != nil {
		return nil, fmt.Errorf("build source cluster client for drill image rewrite: %w", err)
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

// executeFailoverStep 执行具体的 Failover 步骤
func (r *DisasterOperationReconciler) executeFailoverStep(ctx context.Context, log logr.Logger, step *disasterv1.StepStatus, instance *disasterv1.DisasterInstance, operation *disasterv1.DisasterOperation) (bool, error) {
	stepName := disasterv1.FailoverStep(step.Name)
	switch stepName {
	case disasterv1.FailoverStepPreCheck:
		return r.executePreCheck(ctx, log, instance, operation)
	case disasterv1.FailoverStepPauseSchedules:
		return r.executePauseSchedules(ctx, instance) // Pause schedules doesn't strictly need trace ID propagation since it's just disabling
	case disasterv1.FailoverStepScaleDownSource:
		return r.executeScaleDownSource(ctx, instance, operation)
	case disasterv1.FailoverStepFinalSync:
		return r.executeFinalSync(ctx, instance, step, operation)
	case disasterv1.FailoverStepScaleUpTarget:
		return r.executeScaleUpTarget(ctx, instance)
	case disasterv1.FailoverStepCheckReplicas:
		return r.executeCheckReplicas(ctx, instance, operation)
	case disasterv1.FailoverStepResumeSchedules:
		return r.executeResumeSchedules(ctx, instance)
	case disasterv1.FailoverStepSwitchRoles:
		// 角色切换在 loop 之后统一处理，这里直接返回完成
		return true, nil
	default:
		return true, nil
	}
}

func (r *DisasterOperationReconciler) validateModifierRulesForPreCheck(
	ctx context.Context,
	instance *disasterv1.DisasterInstance,
	baselineSource string,
	baselineTarget string,
	sourceClusterName string,
) error {
	if instance == nil || instance.Spec.RestorePolicy == nil || len(instance.Spec.RestorePolicy.ModifierRules) == 0 {
		return nil
	}
	if validator := r.ModifierSubmissionValidator; validator != nil {
		return validator(ctx, instance, baselineSource, baselineTarget, sourceClusterName)
	}

	restConfig, err := r.getClusterRESTConfig(ctx, sourceClusterName)
	if err != nil {
		return err
	}
	locator, err := restore.NewDynamicModifierRuleResourceLocator(restConfig)
	if err != nil {
		return fmt.Errorf("build source cluster resource locator failed: %w", err)
	}

	return restore.ValidateModifierRulesAtSubmission(ctx, instance, baselineSource, baselineTarget, locator)
}

// executePreCheck 执行预检查
func (r *DisasterOperationReconciler) executePreCheck(ctx context.Context, log logr.Logger, instance *disasterv1.DisasterInstance, operation *disasterv1.DisasterOperation) (bool, error) {
	config := &disasterv1.DisasterConfig{}
	if err := r.Get(ctx, types.NamespacedName{Name: instance.Spec.Config}, config); err != nil {
		return false, fmt.Errorf("failed to get config: %w", err)
	}

	// 1. 检查目标集群 (Secondary in Failover) 连通性
	target := instance.Status.SecondaryCluster
	if target == "" {
		target = config.Spec.TargetCluster
	}
	targetClient, err := r.getClusterClient(ctx, target)
	if err != nil {
		return false, fmt.Errorf("pre-check failed: target cluster %s unreachable: %w", target, err)
	}
	log.Info("预检查: 目标集群连通性 OK", "cluster", target)

	// 1.1 执行实例级 RestorePolicy dry-run（DataSync/ResourceSync）。
	// 这样可以在进入破坏性步骤前提前失败，避免长链路运行后才暴露输入问题。
	if instance.Spec.RestorePolicy != nil {
		for _, applyTarget := range []disasterv1.RestoreModifierApplyTarget{
			disasterv1.RestoreModifierApplyDataSync,
			disasterv1.RestoreModifierApplyResourceSync,
		} {
			dryRun := &disasterv1.AppRestoreSpec{}
			policySummary, err := restore.ApplyInstanceRestorePolicy(
				ctx,
				dryRun,
				instance,
				targetClient,
				restore.WithBaselineClusters(config.Spec.SourceCluster, config.Spec.TargetCluster),
				restore.WithApplyTarget(applyTarget),
			)
			if err != nil {
				return false, fmt.Errorf("pre-check failed: restore policy validation failed (applyTarget=%s): %w", applyTarget, err)
			}
			r.reportOperationProgress(
				ctx,
				operation,
				instance,
				fmt.Sprintf("RestorePolicy dry-run 通过 (applyTarget=%s): %s", applyTarget, restore.ModifierAuditMessage(policySummary)),
			)
		}
		log.Info("预检查: RestorePolicy dry-run 校验 OK", "cluster", target)
	}

	// 2. 规则提交期等价校验（live object selection + path locatability）。
	// 该校验失败时必须 fail-closed，禁止进入后续破坏步骤。
	source := instance.Status.PrimaryCluster
	if source == "" {
		source = config.Spec.SourceCluster
	}
	if err := r.validateModifierRulesForPreCheck(
		ctx,
		instance,
		config.Spec.SourceCluster,
		config.Spec.TargetCluster,
		source,
	); err != nil {
		return false, fmt.Errorf("pre-check failed: restore policy submission validation failed: %w", err)
	}
	log.Info("预检查: RestorePolicy live 校验 OK", "cluster", source)

	// 3. 检查源集群 (Primary) 连通性 (仅当非强制时)
	// 如果 Force=true，则允许源集群不可达
	if !operation.Spec.Force {
		if _, err := r.getClusterClient(ctx, source); err != nil {
			return false, fmt.Errorf("pre-check failed: source cluster %s unreachable (use force=true to skip): %w", source, err)
		}
		log.Info("预检查: 源集群连通性 OK", "cluster", source)
	} else {
		log.Info("预检查: 强制模式，跳过源集群连通性检查")
	}

	// 3. 检查 RPO (DataSync Status)
	if instance.Status.DataSyncName != "" {
		ds := &disasterv1.DataSync{}
		if err := r.Get(ctx, client.ObjectKey{Namespace: instance.Namespace, Name: instance.Status.DataSyncName}, ds); err == nil {
			if ds.Status.LastSyncTime != nil {
				since := time.Since(ds.Status.LastSyncTime.Time)
				log.Info("预检查: 上次数据同步时间", "ago", since)
				// 可以设置阈值报警，这里暂仅记录
			}
		}
	}

	return true, nil
}

// executeScaleDownSource 缩容源集群工作负载
func (r *DisasterOperationReconciler) executeScaleDownSource(ctx context.Context, instance *disasterv1.DisasterInstance, operation *disasterv1.DisasterOperation) (bool, error) {
	// 显式跳过缩零：仅在 failover 场景由参数控制生效
	if shouldSkipScaleDownSource(operation) {
		r.Log.Info("Skipping source scale down due to SkipScaleDownSource configuration")
		helper.ReportDiagnosticEvent(r.Recorder, instance, corev1.EventTypeNormal, "ScaleDownSourceSkipped", "ScaleDownSource skipped due to SkipScaleDownSource configuration")
		return true, nil
	}

	// 1. 获取 DisasterConfig 以确定源集群
	config := &disasterv1.DisasterConfig{}
	if err := r.Get(ctx, types.NamespacedName{Name: instance.Spec.Config}, config); err != nil {
		return false, fmt.Errorf("failed to get config %s: %w", instance.Spec.Config, err)
	}

	// 2. 获取源集群 Client
	// 对于 ScaleDownSource，我们通常是在 Failover (Active -> Passive) 或 Failback (Active -> Passive)
	// DisasterInstance.Status.PrimaryCluster 记录了当前的 Active 侧。
	// 在 Failover 开始时，PrimaryCluster 是 Source。
	// 我们需要 Scale Down 当前的 PrimaryCluster。
	targetClusterName := instance.Status.PrimaryCluster
	if targetClusterName == "" {
		// Fallback to config source if status not set (first run)
		targetClusterName = config.Spec.SourceCluster
	}

	remoteClient, err := r.getClusterClient(ctx, targetClusterName)
	if err != nil {
		return false, err
	}

	// ===== 关键修复: 在缩容之前先记录副本数到 ConfigMap =====
	// 这样确保 ScaleUpTarget 读取到的是缩容前的原始副本数
	if err := r.recordReplicasBeforeScaleDown(ctx, instance, remoteClient, operation); err != nil {

		// 记录失败不应阻塞 Failover，但要记录警告
		r.Log.Error(err, "Warning: failed to record replicas before scale down")
		helper.ReportDiagnosticEventf(r.Recorder, instance, corev1.EventTypeWarning, "RecordReplicasFailed",
			"Failed to record replicas before scale down: %v", err)
	} else {
		r.Log.Info("Successfully recorded replicas before scale down")
		helper.ReportDiagnosticEvent(r.Recorder, instance, corev1.EventTypeNormal, "ReplicasRecorded",
			"Successfully recorded original replicas before scale down")
	}

	// 3. 遍历 Namespace 进行缩容
	for _, ns := range instance.Spec.Namespaces {
		// Deployments
		deployList := &appsv1.DeploymentList{}
		if err := remoteClient.List(ctx, deployList, client.InNamespace(ns)); err != nil {
			return false, err
		}
		for _, deploy := range deployList.Items {
			if deploy.Spec.Replicas != nil && *deploy.Spec.Replicas != 0 {
				// 保存原始副本数到 annotation (作为备份)
				originalReplicas := fmt.Sprintf("%d", *deploy.Spec.Replicas)
				if deploy.Annotations == nil {
					deploy.Annotations = make(map[string]string)
				}
				deploy.Annotations["testudo.softcdata.com/original-replicas"] = originalReplicas

				zero := int32(0)
				deploy.Spec.Replicas = &zero
				if err := remoteClient.Update(ctx, &deploy); err != nil {
					return false, err
				}
			}
		}

		// StatefulSets
		stsList := &appsv1.StatefulSetList{}
		if err := remoteClient.List(ctx, stsList, client.InNamespace(ns)); err != nil {
			return false, err
		}
		for _, sts := range stsList.Items {
			if sts.Spec.Replicas != nil && *sts.Spec.Replicas != 0 {
				originalReplicas := fmt.Sprintf("%d", *sts.Spec.Replicas)
				if sts.Annotations == nil {
					sts.Annotations = make(map[string]string)
				}
				sts.Annotations["testudo.softcdata.com/original-replicas"] = originalReplicas

				zero := int32(0)
				sts.Spec.Replicas = &zero
				if err := remoteClient.Update(ctx, &sts); err != nil {
					return false, err
				}
			}
		}
	}
	return true, nil
}

func shouldSkipScaleDownSource(operation *disasterv1.DisasterOperation) bool {
	if operation == nil {
		return false
	}
	if operation.Spec.SkipScaleDownSource {
		return true
	}
	if operation.Annotations == nil {
		return false
	}
	raw, ok := operation.Annotations[annotationSkipScaleDownSource]
	if !ok || strings.TrimSpace(raw) == "" {
		return false
	}
	parsed, err := strconv.ParseBool(raw)
	if err != nil {
		return false
	}
	return parsed
}

// recordReplicasBeforeScaleDown 在缩容之前记录所有工作负载的副本数到 ConfigMap
// 这是关键步骤，确保 ScaleUpTarget 能读取到正确的副本数
func (r *DisasterOperationReconciler) recordReplicasBeforeScaleDown(ctx context.Context, instance *disasterv1.DisasterInstance, remoteClient client.Client, operation *disasterv1.DisasterOperation) error {
	replicasMap := make(map[string]int32)

	for _, ns := range instance.Spec.Namespaces {
		// Deployments
		deployList := &appsv1.DeploymentList{}
		if err := remoteClient.List(ctx, deployList, client.InNamespace(ns)); err != nil {
			return fmt.Errorf("failed to list deployments in namespace %s: %w", ns, err)
		}
		for _, deploy := range deployList.Items {
			if deploy.Spec.Replicas != nil && *deploy.Spec.Replicas > 0 {
				key := fmt.Sprintf("%s/deployments/%s", ns, deploy.Name)
				replicasMap[key] = *deploy.Spec.Replicas
				r.Log.V(1).Info("Recording deployment replicas", "key", key, "replicas", *deploy.Spec.Replicas)
			}
		}

		// StatefulSets
		stsList := &appsv1.StatefulSetList{}
		if err := remoteClient.List(ctx, stsList, client.InNamespace(ns)); err != nil {
			return fmt.Errorf("failed to list statefulsets in namespace %s: %w", ns, err)
		}
		for _, sts := range stsList.Items {
			if sts.Spec.Replicas != nil && *sts.Spec.Replicas > 0 {
				key := fmt.Sprintf("%s/statefulsets/%s", ns, sts.Name)
				replicasMap[key] = *sts.Spec.Replicas
				r.Log.V(1).Info("Recording statefulset replicas", "key", key, "replicas", *sts.Spec.Replicas)
			}
		}
	}

	// 如果没有需要记录的副本数，直接返回
	if len(replicasMap) == 0 {
		r.Log.Info("No replicas to record (all workloads already have 0 replicas)")
		return nil
	}

	// 序列化副本数
	data, err := json.Marshal(replicasMap)
	if err != nil {
		return fmt.Errorf("failed to marshal replicas map: %w", err)
	}

	// 获取 ConfigMap 名称
	cmName := fmt.Sprintf("replicas-%s", instance.Status.ResourceSyncName)
	if instance.Status.ResourceSyncName == "" {
		// Fallback: 使用 instance 名称
		cmName = fmt.Sprintf("replicas-dr-rs-%s", instance.Name)
	}

	r.Log.Info("Saving replicas to ConfigMap", "configmap", cmName, "count", len(replicasMap))

	// 创建或更新 ConfigMap
	cm := &corev1.ConfigMap{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: instance.Namespace, Name: cmName}, cm); err != nil {
		if errors.IsNotFound(err) {
			// 创建新的 ConfigMap
			cm = &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cmName,
					Namespace: instance.Namespace,
					Labels: map[string]string{
						"testudo.softcdata.com/instance": instance.Name,
						"testudo.softcdata.com/type":     "replicas-record",
					},
					Annotations: map[string]string{},
				},
				Data: map[string]string{
					"replicas": string(data),
				},
			}
			// Inject Trace ID
			if operation != nil {
				if tid, ok := operation.Annotations[metadata.AnnotationTraceID]; ok {
					cm.Annotations[metadata.AnnotationTraceID] = tid
				}
			}

			if err := r.Create(ctx, cm); err != nil {
				return fmt.Errorf("failed to create replicas ConfigMap: %w", err)
			}
			r.Log.Info("Created replicas ConfigMap", "name", cmName)
			return nil
		}
		return fmt.Errorf("failed to get replicas ConfigMap: %w", err)
	}

	// 更新现有的 ConfigMap
	if cm.Data == nil {
		cm.Data = make(map[string]string)
	}
	cm.Data["replicas"] = string(data)
	// Inject Trace ID
	if operation != nil {
		if tid, ok := operation.Annotations[metadata.AnnotationTraceID]; ok {
			if cm.Annotations == nil {
				cm.Annotations = make(map[string]string)
			}
			cm.Annotations[metadata.AnnotationTraceID] = tid
		}
	}
	if err := r.Update(ctx, cm); err != nil {
		return fmt.Errorf("failed to update replicas ConfigMap: %w", err)
	}
	r.Log.Info("Updated replicas ConfigMap", "name", cmName)
	return nil
}

func (r *DisasterOperationReconciler) triggerDataSyncManualWithRetry(ctx context.Context, namespace, name, traceID string) error {
	return r.triggerDataSyncManualAtWithRetry(ctx, namespace, name, time.Now().Format(time.RFC3339), traceID)
}

func (r *DisasterOperationReconciler) triggerDataSyncManualAtWithRetry(ctx context.Context, namespace, name, manualTime, traceID string) error {
	if manualTime == "" {
		manualTime = time.Now().Format(time.RFC3339)
	}
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		latest := &disasterv1.DataSync{}
		if err := r.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, latest); err != nil {
			return err
		}
		// 已在执行中则无需再次触发，避免无意义写冲突。
		if latest.Status.State == disasterv1.DataSyncStateInProgress {
			return nil
		}

		latest.Spec.Trigger.Manual = manualTime
		if traceID != "" {
			if latest.Annotations == nil {
				latest.Annotations = make(map[string]string)
			}
			latest.Annotations[metadata.AnnotationLastTraceID] = traceID
		}
		return r.Update(ctx, latest)
	})
}

func (r *DisasterOperationReconciler) triggerResourceSyncManualWithRetry(ctx context.Context, namespace, name, traceID string) error {
	return r.triggerResourceSyncManualAtWithRetry(ctx, namespace, name, time.Now().Format(time.RFC3339), traceID)
}

func (r *DisasterOperationReconciler) triggerResourceSyncManualAtWithRetry(ctx context.Context, namespace, name, manualTime, traceID string) error {
	if manualTime == "" {
		manualTime = time.Now().Format(time.RFC3339)
	}
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		latest := &disasterv1.ResourceSync{}
		if err := r.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, latest); err != nil {
			return err
		}
		// 已在执行中则无需再次触发，避免无意义写冲突。
		if latest.Status.State == disasterv1.ResourceSyncStateInProgress {
			return nil
		}

		latest.Spec.Trigger.Manual = manualTime
		if traceID != "" {
			if latest.Annotations == nil {
				latest.Annotations = make(map[string]string)
			}
			latest.Annotations[metadata.AnnotationLastTraceID] = traceID
		}
		return r.Update(ctx, latest)
	})
}

func (r *DisasterOperationReconciler) updateOperationMetadataWithRetry(ctx context.Context, namespace, name string, mutate func(*disasterv1.DisasterOperation)) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		latest := &disasterv1.DisasterOperation{}
		if err := r.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, latest); err != nil {
			return err
		}
		mutate(latest)
		return r.Update(ctx, latest)
	})
}

func (r *DisasterOperationReconciler) updateOperationStatusWithRetry(ctx context.Context, namespace, name string, mutate func(*disasterv1.DisasterOperation)) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		latest := &disasterv1.DisasterOperation{}
		if err := r.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, latest); err != nil {
			return err
		}
		mutate(latest)
		return r.Status().Update(ctx, latest)
	})
}

// executeFinalSync 触发并等待最后一次同步
func (r *DisasterOperationReconciler) executeFinalSync(ctx context.Context, instance *disasterv1.DisasterInstance, step *disasterv1.StepStatus, operation *disasterv1.DisasterOperation) (bool, error) {
	// 检查是否配置了跳过最终同步
	if operation.Spec.SkipFinalSync {
		r.Log.Info("Skipping FinalSync due to SkipFinalSync configuration")
		helper.ReportDiagnosticEvent(r.Recorder, instance, corev1.EventTypeNormal, "FinalSyncSkipped", "FinalSync skipped due to SkipFinalSync configuration")
		return true, nil
	}

	allSynced := true
	traceID := ""
	if operation != nil {
		traceID = operation.Annotations[metadata.AnnotationTraceID]
	}
	var triggeredAt time.Time
	if step != nil && step.StartTime != nil {
		triggeredAt = step.StartTime.Time
	}

	// Check DataSync
	dataSyncList := &disasterv1.DataSyncList{}
	if err := r.List(ctx, dataSyncList, client.InNamespace(instance.Namespace)); err != nil {
		return false, err
	}
	for i := range dataSyncList.Items {
		ds := &dataSyncList.Items[i]
		if ds.Spec.Instance != instance.Name {
			continue
		}
		if failed, reason := hasDataSyncFailureSince(ds, triggeredAt); failed {
			return false, fmt.Errorf("DataSync %s is Failed: %s", ds.Name, reason)
		}

		lastSync := ds.Status.LastSyncTime
		// 如果从未同步，或最后一次同步早于步骤开始时间，则触发同步
		if lastSync == nil || (step.StartTime != nil && lastSync.Before(step.StartTime)) {
			// 只有在非 Running 状态才触发，避免重复触发
			if ds.Status.State != disasterv1.DataSyncStateInProgress {
				if err := r.triggerDataSyncManualWithRetry(ctx, ds.Namespace, ds.Name, traceID); err != nil {
					return false, err
				}
			}
			allSynced = false
		} else {
			// 等待同步完成 (Ready)
			if ds.Status.State != disasterv1.DataSyncStateReady {
				allSynced = false
			}
		}
	}

	// Check ResourceSync
	resourceSyncList := &disasterv1.ResourceSyncList{}
	if err := r.List(ctx, resourceSyncList, client.InNamespace(instance.Namespace)); err != nil {
		return false, err
	}
	for i := range resourceSyncList.Items {
		rs := &resourceSyncList.Items[i]
		if rs.Spec.Instance != instance.Name {
			continue
		}
		if failed, reason := hasResourceSyncFailureSince(rs, triggeredAt); failed {
			return false, fmt.Errorf("ResourceSync %s is Failed: %s", rs.Name, reason)
		}

		lastSync := rs.Status.LastSyncTime
		if lastSync == nil || (step.StartTime != nil && lastSync.Before(step.StartTime)) {
			if rs.Status.State != disasterv1.ResourceSyncStateInProgress {
				if err := r.triggerResourceSyncManualWithRetry(ctx, rs.Namespace, rs.Name, traceID); err != nil {
					return false, err
				}
			}
			allSynced = false
		} else {
			if rs.Status.State != disasterv1.ResourceSyncStateReady {
				allSynced = false
			}
		}
	}

	return allSynced, nil
}

// executePauseSchedules 暂停所有关联的同步任务
func (r *DisasterOperationReconciler) executePauseSchedules(ctx context.Context, instance *disasterv1.DisasterInstance) (bool, error) {
	// 暂停 DataSync
	dataSyncList := &disasterv1.DataSyncList{}
	if err := r.List(ctx, dataSyncList, client.InNamespace(instance.Namespace)); err != nil {
		return false, err
	}
	for i := range dataSyncList.Items {
		ds := &dataSyncList.Items[i]
		if ds.Spec.Instance == instance.Name && !ds.Spec.Paused {
			ds.Spec.Paused = true
			if err := r.Update(ctx, ds); err != nil {
				return false, err
			}
		}
	}

	// 暂停 ResourceSync
	resourceSyncList := &disasterv1.ResourceSyncList{}
	if err := r.List(ctx, resourceSyncList, client.InNamespace(instance.Namespace)); err != nil {
		return false, err
	}
	for i := range resourceSyncList.Items {
		rs := &resourceSyncList.Items[i]
		if rs.Spec.Instance == instance.Name && !rs.Spec.Paused {
			rs.Spec.Paused = true
			if err := r.Update(ctx, rs); err != nil {
				return false, err
			}
		}
	}

	return true, nil
}

// executeScaleUpTarget 恢复目标集群工作负载和流量
func (r *DisasterOperationReconciler) executeScaleUpTarget(ctx context.Context, instance *disasterv1.DisasterInstance) (bool, error) {
	// 1. 获取 DisasterConfig
	config := &disasterv1.DisasterConfig{}
	if err := r.Get(ctx, types.NamespacedName{Name: instance.Spec.Config}, config); err != nil {
		return false, fmt.Errorf("failed to get config %s: %w", instance.Spec.Config, err)
	}

	// 2. 获取目标集群 Client
	// 目标集群是当前的 SecondaryCluster (即将变为 Primary)
	targetClusterName := instance.Status.SecondaryCluster
	if targetClusterName == "" {
		targetClusterName = config.Spec.TargetCluster
	}

	remoteClient, err := r.getClusterClient(ctx, targetClusterName)
	if err != nil {
		return false, err
	}

	// 3.获取 Replicas Map
	replicasMap := make(map[string]int32)
	if instance.Status.ResourceSyncName != "" {
		cmName := fmt.Sprintf("replicas-%s", instance.Status.ResourceSyncName)
		cm := &corev1.ConfigMap{}
		if err := r.Get(ctx, types.NamespacedName{Namespace: instance.Namespace, Name: cmName}, cm); err == nil {
			if data, ok := cm.Data["replicas"]; ok {
				var storedMap map[string]int32
				if err := json.Unmarshal([]byte(data), &storedMap); err == nil {
					replicasMap = storedMap
				}
			}
		} else {
			// 如果 ConfigMap 不存在，可能无法恢复精确副本数，但这不应阻塞 Failover
			// log.Info("Warning: Replicas ConfigMap not found", "name", cmName)
			helper.ReportDiagnosticEventf(r.Recorder, instance, corev1.EventTypeWarning, "ReplicasConfigMapNotFound", "Replicas ConfigMap not found: %s", cmName)
		}
	}

	for _, ns := range instance.Spec.Namespaces {

		// 2. 扩容 Deployments (恢复原始副本数)
		deployList := &appsv1.DeploymentList{}
		if err := remoteClient.List(ctx, deployList, client.InNamespace(ns)); err != nil {
			return false, err
		}
		for _, deploy := range deployList.Items {
			// 只有当当前为0时才恢复，避免覆盖用户手动操作
			if deploy.Spec.Replicas != nil && *deploy.Spec.Replicas == 0 {
				var targetReplicas int32 = -1

				// 优先查找 ConfigMap
				key := fmt.Sprintf("%s/deployments/%s", ns, deploy.Name)
				if val, ok := replicasMap[key]; ok {
					targetReplicas = val
				} else if val, ok := deploy.Annotations["testudo.softcdata.com/original-replicas"]; ok {
					var r int32
					if _, err := fmt.Sscanf(val, "%d", &r); err == nil {
						targetReplicas = r
					}
				}

				if targetReplicas != -1 {
					r.Log.Info("Scaling up Deployment", "namespace", ns, "name", deploy.Name, "replicas", targetReplicas)
					helper.ReportDiagnosticEventf(r.Recorder, instance, corev1.EventTypeNormal, "ScalingUp", "Scaling up Deployment %s/%s to %d replicas", ns, deploy.Name, targetReplicas)

					deploy.Spec.Replicas = &targetReplicas
					if err := remoteClient.Update(ctx, &deploy); err != nil {
						return false, err
					}
				} else {
					r.Log.Info("Warning: Original replicas not found, skipping scale up", "kind", "Deployment", "namespace", ns, "name", deploy.Name)
					helper.ReportDiagnosticEventf(r.Recorder, instance, corev1.EventTypeWarning, "ScaleUpSkipped", "Original replicas not found for Deployment %s/%s", ns, deploy.Name)
				}
			}
		}

		// 3. 扩容 StatefulSets
		stsList := &appsv1.StatefulSetList{}
		if err := remoteClient.List(ctx, stsList, client.InNamespace(ns)); err != nil {
			return false, err
		}
		for _, sts := range stsList.Items {
			if sts.Spec.Replicas != nil && *sts.Spec.Replicas == 0 {
				var targetReplicas int32 = -1

				key := fmt.Sprintf("%s/statefulsets/%s", ns, sts.Name)
				if val, ok := replicasMap[key]; ok {
					targetReplicas = val
				} else if val, ok := sts.Annotations["testudo.softcdata.com/original-replicas"]; ok {
					var r int32
					if _, err := fmt.Sscanf(val, "%d", &r); err == nil {
						targetReplicas = r
					}
				}

				if targetReplicas != -1 {
					r.Log.Info("Scaling up StatefulSet", "namespace", ns, "name", sts.Name, "replicas", targetReplicas)
					helper.ReportDiagnosticEventf(r.Recorder, instance, corev1.EventTypeNormal, "ScalingUp", "Scaling up StatefulSet %s/%s to %d replicas", ns, sts.Name, targetReplicas)

					sts.Spec.Replicas = &targetReplicas
					if err := remoteClient.Update(ctx, &sts); err != nil {
						return false, err
					}
				} else {
					r.Log.Info("Warning: Original replicas not found, skipping scale up", "kind", "StatefulSet", "namespace", ns, "name", sts.Name)
					helper.ReportDiagnosticEventf(r.Recorder, instance, corev1.EventTypeWarning, "ScaleUpSkipped", "Original replicas not found for StatefulSet %s/%s", ns, sts.Name)
				}
			}
		}
	}

	// ScaleUpTarget 职责仅限于下发 spec.replicas 配置
	// 就绪检查由后续的 CheckReplicas 步骤根据 WaitUntilReady 参数决定是否执行
	r.Log.Info("ScaleUpTarget completed: replicas configuration applied, readiness check will be done in CheckReplicas step")
	return true, nil
}

// SetupWithManager 将控制器注册到 Manager
func (r *DisasterOperationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&disasterv1.DisasterOperation{}).
		Complete(r)
}

// handleDrill 处理容灾演练操作
func (r *DisasterOperationReconciler) handleDrill(ctx context.Context, log logr.Logger, operation *disasterv1.DisasterOperation) (ctrl.Result, error) {
	log.Info("处理 Drill 操作")

	// 获取 Instance
	instance := &disasterv1.DisasterInstance{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: operation.Namespace, Name: operation.Spec.InstanceName}, instance); err != nil {
		operation.Status.State = disasterv1.OperationStateFailed
		operation.Status.Message = "DisasterInstance 未找到"
		return ctrl.Result{}, r.Status().Update(ctx, operation)
	}

	// 获取 Config
	config := &disasterv1.DisasterConfig{}
	if err := r.Get(ctx, types.NamespacedName{Name: instance.Spec.Config}, config); err != nil {
		operation.Status.State = disasterv1.OperationStateFailed
		operation.Status.Message = fmt.Sprintf("DisasterConfig %s 未找到", instance.Spec.Config)
		return ctrl.Result{}, r.Status().Update(ctx, operation)
	}

	// 确定目标集群
	drillConfig := operation.Spec.DrillConfig
	targetCluster := instance.Status.SecondaryCluster
	if drillConfig != nil && drillConfig.TargetCluster != "" {
		targetCluster = drillConfig.TargetCluster
	}

	// 演练始终使用完整恢复模式
	restoreMode := disasterv1.RestoreModeFullRestore

	// 初始化步骤：恢复资源 -> 恢复数据 -> 扩容
	if len(operation.Status.Steps) == 0 {
		steps := []string{
			string(disasterv1.DrillOperationStepRestoreResource),
			string(disasterv1.DrillOperationStepRestoreData),
			string(disasterv1.DrillOperationStepScaleUp),
		}
		for _, stepName := range steps {
			operation.Status.Steps = append(operation.Status.Steps, disasterv1.StepStatus{
				Name:  stepName,
				State: "Pending",
			})
		}
		operation.Status.CurrentStep = steps[0]
		operation.Status.State = disasterv1.OperationStateRunning
		if err := r.Status().Update(ctx, operation); err != nil {
			return ctrl.Result{}, err
		}
		helper.ReportDiagnosticEventf(r.Recorder, operation, "Normal", "Started", "演练 %s 开始", operation.Name)
		r.reportOperationProgress(ctx, operation, instance, "演练步骤初始化完成，开始执行")
		return ctrl.Result{Requeue: true}, nil
	}

	// 执行步骤
	for i := range operation.Status.Steps {
		step := &operation.Status.Steps[i]
		if step.State == "Pending" {
			step.State = "Running"
			step.StartTime = &metav1.Time{Time: time.Now()}
			operation.Status.CurrentStep = step.Name
			log.Info("开始 Drill 步骤", "step", step.Name)
			helper.ReportDiagnosticEventf(r.Recorder, operation, "Normal", "StepStarted", "步骤 %s 已开始", step.Name)
			r.reportOperationProgress(ctx, operation, instance, fmt.Sprintf("步骤 %s 已开始", step.Name))
			r.Status().Update(ctx, operation)
			return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
		}
		if step.State == "Running" {
			completed, err := r.executeDrillStep(ctx, log, step, instance, operation, targetCluster, restoreMode)
			if err != nil {
				step.State = "Failed"
				step.Message = fmt.Sprintf("Failed: %v", err)
				operation.Status.State = disasterv1.OperationStateFailed
				operation.Status.Message = step.Message
				helper.ReportDiagnosticEventf(r.Recorder, operation, "Warning", "StepFailed", "步骤 %s 失败: %v", step.Name, err)
				r.reportOperationProgress(ctx, operation, instance, fmt.Sprintf("步骤 %s 失败: %v", step.Name, err))
				r.Status().Update(ctx, operation)
				return ctrl.Result{}, nil
			}
			if !completed {
				return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
			}
			step.State = "Completed"
			step.CompletionTime = &metav1.Time{Time: time.Now()}
			helper.ReportDiagnosticEventf(r.Recorder, operation, "Normal", "StepCompleted", "步骤 %s 已完成", step.Name)
			r.reportOperationProgress(ctx, operation, instance, fmt.Sprintf("步骤 %s 已完成", step.Name))
			r.Status().Update(ctx, operation)
			return ctrl.Result{Requeue: true}, nil
		}
	}

	// 全部完成
	operation.Status.State = disasterv1.OperationStateCompleted
	operation.Status.CompletionTime = &metav1.Time{Time: time.Now()}
	operation.Status.Message = "演练完成"
	helper.ReportDiagnosticEventf(r.Recorder, operation, "Normal", "Completed", "演练 %s 已完成", operation.Name)
	r.reportOperationProgress(ctx, operation, instance, "演练全部步骤已完成")
	r.Status().Update(ctx, operation)
	return ctrl.Result{}, nil
}

// handleDrillCleanup 处理容灾演练清理操作
func (r *DisasterOperationReconciler) handleDrillCleanup(ctx context.Context, log logr.Logger, operation *disasterv1.DisasterOperation) (ctrl.Result, error) {
	log.Info("处理 Drill Cleanup 操作")

	// 获取 Instance
	instance := &disasterv1.DisasterInstance{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: operation.Namespace, Name: operation.Spec.InstanceName}, instance); err != nil {
		operation.Status.State = disasterv1.OperationStateFailed
		operation.Status.Message = "DisasterInstance 未找到"
		return ctrl.Result{}, r.Status().Update(ctx, operation)
	}

	drillConfig := operation.Spec.DrillConfig
	targetCluster := instance.Status.SecondaryCluster
	if drillConfig != nil && drillConfig.TargetCluster != "" {
		targetCluster = drillConfig.TargetCluster
	}

	remoteClient, err := r.getClusterClient(ctx, targetCluster)
	if err != nil {
		operation.Status.State = disasterv1.OperationStateFailed
		operation.Status.Message = fmt.Sprintf("无法连接目标集群 %s: %v", targetCluster, err)
		return ctrl.Result{}, r.Status().Update(ctx, operation)
	}

	// 解析 NamespaceMapping
	var namespaceMapping map[string]string
	if drillConfig != nil && len(drillConfig.NamespaceMapping) > 0 {
		namespaceMapping = drillConfig.NamespaceMapping
	}

	if len(namespaceMapping) == 0 {
		// 无 NamespaceMapping 场景：复用目标集群空间，缩容目标集群工作负载为 0
		log.Info("无 NamespaceMapping，开始执行清理缩容(ScaleDownTarget)")
		completed, err := r.doScaleDown(ctx, instance, targetCluster)
		if err != nil {
			operation.Status.State = disasterv1.OperationStateFailed
			operation.Status.Message = fmt.Sprintf("清理缩容失败: %v", err)
			helper.ReportDiagnosticEventf(r.Recorder, operation, "Warning", "CleanupFailed", "清理缩容失败: %v", err)
			return ctrl.Result{}, r.Status().Update(ctx, operation)
		}
		if !completed {
			return ctrl.Result{RequeueAfter: 3 * time.Second}, nil
		}
	} else {
		// 有 NamespaceMapping 场景：直接删除目标的 Namespace
		log.Info("发现 NamespaceMapping，开始删除映射空间")
		for _, mappedNs := range namespaceMapping {
			nsOut := &corev1.Namespace{}
			err := remoteClient.Get(ctx, types.NamespacedName{Name: mappedNs}, nsOut)
			if err != nil {
				if !errors.IsNotFound(err) {
					operation.Status.State = disasterv1.OperationStateFailed
					operation.Status.Message = fmt.Sprintf("获取映射命名空间 %s 失败: %v", mappedNs, err)
					return ctrl.Result{}, r.Status().Update(ctx, operation)
				}
				log.Info("映射命名空间已不存在，跳过删除", "namespace", mappedNs)
				continue
			}

			if nsOut.DeletionTimestamp == nil {
				log.Info("删除映射命名空间", "namespace", mappedNs)
				if err := remoteClient.Delete(ctx, nsOut); err != nil {
					operation.Status.State = disasterv1.OperationStateFailed
					operation.Status.Message = fmt.Sprintf("删除映射命名空间 %s 失败: %v", mappedNs, err)
					return ctrl.Result{}, r.Status().Update(ctx, operation)
				}
			}
		}
	}

	// 成功清理
	operation.Status.State = disasterv1.OperationStateCompleted
	operation.Status.CompletionTime = &metav1.Time{Time: time.Now()}
	operation.Status.Message = "演练资源清理完成"
	if err := r.Status().Update(ctx, operation); err != nil {
		return ctrl.Result{}, err
	}

	helper.ReportDiagnosticEvent(r.Recorder, operation, "Normal", "Completed", "演练清理完成")
	return ctrl.Result{}, nil
}

// executeDrillStep 执行单个演练步骤
func (r *DisasterOperationReconciler) executeDrillStep(ctx context.Context, log logr.Logger, step *disasterv1.StepStatus, instance *disasterv1.DisasterInstance, operation *disasterv1.DisasterOperation, targetCluster string, restoreMode disasterv1.RestoreMode) (bool, error) {
	stepName := disasterv1.DrillOperationStep(step.Name)
	switch stepName {
	case disasterv1.DrillOperationStepRestoreResource:
		// 恢复资源：从 ResourceSync 备份恢复 K8s 资源
		return r.executeDrillRestoreResource(ctx, log, instance, operation, targetCluster)
	case disasterv1.DrillOperationStepRestoreData:
		// 恢复数据：从 DataSync 备份恢复 PVC 数据
		return r.executeDrillRestoreData(ctx, log, instance, operation, targetCluster)
	case disasterv1.DrillOperationStepScaleUp:
		// 扩容目标集群
		return r.executeDrillScaleUp(ctx, log, instance, operation, targetCluster, restoreMode)
	default:
		return true, nil
	}
}

// executeDrillRestoreResource 执行资源恢复步骤 (从 ResourceSync 备份恢复 K8s 资源)
func (r *DisasterOperationReconciler) executeDrillRestoreResource(ctx context.Context, log logr.Logger, instance *disasterv1.DisasterInstance, operation *disasterv1.DisasterOperation, targetCluster string) (bool, error) {
	log.Info("执行 Drill 资源恢复步骤", "targetCluster", targetCluster)

	drillConfig := operation.Spec.DrillConfig
	drillRestorePolicy := func() *disasterv1.RestorePolicy {
		if drillConfig == nil {
			return nil
		}
		return drillConfig.RestorePolicy
	}()

	// 0. Fetch DisasterConfig
	config := &disasterv1.DisasterConfig{}
	if err := r.Get(ctx, types.NamespacedName{Name: instance.Spec.Config}, config); err != nil {
		return false, fmt.Errorf("failed to get config %s: %w", instance.Spec.Config, err)
	}

	// 1. 尝试从 Status 获取
	if operation.Status.ResourceRestoreName != "" {
		finished, err := r.checkAppRestoreStatus(ctx, instance.Namespace, operation.Status.ResourceRestoreName)
		if err != nil {
			return false, err
		}
		if finished {
			return true, nil
		}
		return false, nil
	}

	// 2. 尝试通过 Label 查找已存在的 AppRestore (防止 Status 更新失败导致的重复创建)
	existingRestore, err := r.findAppRestoreByLabel(ctx, instance.Namespace, operation.Name, "resource")
	if err != nil {
		return false, err
	}
	if existingRestore != nil {
		log.Info("发现已存在的资源恢复 AppRestore，复用之", "name", existingRestore.Name)
		operation.Status.ResourceRestoreName = existingRestore.Name
		if err := r.Status().Update(ctx, operation); err != nil {
			return false, err
		}
		// 状态更新后，返回 false 等待下一次 Reconcile 检查状态
		return false, nil
	}

	// 3. 首次执行：查找最近的 ResourceSync 备份
	resourceSync := &disasterv1.ResourceSync{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: instance.Namespace, Name: instance.Status.ResourceSyncName}, resourceSync); err != nil {
		return false, fmt.Errorf("ResourceSync 未找到: %w", err)
	}

	if resourceSync.Status.LastBackupName == "" {
		return false, fmt.Errorf("ResourceSync 没有可用的备份")
	}

	// 4. 创建 AppRestore (使用确定性命名，且尽量短)
	// 格式: drr-{opName[:10]}-{hash[:6]}
	// 总长度 ≈ 4 + 10 + 1 + 6 = 21 字符，远小于 63
	opNameLen := len(operation.Name)
	if opNameLen > 10 {
		opNameLen = 10
	}
	opHash := fmt.Sprintf("%x", md5.Sum([]byte(operation.Name)))[:6]
	restoreName := fmt.Sprintf("drr-%s-%s", operation.Name[:opNameLen], opHash)

	// 构造命名空间映射
	var namespaceMapping map[string]string
	if drillConfig != nil && len(drillConfig.NamespaceMapping) > 0 {
		namespaceMapping = drillConfig.NamespaceMapping
	}

	sourceCluster := instance.Status.PrimaryCluster
	if sourceCluster == "" {
		sourceCluster = config.Spec.SourceCluster
	}

	imageRewriteRules, err := r.buildImageRewriteRulesForDrill(ctx, config, instance, sourceCluster, targetCluster)
	if err != nil {
		return false, fmt.Errorf("build drill image rewrite rules: %w", err)
	}

	effectiveInstance := instance
	if instance != nil {
		effectiveInstance = instance.DeepCopy()
		effectiveInstance.Spec.RestorePolicy = restore.EffectiveRestorePolicy(instance, drillRestorePolicy)
	}
	runtimeImageRewriteRules := []disasterv1.RestoreModifierRule(nil)
	if effectiveInstance != nil && restore.HasDynamicImageRewriteActions(effectiveInstance.Spec.RestorePolicy, disasterv1.RestoreModifierApplyDrill) {
		sourceClient, err := r.getClusterClient(ctx, sourceCluster)
		if err != nil {
			return false, fmt.Errorf("build drill source cluster client for dynamic image rewrite: %w", err)
		}
		runtimeImageRewriteRules, _, err = (&restore.DynamicImageRewriteCompiler{}).CompileDynamicImageRewriteRules(
			ctx,
			effectiveInstance,
			sourceClient,
			disasterv1.RestoreModifierApplyDrill,
			restore.WithDynamicImageRewriteBaseline(config.Spec.SourceCluster, config.Spec.TargetCluster),
		)
		if err != nil {
			return false, fmt.Errorf("compile drill dynamic image rewrite rules: %w", err)
		}
	}

	// 使用共享构建器
	restoreSpec := restore.BuildAppRestoreSpec(restore.BuilderConfig{
		RestoreType:                restore.RestoreTypeResource,
		BackupSource:               resourceSync.Status.LastBackupName,
		BackupName:                 resourceSync.Status.LastBackupName,
		TargetCluster:              targetCluster,
		SourceCluster:              sourceCluster,
		StorageRepository:          config.Spec.StorageRepository,
		IncludedNamespaces:         instance.Spec.Namespaces,
		NamespaceMapping:           namespaceMapping,
		IsForDrill:                 true,
		ExtraResourceModifierRules: imageRewriteRules,
	})

	var targetClient client.Client
	if restore.RequiresTargetClassValidationWithOverride(instance, drillRestorePolicy) {
		c, err := r.getClusterClient(ctx, targetCluster)
		if err != nil {
			return false, fmt.Errorf("build target cluster client for restore policy: %w", err)
		}
		targetClient = c
	}
	policySummary, err := restore.ApplyInstanceRestorePolicy(
		ctx,
		&restoreSpec,
		instance,
		targetClient,
		restore.WithBaselineClusters(config.Spec.SourceCluster, config.Spec.TargetCluster),
		restore.WithApplyTarget(disasterv1.RestoreModifierApplyDrill),
		restore.WithRestorePolicyOverride(drillRestorePolicy),
		restore.WithRuntimeModifierRules(runtimeImageRewriteRules),
	)
	if err != nil {
		return false, fmt.Errorf("apply drill restore policy: %w", err)
	}
	r.reportOperationProgress(ctx, operation, instance, fmt.Sprintf("Drill 资源恢复策略编译完成: %s", restore.ModifierAuditMessage(policySummary)))

	appRestore := &disasterv1.AppRestore{
		ObjectMeta: metav1.ObjectMeta{
			Name:      restoreName,
			Namespace: instance.Namespace,
			Labels: map[string]string{
				"testudo.softcdata.com/drill":        truncateLabelValue(operation.Name),
				"testudo.softcdata.com/instance":     truncateLabelValue(instance.Name),
				"testudo.softcdata.com/restore-type": "resource",
			},
		},
		Spec: restoreSpec,
	}
	restore.ApplyPolicySummaryAnnotations(&appRestore.ObjectMeta, policySummary)

	if err := r.Create(ctx, appRestore); err != nil {
		if errors.IsAlreadyExists(err) {
			// 如果已存在 (且 Label 查找没找到? 极端情况), 更新 Status
			operation.Status.ResourceRestoreName = restoreName
			return false, r.Status().Update(ctx, operation)
		}
		return false, err
	}

	// 保存 ResourceRestoreName 到 Status
	operation.Status.ResourceRestoreName = restoreName
	if err := r.Status().Update(ctx, operation); err != nil {
		return false, err
	}

	return false, nil
}

func resolveDrillDataRestoreHooks(instance *disasterv1.DisasterInstance, drillConfig *disasterv1.DrillConfig) *velerov1.RestoreHooks {
	if drillConfig != nil && drillConfig.VeleroHooks != nil {
		return drillConfig.VeleroHooks.DataRestore
	}
	if instance == nil || instance.Spec.VeleroHooks == nil {
		return nil
	}
	return instance.Spec.VeleroHooks.DataRestore
}

// executeDrillRestoreData 执行数据恢复步骤 (从 DataSync 备份恢复 PVC 数据)
func (r *DisasterOperationReconciler) executeDrillRestoreData(ctx context.Context, log logr.Logger, instance *disasterv1.DisasterInstance, operation *disasterv1.DisasterOperation, targetCluster string) (bool, error) {
	log.Info("执行 Drill 数据恢复步骤", "targetCluster", targetCluster)

	drillConfig := operation.Spec.DrillConfig
	drillRestorePolicy := func() *disasterv1.RestorePolicy {
		if drillConfig == nil {
			return nil
		}
		return drillConfig.RestorePolicy
	}()
	drillDataRestoreHooks := resolveDrillDataRestoreHooks(instance, drillConfig)

	// 0. Fetch DisasterConfig
	config := &disasterv1.DisasterConfig{}
	if err := r.Get(ctx, types.NamespacedName{Name: instance.Spec.Config}, config); err != nil {
		return false, fmt.Errorf("failed to get config %s: %w", instance.Spec.Config, err)
	}

	// 1. 尝试从 Status 获取
	if operation.Status.DataRestoreName != "" {
		finished, err := r.checkAppRestoreStatus(ctx, instance.Namespace, operation.Status.DataRestoreName)
		if err != nil {
			return false, err
		}
		if finished {
			// 数据恢复完成后，必须清理 Trafficless Pods，否则 ScaleUp 时会发生命名冲突
			namespaceMapping := operation.Spec.DrillConfig.NamespaceMapping
			return r.cleanupTrafficlessPods(ctx, log, instance, targetCluster, namespaceMapping)
		}
		return false, nil
	}

	// 2. 尝试通过 Label 查找已存在的 AppRestore
	existingRestore, err := r.findAppRestoreByLabel(ctx, instance.Namespace, operation.Name, "data")
	if err != nil {
		return false, err
	}
	if existingRestore != nil {
		log.Info("发现已存在的数据恢复 AppRestore，复用之", "name", existingRestore.Name)
		operation.Status.DataRestoreName = existingRestore.Name
		if err := r.Status().Update(ctx, operation); err != nil {
			return false, err
		}
		return false, nil
	}

	// 3. 首次执行：查找最近的 DataSync 备份
	dataSync := &disasterv1.DataSync{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: instance.Namespace, Name: instance.Status.DataSyncName}, dataSync); err != nil {
		return false, fmt.Errorf("DataSync 未找到: %w", err)
	}

	if dataSync.Status.LastBackupName == "" {
		return false, fmt.Errorf("DataSync 没有可用的备份")
	}

	// 4. 创建 AppRestore (使用确定性命名，且尽量短)
	// 格式: ddr-{opName[:10]}-{hash[:6]}
	opNameLen := len(operation.Name)
	if opNameLen > 10 {
		opNameLen = 10
	}
	opHash := fmt.Sprintf("%x", md5.Sum([]byte(operation.Name)))[:6]
	restoreName := fmt.Sprintf("ddr-%s-%s", operation.Name[:opNameLen], opHash)

	// 构造命名空间映射
	var namespaceMapping map[string]string
	if drillConfig != nil && len(drillConfig.NamespaceMapping) > 0 {
		namespaceMapping = drillConfig.NamespaceMapping
	}
	preparedDrillDataRestoreHooks, hookMarkerRules := restore.PrepareTrafficlessDataRestoreHooks(
		drillDataRestoreHooks,
		instance.Spec.Namespaces,
		namespaceMapping,
	)
	hasRestorePolicy := instance.Spec.RestorePolicy != nil || drillRestorePolicy != nil

	// 使用共享构建器
	restoreSpec := restore.BuildAppRestoreSpec(restore.BuilderConfig{
		RestoreType:        restore.RestoreTypeData,
		BackupSource:       dataSync.Status.LastBackupName,
		BackupName:         dataSync.Status.LastBackupName,
		TargetCluster:      targetCluster,
		SourceCluster:      instance.Status.PrimaryCluster,
		StorageRepository:  config.Spec.StorageRepository,
		IncludedNamespaces: instance.Spec.Namespaces,
		NamespaceMapping:   namespaceMapping,
		IsForDrill:         true,
		DataRestoreHooks:   preparedDrillDataRestoreHooks,
	})
	if len(hookMarkerRules) > 0 && !hasRestorePolicy {
		restoreSpec.ResourceModifierRules = append(restoreSpec.ResourceModifierRules, hookMarkerRules...)
	}
	cleanupRule, needsCleanup := buildDrillPVCVolumeNameCleanupRule(instance, namespaceMapping)
	if needsCleanup && !hasRestorePolicy {
		// No restorePolicy path: ApplyInstanceRestorePolicy returns early, so append directly here.
		restoreSpec.ResourceModifierRules = append(restoreSpec.ResourceModifierRules, cleanupRule)
	}

	var targetClient client.Client
	if restore.RequiresTargetClassValidationWithOverride(instance, drillRestorePolicy) {
		c, err := r.getClusterClient(ctx, targetCluster)
		if err != nil {
			return false, fmt.Errorf("build target cluster client for restore policy: %w", err)
		}
		targetClient = c
	}
	applyOpts := []restore.ApplyInstanceRestorePolicyOption{
		restore.WithBaselineClusters(config.Spec.SourceCluster, config.Spec.TargetCluster),
		restore.WithApplyTarget(disasterv1.RestoreModifierApplyDrill),
		restore.WithRestorePolicyOverride(drillRestorePolicy),
	}
	systemProtectRules := make([]disasterv1.ResourceModifierRule, 0, 1+len(hookMarkerRules))
	if needsCleanup && hasRestorePolicy {
		// restorePolicy path: inject as system-protect to prevent user rule override.
		systemProtectRules = append(systemProtectRules, cleanupRule)
	}
	if len(hookMarkerRules) > 0 && hasRestorePolicy {
		systemProtectRules = append(systemProtectRules, hookMarkerRules...)
	}
	if len(systemProtectRules) > 0 {
		applyOpts = append(applyOpts, restore.WithSystemProtectRules(systemProtectRules))
	}
	policySummary, err := restore.ApplyInstanceRestorePolicy(
		ctx,
		&restoreSpec,
		instance,
		targetClient,
		applyOpts...,
	)
	if err != nil {
		return false, fmt.Errorf("apply drill restore policy: %w", err)
	}
	r.reportOperationProgress(ctx, operation, instance, fmt.Sprintf("Drill 数据恢复策略编译完成: %s", restore.ModifierAuditMessage(policySummary)))

	appRestore := &disasterv1.AppRestore{
		ObjectMeta: metav1.ObjectMeta{
			Name:      restoreName,
			Namespace: instance.Namespace,
			Labels: map[string]string{
				"testudo.softcdata.com/drill":        truncateLabelValue(operation.Name),
				"testudo.softcdata.com/instance":     truncateLabelValue(instance.Name),
				"testudo.softcdata.com/restore-type": "data",
			},
		},
		Spec: restoreSpec,
	}
	restore.ApplyPolicySummaryAnnotations(&appRestore.ObjectMeta, policySummary)

	if err := r.Create(ctx, appRestore); err != nil {
		if errors.IsAlreadyExists(err) {
			operation.Status.DataRestoreName = restoreName
			return false, r.Status().Update(ctx, operation)
		}
		return false, err
	}

	// 保存 DataRestoreName 到 Status
	operation.Status.DataRestoreName = restoreName
	if err := r.Status().Update(ctx, operation); err != nil {
		return false, err
	}

	return false, nil
}

func buildDrillPVCVolumeNameCleanupRule(
	instance *disasterv1.DisasterInstance,
	namespaceMapping map[string]string,
) (disasterv1.ResourceModifierRule, bool) {
	if !shouldDrillCleanupPVCVolumeName(instance, namespaceMapping) {
		return disasterv1.ResourceModifierRule{}, false
	}
	return restore.MakePVCVolumeNameCleanupRule(drillPVCVolumeCleanupNamespaces(instance.Spec.Namespaces, namespaceMapping)), true
}

// shouldDrillCleanupPVCVolumeName returns true when drill mapping redirects at least one namespace to a different target namespace.
func shouldDrillCleanupPVCVolumeName(
	instance *disasterv1.DisasterInstance,
	namespaceMapping map[string]string,
) bool {
	if instance == nil || len(namespaceMapping) == 0 {
		return false
	}
	for sourceNS, targetNS := range namespaceMapping {
		sourceNS = strings.TrimSpace(sourceNS)
		targetNS = strings.TrimSpace(targetNS)
		if sourceNS == "" || targetNS == "" {
			continue
		}
		if sourceNS != targetNS {
			return true
		}
	}
	return false
}

func drillPVCVolumeCleanupNamespaces(sourceNamespaces []string, namespaceMapping map[string]string) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, len(sourceNamespaces)+len(namespaceMapping))
	appendNS := func(ns string) {
		ns = strings.TrimSpace(ns)
		if ns == "" {
			return
		}
		if _, ok := seen[ns]; ok {
			return
		}
		seen[ns] = struct{}{}
		out = append(out, ns)
	}

	for _, ns := range sourceNamespaces {
		appendNS(ns)
	}
	for _, mapped := range namespaceMapping {
		appendNS(mapped)
	}

	if len(out) == 0 {
		return nil
	}
	return out
}

// checkAppRestoreStatus 检查 AppRestore 状态
func (r *DisasterOperationReconciler) checkAppRestoreStatus(ctx context.Context, namespace, name string) (bool, error) {
	existingRestore := &disasterv1.AppRestore{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, existingRestore); err != nil {
		return false, err
	}

	if existingRestore.Status.Status == disasterv1.PhaseSucceeded {
		return true, nil
	}
	if disasterv1.IsFailedAppRestorePhase(existingRestore.Status.Status) {
		return false, fmt.Errorf("AppRestore %s 失败: status=%s message=%s", name, existingRestore.Status.Status, existingRestore.Status.Message)
	}

	// 仍在进行中
	return false, nil
}

// findAppRestoreByLabel 通过 Label 查找 AppRestore
func (r *DisasterOperationReconciler) findAppRestoreByLabel(ctx context.Context, namespace, operationName, restoreType string) (*disasterv1.AppRestore, error) {
	opts := []client.ListOption{
		client.InNamespace(namespace),
		client.MatchingLabels{
			"testudo.softcdata.com/drill":        truncateLabelValue(operationName),
			"testudo.softcdata.com/restore-type": restoreType,
		},
	}
	list := &disasterv1.AppRestoreList{}
	if err := r.List(ctx, list, opts...); err != nil {
		return nil, err
	}
	if len(list.Items) > 0 {
		return &list.Items[0], nil
	}
	return nil, nil
}

// executeDrillScaleUp 执行扩容步骤
func (r *DisasterOperationReconciler) executeDrillScaleUp(ctx context.Context, log logr.Logger, instance *disasterv1.DisasterInstance, operation *disasterv1.DisasterOperation, targetCluster string, restoreMode disasterv1.RestoreMode) (bool, error) {
	log.Info("执行 Drill 扩容步骤", "targetCluster", targetCluster)

	// 始终从 ConfigMap/备份获取副本数
	replicasMap := r.fetchReplicasMap(ctx, instance)
	waitUntilReady := resolveWaitUntilReady(instance, operation)

	// 执行扩容
	return r.doScaleUp(ctx, instance, targetCluster, replicasMap, waitUntilReady, operation.Spec.DrillConfig.NamespaceMapping)
}

// handleCancel 处理取消/中止 Failover 操作
func (r *DisasterOperationReconciler) handleCancel(ctx context.Context, log logr.Logger, operation *disasterv1.DisasterOperation) (ctrl.Result, error) {
	instance := &disasterv1.DisasterInstance{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: operation.Namespace, Name: operation.Spec.InstanceName}, instance); err != nil {
		operation.Status.State = disasterv1.OperationStateFailed
		operation.Status.Message = "DisasterInstance 未找到"
		return ctrl.Result{}, r.Status().Update(ctx, operation)
	}

	if instance.Status.FsmState == disasterv1.FsmStateProtected {
		if _, err := r.executeResumeSchedules(ctx, instance); err != nil {
			operation.Status.State = disasterv1.OperationStateFailed
			operation.Status.Message = fmt.Sprintf("实例已处于 Protected，但恢复同步调度失败: %v", err)
			return ctrl.Result{}, r.Status().Update(ctx, operation)
		}
		now := metav1.Now()
		completeCancelStepsForProtectedIdempotency(operation, &now)
		operation.Status.State = disasterv1.OperationStateCompleted
		operation.Status.CompletionTime = &now
		operation.Status.CurrentStep = ""
		operation.Status.Reason = ""
		operation.Status.Message = "实例已处于 Protected，Cancel 已按幂等操作完成"
		operation.Status.RoleStatus = &disasterv1.RoleStatus{
			PrimaryCluster:   instance.Status.PrimaryCluster,
			SecondaryCluster: instance.Status.SecondaryCluster,
		}
		return ctrl.Result{}, r.Status().Update(ctx, operation)
	}

	// 允许在 FailingOver 或 failover 失败后的 Failed 状态执行 Cancel，避免实例被永久阻断。
	if !canRunCancel(instance.Status.FsmState) {
		operation.Status.State = disasterv1.OperationStateFailed
		operation.Status.Message = fmt.Sprintf("必须在 FailingOver 或 Failed 状态下才能 Cancel (当前: %s)", instance.Status.FsmState)
		return ctrl.Result{}, r.Status().Update(ctx, operation)
	}

	steps := []string{
		string(disasterv1.CancelStepScaleDownTarget), // Scale Down Target (Explicit)
		string(disasterv1.CancelStepScaleUpSource),   // Scale Up Source (Explicit)
		string(disasterv1.CancelStepResumeSchedules), // Resume
	}

	if len(operation.Status.Steps) == 0 {
		for _, stepName := range steps {
			operation.Status.Steps = append(operation.Status.Steps, disasterv1.StepStatus{
				Name:  stepName,
				State: "Pending",
			})
		}
		operation.Status.CurrentStep = steps[0]
		if err := r.Status().Update(ctx, operation); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	for i := range operation.Status.Steps {
		step := &operation.Status.Steps[i]
		if step.State == "Pending" {
			step.State = "Running"
			step.StartTime = &metav1.Time{Time: time.Now()}
			operation.Status.CurrentStep = step.Name
			log.Info("开始 Cancel 步骤", "step", step.Name)
			r.Status().Update(ctx, operation)
			return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
		}
		if step.State == "Running" {
			timeoutMinutes := effectiveOperationTimeoutMinutes(operation, instance)
			if timeoutMinutes > 0 && step.StartTime != nil {
				elapsed := time.Since(step.StartTime.Time)
				timeout := time.Duration(timeoutMinutes) * time.Minute
				if elapsed > timeout {
					step.State = "Failed"
					step.Message = buildStepTimeoutMessage(step, elapsed, timeoutMinutes)
					step.CompletionTime = &metav1.Time{Time: time.Now()}

					operation.Status.State = disasterv1.OperationStateFailed
					operation.Status.CompletionTime = &metav1.Time{Time: time.Now()}
					operation.Status.Message = fmt.Sprintf("步骤 %s 超时: %s", step.Name, step.Message)

					helper.ReportDiagnosticEventf(r.Recorder, operation, corev1.EventTypeWarning, "StepTimeout", "步骤 %s 超时 (已等待 %v)", step.Name, elapsed.Round(time.Second))
					r.settleInstanceStateOnStepFailure(ctx, log, disasterv1.OperationTypeCancel, instance, step.Name, step.Message)
					return ctrl.Result{}, r.Status().Update(ctx, operation)
				}
			}

			completed, err := r.executeCancelStep(ctx, log, step, instance, operation)
			if err != nil {
				r.settleInstanceStateOnStepFailure(ctx, log, disasterv1.OperationTypeCancel, instance, step.Name, err.Error())
				if updateErr := r.failOperationOnStepError(ctx, operation, step, err); updateErr != nil {
					return ctrl.Result{}, updateErr
				}
				helper.ReportDiagnosticEventf(r.Recorder, operation, "Warning", "StepFailed", "步骤 %s 失败: %v", step.Name, err)
				r.reportOperationProgress(ctx, operation, instance, fmt.Sprintf("步骤 %s 失败: %v", step.Name, err))
				return ctrl.Result{}, nil
			}
			if !completed {
				return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
			}
			step.State = "Completed"
			step.CompletionTime = &metav1.Time{Time: time.Now()}
			r.Status().Update(ctx, operation)
			return ctrl.Result{Requeue: true}, nil
		}
	}

	// Finalize: Set State -> Protected
	latestInstance := &disasterv1.DisasterInstance{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: instance.Namespace, Name: instance.Name}, latestInstance); err != nil {
		return ctrl.Result{}, err
	}
	latestInstance.Status.FsmState = disasterv1.FsmStateProtected
	if err := r.Status().Update(ctx, latestInstance); err != nil {
		return ctrl.Result{}, err
	}

	// Update local instance for RoleStatus
	instance.Status = latestInstance.Status

	operation.Status.State = disasterv1.OperationStateCompleted
	operation.Status.CompletionTime = &metav1.Time{Time: time.Now()}
	operation.Status.RoleStatus = &disasterv1.RoleStatus{
		PrimaryCluster:   instance.Status.PrimaryCluster,
		SecondaryCluster: instance.Status.SecondaryCluster,
	}

	r.Status().Update(ctx, operation)
	return ctrl.Result{}, nil
}

func canRunCancel(state string) bool {
	return state == disasterv1.FsmStateFailingOver || state == disasterv1.FsmStateFailed
}

func completeCancelStepsForProtectedIdempotency(operation *disasterv1.DisasterOperation, completionTime *metav1.Time) {
	if operation == nil {
		return
	}
	expectedSteps := []string{
		string(disasterv1.CancelStepScaleDownTarget),
		string(disasterv1.CancelStepScaleUpSource),
		string(disasterv1.CancelStepResumeSchedules),
	}
	stepIndex := make(map[string]int, len(operation.Status.Steps))
	for i := range operation.Status.Steps {
		stepIndex[operation.Status.Steps[i].Name] = i
	}
	for _, name := range expectedSteps {
		idx, ok := stepIndex[name]
		if !ok {
			operation.Status.Steps = append(operation.Status.Steps, disasterv1.StepStatus{
				Name:           name,
				State:          "Completed",
				CompletionTime: completionTime,
			})
			continue
		}
		step := &operation.Status.Steps[idx]
		if step.State == "" || step.State == "Pending" || step.State == "Running" {
			step.State = "Completed"
		}
		if step.CompletionTime == nil {
			step.CompletionTime = completionTime
		}
	}
}

func buildStepTimeoutMessage(step *disasterv1.StepStatus, elapsed time.Duration, timeoutMinutes int32) string {
	base := fmt.Sprintf("步骤超时 (已等待 %v，超时设置 %d 分钟)", elapsed.Round(time.Second), timeoutMinutes)
	if step == nil {
		return base
	}

	detail := strings.TrimSpace(step.Message)
	if detail == "" || detail == "已完成" || strings.HasPrefix(detail, "步骤超时") {
		return base
	}
	return fmt.Sprintf("%s；阻塞详情: %s", base, detail)
}

func effectiveOperationTimeoutMinutes(operation *disasterv1.DisasterOperation, instance *disasterv1.DisasterInstance) int32 {
	if operation != nil && operation.Spec.TimeoutMinutes > 0 {
		return operation.Spec.TimeoutMinutes
	}
	if instance != nil && instance.Spec.OperationTimeoutMinutes > 0 {
		return instance.Spec.OperationTimeoutMinutes
	}
	return defaultOperationTimeoutMinutes
}

func (r *DisasterOperationReconciler) executeCancelStep(ctx context.Context, log logr.Logger, step *disasterv1.StepStatus, instance *disasterv1.DisasterInstance, operation *disasterv1.DisasterOperation) (bool, error) {
	config := &disasterv1.DisasterConfig{}
	if err := r.Get(ctx, types.NamespacedName{Name: instance.Spec.Config}, config); err != nil {
		return false, fmt.Errorf("failed to get config: %w", err)
	}

	stepName := disasterv1.CancelStep(step.Name)
	switch stepName {
	case disasterv1.CancelStepScaleDownTarget:
		return r.doScaleDown(ctx, instance, config.Spec.TargetCluster)
	case disasterv1.CancelStepScaleUpSource:
		replicasMap := r.fetchReplicasMap(ctx, instance)
		return r.doScaleUp(ctx, instance, config.Spec.SourceCluster, replicasMap, true, nil) // Cancel 操作默认等待就绪
	case disasterv1.CancelStepResumeSchedules:
		return r.executeResumeSchedules(ctx, instance)
	default:
		return true, nil
	}
}

// handleUndo 处理撤销操作 (回滚: 放弃 B，缩容 B，恢复 A，建立 A->B 同步)
func (r *DisasterOperationReconciler) handleUndo(ctx context.Context, log logr.Logger, operation *disasterv1.DisasterOperation) (ctrl.Result, error) {
	instance := &disasterv1.DisasterInstance{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: operation.Namespace, Name: operation.Spec.InstanceName}, instance); err != nil {
		operation.Status.State = disasterv1.OperationStateFailed
		operation.Status.Message = "DisasterInstance 未找到"
		return ctrl.Result{}, r.Status().Update(ctx, operation)
	}

	// Ensure Active or FailingOver
	if instance.Status.FsmState != disasterv1.FsmStateActive && instance.Status.FsmState != disasterv1.FsmStateFailingOver {
		operation.Status.State = disasterv1.OperationStateFailed
		operation.Status.Message = fmt.Sprintf("必须在 Active 或 FailingOver 状态下才能 Undo (当前: %s)", instance.Status.FsmState)
		return ctrl.Result{}, r.Status().Update(ctx, operation)
	}

	steps := []string{
		string(disasterv1.UndoStepScaleDownTarget), // Scale Down B (Primary)
		string(disasterv1.UndoStepScaleUpSource),   // Scale Up A (Secondary)
		string(disasterv1.UndoStepSwitchRoles),     // Swap Roles (Primary=A)
		string(disasterv1.UndoStepResumeSchedules), // Resume
	}

	if len(operation.Status.Steps) == 0 {
		for _, stepName := range steps {
			operation.Status.Steps = append(operation.Status.Steps, disasterv1.StepStatus{
				Name:  stepName,
				State: "Pending",
			})
		}
		operation.Status.CurrentStep = steps[0]
		if err := r.Status().Update(ctx, operation); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	for i := range operation.Status.Steps {
		step := &operation.Status.Steps[i]
		if step.State == "Pending" {
			step.State = "Running"
			step.StartTime = &metav1.Time{Time: time.Now()}
			operation.Status.CurrentStep = step.Name
			log.Info("开始 Undo 步骤", "step", step.Name)
			helper.ReportDiagnosticEventf(r.Recorder, operation, "Normal", "StepStarted", "步骤 %s 已开始", step.Name)
			r.reportOperationProgress(ctx, operation, instance, fmt.Sprintf("步骤 %s 已开始", step.Name))
			r.Status().Update(ctx, operation)
			return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
		}
		if step.State == "Running" {
			if operation.Spec.TimeoutMinutes > 0 && step.StartTime != nil {
				elapsed := time.Since(step.StartTime.Time)
				timeout := time.Duration(operation.Spec.TimeoutMinutes) * time.Minute
				if elapsed > timeout {
					step.State = "Failed"
					step.Message = buildStepTimeoutMessage(step, elapsed, operation.Spec.TimeoutMinutes)
					step.CompletionTime = &metav1.Time{Time: time.Now()}

					operation.Status.State = disasterv1.OperationStateFailed
					operation.Status.CompletionTime = &metav1.Time{Time: time.Now()}
					operation.Status.Message = fmt.Sprintf("步骤 %s 超时: %s", step.Name, step.Message)

					helper.ReportDiagnosticEventf(r.Recorder, operation, corev1.EventTypeWarning, "StepTimeout", "步骤 %s 超时 (已等待 %v)", step.Name, elapsed.Round(time.Second))
					r.settleInstanceStateOnStepFailure(ctx, log, disasterv1.OperationTypeUndo, instance, step.Name, step.Message)
					return ctrl.Result{}, r.Status().Update(ctx, operation)
				}
			}

			completed, err := r.executeUndoStep(ctx, log, step, instance, operation)
			if err != nil {
				r.settleInstanceStateOnStepFailure(ctx, log, disasterv1.OperationTypeUndo, instance, step.Name, err.Error())
				if updateErr := r.failOperationOnStepError(ctx, operation, step, err); updateErr != nil {
					return ctrl.Result{}, updateErr
				}
				helper.ReportDiagnosticEventf(r.Recorder, operation, "Warning", "StepFailed", "步骤 %s 失败: %v", step.Name, err)
				r.reportOperationProgress(ctx, operation, instance, fmt.Sprintf("步骤 %s 失败: %v", step.Name, err))
				return ctrl.Result{}, nil
			}
			if !completed {
				return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
			}
			step.State = "Completed"
			step.CompletionTime = &metav1.Time{Time: time.Now()}
			step.Message = "已完成"
			log.Info("撤销切换步骤已完成", "step", step.Name)
			helper.ReportDiagnosticEventf(r.Recorder, operation, "Normal", "StepCompleted", "步骤 %s 已完成", step.Name)
			r.reportOperationProgress(ctx, operation, instance, fmt.Sprintf("步骤 %s 已完成", step.Name))
			r.Status().Update(ctx, operation)
			return ctrl.Result{Requeue: true}, nil
		}
	}

	// Finalize
	// 重新获取最新的 DisasterInstance
	latestInstance := &disasterv1.DisasterInstance{}
	if getErr := r.Get(ctx, client.ObjectKey{Namespace: instance.Namespace, Name: instance.Name}, latestInstance); getErr != nil {
		return ctrl.Result{}, getErr
	}

	// 幂等状态确认：如果在之前的 Undo 调谐中已经应用了 Protected（因为 Operation Update 冲突导致外层重试），
	// 则直接同步内存使用即可。如果不等于，则正常更新。
	if latestInstance.Status.FsmState != disasterv1.FsmStateProtected {
		latestInstance.Status.FsmState = disasterv1.FsmStateProtected
		if updateErr := r.Status().Update(ctx, latestInstance); updateErr != nil {
			return ctrl.Result{}, updateErr // 控制器标准乐观锁外层报错重入
		}
	}
	// 更新本地指针供 RoleStatus 使用
	instance.Status = latestInstance.Status

	operation.Status.State = disasterv1.OperationStateCompleted
	operation.Status.CompletionTime = &metav1.Time{Time: time.Now()}
	operation.Status.CurrentStep = ""
	operation.Status.RoleStatus = &disasterv1.RoleStatus{
		PrimaryCluster:   instance.Status.PrimaryCluster,
		SecondaryCluster: instance.Status.SecondaryCluster,
	}
	operation.Status.Message = "撤销切换成功完成 (原集群已恢复)"

	if err := r.Status().Update(ctx, operation); err != nil {
		return ctrl.Result{}, err
	}

	helper.ReportDiagnosticEvent(r.Recorder, operation, "Normal", "Completed", "撤销切换已完成")
	helper.ReportDiagnosticEvent(r.Recorder, instance, "Normal", "UndoCompleted", "撤销切换已完成，已恢复到原主集群")
	return ctrl.Result{}, nil
}

func (r *DisasterOperationReconciler) executeUndoStep(ctx context.Context, log logr.Logger, step *disasterv1.StepStatus, instance *disasterv1.DisasterInstance, operation *disasterv1.DisasterOperation) (bool, error) {
	stepName := disasterv1.UndoStep(step.Name)
	switch stepName {
	case disasterv1.UndoStepScaleDownTarget:
		return r.executeScaleDownSource(ctx, instance, operation) // Scales Primary (B)
	case disasterv1.UndoStepScaleUpSource:
		return r.executeScaleUpTarget(ctx, instance) // Scales Secondary (A)
	case disasterv1.UndoStepSwitchRoles:
		// 获取配置以确定原始角色
		config := &disasterv1.DisasterConfig{}
		if err := r.Get(ctx, types.NamespacedName{Name: instance.Spec.Config}, config); err != nil {
			return false, fmt.Errorf("failed to get config: %w", err)
		}

		latestInstance := &disasterv1.DisasterInstance{}
		if getErr := r.Get(ctx, client.ObjectKey{Namespace: instance.Namespace, Name: instance.Name}, latestInstance); getErr != nil {
			return false, getErr
		}

		// 幂等设置角色: 恢复为 (Src为主, Tgt为备)，由于是固定值赋予，天然具备幂等性
		latestInstance.Status.PrimaryCluster = config.Spec.SourceCluster
		latestInstance.Status.SecondaryCluster = config.Spec.TargetCluster

		if updateErr := r.Status().Update(ctx, latestInstance); updateErr != nil {
			return false, updateErr // 返回错误，借用 controller-runtime 实现乐观锁的外层自然重试
		}
		// 回置最新的内容给 instance (内存对象，保证上下文后续可以使用)
		instance.Status = latestInstance.Status
		return true, nil
	case disasterv1.UndoStepResumeSchedules:
		return r.executeResumeSchedules(ctx, instance)
	default:
		return true, nil
	}
}

// executeResumeSchedules 恢复所有关联的同步任务
func (r *DisasterOperationReconciler) executeResumeSchedules(ctx context.Context, instance *disasterv1.DisasterInstance) (bool, error) {
	dataSyncNames := make([]string, 0, 1)
	dataSyncSeen := make(map[string]struct{})
	dataSyncNames = appendUniqueScheduleName(dataSyncNames, dataSyncSeen, instance.Status.DataSyncName)

	dataSyncList := &disasterv1.DataSyncList{}
	if err := r.List(ctx, dataSyncList, client.InNamespace(instance.Namespace)); err != nil {
		return false, err
	}
	for i := range dataSyncList.Items {
		ds := &dataSyncList.Items[i]
		if ds.Spec.Instance == instance.Name {
			dataSyncNames = appendUniqueScheduleName(dataSyncNames, dataSyncSeen, ds.Name)
		}
	}
	for _, name := range dataSyncNames {
		if err := r.resumeDataSyncSchedule(ctx, instance.Namespace, name); err != nil {
			return false, err
		}
	}

	resourceSyncNames := make([]string, 0, 1)
	resourceSyncSeen := make(map[string]struct{})
	resourceSyncNames = appendUniqueScheduleName(resourceSyncNames, resourceSyncSeen, instance.Status.ResourceSyncName)

	resourceSyncList := &disasterv1.ResourceSyncList{}
	if err := r.List(ctx, resourceSyncList, client.InNamespace(instance.Namespace)); err != nil {
		return false, err
	}
	for i := range resourceSyncList.Items {
		rs := &resourceSyncList.Items[i]
		if rs.Spec.Instance == instance.Name {
			resourceSyncNames = appendUniqueScheduleName(resourceSyncNames, resourceSyncSeen, rs.Name)
		}
	}
	for _, name := range resourceSyncNames {
		if err := r.resumeResourceSyncSchedule(ctx, instance.Namespace, name); err != nil {
			return false, err
		}
	}
	return true, nil
}

func appendUniqueScheduleName(names []string, seen map[string]struct{}, name string) []string {
	name = strings.TrimSpace(name)
	if name == "" {
		return names
	}
	if _, ok := seen[name]; ok {
		return names
	}
	seen[name] = struct{}{}
	return append(names, name)
}

func (r *DisasterOperationReconciler) resumeDataSyncSchedule(ctx context.Context, namespace, name string) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		dataSync := &disasterv1.DataSync{}
		if err := r.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, dataSync); err != nil {
			return client.IgnoreNotFound(err)
		}
		if !dataSync.Spec.Paused {
			return nil
		}
		dataSync.Spec.Paused = false
		return r.Update(ctx, dataSync)
	})
}

func (r *DisasterOperationReconciler) resumeResourceSyncSchedule(ctx context.Context, namespace, name string) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		resourceSync := &disasterv1.ResourceSync{}
		if err := r.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, resourceSync); err != nil {
			return client.IgnoreNotFound(err)
		}
		if !resourceSync.Spec.Paused {
			return nil
		}
		resourceSync.Spec.Paused = false
		return r.Update(ctx, resourceSync)
	})
}

// handleGroupOperation 处理 Group 级别的操作编排
func (r *DisasterOperationReconciler) handleGroupOperation(ctx context.Context, log logr.Logger, operation *disasterv1.DisasterOperation) (ctrl.Result, error) {
	// 获取 DisasterGroup
	group := &disasterv1.DisasterGroup{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: operation.Namespace, Name: operation.Spec.GroupName}, group); err != nil {
		if errors.IsNotFound(err) {
			operation.Status.State = disasterv1.OperationStateFailed
			operation.Status.Message = fmt.Sprintf("DisasterGroup %s 未找到", operation.Spec.GroupName)
			return ctrl.Result{}, r.Status().Update(ctx, operation)
		}
		return ctrl.Result{}, err
	}

	// 解析当前 Level
	currentLevel := 0
	if operation.Status.CurrentStep != "" {
		if strings.HasPrefix(operation.Status.CurrentStep, "Level-") {
			var err error
			currentLevel, err = strconv.Atoi(strings.TrimPrefix(operation.Status.CurrentStep, "Level-"))
			if err != nil {
				log.Error(err, "解析 CurrentLevel 失败，重置为 0")
				currentLevel = 0
			}
		}
	} else {
		// 初始化
		operation.Status.CurrentStep = "Level-0"
		if err := r.Status().Update(ctx, operation); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// 检查是否所有 Level 已完成
	if currentLevel >= len(group.Spec.Levels) {
		operation.Status.State = disasterv1.OperationStateCompleted
		operation.Status.CompletionTime = &metav1.Time{Time: time.Now()}
		operation.Status.Message = fmt.Sprintf("即所有 %d 层级操作已完成", len(group.Spec.Levels))
		helper.ReportDiagnosticEvent(r.Recorder, operation, "Normal", "Completed", "Group 操作已完成")
		r.reportOperationProgress(ctx, operation, nil, fmt.Sprintf("组操作已完成，总层级=%d", len(group.Spec.Levels)))
		return ctrl.Result{}, r.Status().Update(ctx, operation)
	}

	// 获取当前 Level 的实例
	instances := group.Spec.Levels[currentLevel]
	log.Info("正在处理 Group Level", "level", currentLevel, "instances", len(instances))

	allCompleted := true
	anyFailed := false
	failedMessage := ""
	// 收集子操作进度，用于更新 status.Message 触发 Watch 推送
	childProgress := make([]string, 0, len(instances))

	for _, instanceName := range instances {
		// Task 4: 检查实例是否存在，若不存在则提前预警
		inst := &disasterv1.DisasterInstance{}
		if err := r.Get(ctx, client.ObjectKey{Namespace: operation.Namespace, Name: instanceName}, inst); err != nil {
			if errors.IsNotFound(err) {
				log.Info("正在处理 Group Level: 实例不存在", "instance", instanceName)
				helper.ReportDiagnosticEventf(r.Recorder, operation, "Warning", "InstanceNotFound", "容灾组内引用的实例 %s 不存在，请检查配置", instanceName)

				if group.Spec.Policy.FailPolicy == "Continue" {
					// Continue 策略：记录不存在但不中断，视为该实例已跳过
					childProgress = append(childProgress, fmt.Sprintf("%s=NotFound", instanceName))
					continue
				}
				anyFailed = true
				failedMessage = fmt.Sprintf("DisasterInstance %s 未找到", instanceName)
				break
			}
			return ctrl.Result{}, err
		}

		// 检查/创建子 Operation
		childOpName := fmt.Sprintf("%s-%s", operation.Name, instanceName)
		// 长度限制截断? 暂时忽略

		childOp := &disasterv1.DisasterOperation{}
		err := r.Get(ctx, client.ObjectKey{Namespace: operation.Namespace, Name: childOpName}, childOp)

		if errors.IsNotFound(err) {
			// 创建子 Operation
			newOp := &disasterv1.DisasterOperation{
				ObjectMeta: metav1.ObjectMeta{
					Name:      childOpName,
					Namespace: operation.Namespace,
					Labels: map[string]string{
						"testudo.softcdata.com/owner-operation": truncateLabelValue(operation.Name),
						"testudo.softcdata.com/instance":        truncateLabelValue(instanceName),
						"testudo.softcdata.com/level":           fmt.Sprintf("%d", currentLevel),
					},
				},
				Spec: disasterv1.DisasterOperationSpec{
					InstanceName:        instanceName,
					OperationType:       operation.Spec.OperationType,
					Force:               operation.Spec.Force,
					SkipFinalSync:       operation.Spec.SkipFinalSync,
					SkipScaleDownSource: operation.Spec.SkipScaleDownSource,
					SkipPodReadyCheck:   cloneBoolPtr(operation.Spec.SkipPodReadyCheck),
					WaitUntilReady:      operation.Spec.WaitUntilReady,
					RetryPolicy:         operation.Spec.RetryPolicy,
					DrillConfig:         operation.Spec.DrillConfig, // 透传演练配置
				},
			}
			// Propagate trace ID
			if tid, ok := operation.Annotations[metadata.AnnotationTraceID]; ok {
				if newOp.Annotations == nil {
					newOp.Annotations = make(map[string]string)
				}
				newOp.Annotations[metadata.AnnotationTraceID] = tid
			}
			// 兼容模式下透传跳过缩零 annotation，避免 CRD 未升级时子操作丢失语义
			if rawSkip, ok := operation.Annotations[annotationSkipScaleDownSource]; ok && strings.TrimSpace(rawSkip) != "" {
				if newOp.Annotations == nil {
					newOp.Annotations = make(map[string]string)
				}
				newOp.Annotations[annotationSkipScaleDownSource] = rawSkip
			}

			// 设置 OwnerRef
			if err := controllerutil.SetControllerReference(operation, newOp, r.Scheme); err != nil {
				log.Error(err, "设置 OwnerRef 失败", "child", childOpName)
				return ctrl.Result{}, err
			}

			if err := r.Create(ctx, newOp); err != nil {
				if errors.IsAlreadyExists(err) {
					// 竞态条件：另一个 Reconcile 已经创建了该子 Operation，视为正常
					log.V(1).Info("子 Operation 已存在，跳过创建", "child", childOpName)
				} else {
					log.Error(err, "创建子 Operation 失败", "child", childOpName)
					return ctrl.Result{}, err
				}
			} else {
				log.Info("已创建子 Operation", "child", childOpName)
			}
			allCompleted = false
			continue
		} else if err != nil {
			return ctrl.Result{}, err
		}

		// 检查子 Operation 状态
		if childOp.Status.State == disasterv1.OperationStateFailed {
			if group.Spec.Policy.FailPolicy == "Continue" {
				// Continue 策略：记录错误但不中断
				log.Info("子操作失败但策略为 Continue，忽略错误", "instance", instanceName, "error", childOp.Status.Message)
				// 视为已完成（失败也是一种终态），不设置 allCompleted=false
				childProgress = append(childProgress, fmt.Sprintf("%s=Failed", instanceName))
				continue
			}
			anyFailed = true
			failedMessage = fmt.Sprintf("Instance %s 操作失败: %s", instanceName, childOp.Status.Message)
			break
		}

		if childOp.Status.State != disasterv1.OperationStateCompleted {
			allCompleted = false
			// 记录当前步骤用于进度展示
			if childOp.Status.CurrentStep != "" {
				childProgress = append(childProgress, fmt.Sprintf("%s=%s(%s)", instanceName, childOp.Status.State, childOp.Status.CurrentStep))
			} else {
				childProgress = append(childProgress, fmt.Sprintf("%s=%s", instanceName, childOp.Status.State))
			}
		} else {
			childProgress = append(childProgress, fmt.Sprintf("%s=Completed", instanceName))
		}
	}

	if anyFailed {
		operation.Status.State = disasterv1.OperationStateFailed
		operation.Status.Message = failedMessage
		helper.ReportDiagnosticEvent(r.Recorder, operation, "Warning", "ChildFailed", failedMessage)
		r.reportOperationProgress(ctx, operation, nil, failedMessage)
		return ctrl.Result{}, r.Status().Update(ctx, operation)
	}

	if allCompleted {
		// 当前 Level 完成，进入下一 Level
		nextLevel := currentLevel + 1
		operation.Status.CurrentStep = fmt.Sprintf("Level-%d", nextLevel)
		helper.ReportDiagnosticEventf(r.Recorder, operation, "Normal", "LevelCompleted", "Level %d 已完成，进入下一层级", currentLevel)
		r.reportOperationProgress(ctx, operation, nil, fmt.Sprintf("Level %d 已完成，进入 Level %d", currentLevel, nextLevel))
		if err := r.Status().Update(ctx, operation); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// 等待子操作完成：更新 status.Message 以触发 Kubernetes Watch MODIFIED 事件，
	// 使前端 WebSocket 能感知到操作进展（否则轮询期间无任何事件推送）
	progressMsg := fmt.Sprintf("Level-%d 执行中 [%s]", currentLevel, strings.Join(childProgress, ", "))
	if operation.Status.Message != progressMsg {
		operation.Status.Message = progressMsg
		if err := r.Status().Update(ctx, operation); err != nil {
			log.V(1).Info("更新进度消息失败（非致命）", "error", err)
		}
	}
	return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
}

// ==================== Helper Functions for Explicit Cluster Operations ====================

func (r *DisasterOperationReconciler) fetchReplicasMap(ctx context.Context, instance *disasterv1.DisasterInstance) map[string]int32 {
	replicasMap := make(map[string]int32)
	if instance.Status.ResourceSyncName != "" {
		cmName := fmt.Sprintf("replicas-%s", instance.Status.ResourceSyncName)
		cm := &corev1.ConfigMap{}
		if err := r.Get(ctx, types.NamespacedName{Namespace: instance.Namespace, Name: cmName}, cm); err == nil {
			if data, ok := cm.Data["replicas"]; ok {
				var storedMap map[string]int32
				if err := json.Unmarshal([]byte(data), &storedMap); err == nil {
					replicasMap = storedMap
				}
			}
		}
	}
	return replicasMap
}

func (r *DisasterOperationReconciler) doScaleDown(ctx context.Context, instance *disasterv1.DisasterInstance, clusterName string) (bool, error) {
	remoteClient, err := r.getClusterClient(ctx, clusterName)
	if err != nil {
		return false, err
	}

	r.Log.Info("Executing ScaleDown", "cluster", clusterName, "namespaces", instance.Spec.Namespaces)

	for _, ns := range instance.Spec.Namespaces {
		// Deployments
		deployList := &appsv1.DeploymentList{}
		if err := remoteClient.List(ctx, deployList, client.InNamespace(ns)); err != nil {
			r.Log.Error(err, "Failed to list deployments", "namespace", ns, "cluster", clusterName)
			return false, err
		}
		r.Log.Info("Listed deployments for ScaleDown", "namespace", ns, "count", len(deployList.Items))
		for _, deploy := range deployList.Items {
			currentReplicas := int32(0)
			if deploy.Spec.Replicas != nil {
				currentReplicas = *deploy.Spec.Replicas
			}
			r.Log.Info("Checking deployment", "name", deploy.Name, "namespace", ns, "currentReplicas", currentReplicas)
			if deploy.Spec.Replicas != nil && *deploy.Spec.Replicas != 0 {
				r.Log.Info("Scaling down deployment to 0", "name", deploy.Name, "namespace", ns, "from", *deploy.Spec.Replicas)
				zero := int32(0)
				deploy.Spec.Replicas = &zero
				if err := remoteClient.Update(ctx, &deploy); err != nil {
					r.Log.Error(err, "Failed to update deployment", "name", deploy.Name)
					return false, err
				}
				r.Log.Info("Successfully scaled down deployment", "name", deploy.Name)
			}
		}
		// StatefulSets
		stsList := &appsv1.StatefulSetList{}
		if err := remoteClient.List(ctx, stsList, client.InNamespace(ns)); err != nil {
			r.Log.Error(err, "Failed to list statefulsets", "namespace", ns, "cluster", clusterName)
			return false, err
		}
		r.Log.Info("Listed statefulsets for ScaleDown", "namespace", ns, "count", len(stsList.Items))
		for _, sts := range stsList.Items {
			currentReplicas := int32(0)
			if sts.Spec.Replicas != nil {
				currentReplicas = *sts.Spec.Replicas
			}
			r.Log.Info("Checking statefulset", "name", sts.Name, "namespace", ns, "currentReplicas", currentReplicas)
			if sts.Spec.Replicas != nil && *sts.Spec.Replicas != 0 {
				r.Log.Info("Scaling down statefulset to 0", "name", sts.Name, "namespace", ns, "from", *sts.Spec.Replicas)
				zero := int32(0)
				sts.Spec.Replicas = &zero
				if err := remoteClient.Update(ctx, &sts); err != nil {
					r.Log.Error(err, "Failed to update statefulset", "name", sts.Name)
					return false, err
				}
				r.Log.Info("Successfully scaled down statefulset", "name", sts.Name)
			}
		}
	}
	return true, nil
}

func (r *DisasterOperationReconciler) doScaleUp(ctx context.Context, instance *disasterv1.DisasterInstance, clusterName string, replicasMap map[string]int32, waitUntilReady bool, namespaceMapping map[string]string) (bool, error) {
	remoteClient, err := r.getClusterClient(ctx, clusterName)
	if err != nil {
		return false, err
	}

	r.Log.Info("Executing ScaleUp", "cluster", clusterName, "waitUntilReady", waitUntilReady)

	// 1. Scale Up Workloads
	for _, ns := range instance.Spec.Namespaces {
		// Determine target namespace
		targetNs := ns
		if namespaceMapping != nil {
			if mapped, ok := namespaceMapping[ns]; ok {
				targetNs = mapped
			}
		}

		// Deployments
		deployList := &appsv1.DeploymentList{}
		if err := remoteClient.List(ctx, deployList, client.InNamespace(targetNs)); err != nil {
			return false, err
		}
		for _, deploy := range deployList.Items {
			if deploy.Spec.Replicas != nil && *deploy.Spec.Replicas == 0 {
				var targetReplicas int32 = -1
				key := fmt.Sprintf("%s/deployments/%s", ns, deploy.Name)
				if val, ok := replicasMap[key]; ok {
					targetReplicas = val
				} else if val, ok := deploy.Annotations["testudo.softcdata.com/original-replicas"]; ok {
					var r int32
					if _, err := fmt.Sscanf(val, "%d", &r); err == nil {
						targetReplicas = r
					}
				}

				if targetReplicas > 0 {
					r.Log.Info("Scaling up Deployment", "ns", ns, "name", deploy.Name, "replicas", targetReplicas)
					deploy.Spec.Replicas = &targetReplicas
					if err := remoteClient.Update(ctx, &deploy); err != nil {
						return false, err
					}
				}
			}
		}
		// StatefulSets
		stsList := &appsv1.StatefulSetList{}
		if err := remoteClient.List(ctx, stsList, client.InNamespace(targetNs)); err != nil {
			return false, err
		}
		for _, sts := range stsList.Items {
			if sts.Spec.Replicas != nil && *sts.Spec.Replicas == 0 {
				var targetReplicas int32 = -1
				key := fmt.Sprintf("%s/statefulsets/%s", ns, sts.Name)
				if val, ok := replicasMap[key]; ok {
					targetReplicas = val
				} else if val, ok := sts.Annotations["testudo.softcdata.com/original-replicas"]; ok {
					var r int32
					if _, err := fmt.Sscanf(val, "%d", &r); err == nil {
						targetReplicas = r
					}
				}

				if targetReplicas > 0 {
					r.Log.Info("Scaling up StatefulSet", "ns", ns, "name", sts.Name, "replicas", targetReplicas)
					sts.Spec.Replicas = &targetReplicas
					if err := remoteClient.Update(ctx, &sts); err != nil {
						return false, err
					}
				}
			}
		}
	}

	// 2. Check Readiness (Only if waitUntilReady is true)
	if !waitUntilReady {
		return true, nil
	}

	for _, ns := range instance.Spec.Namespaces {
		// Determine target namespace
		targetNs := ns
		if namespaceMapping != nil {
			if mapped, ok := namespaceMapping[ns]; ok {
				targetNs = mapped
			}
		}

		deployList := &appsv1.DeploymentList{}
		if err := remoteClient.List(ctx, deployList, client.InNamespace(targetNs)); err != nil {
			return false, err
		}
		for _, deploy := range deployList.Items {
			if deploy.Spec.Replicas != nil && *deploy.Spec.Replicas > 0 {
				if deploy.Status.ReadyReplicas < *deploy.Spec.Replicas {
					r.Log.Info("Waiting for Deployment", "ns", ns, "name", deploy.Name, "ready", deploy.Status.ReadyReplicas, "target", *deploy.Spec.Replicas)
					return false, nil
				}
			}
		}
		stsList := &appsv1.StatefulSetList{}
		if err := remoteClient.List(ctx, stsList, client.InNamespace(targetNs)); err != nil {
			return false, err
		}
		for _, sts := range stsList.Items {
			if sts.Spec.Replicas != nil && *sts.Spec.Replicas > 0 {
				if sts.Status.ReadyReplicas < *sts.Spec.Replicas {
					r.Log.Info("Waiting for StatefulSet", "ns", ns, "name", sts.Name, "ready", sts.Status.ReadyReplicas, "target", *sts.Spec.Replicas)
					return false, nil
				}
			}
		}
	}
	return true, nil
}

// cleanupTrafficlessPods 清理 Trafficless Pods
func (r *DisasterOperationReconciler) cleanupTrafficlessPods(ctx context.Context, log logr.Logger, instance *disasterv1.DisasterInstance, targetCluster string, namespaceMapping map[string]string) (bool, error) {
	remoteClient, err := r.getClusterClient(ctx, targetCluster)
	if err != nil {
		return false, err
	}

	allClean := true
	for _, ns := range instance.Spec.Namespaces {
		// Determine target namespace
		targetNs := ns
		if namespaceMapping != nil {
			if mapped, ok := namespaceMapping[ns]; ok {
				targetNs = mapped
			}
		}

		// Explicitly construct ListOptions
		listOpts := []client.ListOption{
			client.InNamespace(targetNs),
			client.MatchingLabels{"trafficless": "true"},
		}

		pods := &corev1.PodList{}
		if err := remoteClient.List(ctx, pods, listOpts...); err != nil {
			log.Error(err, "Failed to list trafficless pods", "namespace", targetNs)
			return false, err
		}

		if len(pods.Items) > 0 {
			// Check if any pod is still executing Init Containers (Data Restore)
			// We MUST wait for Init to finish before deleting, otherwise data restore is interrupted.
			for _, pod := range pods.Items {
				if !isInitFinished(&pod) {
					log.Info("Waiting for trafficless pod init/restore to finish", "pod", pod.Name, "phase", pod.Status.Phase)
					return false, nil // Requeue and wait
				}
			}

			allClean = false
			log.Info("Found trafficless pods to cleanup", "count", len(pods.Items), "namespace", targetNs)
			for _, pod := range pods.Items {
				log.Info("Deleting trafficless pod", "pod", pod.Name, "namespace", targetNs)
				if err := remoteClient.Delete(ctx, &pod); err != nil {
					if !errors.IsNotFound(err) {
						log.Error(err, "Failed to delete trafficless pod", "pod", pod.Name)
						return false, err
					}
				}
			}
		} else {
			log.V(1).Info("No trafficless pods found", "namespace", targetNs)
		}
	}

	if !allClean {
		log.Info("Waiting for trafficless pods to be deleted...")
		// 如果删除了 Pod，返回 false 等待它们消失 (List check next loop)
		return false, nil
	}

	return true, nil
}

// isInitFinished 检查 Pod 的 Init 容器是否全部完成
func isInitFinished(pod *corev1.Pod) bool {
	// If Succeeded or Failed, Init is definitely finished
	if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
		return true
	}
	// If Pending, Init not started or pending
	if pod.Status.Phase == corev1.PodPending {
		return false
	}
	// If Running, check InitStatuses
	// Note: When Pod is Running, all Init Containers must have completed successfully.
	// Because Pod transitions to Running only after Init containers exit 0.
	// Exception: If restartPolicy is Always, Init containers might restart? No, Init containers run to completion.
	// Sidecar containers in Init? (KEP-753). If using sidecar containers, they might be Running.
	// But standard Init containers are Terminated.

	// Let's double check InitStatuses just to be safe (e.g. during PodInitializing state which is part of Pending/Running transition)
	for _, status := range pod.Status.InitContainerStatuses {
		// If using SidecarContainers feature (k8s 1.28+), restartable init containers might be running.
		// But for data restore, usually it's run-to-completion.
		// If state is Terminated, it's done.
		if status.State.Terminated == nil {
			// If it's a sidecar (RestartPolicy=Always), it might be valid.
			// But assuming standard restore init container.
			// If not Terminated, it's running/waiting.
			return false
		}
	}
	return true
}

// syncStatistics 按统计规范更新 DisasterOperation 关联的 BackupRestoreStatistics CR
// 命名规则: op-<operation.Name>-stats
// 标签:  testudo.softcdata.com/owner-kind=DisasterOperation
// 遵循与 DataSync/ResourceSync syncStatistics 相同的模式
func (r *DisasterOperationReconciler) syncStatistics(ctx context.Context, operation *disasterv1.DisasterOperation) error {
	statsName := fmt.Sprintf("op-%s-stats", operation.Name)
	// 单个操作对应单个统计 CR，Total 始终为 1
	var completed, failed, inProgress int32
	switch operation.Status.State {
	case disasterv1.OperationStateCompleted:
		completed = 1
	case disasterv1.OperationStateFailed:
		failed = 1
	case disasterv1.OperationStateRunning:
		inProgress = 1
	}

	stats := &disasterv1.BackupRestoreStatistics{
		ObjectMeta: metav1.ObjectMeta{
			Name:      statsName,
			Namespace: operation.Namespace,
		},
	}

	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, stats, func() error {
		if stats.Labels == nil {
			stats.Labels = make(map[string]string)
		}
		stats.Labels["testudo.softcdata.com/owner-kind"] = "DisasterOperation"
		stats.Labels["testudo.softcdata.com/operation-type"] = string(operation.Spec.OperationType)
		stats.Labels["disaster.io/scope-uid"] = string(operation.UID)
		stats.Spec.ScopeType = disasterv1.ScopeTypeCustom
		stats.Spec.ScopeRef = disasterv1.ScopeReference{
			Kind:      "DisasterOperation",
			Name:      operation.Name,
			Namespace: operation.Namespace,
			UID:       operation.UID,
		}
		return nil
	}); err != nil {
		return err
	}

	stats.Status.Statistics.Total = 1
	stats.Status.Statistics.Completed = completed
	stats.Status.Statistics.Failed = failed
	stats.Status.Statistics.InProgress = inProgress
	now := metav1.Now()
	stats.Status.LastUpdateTime = &now
	stats.Status.LastUpdateReason = string(operation.Status.State)

	return r.Status().Update(ctx, stats)
}

// truncateLabelValue 缩短标签值以符合 K8s 63 字符限制
func truncateLabelValue(s string) string {
	if len(s) <= 63 {
		return s
	}
	// 使用 MD5 哈希保证唯一性，保留前缀方便识别
	hash := md5.Sum([]byte(s))
	// 50 字符前缀 + 1 分隔符 + 12 字符哈希 = 63 字符
	prefix := s
	if len(prefix) > 50 {
		prefix = prefix[:50]
	}
	return fmt.Sprintf("%s-%x", prefix, hash[:6])
}
