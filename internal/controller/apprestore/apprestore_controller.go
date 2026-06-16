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

//+kubebuilder:rbac:groups=testudo.softcdata.com,resources=apprestores,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=testudo.softcdata.com,resources=apprestores/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=testudo.softcdata.com,resources=apprestores/finalizers,verbs=update
//+kubebuilder:rbac:groups=testudo.softcdata.com,resources=backuprestorestatisticses,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=testudo.softcdata.com,resources=backuprestorestatisticses,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=testudo.softcdata.com,resources=backuprestorestatisticses/status,verbs=get;update;patch
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete

package apprestore

import (
	"context"
	"fmt"
	"reflect"

	"github.com/softcdata/testudo-operator/internal/controller"
	. "github.com/softcdata/testudo-operator/internal/controller"
	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	. "github.com/softcdata/testudo-operator/pkg/metadata"
	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	corev1api "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlcontroller "sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/softcdata/testudo-operator/pkg/helper"
)

// AppRestoreReconciler reconciles a AppRestore object
type AppRestoreReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	Recorder      record.EventRecorder
	ClientFactory ClientFactory
	StatsHelper   helper.StatisticsHelper
	// restoreRuntimeOptions customizes restoring runtime behavior.
	// Configure via NewAppRestoreReconciler(..., WithRestoreRuntime(...)).
	restoreRuntimeOptions []RestoreRuntimeOption
}

