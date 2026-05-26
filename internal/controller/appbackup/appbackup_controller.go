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
	"reflect"
	"sort"
	"time"

	"github.com/softcdata/testudo-operator/pkg/helper"
	. "github.com/softcdata/testudo-operator/pkg/metadata"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	. "github.com/softcdata/testudo-operator/internal/controller"
	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
)

// AppBackupReconciler reconciles a AppBackup object
type AppBackupReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	Recorder      record.EventRecorder
	ClientFactory ClientFactory
	StatsHelper   helper.StatisticsHelper
}

const (
	appBackupReasonBackupFailed          = "BackupFailed"
	appBackupReasonBackupPartiallyFailed = "BackupPartiallyFailed"
	appBackupReasonTimeoutExceeded       = "TimeoutExceeded"
)

// +kubebuilder:rbac:groups=testudo.softcdata.com,resources=appbackups,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=testudo.softcdata.com,resources=appbackups/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=testudo.softcdata.com,resources=appbackups/finalizers,verbs=update
// +kubebuilder:rbac:groups=testudo.softcdata.com,resources=backuprestorestatisticses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=testudo.softcdata.com,resources=backuprestorestatisticses/status,verbs=get;update;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the AppBackup object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/reconcile
func (r *AppBackupReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)
	appBackup := &disasterv1.AppBackup{}
	if err := r.Get(ctx, req.NamespacedName, appBackup); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	ctx = context.WithValue(ctx, TraceIDKey, appBackup.Annotations[AnnotationTraceID])
	logger = logger.WithValues(TraceIDKey, appBackup.Annotations[AnnotationTraceID])
	// Handle Deletion
	if !appBackup.ObjectMeta.DeletionTimestamp.IsZero() {
		logger.Info("AppBackup is being deleted", "deletionTimestamp", appBackup.ObjectMeta.DeletionTimestamp)
		handler := &DeletingHandler{}
		_, res, err := handler.Handle(ctx, r, appBackup)
		if err != nil {
			return res, err
		}
		if err := r.Update(ctx, appBackup); err != nil {
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
		return res, nil
	}

	// Determine Phase
	phase := AppBackupPhase(appBackup.Status.Status)
	if phase == "" {
		phase = PhasePending
	}

	// Safety check: If status is Deleting but DeletionTimestamp is zero, reset to Pending.
	// This prevents the controller from getting stuck in DeletingHandler if the status was accidentally set to Deleting.
	if phase == PhaseDeleting && appBackup.ObjectMeta.DeletionTimestamp.IsZero() {
		logger.Info("Status is Deleting but DeletionTimestamp is zero, resetting to Pending", "original_status", appBackup.Status.Status)
		phase = PhasePending
	}

	logger.Info("Reconciling AppBackup", "phase", phase, "status", appBackup.Status.Status)
	// Select Handler
	var handler StateHandler
	switch phase {
	case PhasePending:
		handler = &PendingHandler{}
	case PhaseReady:
		handler = &ReadyHandler{}
	case PhaseFailed:
		handler = &FailedHandler{}
	case PhaseDeleting:
		handler = &DeletingHandler{}
	default:
		handler = &PendingHandler{}
	}

	// Execute Handler
	nextPhase, result, handlerErr := handler.Handle(ctx, r, appBackup)
	if handlerErr != nil {
		r.Recorder.Event(appBackup, corev1.EventTypeWarning, "ReconcileError", handlerErr.Error())
		helper.SetStatusError(&appBackup.Status, "ReconcileError", handlerErr.Error())
	} else {
		// Keep handler-assigned reason/message (for example, backup timeout/failure details).
		// Only clear stale reconcile-level errors or stale backup failures after recovery.
		if appBackup.Status.Reason == "ReconcileError" {
			helper.ClearStatusError(&appBackup.Status)
		} else if appBackup.Status.LatestBackupStatus != disasterv1.LastBackupStatusFailed &&
			(appBackup.Status.Reason == appBackupReasonBackupFailed ||
				appBackup.Status.Reason == appBackupReasonBackupPartiallyFailed ||
				appBackup.Status.Reason == appBackupReasonTimeoutExceeded) {
			helper.ClearStatusError(&appBackup.Status)
		}
	}

	// Update Status if Phase changed
	statusChanged := false
	if nextPhase != "" && (nextPhase != phase || appBackup.Status.Status == "") {
		if nextPhase != phase {
			r.Recorder.Eventf(appBackup, corev1.EventTypeNormal, "PhaseChange", "Phase transitioned from %s to %s", phase, nextPhase)
		}
		appBackup.Status.Status = string(nextPhase)
		statusChanged = true
	}

	// Sync Labels
	if err := r.syncLabels(ctx, r.Client, appBackup); err != nil {
		logger.Error(err, "failed to sync labels")
	}

	//启用了 Status 子资源后，对主资源（AppBackup）的 Update 操作会忽略 Status 字段的变更。Kubernetes API Server 只会更新 Metadata 和 Spec
	//暂存状态
	currentStatus := appBackup.Status

	// Update Object (Labels/Finalizers)
	if err := r.Update(ctx, appBackup); err != nil {
		if handlerErr != nil {
			logger.Error(err, "failed to update resource", "handlerError", handlerErr)
			return result, handlerErr
		}
		return ctrl.Result{}, err
	}

	// Restore Status
	appBackup.Status = currentStatus

	// Update Status
	if statusChanged || phase == PhaseReady || handlerErr != nil {
		if err := r.Status().Update(ctx, appBackup); err != nil {
			if handlerErr != nil {
				logger.Error(err, "failed to update status", "handlerError", handlerErr)
				return result, handlerErr
			}
			return ctrl.Result{}, err
		}
	}
	logger.Info("Reconcile completed", "nextPhase", nextPhase, "statusChanged", statusChanged)

	// Sync Statistics
	if appBackup.Spec.Cluster != "" {
		targetCli, err := r.ClientFactory.GetKubeClient(ctx, r.Client, r.Scheme, appBackup.Spec.Cluster)
		if err == nil {
			if err := r.syncStatistics(ctx, appBackup, targetCli); err != nil {
				logger.Error(err, "Failed to sync statistics")
			}
		}
	}

	return result, handlerErr
}

