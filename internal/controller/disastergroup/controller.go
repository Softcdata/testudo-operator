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

package disastergroup

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	"github.com/softcdata/testudo-operator/pkg/helper"
	"github.com/softcdata/testudo-operator/pkg/metadata"
)

// DisasterGroupReconciler 负责调谐 DisasterGroup 对象
type DisasterGroupReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Log      logr.Logger
	Recorder record.EventRecorder
}

const annotationGroupCreateTaskEmitted = "testudo.softcdata.com/group-create-task-emitted"

const (
	groupReasonInstanceNotFound = "InstanceNotFound"
	groupReasonInstanceFailed   = "InstanceFailed"
)

func groupAuditContext(group *disasterv1.DisasterGroup) (user, traceID string) {
	if group == nil {
		return "system", "-"
	}
	user = group.Annotations[metadata.AnnotationUser]
	if user == "" {
		user = "system"
	}
	traceID = group.Annotations[metadata.AnnotationTraceID]
	if traceID == "" {
		traceID = "-"
	}
	return user, traceID
}

func classifyGroupStatusEvent(readyInstances, totalInstances int, reason string) (eventType, eventReason string) {
	if reason != "" || readyInstances < totalInstances {
		return corev1.EventTypeWarning, "GroupDegraded"
	}
	return corev1.EventTypeNormal, "GroupHealthy"
}

func (r *DisasterGroupReconciler) reportGroupStatusChange(
	group *disasterv1.DisasterGroup,
	beforeReady, beforeTotal int,
	beforeReason, beforeMessage string,
) {
	if group == nil {
		return
	}
	afterReady := group.Status.ReadyInstances
	afterTotal := group.Status.TotalInstances
	afterReason := group.Status.Reason
	afterMessage := group.Status.Message
	eventType, eventReason := classifyGroupStatusEvent(afterReady, afterTotal, afterReason)
	helper.ReportDiagnosticEventf(
		r.Recorder,
		group,
		eventType,
		eventReason,
		"容灾组状态变化: ready %d/%d -> %d/%d, reason %q -> %q, message %q -> %q",
		beforeReady,
		beforeTotal,
		afterReady,
		afterTotal,
		beforeReason,
		afterReason,
		beforeMessage,
		afterMessage,
	)
}

func (r *DisasterGroupReconciler) ensureGroupCreateEvent(ctx context.Context, group *disasterv1.DisasterGroup) {
	if group == nil {
		return
	}
	if group.Annotations != nil && group.Annotations[annotationGroupCreateTaskEmitted] != "" {
		return
	}

	user, traceID := groupAuditContext(group)
	taskName := fmt.Sprintf("创建容灾组 %s", group.Name)
	helper.ReportTaskStartedWithClient(ctx, r.Client, r.Scheme, group, taskName, "-", user, traceID, "容灾组创建任务开始")
	now := metav1.Now()
	helper.ReportTaskFinishedWithClient(ctx, r.Client, r.Scheme, group, taskName, "-", helper.TaskStatusSuccess, nil, &now, user, traceID, "容灾组创建完成")

	latest := &disasterv1.DisasterGroup{}
	if err := r.Get(ctx, client.ObjectKeyFromObject(group), latest); err != nil {
		return
	}
	if latest.Annotations == nil {
		latest.Annotations = make(map[string]string)
	}
	if latest.Annotations[annotationGroupCreateTaskEmitted] != "" {
		return
	}
	original := latest.DeepCopy()
	latest.Annotations[annotationGroupCreateTaskEmitted] = time.Now().Format(time.RFC3339Nano)
	_ = r.Patch(ctx, latest, client.MergeFrom(original))
}

// +kubebuilder:rbac:groups=testudo.softcdata.com,resources=disastergroups,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=testudo.softcdata.com,resources=disastergroups/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=testudo.softcdata.com,resources=disasterinstances,verbs=get;list;watch