func (r *AppRestoreReconciler) restoreRuntimeConfig() RestoreRuntimeConfig {
	return NewRestoreRuntimeConfig(r.restoreRuntimeOptions...)
}

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *AppRestoreReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)

	// Fetch the AppRestore instance
	var appRestore disasterv1.AppRestore
	if err := r.Get(ctx, req.NamespacedName, &appRestore); err != nil {
		logger.Info("unable to fetch AppRestore", "error", err)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	logger = logger.WithValues("TraceIDKey", appRestore.Annotations[AnnotationTraceID])
	ctx = context.WithValue(ctx, TraceIDKey, appRestore.Annotations[AnnotationTraceID])
	ctx = logf.IntoContext(ctx, logger)
	// Handle Deletion
	if !appRestore.ObjectMeta.DeletionTimestamp.IsZero() {
		logger.Info("AppRestore is being deleted", "deletionTimestamp", appRestore.ObjectMeta.DeletionTimestamp)

		handler := &DeletingHandler{}
		_, res, err := handler.Handle(ctx, r, &appRestore)
		if err != nil {
			return res, err
		}

		// Re-fetch the object to ensure we have the latest version before removing finalizer
		// This is crucial because deleteExternalResources might take time, and the object
		// could have been updated in the meantime.
		if err := r.Get(ctx, req.NamespacedName, &appRestore); err != nil {
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}

		// Capture state for patch
		original := appRestore.DeepCopy()

		// Remove Finalizer (if it exists)
		if controllerutil.ContainsFinalizer(&appRestore, LabelAppRestoreFinalizer) {
			controllerutil.RemoveFinalizer(&appRestore, LabelAppRestoreFinalizer)
			// Use Patch instead of Update to avoid conflicts and UID issues
			if err := r.Patch(ctx, &appRestore, client.MergeFrom(original)); err != nil {
				return ctrl.Result{}, client.IgnoreNotFound(err)
			}
		}

		return res, nil
	}

	// 保存原始状态用于后续比较，避免不必要的更新冲突
	original := appRestore.DeepCopy()

	if appRestore.Status.Status == "" {
		appRestore.Status.Status = disasterv1.PhasePending
	}
	phase := disasterv1.AppRestorePhase(appRestore.Status.Status)
	// 根据状态选择处理器
	//apprestore没有管理状态，即就绪状态，成功或失败后，不进行处理
	var handler StateHandler
	switch phase {
	case disasterv1.PhasePending:
		handler = &PendingHandler{}
	case disasterv1.PhaseRestoring:
		handler = &RestoringHandler{}
	case disasterv1.PhaseSucceeded:
		handler = &SucceededHandler{}
	case disasterv1.PhaseFailed:
		handler = &FailedHandler{}
	case disasterv1.PhasePartiallyFailed:
		handler = &PartiallyFailedHandler{}
	case disasterv1.PhaseCancelled:
		handler = &CancelledHandler{}
	case disasterv1.PhaseDeleting:
		handler = &DeletingHandler{}
	case disasterv1.PhaseInitiating:
		handler = &InitiatingHandler{}
	default:
		err := fmt.Errorf("unknown phase: %s", phase)
		logger.Error(err, "unknown phase encountered")
		return ctrl.Result{}, err
	}
	// 执行处理器逻辑
	nextPhase, result, handlerErr := handler.Handle(ctx, r, &appRestore)
	if handlerErr != nil {
		r.Recorder.Event(&appRestore, corev1.EventTypeWarning, "ReconcileError", handlerErr.Error())
		if appRestore.Status.Reason == "" {
			helper.SetStatusError(&appRestore.Status, "ReconcileError", handlerErr.Error())
		}
		if appRestore.Status.Message == "" {
			helper.SetStatusError(&appRestore.Status, appRestore.Status.Reason, handlerErr.Error())
		}
	} else {
		// Preserve handler-assigned failure details when entering failed terminal phases.
		// Clear stale errors when current/next phase is not failed.
		resolvedPhase := phase
		if nextPhase != "" {
			resolvedPhase = nextPhase
		}
		if !disasterv1.IsFailedAppRestorePhase(resolvedPhase) {
			helper.ClearStatusError(&appRestore.Status)
		}
	}

	if nextPhase != "" && nextPhase != phase {
		r.Recorder.Eventf(&appRestore, corev1.EventTypeNormal, "PhaseChange", "Phase transitioned from %s to %s", appRestore.Status.Status, nextPhase)
		appRestore.Status.Status = disasterv1.AppRestorePhase(nextPhase)
	}

	// ------------------------------------------------------------------
	// 核心修复：分离 Status 更新和 Metadata 更新，避免 ResourceVersion 冲突
	// ------------------------------------------------------------------

	// 1. 优先检查并更新 Status
	// 如果 Status 发生了变化，我们只更新 Status 并返回。
	// 这会触发新的 Reconcile，在下一次循环中再处理 Metadata 的更新。
	if !reflect.DeepEqual(original.Status, appRestore.Status) {
		if err := r.Status().Update(ctx, &appRestore); err != nil {
			if handlerErr != nil {
				logger.Error(err, "failed to update status", "handlerError", handlerErr)
				return result, handlerErr
			}
			return ctrl.Result{}, err
		}
		// Status 更新成功，直接返回，等待下一次 Reconcile
		return result, handlerErr
	}

	// 2. 如果 Status 没有变化，检查并更新 Metadata (Labels, Finalizers)
	if err := r.syncLabels(ctx, &appRestore); err != nil {
		logger.Error(err, "failed to sync labels")
	}

	// 检查 Metadata 是否有变化 (Labels/Annotations 或 Finalizers)
	if !reflect.DeepEqual(original.ObjectMeta.Labels, appRestore.ObjectMeta.Labels) ||
		!reflect.DeepEqual(original.ObjectMeta.Annotations, appRestore.ObjectMeta.Annotations) ||
		!reflect.DeepEqual(original.ObjectMeta.Finalizers, appRestore.ObjectMeta.Finalizers) {
		if err := r.Update(ctx, &appRestore); err != nil {
			if handlerErr != nil {
				logger.Error(err, "failed to update resource", "handlerError", handlerErr)
				return result, handlerErr
			}
			return ctrl.Result{}, err
		}
	}

	// Sync Statistics
	if appRestore.Spec.Cluster != "" {
		targetCli, err := r.ClientFactory.GetKubeClient(ctx, r.Client, r.Scheme, appRestore.Spec.Cluster)
		if err == nil {
			if err := r.syncStatistics(ctx, &appRestore, targetCli); err != nil {
				logger.Error(err, "Failed to sync statistics")
			}
		}
	}

	return result, handlerErr
}

func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

func removeString(slice []string, s string) []string {
	result := []string{}
	for _, item := range slice {
		if item != s {
			result = append(result, item)
		}
	}
	return result
}