// SetupWithManager sets up the controller with the Manager.
func (r *AppBackupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Recorder = mgr.GetEventRecorderFor("appbackup-controller")
	r.Scheme = mgr.GetScheme()
	if r.ClientFactory == nil {
		r.ClientFactory = &DefaultClientFactory{}
	}
	r.StatsHelper = helper.NewStatisticsHelper(r.Client)
	return ctrl.NewControllerManagedBy(mgr).
		For(&disasterv1.AppBackup{}).
		Watches(
			&disasterv1.DisasterPolicy{},
			handler.EnqueueRequestsFromMapFunc(r.mapAppBackupsForAutoBackupPolicy),
		).
		//监听 Velero Backup 资源的变化
		Watches(
			&velerov1.Backup{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []reconcile.Request {
				backup := o.(*velerov1.Backup)
				uid := backup.Labels[LabelAppBackupUID]
				if uid == "" {
					return nil
				}

				// Find AppBackup by UID
				list := &disasterv1.AppBackupList{}
				if err := mgr.GetClient().List(ctx, list); err != nil {
					return nil
				}
				//找到管理该 Backup 的 AppBackup 资源
				for _, ab := range list.Items {
					if string(ab.UID) == uid {
						//与管理状态不同步时触发重新调度
						if string(ab.Status.BackupStatus.Phase) != string(backup.Status.Phase) {
							return []reconcile.Request{{NamespacedName: types.NamespacedName{
								Name:      ab.Name,
								Namespace: ab.Namespace,
							}}}
						}
					}
				}
				return nil
			}),
		).
		WithEventFilter(IgnoreStatusUpdatesPredicate{}). // 忽略status更新
		Named("appbackup").
		Complete(r)
}

func (r *AppBackupReconciler) mapAppBackupsForAutoBackupPolicy(ctx context.Context, o client.Object) []reconcile.Request {
	policy, ok := o.(*disasterv1.DisasterPolicy)
	if !ok || policy.Spec.Type != disasterv1.PolicyTypeAutoBackup {
		return nil
	}
	list := &disasterv1.AppBackupList{}
	if err := r.List(ctx, list, client.InNamespace(policy.Namespace)); err != nil {
		return nil
	}
	requests := make([]reconcile.Request, 0)
	for i := range list.Items {
		if list.Items[i].Spec.DisasterPolicy != policy.Name {
			continue
		}
		requests = append(requests, reconcile.Request{NamespacedName: types.NamespacedName{
			Name:      list.Items[i].Name,
			Namespace: list.Items[i].Namespace,
		}})
	}
	return requests
}

// ListAppBackups lists all Velero Backups for a given AppBackup, sorted by creation timestamp descending
func (r *AppBackupReconciler) ListAppBackups(ctx context.Context, cli client.Client, ab *disasterv1.AppBackup) ([]velerov1.Backup, bool, error) {
	backupList := &velerov1.BackupList{}
	// Use UID for selection - this is the source of truth for controller logic
	labelSelector := client.MatchingLabels{
		LabelAppBackupUID: string(ab.UID),
	}
	if err := cli.List(ctx, backupList, labelSelector); err != nil {
		return nil, false, err
	}

	// Sort backups by creation timestamp (descending)
	sort.Slice(backupList.Items, func(i, j int) bool {
		return backupList.Items[i].CreationTimestamp.Time.After(backupList.Items[j].CreationTimestamp.Time)
	})

	return backupList.Items, true, nil
}
func NamSpaceLabels(includedNamespaces []string) string {
	ns := ""
	if len(includedNamespaces) > 0 {
		for i, n := range includedNamespaces {
			if i > 0 {
				ns += "."
			}
			ns += n
		}
	}
	// Kubernetes Label Value 长度限制为 63 字符
	if len(ns) > 63 {
		return ns[:63]
	}
	return ns
}

func appBackupTypeLabelValue(ab *disasterv1.AppBackup) string {
	if ab.Spec.Schedule != "" {
		return "Schedule"
	}
	return "Manual"
}

