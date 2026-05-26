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
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/softcdata/testudo-operator/internal/controller"
	"github.com/softcdata/testudo-operator/pkg/helper"
	. "github.com/softcdata/testudo-operator/pkg/metadata"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
)

type PendingHandler struct{}

func (h *PendingHandler) Handle(ctx context.Context, r *AppBackupReconciler, appBackup *disasterv1.AppBackup) (AppBackupPhase, ctrl.Result, error) {
	logger := logf.FromContext(ctx)

	// 1. Ensure Finalizer
	if !controllerutil.ContainsFinalizer(appBackup, LabelAppBackupFinalizer) {
		controllerutil.AddFinalizer(appBackup, LabelAppBackupFinalizer)
		r.Recorder.Event(appBackup, corev1.EventTypeNormal, "FinalizerAdded", "Added finalizer to AppBackup")

		// 创建应用备份 Started+Finished 事件
		user := appBackup.Annotations["testudo.softcdata.com/user"]
		if user == "" {
			user = "system"
		}
		traceID := appBackup.Annotations[AnnotationTraceID]
		taskName := fmt.Sprintf("创建应用备份 %s", appBackup.Name)
		now := metav1.Now()
		// 由于资源创建是瞬时完成的，我们直接发射 Started + Finished
		helper.ReportTaskStartedWithClient(ctx, r.Client, r.Scheme, appBackup, taskName, appBackup.Spec.Cluster, user, traceID, "开始创建应用备份")
		helper.ReportTaskFinishedWithClient(ctx, r.Client, r.Scheme, appBackup, taskName, appBackup.Spec.Cluster, helper.TaskStatusSuccess, &now, &now, user, traceID, "应用备份创建完成")

		// Continue to next steps instead of returning, to speed up the process
		return PhasePending, ctrl.Result{Requeue: true}, nil
	}

	// 2. Validate Cluster
	if appBackup.Spec.Cluster == "" {
		err := fmt.Errorf("cluster is invalid")
		r.Recorder.Event(appBackup, corev1.EventTypeWarning, "ConfigError", err.Error())
		return PhaseFailed, ctrl.Result{}, nil // No error returned, just transition to Failed
	}

	// 3. Get KubeClient
	cli, err := r.ClientFactory.GetKubeClient(ctx, r.Client, r.Scheme, appBackup.Spec.Cluster)
	if err != nil {
		logger.Error(err, "error creating kube client")
		r.Recorder.Event(appBackup, corev1.EventTypeWarning, "CreateKubeClientFailed", err.Error())
		// Return error so it appears in Status.Message
		return PhasePending, ctrl.Result{}, err
	}

	// 4. Check StorageRepository
	sr := &disasterv1.StorageRepository{}
	err = r.Get(ctx, types.NamespacedName{Name: appBackup.Spec.Template.StorageLocation, Namespace: ManagementNamespace()}, sr)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("storage repository not found")
			r.Recorder.Event(appBackup, corev1.EventTypeWarning, "StorageRepositoryNotFound", "StorageRepository not found")
			return PhaseFailed, ctrl.Result{}, nil
		}
		logger.Error(err, "error getting storage repository")
		return PhasePending, ctrl.Result{}, err
	}
	var defaultBSL DefaultBSL
	// 5. Apply StorageRepository
	bslName := sr.Name + "-" + appBackup.Spec.Cluster
	err = defaultBSL.ApplyStorageRepository(ctx, r.Client, cli, sr, bslName, appBackup.Spec.Cluster)
	if err != nil {
		// Check if it's a BSL unavailable error
		if err.Error() == fmt.Sprintf("BackupStorageLocation %s is in Unavailable status", bslName) {
			logger.Info("BackupStorageLocation is unavailable, requeueing", "bslName", bslName)
			r.Recorder.Event(appBackup, corev1.EventTypeWarning, "BSLUnavailable", "BackupStorageLocation is unavailable, waiting...")
			return PhasePending, ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}

		logger.Error(err, "error applying storage repository")
		r.Recorder.Event(appBackup, corev1.EventTypeWarning, "ApplyStorageRepositoryFailed", err.Error())
		return PhaseFailed, ctrl.Result{}, nil
	}

	// All checks passed
	return PhaseReady, ctrl.Result{Requeue: true}, nil
}