func (r *AppRestoreReconciler) syncLabels(ctx context.Context, appRestore *disasterv1.AppRestore) error {
	// 计算期望的标签
	if appRestore.Labels == nil {
		appRestore.Labels = make(map[string]string)
	}
	appRestore.Labels[LabelAppRestoreName] = appRestore.Name
	// appRestore.Labels[LabelAppRestoreNamespace] = appRestore.Namespace
	appRestore.Labels[LabelAppRestoreCluster] = appRestore.Spec.Cluster
	appRestore.Labels[LabelAppRestoreSource] = appRestore.Spec.BackupSource
	appRestore.Labels[LabelAppRestoreStatus] = string(appRestore.Status.Status)
	// 来源标签（user / disaster-instance）
	_ = EnsureAppResourceOriginLabels(appRestore.Labels, appRestore.OwnerReferences)

	// Ensure we delete the obsolete dynamic label so it doesn't trigger endless updates
	delete(appRestore.Labels, LabelAppRestoreUpdated)

	// Ensure LabelAppRestoreSourceType is set from AppBackup
	if appRestore.Labels[LabelAppRestoreSourceType] == "" {
		appBackup := &disasterv1.AppBackup{}
		// Try to find AppBackup in the same namespace with the name from Spec.BackupSource
		if err := r.Get(ctx, types.NamespacedName{Name: appRestore.Spec.BackupSource, Namespace: appRestore.Namespace}, appBackup); err == nil {
			if appBackup.Labels != nil && appBackup.Labels[LabelAppBackupType] != "" {
				appRestore.Labels[LabelAppRestoreSourceType] = appBackup.Labels[LabelAppBackupType]
			}
		} else {
			// Fail silently or log? Since this is a sync function called frequently, avoid spamming logs.
			// Just skipping if not found.
		}
	}

	// 通用依赖标签
	_, _, _ = EnsureDependencyTokenLabel(appRestore.Labels, string(appRestore.UID))
	edges := make([]DependencyEdge, 0, 4)

	if appRestore.Spec.Cluster != "" {
		cluster := &disasterv1.Cluster{}
		if err := r.Get(ctx, types.NamespacedName{Name: appRestore.Spec.Cluster}, cluster); err == nil {
			edges = append(edges, DependencyEdge{
				TargetToken:  BuildDependencyToken(string(cluster.UID)),
				RelationCode: "spec.cluster",
			})
		}
	}

	if appRestore.Spec.BackupSource != "" {
		appBackup := &disasterv1.AppBackup{}
		if err := r.Get(ctx, types.NamespacedName{Namespace: appRestore.Namespace, Name: appRestore.Spec.BackupSource}, appBackup); err == nil {
			edges = append(edges, DependencyEdge{
				TargetToken:  BuildDependencyToken(string(appBackup.UID)),
				RelationCode: "spec.backupSource",
			})
		}
	}

	if appRestore.Spec.SourceCluster != "" {
		sourceCluster := &disasterv1.Cluster{}
		if err := r.Get(ctx, types.NamespacedName{Name: appRestore.Spec.SourceCluster}, sourceCluster); err == nil {
			edges = append(edges, DependencyEdge{
				TargetToken:  BuildDependencyToken(string(sourceCluster.UID)),
				RelationCode: "spec.sourceCluster",
			})
		}
	}

	if appRestore.Spec.StorageRepository != "" {
		sr := &disasterv1.StorageRepository{}
		err := r.Get(ctx, types.NamespacedName{Namespace: appRestore.Namespace, Name: appRestore.Spec.StorageRepository}, sr)
		if apierrors.IsNotFound(err) {
			err = r.Get(ctx, types.NamespacedName{Namespace: ManagementNamespace(), Name: appRestore.Spec.StorageRepository}, sr)
		}
		if err == nil {
			edges = append(edges, DependencyEdge{
				TargetToken:  BuildDependencyToken(string(sr.UID)),
				RelationCode: "spec.storageRepository",
			})
		}
	}

	_, _ = RebuildDependencyToLabels(appRestore.Labels, edges)
	return nil
}

// GenRestoreName generates a name for the Velero Restore resource
func (r *AppRestoreReconciler) GenRestoreName(appRestore *disasterv1.AppRestore) string {
	return "res-" + appRestore.Name
}