func (r *AppBackupReconciler) syncLabels(ctx context.Context, cli client.Client, ab *disasterv1.AppBackup) error {
	if ab.Labels == nil {
		ab.Labels = make(map[string]string)
	}

	// Namespace

	ab.Labels[LabelAppBackupIncludeNamespace] = NamSpaceLabels(ab.Spec.Template.IncludedNamespaces)
	ab.Labels[LabelAppBackupName] = ab.Name
	ab.Labels[LabelAppBackupCluster] = ab.Spec.Cluster
	if ab.Spec.Schedule != "" {
		ab.Labels[LabelAppBackupType] = "Schedule"
	} else {
		ab.Labels[LabelAppBackupType] = "Manual"
	}
	// 来源标签（user / disaster-instance）
	_ = EnsureAppResourceOriginLabels(ab.Labels, ab.OwnerReferences)
	// Status
	status := string(ab.Status.LatestBackupStatus)
	if status == "" {
		status = ab.Status.Status
	}
	ab.Labels[LabelAppBackupStatus] = status

	// DisasterPolicy 关联标签（用于策略删除保护）
	if ab.Spec.DisasterPolicy != "" {
		ab.Labels[LabelDisasterPolicyName] = ab.Spec.DisasterPolicy
	} else {
		delete(ab.Labels, LabelDisasterPolicyName)
	}

	// 通用依赖标签
	_, _, _ = EnsureDependencyTokenLabel(ab.Labels, string(ab.UID))
	edges := make([]DependencyEdge, 0, 3)

	if ab.Spec.Cluster != "" {
		cluster := &disasterv1.Cluster{}
		if err := cli.Get(ctx, types.NamespacedName{Name: ab.Spec.Cluster}, cluster); err == nil {
			edges = append(edges, DependencyEdge{
				TargetToken:  BuildDependencyToken(string(cluster.UID)),
				RelationCode: "spec.cluster",
			})
		}
	}

	if ab.Spec.DisasterPolicy != "" {
		policy := &disasterv1.DisasterPolicy{}
		if err := cli.Get(ctx, types.NamespacedName{Namespace: ab.Namespace, Name: ab.Spec.DisasterPolicy}, policy); err == nil {
			edges = append(edges, DependencyEdge{
				TargetToken:  BuildDependencyToken(string(policy.UID)),
				RelationCode: "spec.disasterPolicy",
			})
		}
	}

	if ab.Spec.Template.StorageLocation != "" {
		sr := &disasterv1.StorageRepository{}
		srKey := types.NamespacedName{Namespace: ab.Namespace, Name: ab.Spec.Template.StorageLocation}
		err := cli.Get(ctx, srKey, sr)
		if apierrors.IsNotFound(err) {
			err = cli.Get(ctx, types.NamespacedName{Namespace: ManagementNamespace(), Name: ab.Spec.Template.StorageLocation}, sr)
		}
		if err == nil {
			edges = append(edges, DependencyEdge{
				TargetToken:  BuildDependencyToken(string(sr.UID)),
				RelationCode: "spec.template.storageLocation",
			})
		}
	}
	_, _ = RebuildDependencyToLabels(ab.Labels, edges)

	return nil
}

func (r *AppBackupReconciler) GenBackupName(ab *disasterv1.AppBackup) string {
	if ab.Spec.Action != nil {
		return GenVeleroBackupName(ab.Name, ab.Spec.Action.RequestAt.Time)
	}

	return GenVeleroBackupName(ab.Name, ab.CreationTimestamp.Time)
}

func (r *AppBackupReconciler) GenScheduleName(ab *disasterv1.AppBackup) string {
	return "app-schedule-" + ab.Name
}