// Reconcile 处理 DisasterGroup 的调谐循环
func (r *DisasterGroupReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("disastergroup", req.NamespacedName)

	// 获取 DisasterGroup
	group := &disasterv1.DisasterGroup{}
	if err := r.Get(ctx, req.NamespacedName, group); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		log.Error(err, "获取 DisasterGroup 失败")
		return ctrl.Result{}, err
	}

	if changed, err := r.syncDependencyLabels(ctx, group); err != nil {
		log.Error(err, "同步依赖标签失败")
		return ctrl.Result{}, err
	} else if changed {
		if err := r.Update(ctx, group); err != nil {
			log.Error(err, "更新依赖标签失败")
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	r.ensureGroupCreateEvent(ctx, group)

	beforeTotal := group.Status.TotalInstances
	beforeReady := group.Status.ReadyInstances
	beforeReason := group.Status.Reason
	beforeMessage := group.Status.Message

	// 聚合状态
	totalInstances := 0
	readyInstances := 0
	missingInstances := make([]string, 0)
	errorInstances := make([]string, 0)

	// 遍历所有 Level
	for _, level := range group.Spec.Levels {
		for _, instanceName := range level {
			totalInstances++

			// 获取实例状态
			instance := &disasterv1.DisasterInstance{}
			err := r.Get(ctx, client.ObjectKey{Namespace: group.Namespace, Name: instanceName}, instance)
			if err != nil {
				if !errors.IsNotFound(err) {
					log.Error(err, "获取 DisasterInstance 失败", "name", instanceName)
					return ctrl.Result{}, err
				}
				// 如果未找到，记录错误并继续聚合
				missingInstances = append(missingInstances, instanceName)
				continue
			}

			// 检查是否受保护
			if instance.Status.FsmState == disasterv1.FsmStateProtected {
				readyInstances++
			}
			if instance.Status.FsmState == disasterv1.FsmStateFailed || instance.Status.FsmState == disasterv1.FsmStateConfigError || instance.Status.Reason != "" {
				errorInstances = append(errorInstances, summarizeInstanceError(instance))
			}
		}
	}

	newReason, newMessage := summarizeGroupError(missingInstances, errorInstances)
	errorChanged := group.Status.Reason != newReason || group.Status.Message != newMessage
	if errorChanged {
		if newReason == "" {
			helper.ClearStatusError(&group.Status)
			apimeta.SetStatusCondition(&group.Status.Conditions, metav1.Condition{
				Type:               "Error",
				Status:             metav1.ConditionFalse,
				LastTransitionTime: metav1.Now(),
				Reason:             "Healthy",
				Message:            "all instances are available",
			})
		} else {
			helper.SetStatusError(&group.Status, newReason, newMessage)
			helper.SetConditionError(&group.Status.Conditions, "Error", newReason, newMessage)
		}
	}

	// 更新 Status
	// 仅当状态变化时才更新，防止死循环 (虽然 Update 会处理 resourceVersion)
	if group.Status.TotalInstances != totalInstances || group.Status.ReadyInstances != readyInstances || errorChanged {
		group.Status.TotalInstances = totalInstances
		group.Status.ReadyInstances = readyInstances

		if err := r.Status().Update(ctx, group); err != nil {
			log.Error(err, "更新 DisasterGroup Status 失败")
			return ctrl.Result{}, err
		}
		r.reportGroupStatusChange(group, beforeReady, beforeTotal, beforeReason, beforeMessage)
		log.Info("更新 DisasterGroup 状态", "total", totalInstances, "ready", readyInstances)
	}

	return ctrl.Result{}, nil
}

// SetupWithManager 将控制器注册到 Manager
func (r *DisasterGroupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&disasterv1.DisasterGroup{}).
		Watches(
			&disasterv1.DisasterInstance{},
			handler.EnqueueRequestsFromMapFunc(r.mapInstanceToGroups),
		).
		Complete(r)
}

func (r *DisasterGroupReconciler) mapInstanceToGroups(ctx context.Context, obj client.Object) []ctrl.Request {
	instance, ok := obj.(*disasterv1.DisasterInstance)
	if !ok || instance == nil {
		return nil
	}

	groups := &disasterv1.DisasterGroupList{}
	if err := r.List(ctx, groups, client.InNamespace(instance.Namespace)); err != nil {
		r.Log.Error(err, "列出 DisasterGroup 失败，无法触发组级状态重算", "instance", instance.Name, "namespace", instance.Namespace)
		return nil
	}

	requests := make([]ctrl.Request, 0, len(groups.Items))
	for i := range groups.Items {
		group := &groups.Items[i]
		if groupContainsInstance(group, instance.Name) {
			requests = append(requests, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(group)})
		}
	}

	return requests
}

func groupContainsInstance(group *disasterv1.DisasterGroup, instanceName string) bool {
	for _, level := range group.Spec.Levels {
		for _, name := range level {
			if name == instanceName {
				return true
			}
		}
	}
	return false
}

func (r *DisasterGroupReconciler) syncDependencyLabels(ctx context.Context, group *disasterv1.DisasterGroup) (bool, error) {
	if group.Labels == nil {
		group.Labels = make(map[string]string)
	}
	_, _, tokenChanged := metadata.EnsureDependencyTokenLabel(group.Labels, string(group.UID))
	edges := make([]metadata.DependencyEdge, 0)

	for _, level := range group.Spec.Levels {
		for _, instanceName := range level {
			if instanceName == "" {
				continue
			}
			instance := &disasterv1.DisasterInstance{}
			if err := r.Get(ctx, client.ObjectKey{Namespace: group.Namespace, Name: instanceName}, instance); err == nil {
				edges = append(edges, metadata.DependencyEdge{
					TargetToken:  metadata.BuildDependencyToken(string(instance.UID)),
					RelationCode: "spec.levels",
				})
			}
		}
	}

	_, depChanged := metadata.RebuildDependencyToLabels(group.Labels, edges)
	return tokenChanged || depChanged, nil
}

func summarizeGroupError(missingInstances, errorInstances []string) (reason, message string) {
	if len(missingInstances) > 0 {
		sort.Strings(missingInstances)
		return groupReasonInstanceNotFound, fmt.Sprintf("instances not found: %s", strings.Join(missingInstances, ","))
	}
	if len(errorInstances) > 0 {
		sort.Strings(errorInstances)
		return groupReasonInstanceFailed, fmt.Sprintf("instances in error state: %s", strings.Join(errorInstances, ","))
	}
	return "", ""
}

func summarizeInstanceError(instance *disasterv1.DisasterInstance) string {
	if instance == nil {
		return ""
	}
	if instance.Status.Reason == "" {
		return instance.Name
	}
	if instance.Status.FsmState != "" {
		return fmt.Sprintf("%s[%s:%s]", instance.Name, instance.Status.FsmState, instance.Status.Reason)
	}
	return fmt.Sprintf("%s[%s]", instance.Name, instance.Status.Reason)
}