func (r *AppRestoreReconciler) deleteExternalResources(ctx context.Context, cli client.Client, ar *disasterv1.AppRestore) error {
	log := logf.FromContext(ctx)
	log.Info("Deleting external resources for AppRestore", "name", ar.Name, "uid", ar.UID)

	// Delete all associated Restores using DeleteAllOf
	// Only delete restores that belong to this specific AppRestore instance (by UID)
	err := cli.DeleteAllOf(ctx, &velerov1.Restore{},
		client.InNamespace(VeleroNamespace),
		client.MatchingLabels{LabelAppRestoreUID: string(ar.UID)},
	)
	if err != nil {
		log.Error(err, "failed to delete associated Velero Restores by UID")
		return err
	}

	// Delete associated ResourceModifier ConfigMaps
	cmManager := NewConfigMapManager(cli)
	if err := cmManager.DeleteConfigMap(ctx, ar); err != nil {
		log.Error(err, "failed to delete resource modifier configmap")
		return err
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *AppRestoreReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.Recorder == nil {
		r.Recorder = mgr.GetEventRecorderFor("apprestore-controller")
	}
	if r.ClientFactory == nil {
		r.ClientFactory = &controller.DefaultClientFactory{}
	}
	r.StatsHelper = helper.NewStatisticsHelper(r.Client)
	return ctrl.NewControllerManagedBy(mgr).
		For(&disasterv1.AppRestore{}).
		WithOptions(ctrlcontroller.Options{MaxConcurrentReconciles: 8}).
		// 监听 Velero Restore 资源的变化
		// Watches(
		// 	&velerov1.Restore{},
		// 	handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []reconcile.Request {
		// 		restore := o.(*velerov1.Restore)
		// 		uid := restore.Labels[AppRestoreUIDLabel]
		// 		if uid == "" {
		// 			return nil
		// 		}

		// 		// Find AppRestore by UID
		// 		list := &disasterv1.AppRestoreList{}
		// 		if err := mgr.GetClient().List(ctx, list); err != nil {
		// 			return nil
		// 		}
		// 		// 找到管理该 Restore 的 AppRestore 资源
		// 		for _, ar := range list.Items {
		// 			if string(ar.UID) == uid {
		// 				// 当管理状态与实际状态不同步时触发重新调度
		// 				if string(ar.Status.RestoreStatus.Phase) != string(restore.Status.Phase) {
		// 					return []reconcile.Request{{NamespacedName: types.NamespacedName{
		// 						Name:      ar.Name,
		// 						Namespace: ar.Namespace,
		// 					}}}
		// 				}
		// 			}
		// 		}
		// 		return nil
		// 	}),
		// ).
		// WithEventFilter(IgnoreStatusUpdatesPredicate{}). // 忽略status更新
		Named("apprestore").
		Complete(r)
}

// forceTerminateRestore forces the deletion of the Velero Restore, including removing finalizers if necessary.
func (r *AppRestoreReconciler) forceTerminateRestore(ctx context.Context, cli client.Client, appRestore *disasterv1.AppRestore, restore *velerov1.Restore) error {
	logger := logf.FromContext(ctx)

	// 1. Try to delete conventionally
	if restore.DeletionTimestamp.IsZero() {
		logger.Info("Deleting Velero Restore", "name", restore.Name)
		if err := cli.Delete(ctx, restore); err != nil {
			if client.IgnoreNotFound(err) != nil {
				logger.Error(err, "failed to delete Velero Restore")
				return err
			}
			// If not found, we are done
			return nil
		}
	}

	// 2. Check if it's still there (wait a bit? No, reconciliation will retry. But for robustness we check immediately if possible or rely on next reconcile)
	// We'll perform a patch to remove finalizers if it's already in deleting state but stuck
	if !restore.DeletionTimestamp.IsZero() && len(restore.Finalizers) > 0 {
		logger.Info("Velero Restore is stuck in terminating, removing finalizers", "name", restore.Name)
		patch := client.MergeFrom(restore.DeepCopy())
		restore.Finalizers = nil
		if err := cli.Patch(ctx, restore, patch); err != nil {
			if client.IgnoreNotFound(err) != nil {
				logger.Error(err, "failed to remove finalizers from Velero Restore")
				return err
			}
		}
	}
	return nil
}

// cleanupPendingRestoredResources cleans up Pending resources created by a Velero Restore.
// It deletes controllers first (Deployment, StatefulSet, Job) to prevent Pod recreation,
// then cleans up standalone Pods and Pending PVCs.
func (r *AppRestoreReconciler) cleanupPendingRestoredResources(ctx context.Context, cli client.Client, appRestore *disasterv1.AppRestore, restoreName string) error {
	logger := logf.FromContext(ctx)

	// Get target namespaces from AppRestore spec
	namespaces, _ := r.getTargetNamespaces(ctx, appRestore)
	if len(namespaces) == 0 {
		logger.Info("No target namespaces found, skipping pending resources cleanup")
		return nil
	}

	labelSelector := client.MatchingLabels{
		"velero.io/restore-name": restoreName,
	}

	var cleanedCount int

	for _, ns := range namespaces {
		// 1. Delete controllers with restore label that have no Ready pods (failed to start)
		// 1.1 Deployments
		deployList := &appsv1.DeploymentList{}
		if err := cli.List(ctx, deployList, client.InNamespace(ns), labelSelector); err == nil {
			for i := range deployList.Items {
				deploy := &deployList.Items[i]
				// Only delete if it has no Ready replicas (failed to start)
				if deploy.Status.ReadyReplicas == 0 && deploy.Spec.Replicas != nil && *deploy.Spec.Replicas > 0 {
					if err := cli.Delete(ctx, deploy); err == nil {
						cleanedCount++
						logger.Info("Deleted pending Deployment", "name", deploy.Name, "namespace", ns)
					} else {
						logger.Error(err, "Failed to delete Deployment", "name", deploy.Name, "namespace", ns)
					}
				}
			}
		}

		// 1.2 StatefulSets
		stsList := &appsv1.StatefulSetList{}
		if err := cli.List(ctx, stsList, client.InNamespace(ns), labelSelector); err == nil {
			for i := range stsList.Items {
				sts := &stsList.Items[i]
				if sts.Status.ReadyReplicas == 0 && sts.Spec.Replicas != nil && *sts.Spec.Replicas > 0 {
					if err := cli.Delete(ctx, sts); err == nil {
						cleanedCount++
						logger.Info("Deleted pending StatefulSet", "name", sts.Name, "namespace", ns)
					} else {
						logger.Error(err, "Failed to delete StatefulSet", "name", sts.Name, "namespace", ns)
					}
				}
			}
		}

		// 1.3 Jobs (delete failed jobs)
		jobList := &batchv1.JobList{}
		if err := cli.List(ctx, jobList, client.InNamespace(ns), labelSelector); err == nil {
			for i := range jobList.Items {
				job := &jobList.Items[i]
				// Delete jobs that haven't succeeded
				if job.Status.Succeeded == 0 {
					propagation := metav1.DeletePropagationBackground
					if err := cli.Delete(ctx, job, &client.DeleteOptions{
						PropagationPolicy: &propagation,
					}); err == nil {
						cleanedCount++
						logger.Info("Deleted pending/failed Job", "name", job.Name, "namespace", ns)
					} else {
						logger.Error(err, "Failed to delete Job", "name", job.Name, "namespace", ns)
					}
				}
			}
		}

		// 2. Delete standalone Pending Pods (no OwnerReferences)
		podList := &corev1.PodList{}
		if err := cli.List(ctx, podList, client.InNamespace(ns), labelSelector); err == nil {
			for i := range podList.Items {
				pod := &podList.Items[i]
				if pod.Status.Phase == corev1.PodPending {
					// Only delete standalone pods (no controller)
					if len(pod.OwnerReferences) == 0 {
						if err := cli.Delete(ctx, pod); err == nil {
							cleanedCount++
							logger.Info("Deleted standalone pending Pod", "name", pod.Name, "namespace", ns)
						} else {
							logger.Error(err, "Failed to delete Pod", "name", pod.Name, "namespace", ns)
						}
					}
				}
			}
		}

		// 3. Delete Pending PVCs
		pvcList := &corev1.PersistentVolumeClaimList{}
		if err := cli.List(ctx, pvcList, client.InNamespace(ns), labelSelector); err == nil {
			for i := range pvcList.Items {
				pvc := &pvcList.Items[i]
				if pvc.Status.Phase == corev1.ClaimPending {
					if err := cli.Delete(ctx, pvc); err == nil {
						cleanedCount++
						logger.Info("Deleted pending PVC", "name", pvc.Name, "namespace", ns)
					} else {
						logger.Error(err, "Failed to delete PVC", "name", pvc.Name, "namespace", ns)
					}
				}
			}
		}
	}

	if cleanedCount > 0 {
		r.Recorder.Eventf(appRestore, corev1.EventTypeNormal, "PendingResourcesCleaned",
			"Cleaned up %d pending resources created by restore %s", cleanedCount, restoreName)
	}
	logger.Info("Pending resources cleanup completed", "cleanedCount", cleanedCount, "restoreName", restoreName)
	return nil
}

// getTargetNamespaces 从 AppRestore spec 提取目标命名空间
// Deprecated: 请使用 calculateTargetNamespaces 以保持逻辑一致性
func (r *AppRestoreReconciler) getTargetNamespaces(ctx context.Context, appRestore *disasterv1.AppRestore) ([]string, error) {
	return r.calculateTargetNamespaces(ctx, appRestore)
}

func (r *AppRestoreReconciler) calculateTargetNamespaces(ctx context.Context, appRestore *disasterv1.AppRestore) ([]string, error) {
	included := appRestore.Spec.Template.IncludedNamespaces

	// 如果 IncludedNamespaces 为空，尝试从源 AppBackup 获取
	if len(included) == 0 && appRestore.Spec.BackupSource != "" {
		appBackup := &disasterv1.AppBackup{}
		// AppBackup 预期在同一命名空间中
		if err := r.Get(ctx, types.NamespacedName{Name: appRestore.Spec.BackupSource, Namespace: appRestore.Namespace}, appBackup); err == nil {
			included = appBackup.Spec.Template.IncludedNamespaces
		} else {
			// 通过 recorder 或 logger 记录错误？目前我们返回潜在的部分失败
			// 但如果我们根本找不到它（可能已被删除），我们不阻止有效流程。
			// 但是，如果找不到源，我们就无法推断命名空间。
			// 仅使用空的 included 继续，这在 Velero 上下文中意味着“全部”，
			// 或者如果我们在 UI 方面严格计算，可能意味着“无”。
		}
	}

	mapping := appRestore.Spec.Template.NamespaceMapping

	if len(mapping) == 0 {
		return included, nil
	}

	targets := make([]string, 0, len(included))
	for _, ns := range included {
		if target, ok := mapping[ns]; ok {
			targets = append(targets, target)
		} else {
			targets = append(targets, ns)
		}
	}
	return targets, nil
}

// createVeleroRestore creates a Velero Restore resource
func (r *AppRestoreReconciler) createVeleroRestore(ctx context.Context, cli client.Client, appRestore *disasterv1.AppRestore) error {
	logger := logf.FromContext(ctx)
	r.Recorder.Event(appRestore, corev1.EventTypeNormal, "CreateVeleroRestore", "Start creating Velero Restore")

	// Ensure ConfigMap for ResourceModifier
	cmManager := NewConfigMapManager(cli)
	cmName, err := cmManager.EnsureConfigMap(ctx, appRestore)
	if err != nil {
		logger.Error(err, "failed to ensure resource modifier configmap")
		return err
	}

	template := appRestore.Spec.Template
	if cmName != "" {
		template.ResourceModifier = &corev1api.TypedLocalObjectReference{
			Kind: "ConfigMap",
			Name: cmName,
		}
	}
	// else {
	// 	// Fallback to default if no rules provided
	// 	template.ResourceModifier = &corev1api.TypedLocalObjectReference{
	// 		Kind: "ConfigMap",
	// 		Name: "clean-pvc-volumename",
	// 	}
	// }

	restoreName := r.GenRestoreName(appRestore)
	restore := &velerov1.Restore{
		ObjectMeta: metav1.ObjectMeta{
			Name:      restoreName,
			Namespace: VeleroNamespace,
			Annotations: map[string]string{
				AnnotationTraceID: appRestore.Annotations[AnnotationTraceID],
			},
			Labels: map[string]string{
				LabelAppRestoreName: appRestore.Name,
				LabelAppRestoreUID:  string(appRestore.UID),
				AnnotationTraceID:   appRestore.Annotations[AnnotationTraceID],
			},
		},
		Spec: template,
	}
	restore.Labels, _ = EnsureCleanupLabels(restore.Labels, CleanupDescriptor{
		OwnerUID:     string(appRestore.UID),
		RelationCode: "finalizer.veleroRestore",
		Strategy:     CleanupStrategyDelete,
	})
	err = cli.Create(ctx, restore)
	if err != nil {
		r.Recorder.Event(appRestore, corev1.EventTypeWarning, "CreateVeleroRestore", "Create Velero Restore failed")
		logger.Error(err, "error creating Velero Restore")
		return err
	}
	r.Recorder.Event(appRestore, corev1.EventTypeNormal, "CreateVeleroRestore", "Velero Restore created successfully")
	logger.Info("Velero Restore created")
	return nil
}

// processAction checks if action is updated and executes the action if needed
func (r *AppRestoreReconciler) processAction(ctx context.Context, cli client.Client, appRestore *disasterv1.AppRestore, restore *velerov1.Restore) (disasterv1.AppRestorePhase, ctrl.Result, error) {
	logger := logf.FromContext(ctx)
	if appRestore.Spec.Action != nil {
		shouldRun := false
		if appRestore.Status.LastAction == nil {
			shouldRun = true
		} else if appRestore.Spec.Action.RequestAt.Time.After(appRestore.Status.LastAction.RequestAt.Time) {
			shouldRun = true
		}
		if shouldRun {
			// 更新 LastAction
			appRestore.Status.LastAction = &disasterv1.RestoreAction{
				Type:      appRestore.Spec.Action.Type,
				RequestAt: appRestore.Spec.Action.RequestAt,
			}

			// 获取用户信息
			user := appRestore.Annotations["testudo.softcdata.com/user"]
			if user == "" {
				user = "system"
			}
			traceID := appRestore.Annotations[AnnotationTraceID]

			switch appRestore.Spec.Action.Type {
			case "cancel":
				// 执行取消动作：在进行中可以进行取消，直接删除底层velero restore,然后进入取消状态
				logger.Info("Cancelling restore")
				restoreName := r.GenRestoreName(appRestore)

				// 取消恢复 Started 事件
				taskName := fmt.Sprintf("应用恢复 %s 取消恢复", appRestore.Name)
				helper.ReportTaskStartedWithClient(ctx, r.Client, r.Scheme, appRestore, taskName, appRestore.Spec.Cluster, user, traceID, "取消恢复开始")

				// Force terminate the Velero Restore
				err := r.forceTerminateRestore(ctx, cli, appRestore, restore)
				if err != nil {
					logger.Error(err, "failed to force terminate Velero Restore for cancellation")
					r.Recorder.Event(appRestore, corev1.EventTypeWarning, "CancelFailed", err.Error())
					// 取消恢复失败事件
					now := metav1.Now()
					errorCode := appRestore.Status.Reason
					if errorCode == "" {
						errorCode = "RestoreActionFailed"
					}
					helper.ReportTaskFinishedWithClient(ctx, r.Client, r.Scheme, appRestore, taskName, appRestore.Spec.Cluster, helper.TaskStatusFailed, &now, &now, user, traceID, "取消恢复失败: "+err.Error(), errorCode)
					return disasterv1.PhaseFailed, ctrl.Result{}, err
				}

				// Clean up pending resources created by the restore
				if err := r.cleanupPendingRestoredResources(ctx, cli, appRestore, restoreName); err != nil {
					logger.Error(err, "failed to cleanup pending restored resources")
					// Don't fail the cancel action, just log the error
				}

				r.Recorder.Event(appRestore, corev1.EventTypeNormal, "RestoreCancelled", "Velero Restore cancelled and pending resources cleaned")

				// 取消恢复成功事件
				now := metav1.Now()
				helper.ReportTaskFinishedWithClient(ctx, r.Client, r.Scheme, appRestore, taskName, appRestore.Spec.Cluster, helper.TaskStatusSuccess, &now, &now, user, traceID, "取消恢复完成")

				appRestore.Status.RestoreStatus = velerov1.RestoreStatus{}
				return disasterv1.PhaseCancelled, ctrl.Result{}, nil
			case "retry":
				// 执行重试动作：删除当前restore，重新创建
				logger.Info("Retrying restore")

				// 重试恢复 Started 事件
				taskName := fmt.Sprintf("应用恢复 %s 重试恢复", appRestore.Name)
				helper.ReportTaskStartedWithClient(ctx, r.Client, r.Scheme, appRestore, taskName, appRestore.Spec.Cluster, user, traceID, "重试恢复开始")

				if appRestore.Status.RestoreStatus.Phase != "" {
					err := cli.Delete(ctx, restore)
					if err != nil {
						logger.Error(err, "failed to delete Velero Restore for retry")
						r.Recorder.Event(appRestore, corev1.EventTypeWarning, "RetryFailed", err.Error())
						// 重试恢复失败事件
						now := metav1.Now()
						errorCode := appRestore.Status.Reason
						if errorCode == "" {
							errorCode = "RestoreActionFailed"
						}
						helper.ReportTaskFinishedWithClient(ctx, r.Client, r.Scheme, appRestore, taskName, appRestore.Spec.Cluster, helper.TaskStatusFailed, &now, &now, user, traceID, "重试恢复失败: "+err.Error(), errorCode)
						return disasterv1.PhaseFailed, ctrl.Result{}, err
					}
					r.Recorder.Event(appRestore, corev1.EventTypeNormal, "RestoreDeleted", "Velero Restore deleted for retry")
					appRestore.Status.RestoreStatus = velerov1.RestoreStatus{}
				}

				// 重试恢复成功事件（开始重试，进入 Pending 状态）
				now := metav1.Now()
				helper.ReportTaskFinishedWithClient(ctx, r.Client, r.Scheme, appRestore, taskName, appRestore.Spec.Cluster, helper.TaskStatusSuccess, &now, &now, user, traceID, "重试恢复已触发")
				return disasterv1.PhasePending, ctrl.Result{Requeue: true}, nil

			}
		}
	}
	return "", ctrl.Result{}, nil
}

// GetBackupSourceInfo retrieves the Velero Backup information for the AppRestore
func (r *AppRestoreReconciler) GetBackupSourceInfo(ctx context.Context, cli client.Client, appRestore *disasterv1.AppRestore) (*velerov1.Backup, error) {
	logger := logf.FromContext(ctx)
	backupSource := &velerov1.Backup{}
	err := cli.Get(ctx, types.NamespacedName{Name: appRestore.Spec.Template.BackupName, Namespace: VeleroNamespace}, backupSource)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			logger.Error(err, "unable to fetch Velero Backup", "backupSource", appRestore.Spec.Template.BackupName)
		}
		return nil, err
	}
	return backupSource, nil
}