// CreateVeleroBackup creates a Velero Backup resource
func (r *AppBackupReconciler) CreateVeleroBackup(ctx context.Context, cli client.Client, ab *disasterv1.AppBackup, storageRepository string, nameOverride string) (*velerov1.Backup, bool, error) {
	logger := logf.FromContext(ctx)
	r.Recorder.Event(ab, corev1.EventTypeNormal, "CreateVeleroBackup", "Start creating Velero Backup")
	backupName := nameOverride
	if backupName == "" {
		backupName = r.GenBackupName(ab)
	}
	backup := &velerov1.Backup{}
	err := cli.Get(ctx, types.NamespacedName{Name: backupName, Namespace: VeleroNamespace}, backup)
	if apierrors.IsNotFound(err) {
		logger.Info("Velero Backup not found")
		annotations := make(map[string]string)
		backupType := appBackupTypeLabelValue(ab)
		labels := map[string]string{
			LabelAppBackupName:             ab.Name,
			LabelAppBackupUID:              string(ab.UID),
			LabelAppBackupIncludeNamespace: NamSpaceLabels(ab.Spec.Template.IncludedNamespaces),
			LabelAppBackupCluster:          ab.Spec.Cluster,
			LabelAppBackupType:             backupType,
		}
		labels, _ = EnsureCleanupLabels(labels, CleanupDescriptor{
			OwnerUID:     string(ab.UID),
			RelationCode: "finalizer.veleroBackup",
			Strategy:     CleanupStrategyDeleteRequest,
		})
		if traceID, ok := ab.Annotations[AnnotationTraceID]; ok {
			annotations[AnnotationTraceID] = traceID
			labels[AnnotationTraceID] = traceID
		}
		backup := &velerov1.Backup{
			ObjectMeta: metav1.ObjectMeta{
				Name:        backupName,
				Namespace:   VeleroNamespace,
				Annotations: annotations,
				Labels:      labels,
			},
			Spec: ab.Spec.Template,
		}
		backup.Spec.StorageLocation = storageRepository
		// Fix: Velero fails if LabelSelector is empty but not nil (FormatLabelSelector returns "<none>")
		if backup.Spec.LabelSelector != nil && len(backup.Spec.LabelSelector.MatchLabels) == 0 && len(backup.Spec.LabelSelector.MatchExpressions) == 0 {
			backup.Spec.LabelSelector = nil
		}

		// if err := controllerutil.SetControllerReference(ab, backup, r.Scheme); err != nil {
		// 	logger.Error(err, "error setting controller reference")
		// 	return nil, err
		// }
		err = cli.Create(ctx, backup)
		if err != nil {
			r.Recorder.Event(ab, corev1.EventTypeWarning, "CreateVeleroBackup", "Create Velero Backup failed")
			logger.Error(err, "error creating Velero Backup")
			return nil, false, err
		}
		r.Recorder.Event(ab, corev1.EventTypeNormal, "CreateVeleroBackup", "Velero Backup created successfully")
		logger.Info("Velero Backup created")

		// Ensure history is updated/reset even if it existed before (e.g. Retry with same name)
		if ab.Status.History == nil {
			ab.Status.History = make([]disasterv1.BackupRecord, 0)
		}
		found := false
		for i, rec := range ab.Status.History {
			if rec.Name == backupName {
				// Reset status for reused name (Retry scenario)
				ab.Status.History[i].ManagedStatus = disasterv1.LastBackupStatusInProgress
				ab.Status.History[i].Phase = string(velerov1.BackupPhaseNew)
				ab.Status.History[i].StartTimestamp = &metav1.Time{Time: time.Now()}
				// Clear failure/cancel reasons if any
				ab.Status.History[i].Errors = 0
				ab.Status.History[i].Warnings = 0
				found = true
				break
			}
		}
		if !found {
			// Optional: Pre-fill history for immediate feedback, though syncStatus will do it too.
			// Let's do it for responsiveness
			ab.Status.History = append([]disasterv1.BackupRecord{{
				Name:           backupName,
				Phase:          string(velerov1.BackupPhaseNew),
				ManagedStatus:  disasterv1.LastBackupStatusInProgress,
				StartTimestamp: &metav1.Time{Time: time.Now()},
			}}, ab.Status.History...)
		}
		ab.Status.LatestBackupStatus = disasterv1.LastBackupStatusInProgress

		return backup, true, nil
	}
	if err != nil {
		r.Recorder.Event(ab, corev1.EventTypeWarning, "CreateVeleroBackup", "Get Velero Backup failed")
		logger.Error(err, "error getting Velero Backup")
		return nil, false, err
	}

	// Backfill window: historical Velero Backups may not have cleanup labels.
	// The spec requires "create or update" to write these labels, so we ensure them
	// whenever we encounter an existing Backup.
	if labels, changed := EnsureCleanupLabels(backup.Labels, CleanupDescriptor{
		OwnerUID:     string(ab.UID),
		RelationCode: "finalizer.veleroBackup",
		Strategy:     CleanupStrategyDeleteRequest,
	}); changed {
		backup.Labels = labels
		if err := cli.Update(ctx, backup); err != nil {
			// Best-effort: do not block backup flow due to label write failures.
			r.Recorder.Event(ab, corev1.EventTypeWarning, "UpdateVeleroBackupCleanupLabelsFailed", err.Error())
			logger.Error(err, "failed to update cleanup labels for existing Velero Backup", "name", backup.Name)
		}
	}

	r.Recorder.Event(ab, corev1.EventTypeNormal, "CreateVeleroBackup", "Velero Backup already exists")
	if ab.Status.History == nil {
		ab.Status.History = make([]disasterv1.BackupRecord, 0)
	}
	normalizedStart := normalizedBackupStartTimestamp(backup)
	aleady := false
	for i, rec := range ab.Status.History {
		if rec.Name == backup.Name {
			// Already recorded
			aleady = true
			ab.Status.History[i].ManagedStatus = disasterv1.LastBackupStatusInProgress
			ab.Status.History[i].Phase = string(backup.Status.Phase)
			ab.Status.History[i].StartTimestamp = normalizedStart
			break
		}
	}
	if !aleady {
		ab.Status.History = append([]disasterv1.BackupRecord{{
			Name:           backup.Name,
			Phase:          string(backup.Status.Phase),
			ManagedStatus:  disasterv1.LastBackupStatusInProgress,
			StartTimestamp: normalizedStart,
		}}, ab.Status.History...)
	}
	ab.Status.LatestBackupStatus = disasterv1.LastBackupStatusInProgress
	return backup, false, nil
}

