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
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	"github.com/softcdata/testudo-operator/pkg/helper"
	. "github.com/softcdata/testudo-operator/pkg/metadata"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// AppBackupPhase defines the lifecycle phase of AppBackup
type AppBackupPhase string

const (
	PhasePending  AppBackupPhase = "Pending"
	PhaseReady    AppBackupPhase = "Ready"
	PhaseFailed   AppBackupPhase = "Failed"
	PhaseDeleting AppBackupPhase = "Deleting"
)

// StateHandler handles a specific phase of the AppBackup lifecycle
type StateHandler interface {
	Handle(ctx context.Context, r *AppBackupReconciler, appBackup *disasterv1.AppBackup) (AppBackupPhase, ctrl.Result, error)
}

type FailedHandler struct{}

func (h *FailedHandler) Handle(ctx context.Context, r *AppBackupReconciler, appBackup *disasterv1.AppBackup) (AppBackupPhase, ctrl.Result, error) {
	// If we are in Failed state, we want to try again if:
	// 1. The user updated the Spec (Controller triggers immediate reconcile).
	// 2. Enough time has passed (RequeueAfter from previous failure).

	// Since we don't have ObservedGeneration to distinguish, we simply transition to Pending
	// to let PendingHandler re-validate.
	// If PendingHandler fails again, it should return RequeueAfter to prevent tight loops.

	return PhasePending, ctrl.Result{}, nil
}

type DeletingHandler struct{}

func (h *DeletingHandler) Handle(ctx context.Context, r *AppBackupReconciler, appBackup *disasterv1.AppBackup) (AppBackupPhase, ctrl.Result, error) {
	logger := logf.FromContext(ctx)

	if controllerutil.ContainsFinalizer(appBackup, LabelAppBackupFinalizer) {
		r.Recorder.Event(appBackup, corev1.EventTypeNormal, "Deleting", "Starting to delete external resources")

		// 删除应用备份 Started 事件
		user := appBackup.Annotations["testudo.softcdata.com/user"]
		if user == "" {
			user = "system"
		}
		traceID := appBackup.Annotations[AnnotationTraceID]
		taskName := fmt.Sprintf("删除应用备份 %s", appBackup.Name)
		helper.ReportTaskStartedWithClient(ctx, r.Client, r.Scheme, appBackup, taskName, appBackup.Spec.Cluster, user, traceID, "开始删除应用备份")

		// Get KubeClient
		cli, err := r.ClientFactory.GetKubeClient(ctx, r.Client, r.Scheme, appBackup.Spec.Cluster)
		if err != nil {
			if apierrors.IsNotFound(err) {
				logger.Info("Cluster not found, skipping external resource deletion", "cluster", appBackup.Spec.Cluster)
				r.Recorder.Event(appBackup, corev1.EventTypeWarning, "ClusterNotFound", "Cluster not found, skipping external resource deletion")
			} else {
				logger.Error(err, "error creating kube client for deletion")
				return PhaseDeleting, ctrl.Result{}, err
			}
		} else {
			// Execute cleanup logic
			if err := r.deleteExternalResources(ctx, cli, appBackup); err != nil {
				r.Recorder.Event(appBackup, corev1.EventTypeWarning, "DeleteExternalResourcesFailed", err.Error())
				logger.Error(err, "failed to delete external resources")
				return PhaseDeleting, ctrl.Result{}, err
			}
			r.Recorder.Event(appBackup, corev1.EventTypeNormal, "Deleted", "External resources deleted successfully")
		}

		// 删除应用备份 Finished 事件（必须在移除 Finalizer 之前！）
		now := metav1.Now()
		helper.ReportTaskFinishedWithClient(ctx, r.Client, r.Scheme, appBackup, taskName, appBackup.Spec.Cluster, helper.TaskStatusSuccess, appBackup.DeletionTimestamp, &now, user, traceID, "应用备份删除完成")

		// Remove Finalizer
		controllerutil.RemoveFinalizer(appBackup, LabelAppBackupFinalizer)
	}

	// Stop reconciliation
	return PhaseDeleting, ctrl.Result{}, nil
}