func (r *AppRestoreReconciler) syncStatistics(ctx context.Context, appRestore *disasterv1.AppRestore, cli client.Client) error {
	// Calculate snapshot based on AppRestore status
	snapshot := &disasterv1.BackupRestoreStatisticsStatus{
		Statistics: disasterv1.StatisticsData{
			Total: 1,
		},
	}

	switch appRestore.Status.Status {
	case disasterv1.PhaseSucceeded:
		snapshot.Statistics.Completed = 1
	case disasterv1.PhaseFailed, disasterv1.PhasePartiallyFailed:
		snapshot.Statistics.Failed = 1
	case disasterv1.PhaseCancelled:
		snapshot.Statistics.Canceled = 1
	case disasterv1.PhaseRestoring, disasterv1.PhasePending, disasterv1.PhaseInitiating, disasterv1.PhaseDeleting:
		snapshot.Statistics.InProgress = 1
	default:
		if appRestore.Status.Status == "" {
			snapshot.Statistics.InProgress = 1
		} else {
			snapshot.Statistics.Unknown = 1
		}
	}

	// Get or Create Stats
	scopeRef := disasterv1.ScopeReference{
		APIVersion: appRestore.APIVersion,
		Kind:       appRestore.Kind,
		Name:       appRestore.Name,
		Namespace:  appRestore.Namespace,
		UID:        appRestore.UID,
	}

	labels := map[string]string{
		"testudo.softcdata.com/owner-kind": "AppRestore",
	}

	stats, err := r.StatsHelper.GetOrCreate(ctx, disasterv1.ScopeTypeApp, scopeRef, appRestore.Namespace, labels, appRestore, r.Scheme)
	if err != nil {
		return err
	}

	// Sync
	return r.StatsHelper.SyncStats(ctx, stats, snapshot, "Sync from AppRestore Status")
}