// CreateVeleroSchedule creates a Velero Schedule resource
func (r *AppBackupReconciler) CreateVeleroSchedule(ctx context.Context, cli client.Client, ab *disasterv1.AppBackup, storageRepository string) (*velerov1.Schedule, bool, error) {
	logger := logf.FromContext(ctx)
	r.Recorder.Event(ab, corev1.EventTypeNormal, "CreateVeleroSchedule", "Start creating Velero Schedule")
	scheduleName := r.GenScheduleName(ab)
	schedule := &velerov1.Schedule{}
	err := cli.Get(ctx, types.NamespacedName{Name: scheduleName, Namespace: VeleroNamespace}, schedule)
	if apierrors.IsNotFound(err) {
		logger.Info("Velero Schedule not found")
		schedule := r.buildDesiredVeleroSchedule(ab, storageRepository)
		// if err := controllerutil.SetControllerReference(ab, schedule, r.Scheme); err != nil {
		// 	logger.Error(err, "error setting controller reference")
		// 	return nil, err
		// }
		err = cli.Create(ctx, schedule)
		if err != nil {
			r.Recorder.Event(ab, corev1.EventTypeWarning, "CreateVeleroSchedule", "Create Velero Schedule failed")
			logger.Error(err, "error creating Velero Schedule")
			return nil, false, err
		}
		r.Recorder.Event(ab, corev1.EventTypeNormal, "CreateVeleroSchedule", "Velero Schedule created successfully")
		logger.Info("Velero Schedule created")
		return schedule, true, nil
	}
	if err != nil {
		r.Recorder.Event(ab, corev1.EventTypeWarning, "CreateVeleroSchedule", "Get Velero Schedule failed")
		logger.Error(err, "error getting Velero Schedule")
		return nil, false, err
	}

	desired := r.buildDesiredVeleroSchedule(ab, storageRepository)

	// Backfill window: historical Schedules (and their templates) may not have cleanup labels.
	// Ensure schedule cleanup labels + template backup cleanup labels so Velero-created backups
	// will also inherit the labels.
	changed := false
	if !reflect.DeepEqual(schedule.Labels, desired.Labels) {
		schedule.Labels = desired.Labels
		changed = true
	}
	if !reflect.DeepEqual(schedule.Annotations, desired.Annotations) {
		schedule.Annotations = desired.Annotations
		changed = true
	}
	if !reflect.DeepEqual(schedule.Spec, desired.Spec) {
		schedule.Spec = desired.Spec
		changed = true
	}
	if changed {
		if err := cli.Update(ctx, schedule); err != nil {
			if rebuildErr := r.rebuildVeleroSchedule(ctx, cli, ab, desired, err); rebuildErr != nil {
				r.Recorder.Event(ab, corev1.EventTypeWarning, "UpdateVeleroScheduleFailed", rebuildErr.Error())
				logger.Error(rebuildErr, "failed to update existing Velero Schedule", "name", schedule.Name)
				return nil, false, rebuildErr
			}
			r.Recorder.Event(ab, corev1.EventTypeNormal, "RebuildVeleroSchedule", "Velero Schedule rebuilt successfully")
			return desired, true, nil
		}
		r.Recorder.Event(ab, corev1.EventTypeNormal, "UpdateVeleroSchedule", "Velero Schedule updated successfully")
	}

	r.Recorder.Event(ab, corev1.EventTypeNormal, "CreateVeleroSchedule", "Velero Schedule already exists")
	return schedule, false, nil
}

func (r *AppBackupReconciler) buildDesiredVeleroSchedule(ab *disasterv1.AppBackup, storageRepository string) *velerov1.Schedule {
	scheduleName := r.GenScheduleName(ab)
	// Fix: Velero fails if LabelSelector is empty but not nil (FormatLabelSelector returns "<none>")
	// Inject labels into the template so Velero propagates them to the created Backups.
	if ab.Spec.Template.LabelSelector != nil && len(ab.Spec.Template.LabelSelector.MatchLabels) == 0 && len(ab.Spec.Template.LabelSelector.MatchExpressions) == 0 {
		ab.Spec.Template.LabelSelector = nil
	}
	template := ab.Spec.Template
	template.StorageLocation = storageRepository

	if template.Metadata.Labels == nil {
		template.Metadata.Labels = make(map[string]string)
	}
	backupType := appBackupTypeLabelValue(ab)
	template.Metadata.Labels[LabelAppBackupName] = ab.Name
	template.Metadata.Labels[LabelAppBackupUID] = string(ab.UID)
	template.Metadata.Labels[LabelAppBackupIncludeNamespace] = NamSpaceLabels(ab.Spec.Template.IncludedNamespaces)
	template.Metadata.Labels[LabelAppBackupCluster] = ab.Spec.Cluster
	template.Metadata.Labels[LabelAppBackupType] = backupType
	template.Metadata.Labels, _ = EnsureCleanupLabels(template.Metadata.Labels, CleanupDescriptor{
		OwnerUID:     string(ab.UID),
		RelationCode: "finalizer.veleroBackup",
		Strategy:     CleanupStrategyDeleteRequest,
	})

	annotations := make(map[string]string)
	labels := map[string]string{
		LabelAppBackupName:             ab.Name,
		LabelAppBackupUID:              string(ab.UID),
		LabelAppBackupIncludeNamespace: NamSpaceLabels(ab.Spec.Template.IncludedNamespaces),
		LabelAppBackupType:             backupType,
	}
	labels, _ = EnsureCleanupLabels(labels, CleanupDescriptor{
		OwnerUID:     string(ab.UID),
		RelationCode: "finalizer.veleroSchedule",
		Strategy:     CleanupStrategyDelete,
	})

	if traceID, ok := ab.Annotations[AnnotationTraceID]; ok {
		annotations[AnnotationTraceID] = traceID
		labels[AnnotationTraceID] = traceID
		// Propagate trace_id to template labels as well, since Velero Schedule Template doesn't support Annotations.
		template.Metadata.Labels[AnnotationTraceID] = traceID
	}

	return &velerov1.Schedule{
		ObjectMeta: metav1.ObjectMeta{
			Name:        scheduleName,
			Namespace:   VeleroNamespace,
			Annotations: annotations,
			Labels:      labels,
		},
		Spec: velerov1.ScheduleSpec{
			Schedule:                   veleroScheduleExpression(ab.Spec.Schedule),
			Template:                   template,
			Paused:                     ab.Spec.Paused,
			UseOwnerReferencesInBackup: ab.Spec.UseOwnerReferencesInBackup,
			SkipImmediately:            ab.Spec.SkipImmediately,
		},
	}
}

