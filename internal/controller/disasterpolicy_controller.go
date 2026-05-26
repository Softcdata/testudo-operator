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

package controller

import (
	"context"
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	"github.com/softcdata/testudo-operator/pkg/helper"
	. "github.com/softcdata/testudo-operator/pkg/metadata"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// DisasterPolicyReconciler reconciles a DisasterPolicy object
type DisasterPolicyReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

const (
	policyReasonInvalidSchedule = "InvalidSchedule"
	policyReasonInvalidTTL      = "InvalidTTL"
)

// +kubebuilder:rbac:groups=testudo.softcdata.com,resources=disasterpolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=testudo.softcdata.com,resources=disasterpolicies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=testudo.softcdata.com,resources=disasterpolicies/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *DisasterPolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)

	// Fetch the DisasterPolicy instance
	var disasterPolicy disasterv1.DisasterPolicy
	if err := r.Get(ctx, req.NamespacedName, &disasterPolicy); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	logger = logger.WithValues(TraceIDKey, disasterPolicy.Annotations[AnnotationTraceID])
	ctx = context.WithValue(ctx, TraceIDKey, disasterPolicy.Annotations[AnnotationTraceID])

	// 获取 TraceID 和 User 用于事件发射
	traceID := disasterPolicy.Annotations[AnnotationTraceID]
	user := disasterPolicy.Annotations[AnnotationUser]
	if user == "" {
		user = "system"
	}

	// Handle deletion
	if !disasterPolicy.DeletionTimestamp.IsZero() {
		return r.handleDelete(ctx, &disasterPolicy)
	}

	// 记录初始状态用于事件判断
	isNewPolicy := !controllerutil.ContainsFinalizer(&disasterPolicy, LabelPolicyFinalizer)
	oldState := disasterPolicy.Status.LastState
	oldGeneration := disasterPolicy.Status.ObservedGeneration

	// Add finalizer if not present
	if isNewPolicy {
		controllerutil.AddFinalizer(&disasterPolicy, LabelPolicyFinalizer)
		if err := r.Update(ctx, &disasterPolicy); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Validate Cron Expression
	if _, err := cron.ParseStandard(disasterPolicy.Spec.Schedule); err != nil {
		logger.Info("invalid cron expression", "schedule", disasterPolicy.Spec.Schedule, "error", err)
		r.Recorder.Event(&disasterPolicy, corev1.EventTypeWarning, "InvalidSchedule", err.Error())
		disasterPolicy.Status.Phase = disasterv1.PolicyPhaseActive
		helper.SetStatusError(&disasterPolicy.Status, policyReasonInvalidSchedule, err.Error())
		disasterPolicy.Status.ObservedGeneration = disasterPolicy.Generation
		disasterPolicy.Status.LastState = disasterPolicy.Spec.State

		if isNewPolicy || oldGeneration != disasterPolicy.Generation || oldState != disasterPolicy.Spec.State {
			taskName := fmt.Sprintf("编辑策略 %s", disasterPolicy.Name)
			if isNewPolicy {
				taskName = fmt.Sprintf("创建策略 %s", disasterPolicy.Name)
			}
			now := metav1.Now()
			helper.ReportTaskFinishedWithClient(ctx, r.Client, r.Scheme, &disasterPolicy,
				taskName, "-", helper.TaskStatusFailed,
				&disasterPolicy.CreationTimestamp, &now, user, traceID, err.Error(), policyReasonInvalidSchedule)
		}

		if statusErr := r.Status().Update(ctx, &disasterPolicy); statusErr != nil {
			logger.Error(statusErr, "failed to update policy status for invalid schedule")
			return ctrl.Result{}, statusErr
		}

		// Don't requeue if the schedule is invalid, as it won't fix itself.
		return ctrl.Result{}, nil
	}

	if disasterPolicy.Spec.Type == disasterv1.PolicyTypeAutoBackup &&
		disasterPolicy.Spec.TTL != nil &&
		disasterPolicy.Spec.TTL.Duration <= 0 {
		err := fmt.Errorf("ttl must be greater than 0")
		logger.Info("invalid auto backup ttl", "ttl", disasterPolicy.Spec.TTL.Duration.String(), "error", err)
		r.Recorder.Event(&disasterPolicy, corev1.EventTypeWarning, policyReasonInvalidTTL, err.Error())
		disasterPolicy.Status.Phase = disasterv1.PolicyPhaseActive
		helper.SetStatusError(&disasterPolicy.Status, policyReasonInvalidTTL, err.Error())
		disasterPolicy.Status.ObservedGeneration = disasterPolicy.Generation
		disasterPolicy.Status.LastState = disasterPolicy.Spec.State

		if isNewPolicy || oldGeneration != disasterPolicy.Generation || oldState != disasterPolicy.Spec.State {
			taskName := fmt.Sprintf("编辑策略 %s", disasterPolicy.Name)
			if isNewPolicy {
				taskName = fmt.Sprintf("创建策略 %s", disasterPolicy.Name)
			}
			now := metav1.Now()
			helper.ReportTaskFinishedWithClient(ctx, r.Client, r.Scheme, &disasterPolicy,
				taskName, "-", helper.TaskStatusFailed,
				&disasterPolicy.CreationTimestamp, &now, user, traceID, err.Error(), policyReasonInvalidTTL)
		}

		if statusErr := r.Status().Update(ctx, &disasterPolicy); statusErr != nil {
			logger.Error(statusErr, "failed to update policy status for invalid ttl")
			return ctrl.Result{}, statusErr
		}

		return ctrl.Result{}, nil
	}

	// Sync Labels
	if err := r.syncLabels(ctx, &disasterPolicy); err != nil {
		logger.Error(err, "failed to sync labels")
		return ctrl.Result{}, err
	}

	// Update Object (Labels)
	if err := r.Update(ctx, &disasterPolicy); err != nil {
		return ctrl.Result{}, err
	}

	// 发射事件逻辑
	// 1. 首次创建策略
	// 如果是存量资源（CreationTimestamp 距今较久），则不发射创建事件（仅更新 Status）
	isCreatedRecently := time.Since(disasterPolicy.CreationTimestamp.Time) < 5*time.Minute
	if isNewPolicy || (oldGeneration == 0 && isCreatedRecently) {
		helper.ReportTaskFinishedWithClient(ctx, r.Client, r.Scheme, &disasterPolicy,
			fmt.Sprintf("创建策略 %s", disasterPolicy.Name), "-", helper.TaskStatusSuccess,
			&disasterPolicy.CreationTimestamp, nil, user, traceID, "策略创建完成")
	} else if disasterPolicy.Generation > oldGeneration {
		// 2. 状态变更检测（启用/禁用）
		currentState := disasterPolicy.Spec.State
		if oldState != "" && oldState != currentState {
			if currentState == disasterv1.PolicyStateEnabled {
				helper.ReportTaskFinishedWithClient(ctx, r.Client, r.Scheme, &disasterPolicy,
					fmt.Sprintf("启用策略 %s", disasterPolicy.Name), "-", helper.TaskStatusSuccess,
					nil, nil, user, traceID, "策略已启用")
			} else {
				helper.ReportTaskFinishedWithClient(ctx, r.Client, r.Scheme, &disasterPolicy,
					fmt.Sprintf("禁用策略 %s", disasterPolicy.Name), "-", helper.TaskStatusSuccess,
					nil, nil, user, traceID, "策略已禁用")
			}
		} else {
			// 3. 普通编辑
			helper.ReportTaskFinishedWithClient(ctx, r.Client, r.Scheme, &disasterPolicy,
				fmt.Sprintf("编辑策略 %s", disasterPolicy.Name), "-", helper.TaskStatusSuccess,
				nil, nil, user, traceID, "策略配置已更新")
		}
	}

	// 更新 Status 中的追踪字段
	helper.ClearStatusError(&disasterPolicy.Status)
	disasterPolicy.Status.Phase = disasterv1.PolicyPhaseActive
	disasterPolicy.Status.ObservedGeneration = disasterPolicy.Generation
	disasterPolicy.Status.LastState = disasterPolicy.Spec.State
	if err := r.Status().Update(ctx, &disasterPolicy); err != nil {
		logger.Error(err, "failed to update status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *DisasterPolicyReconciler) handleDelete(ctx context.Context, policy *disasterv1.DisasterPolicy) (ctrl.Result, error) {
	// 获取用于事件发射的上下文
	traceID := policy.Annotations[AnnotationTraceID]
	user := policy.Annotations[AnnotationUser]
	if user == "" {
		user = "system"
	}

	// 1. 发射 "删除策略 Started" 事件（仅发射一次，防抖）
	if policy.Status.LastEventPhase != "Deleting" {
		helper.ReportTaskStartedWithClient(ctx, r.Client, r.Scheme, policy,
			fmt.Sprintf("删除策略 %s", policy.Name), "-", user, traceID, "开始删除策略")

		// 临时借用 LastEventPhase 记录状态，但这需要 Status 更新
		policy.Status.LastEventPhase = "Deleting"
		if err := r.Status().Update(ctx, policy); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Legacy finalizer deletion protection is temporarily disabled.
	//
	// The old behavior blocked finalizer removal while DisasterPolicy was still
	// referenced by AppBackup or had running DisasterJob resources. We are
	// temporarily bypassing that legacy protection so deletion can proceed, and
	// will re-introduce the new case-based deletion rules separately.
	/*
		appBackups := &disasterv1.AppBackupList{}
		if err := r.List(ctx, appBackups, client.MatchingLabels{LabelDisasterPolicyName: policy.Name}); err != nil {
			return ctrl.Result{}, err
		}

		if len(appBackups.Items) > 0 {
			var backupNames []string
			for _, ab := range appBackups.Items {
				backupNames = append(backupNames, ab.Name)
			}
			message := fmt.Sprintf("Cannot delete DisasterPolicy %s because it is referenced by AppBackup(s): %v", policy.Name, backupNames)
			logger.Info(message)
			policy.Status.Phase = disasterv1.PolicyPhaseDeleting
			policy.Status.Reason = "DeletionBlocked"
			policy.Status.Message = message

			r.Recorder.Event(policy, corev1.EventTypeWarning, "DeletionBlocked", message)

			if err := r.Status().Update(ctx, policy); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{RequeueAfter: 1 * time.Minute}, nil
		}

		jobs := &disasterv1.DisasterJobList{}
		if err := r.List(ctx, jobs, client.MatchingLabels{LabelDisasterPolicyName: policy.Name}); err != nil {
			return ctrl.Result{}, err
		}

		for _, job := range jobs.Items {
			if job.Status.Phase == disasterv1.DisasterJobPhaseBackuping ||
				job.Status.Phase == disasterv1.DisasterJobPhaseRestoring {
				message := fmt.Sprintf("Cannot delete DisasterPolicy %s because DisasterJob %s is running", policy.Name, job.Name)
				logger.Info(message)
				policy.Status.Phase = disasterv1.PolicyPhaseDeleting
				policy.Status.Reason = "DeletionBlocked"
				policy.Status.Message = message

				r.Recorder.Event(policy, corev1.EventTypeWarning, "DeletionBlocked", message)

				if err := r.Status().Update(ctx, policy); err != nil {
					return ctrl.Result{}, err
				}
				return ctrl.Result{RequeueAfter: 1 * time.Minute}, nil
			}
		}
	*/

	// 2. 发射 "删除策略 Finished" 事件
	now := metav1.Now()
	helper.ReportTaskFinishedWithClient(ctx, r.Client, r.Scheme, policy,
		fmt.Sprintf("删除策略 %s", policy.Name), "-", helper.TaskStatusSuccess,
		policy.DeletionTimestamp, &now, user, traceID, "策略删除完成")

	// Remove finalizer
	if controllerutil.ContainsFinalizer(policy, LabelPolicyFinalizer) {
		controllerutil.RemoveFinalizer(policy, LabelPolicyFinalizer)
		if err := r.Update(ctx, policy); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func (r *DisasterPolicyReconciler) syncLabels(ctx context.Context, disasterPolicy *disasterv1.DisasterPolicy) error {
	if disasterPolicy.Labels == nil {
		disasterPolicy.Labels = make(map[string]string)
	}

	// Sync metadata to labels
	disasterPolicy.Labels[LabelDisasterPolicyType] = string(disasterPolicy.Spec.Type)
	disasterPolicy.Labels[LabelDisasterPolicyName] = disasterPolicy.Name
	disasterPolicy.Labels[LabelDisasterPolicyState] = string(disasterPolicy.Spec.State)

	// 通用依赖标签
	_, _, _ = EnsureDependencyTokenLabel(disasterPolicy.Labels, string(disasterPolicy.UID))

	edges := make([]DependencyEdge, 0, 1)
	if srName := disasterPolicy.Labels[LabelStorageRepositoryName]; srName != "" {
		sr := &disasterv1.StorageRepository{}
		err := r.Get(ctx, types.NamespacedName{Namespace: disasterPolicy.Namespace, Name: srName}, sr)
		if apierrors.IsNotFound(err) {
			err = r.Get(ctx, types.NamespacedName{Namespace: ManagementNamespace(), Name: srName}, sr)
		}
		if err == nil {
			edges = append(edges, DependencyEdge{
				TargetToken:  BuildDependencyToken(string(sr.UID)),
				RelationCode: "label.storageRepositoryName",
			})
		}
	}
	_, _ = RebuildDependencyToLabels(disasterPolicy.Labels, edges)

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *DisasterPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Recorder = mgr.GetEventRecorderFor("disasterpolicy-controller")
	return ctrl.NewControllerManagedBy(mgr).
		For(&disasterv1.DisasterPolicy{}).
		Named("disasterpolicy").
		Complete(r)
}