func (r *AppBackupReconciler) rebuildVeleroSchedule(ctx context.Context, cli client.Client, ab *disasterv1.AppBackup, desired *velerov1.Schedule, updateErr error) error {
	existing := &velerov1.Schedule{}
	key := types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}
	if err := cli.Get(ctx, key, existing); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("update Velero Schedule failed: %v; get before rebuild failed: %w", updateErr, err)
	} else if err == nil {
		if err := cli.Delete(ctx, existing); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("update Velero Schedule failed: %v; delete before rebuild failed: %w", updateErr, err)
		}
	}
	if err := cli.Create(ctx, desired); err != nil {
		return fmt.Errorf("update Velero Schedule failed: %v; recreate failed: %w", updateErr, err)
	}
	r.Recorder.Event(ab, corev1.EventTypeNormal, "RebuildVeleroSchedule", "Velero Schedule rebuilt after update failure")
	return nil
}

func (r *AppBackupReconciler) deleteExternalResources(ctx context.Context, cli client.Client, ab *disasterv1.AppBackup) error {
	logger := logf.FromContext(ctx)
	logger.Info("Deleting external resources for AppBackup", "name", ab.Name, "uid", ab.UID)

	// 1. Delete Schedule FIRST to stop new backups
	scheduleList := &velerov1.ScheduleList{}
	err := cli.List(ctx, scheduleList, client.Limit(1))
	if err != nil {
		if meta.IsNoMatchError(err) {
			logger.Info("Velero Schedule CRD not available, skipping Schedule deletion")
		} else {
			logger.Error(err, "failed to check Velero Schedule CRD availability")
			return err
		}
	} else {
		err = cli.DeleteAllOf(ctx, &velerov1.Schedule{},
			client.InNamespace(VeleroNamespace),
			client.MatchingLabels{LabelAppBackupUID: string(ab.UID)},
		)
		if err != nil {
			logger.Error(err, "failed to delete Velero Schedule by UID")
			return err
		}
	}

	// 2. Delete Backups
	backupList := &velerov1.BackupList{}
	err = cli.List(ctx, backupList, client.Limit(1))
	if err != nil {
		if meta.IsNoMatchError(err) {
			logger.Info("Velero Backup CRD not available, skipping Backup deletion")
			return nil
		}
		logger.Error(err, "failed to check Velero Backup CRD availability")
		return err
	}

	backups, _, err := r.ListAppBackups(ctx, cli, ab)
	if err != nil {
		if meta.IsNoMatchError(err) {
			return nil
		}
		return err
	}

	if len(backups) == 0 {
		logger.Info("No backups found to delete")
		return nil
	}

	// Issue Delete Requests
	for _, backup := range backups {
		if backup.DeletionTimestamp != nil {
			continue // Already deleting
		}
		deleteReq := &velerov1.DeleteBackupRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:      backup.Name + "-" + fmt.Sprintf("%d", time.Now().Unix()), // Unique name
				Namespace: VeleroNamespace,
			},
			Spec: velerov1.DeleteBackupRequestSpec{
				BackupName: backup.Name,
			},
		}
		err = cli.Create(ctx, deleteReq)
		if err != nil {
			// Ignore if request already exists (unlikely with timestamp) or backup not found
			if !apierrors.IsAlreadyExists(err) && !apierrors.IsNotFound(err) {
				logger.Error(err, "failed to create DeleteBackupRequest", "backupName", backup.Name)
			}
		}
	}

	// 3. WAIT for deletion with timeout and FORCE removal
	return r.waitForBackupDeletion(ctx, cli, ab)
}

// waitForBackupDeletion waits for backups to be deleted, forcing finalizer removal on timeout
func (r *AppBackupReconciler) waitForBackupDeletion(ctx context.Context, cli client.Client, ab *disasterv1.AppBackup) error {
	logger := logf.FromContext(ctx)
	// Optimize: Wait for shorter time (Velero usually deletes quickly if healthy)
	timeout := 5 * time.Second
	pollInterval := 500 * time.Millisecond

	// Create a child context for timeout
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	logger.Info("Waiting for backups to be deleted", "timeout", timeout)

	for {
		select {
		case <-waitCtx.Done():
			// Timeout reached: FORCE DELETE
			logger.Info("Timeout waiting for backup deletion, forcing cleanup", "appBackup", ab.Name)

			backups, _, err := r.ListAppBackups(ctx, cli, ab)
			if err != nil {
				return err
			}

			for _, backup := range backups {
				if len(backup.Finalizers) > 0 {
					logger.Info("Forcing finalizer removal", "backup", backup.Name)
					patch := client.MergeFrom(backup.DeepCopy())
					backup.Finalizers = nil // Clear finalizers
					if err := cli.Patch(ctx, &backup, patch); err != nil {
						logger.Error(err, "Failed to remove finalizer", "backup", backup.Name)
					}
				}
				// Force delete CR if still exists
				if err := cli.Delete(ctx, &backup); err != nil && !apierrors.IsNotFound(err) {
					logger.Error(err, "Failed to force delete backup CR", "backup", backup.Name)
				}
			}
			return nil // We did our best to force clean

		case <-time.After(pollInterval):
			// Check if backups are gone
			backups, _, err := r.ListAppBackups(ctx, cli, ab)
			if err != nil {
				logger.Error(err, "Failed to list backups during wait")
				continue
			}
			if len(backups) == 0 {
				logger.Info("All backups deleted successfully")
				return nil
			}
			logger.Info("Waiting for backups to delete...", "remaining", len(backups))
		}
	}
}

// RetryBackup handles the retry logic: delete existing backup if exists, then recreate it
// 重试备份处理逻辑：如果存在则删除现有备份，然后重新创建它
func (r *AppBackupReconciler) RetryBackup(ctx context.Context, cli client.Client, appBackup *disasterv1.AppBackup) (bool, ctrl.Result, error) {
	logger := logf.FromContext(ctx)
	if len(appBackup.Status.History) == 0 {
		logger.Error(fmt.Errorf("no backup history found"), "cannot retry backup")
		return false, ctrl.Result{}, fmt.Errorf("no backup history found")
	}
	latestBackupName := appBackup.Status.History[0].Name

	// Check if backup exists
	backup := &velerov1.Backup{}
	err := cli.Get(ctx, types.NamespacedName{Name: latestBackupName, Namespace: VeleroNamespace}, backup)
	if err == nil {
		// Backup exists, delete it
		r.Recorder.Event(appBackup, corev1.EventTypeNormal, "RetryBackup", fmt.Sprintf("Deleting existing backup %s for retry", latestBackupName))
		if err := cli.Delete(ctx, backup); err != nil {
			logger.Error(err, "failed to delete backup for retry")
			return false, ctrl.Result{}, err
		}
		// Requeue to wait for deletion
		return false, ctrl.Result{RequeueAfter: 2 * time.Second}, nil
	} else if !apierrors.IsNotFound(err) {
		logger.Error(err, "failed to get backup for retry")
		return false, ctrl.Result{}, err
	}

	// Backup is gone (or didn't exist), recreate it with the SAME name
	r.Recorder.Event(appBackup, corev1.EventTypeNormal, "RetryBackup", fmt.Sprintf("Recreating backup %s", latestBackupName))
	bslName := appBackup.Spec.Template.StorageLocation + "-" + appBackup.Spec.Cluster
	newBackup, created, err := r.CreateVeleroBackup(ctx, cli, appBackup, bslName, latestBackupName)
	if err != nil {
		logger.Error(err, "error recreating Velero Backup")
		appBackup.Status.Status = "Failed"
		return false, ctrl.Result{RequeueAfter: 1 * time.Second}, err
	}
	if created {
		appBackup.Status.BackupStatus = newBackup.Status
		if newBackup.Status.Phase != "" {
			appBackup.Status.Status = string(newBackup.Status.Phase)
		}
	}

	return true, ctrl.Result{}, nil
}

func (r *AppBackupReconciler) GetDisasterPolicy(ctx context.Context, cli client.Client, namespace, name string) (*disasterv1.DisasterPolicy, error) {
	policy := &disasterv1.DisasterPolicy{}
	err := cli.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, policy)
	if err != nil {
		return nil, err
	}
	return policy, nil
}

func (r *AppBackupReconciler) syncStatistics(ctx context.Context, appBackup *disasterv1.AppBackup, _ client.Client) error {
	// Calculate snapshot from History
	// We no longer rely on listing remote Velero Backups because Canceled (deleted) backups would be missing.
	// The History is the source of truth for all past executions.
	snapshot := &disasterv1.BackupRestoreStatisticsStatus{
		Statistics: disasterv1.StatisticsData{
			Total: int32(len(appBackup.Status.History)),
		},
	}

	for _, rec := range appBackup.Status.History {
		switch rec.ManagedStatus {
		case disasterv1.LastBackupStatusCompleted:
			snapshot.Statistics.Completed++
		case disasterv1.LastBackupStatusFailed:
			snapshot.Statistics.Failed++
		case disasterv1.LastBackupStatusCanceled:
			snapshot.Statistics.Canceled++
		case disasterv1.LastBackupStatusInProgress:
			snapshot.Statistics.InProgress++
		default:
			// Fallback: check raw Phase if ManagedStatus is somehow empty or unknown
			phase := velerov1.BackupPhase(rec.Phase)
			switch phase {
			case velerov1.BackupPhaseCompleted:
				snapshot.Statistics.Completed++
			case velerov1.BackupPhaseFailed, velerov1.BackupPhasePartiallyFailed:
				snapshot.Statistics.Failed++
			case velerov1.BackupPhaseNew, velerov1.BackupPhaseInProgress:
				snapshot.Statistics.InProgress++
			default:
				if rec.ManagedStatus == "" && rec.Phase == "" {
					// Pending/New without explicit status
					snapshot.Statistics.InProgress++
				} else {
					snapshot.Statistics.Unknown++
				}
			}
		}
	}

	// Get or Create Stats
	scopeRef := disasterv1.ScopeReference{
		APIVersion: appBackup.APIVersion,
		Kind:       appBackup.Kind,
		Name:       appBackup.Name,
		Namespace:  appBackup.Namespace,
		UID:        appBackup.UID,
	}

	labels := map[string]string{
		"testudo.softcdata.com/owner-kind": "AppBackup",
	}

	stats, err := r.StatsHelper.GetOrCreate(ctx, disasterv1.ScopeTypeApp, scopeRef, appBackup.Namespace, labels, appBackup, r.Scheme)
	if err != nil {
		return err
	}

	// Sync
	// Note: We use r.Client (local client) for StatsHelper by default in GetOrCreate signature,
	// but Check if we need target client. Stats exist in the user's cluster (where operator runs),
	// so local client is correct. The previous code passed targetCli but GetOrCreate uses it locally?
	// StatsHelper uses the client passed to NewStatisticsHelper.
	// Wait, StatsHelper.GetOrCreate takes a client if we look at its definition (which we don't see here but usage implies it).
	// Actually, StatsHelper usually manages CRs in the same cluster where Operator runs.
	// If AppBackup runs on management cluster, Stats should be on management cluster.
	// The passed 'cli' was targetCli. If ScopeTypeApp refers to App in Target Cluster?
	// AppBackup is a CR in Management Cluster. Statistics should be in Management Cluster too.
	// The previous code: `r.syncStatistics(ctx, appBackup, targetCli)`
	// Wait, why targetCli? `AppBackup` is in `disaster-system` of the management cluster(?).
	// If `appBackup.Spec.Cluster` is set, it might be managing backups ON a remote cluster,
	// BUT the `BackupRestoreStatistics` CR usually lives alongside the `AppBackup` CR.
	// Let's verify where `BackupRestoreStatistics` is stored.
	// Based on `r.StatsHelper.GetOrCreate(..., appBackup.Namespace, ...)`, it uses AppBackup's namespace.
	// If AppBackup is local, Stats are local.
	// I will assume StatsHelper uses the client initialized in it, or I should use r.Client.
	// The previous code passed `targetCli` to `syncStatistics`.
	// However, `GetOrCreate` and `SyncStats` calls inside `syncStatistics` used `r.StatsHelper`, which was initialized with `r.Client`.
	// Wait, `syncStatistics` signature is `(..., cli client.Client)`.
	// But `r.StatsHelper.GetOrCreate` DOES NOT take a client as first arg (except context).
	// Let's look at `GetOrCreate` call:
	// `stats, err := r.StatsHelper.GetOrCreate(ctx, ...)` -> It uses the client stored in helper.
	// So the `cli` argument (targetCli) was actually UNUSED in the previous implementation logic for CR creation/retrieval!
	// It was only used to list Velero backups: `err := cli.List(ctx, veleroBackups, ...)`
	// Since we are now using History (from AppBackup CR itself), we don't need `cli` anymore.

	return r.StatsHelper.SyncStats(ctx, stats, snapshot, "Sync from AppBackup History")
}

// forceTerminateBackup forces the deletion of the Velero Backup, including removing finalizers if necessary.
// This is used when a backup times out or needs to be forcefully cancelled.
func (r *AppBackupReconciler) forceTerminateBackup(ctx context.Context, cli client.Client, appBackup *disasterv1.AppBackup, backup *velerov1.Backup) error {
	logger := logf.FromContext(ctx)

	// 1. Try to delete conventionally if not already being deleted
	if backup.DeletionTimestamp.IsZero() {
		logger.Info("Deleting Velero Backup", "name", backup.Name)
		if err := cli.Delete(ctx, backup); err != nil {
			if client.IgnoreNotFound(err) != nil {
				logger.Error(err, "failed to delete Velero Backup")
				return err
			}
			// If not found, we are done
			return nil
		}
	}

	// 2. If backup is stuck in terminating state with finalizers, remove them
	if !backup.DeletionTimestamp.IsZero() && len(backup.Finalizers) > 0 {
		logger.Info("Velero Backup is stuck in terminating, removing finalizers", "name", backup.Name)
		patch := client.MergeFrom(backup.DeepCopy())
		backup.Finalizers = nil
		if err := cli.Patch(ctx, backup, patch); err != nil {
			if client.IgnoreNotFound(err) != nil {
				logger.Error(err, "failed to remove finalizers from Velero Backup")
				return err
			}
		}
	}
	return nil
}

// isVeleroBackupRunning checks if the Velero Backup is in a running (non-terminal) state
func isVeleroBackupRunning(phase velerov1.BackupPhase) bool {
	switch phase {
	case velerov1.BackupPhaseCompleted, velerov1.BackupPhaseFailed, velerov1.BackupPhasePartiallyFailed,
		velerov1.BackupPhaseDeleting, velerov1.BackupPhaseFailedValidation:
		return false
	case velerov1.BackupPhaseNew, velerov1.BackupPhaseInProgress, velerov1.BackupPhaseWaitingForPluginOperations,
		velerov1.BackupPhaseFinalizing, velerov1.BackupPhaseFinalizingPartiallyFailed,
		velerov1.BackupPhaseWaitingForPluginOperationsPartiallyFailed:
		return true
	default:
		// Unknown or empty phase still means the Backup is not terminal.
		return true
	}
}
