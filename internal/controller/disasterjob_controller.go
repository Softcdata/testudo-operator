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
	"strconv"
	"time"

	. "github.com/softcdata/testudo-operator/pkg/metadata"
	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	"github.com/softcdata/testudo-operator/pkg/tools"
)

const (
	ChangeStorageClassConfig = "change-storage-class-config"
	ChangeImageNameConfig    = "change-image-name-config"
)

// DisasterJobReconciler reconciles a DisasterJob object
type DisasterJobReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=testudo.softcdata.com,resources=disasterjobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=testudo.softcdata.com,resources=disasterjobs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=testudo.softcdata.com,resources=disasterjobs/finalizers,verbs=update
// +kubebuilder:rbac:groups=velero.io,resources=backups,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the DisasterJob object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/reconcile
func (r *DisasterJobReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)

	dj := &disasterv1.DisasterJob{}
	err := r.Get(ctx, req.NamespacedName, dj)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("DisasterJob not found")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "error getting DisasterJob")
		return ctrl.Result{}, err
	}
	logger = logger.WithValues(TraceIDKey, dj.Annotations[AnnotationTraceID])
	ctx = context.WithValue(ctx, TraceIDKey, dj.Annotations[AnnotationTraceID])

	if changed, err := r.syncDependencyLabels(ctx, dj); err != nil {
		logger.Error(err, "failed to sync dependency labels")
		return ctrl.Result{}, err
	} else if changed {
		if err := r.Update(ctx, dj); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
	}

	if dj.Spec.SyncType == "" {
		dj.Spec.SyncType = disasterv1.SyncTypeForward
		err := r.Update(ctx, dj)
		if err != nil {
			logger.Error(err, "error updating DisasterJob")
			return ctrl.Result{RequeueAfter: 1 * time.Second}, err
		}
		return ctrl.Result{RequeueAfter: 1 * time.Second}, err
	}
	defer func() {
		if !dj.ObjectMeta.DeletionTimestamp.IsZero() {
			return
		}
		err = r.Status().Update(ctx, dj)
		if err != nil {
			logger.Error(err, "unable to update DisasterJob status")
		}
	}()
	if dj.Status.Phase == "" {
		dj.Status.Phase = disasterv1.DisasterJobPhasePending
		return ctrl.Result{RequeueAfter: 1 * time.Second}, err
	}

	// 获取 DisasterBackup
	db := &disasterv1.DisasterBackup{}
	err = r.Get(ctx, types.NamespacedName{Name: dj.Spec.DisasterBackup, Namespace: dj.Namespace}, db)
	if err != nil {
		dj.Status.Phase = disasterv1.DisasterJobPhaseFailed
		dj.Status.Reason = string(apierrors.ReasonForError(err))
		logger.Error(err, "error getting DisasterBackup")
		return ctrl.Result{RequeueAfter: 1 * time.Second}, err
	}
	logger.Info("Getting DisasterBackup", "Name", dj.Spec.DisasterBackup, "Namespace", dj.Namespace)

	// 获取 DisasterConfig
	drc := &disasterv1.DisasterConfig{}
	err = r.Get(ctx, types.NamespacedName{Name: db.Spec.DisasterConfig, Namespace: db.Namespace}, drc)
	if err != nil {
		db.Status.Phase = disasterv1.DisasterBackupPhaseFailed
		dj.Status.Reason = string(apierrors.ReasonForError(err))
		logger.Error(err, "error getting DisasterConfig")
		return ctrl.Result{RequeueAfter: 1 * time.Second}, err
	}
	if dj.ObjectMeta.DeletionTimestamp.IsZero() {
		// 如果为0 ，则资源未被删除，我们需要检测是否存在 finalizer，如果不存在，则添加，并更新到资源对象中
		if !tools.ContainsString(dj.ObjectMeta.Finalizers, disasterv1.DisasterJobFinalizer) {
			dj.SetFinalizers(append(dj.GetFinalizers(), disasterv1.DisasterJobFinalizer))
			if err := r.Update(ctx, dj); err != nil {
				logger.Error(err, "error updating DisasterJob")
				return ctrl.Result{RequeueAfter: 1 * time.Second}, err
			}
		}
	} else {
		// 如果不为 0 ，则对象处于删除中
		if tools.ContainsString(dj.ObjectMeta.Finalizers, disasterv1.DisasterJobFinalizer) {
			dj.Status.Phase = disasterv1.DisasterJobPhaseDeleting
			// 如果存在 finalizer 且与上述声明的 finalizer 匹配，那么执行对应 hook 逻辑
			if err := r.deleteExternalResources(ctx, drc.Spec.SourceCluster, drc.Spec.TargetCluster, drc.Spec.StorageRepository, dj); err != nil {
				logger.Error(err, "error deleting external resources")
				return ctrl.Result{RequeueAfter: 1 * time.Second}, err
			}

			// 如果对应 hook 执行成功，那么清空 finalizers， k8s 删除对应资源
			dj.ObjectMeta.Finalizers = tools.RemoveString(dj.ObjectMeta.Finalizers, disasterv1.DisasterJobFinalizer)
			if err := r.Update(ctx, dj); err != nil {
				logger.Error(err, "error updating DisasterJob")
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, err
	}

	// 更新成功后，不再处理
	if dj.Status.Phase == disasterv1.DisasterBackupPhaseSucceed {
		return ctrl.Result{}, nil
	}
	switch dj.Status.Phase {
	case "":
		dj.Status.Phase = disasterv1.DisasterJobPhasePending
		return ctrl.Result{}, nil
	case disasterv1.DisasterBackupPhaseSucceed, disasterv1.DisasterBackupPhaseFailed:
		return ctrl.Result{}, nil
	}

	if dj.Spec.SyncType == disasterv1.SyncTypeForward {
		return r.SyncResources(ctx, drc.Spec.SourceCluster, drc.Spec.TargetCluster, drc.Spec.StorageRepository, dj, db)
	} else {
		return r.SyncResources(ctx, drc.Spec.TargetCluster, drc.Spec.SourceCluster, drc.Spec.StorageRepository, dj, db)
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *DisasterJobReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Scheme = mgr.GetScheme()
	r.Recorder = mgr.GetEventRecorderFor("disasterjob-controller")

	return ctrl.NewControllerManagedBy(mgr).
		For(&disasterv1.DisasterJob{}).
		WithEventFilter(IgnoreStatusUpdatesPredicate{}). // 应用事件过滤器
		Named("disasterjob").
		Complete(r)
}

func (r *DisasterJobReconciler) deleteExternalResources(ctx context.Context, srcCluster, dstCluster, storageRepository string, dj *disasterv1.DisasterJob) error {
	logger := logf.FromContext(ctx)
	backupName := r.GenBakcupName(dj)
	backup := &velerov1.DeleteBackupRequest{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: backupName,
			Namespace:    "velero",
		},
		Spec: velerov1.DeleteBackupRequestSpec{
			BackupName: backupName,
		},
	}

	srcCli, err := GetKubeClientSet(ctx, r.Client, r.Scheme, srcCluster)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("Source cluster not found, skipping source resource deletion", "cluster", srcCluster)
		} else {
			dj.Status.Phase = disasterv1.DisasterJobPhaseFailed
			dj.Status.Reason = string(apierrors.ReasonForError(err))
			logger.Error(err, "error creating src kube client")
			return err
		}
	} else {
		err = srcCli.Create(ctx, backup)
		if err != nil {
			logger.Error(err, "error deleting src cluster Velero Backup")
		}
	}

	dstCli, err := GetKubeClientSet(ctx, r.Client, r.Scheme, dstCluster)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("Target cluster not found, skipping target resource deletion", "cluster", dstCluster)
		} else {
			dj.Status.Phase = disasterv1.DisasterJobPhaseFailed
			dj.Status.Reason = string(apierrors.ReasonForError(err))
			logger.Error(err, "error creating dst kube client")
			return err
		}
	} else {
		err = dstCli.Create(ctx, backup)
		if err != nil {
			logger.Error(err, "error deleting dst cluster Velero Backup")
		}

		restoreName := r.GenRestoreName(dj)
		restore := &velerov1.Restore{
			ObjectMeta: metav1.ObjectMeta{
				Name:      restoreName,
				Namespace: "velero",
			},
		}
		err = dstCli.Delete(ctx, restore)
		if err != nil {
			logger.Error(err, "error deleting Velero Restore")
		}
	}

	return nil
}

func (r *DisasterJobReconciler) syncDependencyLabels(ctx context.Context, dj *disasterv1.DisasterJob) (bool, error) {
	if dj.Labels == nil {
		dj.Labels = make(map[string]string)
	}
	_, _, tokenChanged := EnsureDependencyTokenLabel(dj.Labels, string(dj.UID))
	edges := make([]DependencyEdge, 0, 2)

	if dj.Spec.DisasterBackup != "" {
		backup := &disasterv1.DisasterBackup{}
		if err := r.Get(ctx, types.NamespacedName{Namespace: dj.Namespace, Name: dj.Spec.DisasterBackup}, backup); err == nil {
			edges = append(edges, DependencyEdge{
				TargetToken:  BuildDependencyToken(string(backup.UID)),
				RelationCode: "spec.disasterBackup",
			})
		}
	}

	if policyName := dj.Labels[LabelDisasterPolicyName]; policyName != "" {
		policy := &disasterv1.DisasterPolicy{}
		if err := r.Get(ctx, types.NamespacedName{Namespace: dj.Namespace, Name: policyName}, policy); err == nil {
			edges = append(edges, DependencyEdge{
				TargetToken:  BuildDependencyToken(string(policy.UID)),
				RelationCode: "label.disasterPolicyName",
			})
		}
	}

	_, depChanged := RebuildDependencyToLabels(dj.Labels, edges)
	return tokenChanged || depChanged, nil
}

// SyncResources 同步灾难恢复作业所需的资源
// 该函数负责协调备份和恢复过程，包括创建Velero备份和恢复对象
//
// 参数:
//   - ctx: 上下文信息
//   - srcCluster: 源集群名称
//   - dstCluster: 目标集群名称
//   - storageRepository: 存储仓库名称
//   - dj: DisasterJob对象指针
//   - db: DisasterBackup对象指针
//
// 返回值:
//   - ctrl.Result: 控制结果，用于确定是否需要重新排队等操作
//   - error: 错误信息，如果执行过程中出现错误则返回
func (r *DisasterJobReconciler) SyncResources(ctx context.Context, srcCluster, dstCluster, storageRepository string, dj *disasterv1.DisasterJob, db *disasterv1.DisasterBackup) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)
	r.Recorder.Event(dj, corev1.EventTypeNormal, "SyncResources", "Start syncing resources")
	// 获取源集群 kube client
	srcCli, err := GetKubeClientSet(ctx, r.Client, r.Scheme, srcCluster)
	if err != nil {
		dj.Status.Phase = disasterv1.DisasterJobPhaseFailed
		dj.Status.Reason = string(apierrors.ReasonForError(err))
		logger.Error(err, "error creating src kube client")
		return ctrl.Result{RequeueAfter: 1 * time.Second}, err
	}

	// 创建 Velero Backup
	backup, err := r.CreateVeleroBackup(ctx, srcCli, dj, db, storageRepository)
	if err != nil {
		dj.Status.Phase = disasterv1.DisasterJobPhaseFailed
		dj.Status.Reason = string(apierrors.ReasonForError(err))
		logger.Error(err, "error creating Velero Backup")
		return ctrl.Result{RequeueAfter: 1 * time.Second}, err
	}

	// 检查备份状态并根据状态执行相应操作
	switch backup.Status.Phase {
	case velerov1.BackupPhaseCompleted:
		r.Recorder.Event(dj, corev1.EventTypeNormal, "SyncResources", "Backup completed")
		// 获取目标集群 kube client
		dstCli, err := GetKubeClientSet(ctx, r.Client, r.Scheme, dstCluster)
		if err != nil {
			dj.Status.Phase = disasterv1.DisasterJobPhaseFailed
			dj.Status.Reason = string(apierrors.ReasonForError(err))
			logger.Error(err, "error creating dst kube client")
			return ctrl.Result{RequeueAfter: 1 * time.Second}, err
		}
		_, err = r.GetVeleroBackup(ctx, dstCli, backup.Name)
		if err != nil {
			// dj.Status.Phase = disasterv1.DisasterJobPhaseFailed
			logger.Info("Backup not sync, waiting")
			return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
		}

		r.Recorder.Event(dj, corev1.EventTypeNormal, "SyncResources", "Begin to restore resources")
		// 创建 Velero Restore
		restore, err := r.CreateVeleroRestore(ctx, dstCli, dj, db)
		if err != nil {
			dj.Status.Phase = disasterv1.DisasterJobPhaseFailed
			dj.Status.Reason = string(apierrors.ReasonForError(err))
			logger.Error(err, "error creating Velero Restore")
			return ctrl.Result{RequeueAfter: 1 * time.Second}, err
		}
		// 检查恢复状态并根据状态更新DisasterJob状态
		switch restore.Status.Phase {
		case velerov1.RestorePhaseCompleted:
			r.Recorder.Event(dj, corev1.EventTypeNormal, "SyncResources", "Restore completed")
			dj.Status.Phase = disasterv1.DisasterJobPhaseSucceed
			logger.Info("DisasterJob succeed")
			return ctrl.Result{}, nil
		case velerov1.RestorePhaseFailed:
			r.Recorder.Event(dj, corev1.EventTypeWarning, "SyncResources", "Restore failed")
			dj.Status.Phase = disasterv1.DisasterJobPhaseFailed
			dj.Status.Reason = string(apierrors.ReasonForError(err))
			logger.Error(err, "Restore failed")
			return ctrl.Result{}, err
		default:
			dj.Status.Phase = disasterv1.DisasterJobPhaseRestoring
		}
	case velerov1.BackupPhaseFailed, velerov1.BackupPhaseFailedValidation:
		r.Recorder.Event(dj, corev1.EventTypeWarning, "SyncResources", "Backup failed")
		dj.Status.Phase = disasterv1.DisasterJobPhaseFailed
		dj.Status.Reason = string(apierrors.ReasonForError(err))
		logger.Error(err, "Backup failed")
		return ctrl.Result{}, err
	default:
		dj.Status.Phase = disasterv1.DisasterJobPhaseBackuping
	}
	return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
}

func (r *DisasterJobReconciler) GetVeleroBackup(ctx context.Context, cli client.Client, name string) (*velerov1.Backup, error) {
	logger := logf.FromContext(ctx)
	backup := &velerov1.Backup{}
	err := cli.Get(ctx, types.NamespacedName{Name: name, Namespace: "velero"}, backup)
	if apierrors.IsNotFound(err) {
		logger.Info("Velero Backup not found")
		return nil, err
	}
	if err != nil {
		logger.Error(err, "error getting Velero Backup")
		return nil, err
	}
	return backup, nil
}

func (r *DisasterJobReconciler) GenBakcupName(dj *disasterv1.DisasterJob) string {
	return "yaoshi-backup-" + dj.Name
}

func (r *DisasterJobReconciler) GenRestoreName(dj *disasterv1.DisasterJob) string {
	return "yaoshi-restore-" + dj.Name
}

// CreateVeleroBackup creates a Velero Backup resource
func (r *DisasterJobReconciler) CreateVeleroBackup(ctx context.Context, cli client.Client, dj *disasterv1.DisasterJob, db *disasterv1.DisasterBackup, storageRepository string) (*velerov1.Backup, error) {
	logger := logf.FromContext(ctx)
	r.Recorder.Event(dj, corev1.EventTypeNormal, "CreateVeleroBackup", "Start creating Velero Backup")
	backupName := r.GenBakcupName(dj)
	backup := &velerov1.Backup{}
	err := cli.Get(ctx, types.NamespacedName{Name: backupName, Namespace: "velero"}, backup)
	if apierrors.IsNotFound(err) {
		logger.Info("Velero Backup not found")
		b := true
		backup := &velerov1.Backup{
			ObjectMeta: metav1.ObjectMeta{
				Name:      backupName,
				Namespace: "velero",
			},
			Spec: velerov1.BackupSpec{
				IncludedNamespaces:       []string{db.Spec.Namespace},
				LabelSelector:            db.Spec.LabelSelector,
				StorageLocation:          storageRepository,
				DefaultVolumesToFsBackup: &b,
			},
		}
		err = cli.Create(ctx, backup)
		if err != nil {
			r.Recorder.Event(dj, corev1.EventTypeWarning, "CreateVeleroBackup", "Create Velero Backup failed")
			logger.Error(err, "error creating Velero Backup")
			return nil, err
		}
		r.Recorder.Event(dj, corev1.EventTypeNormal, "CreateVeleroBackup", "Velero Backup created successfully")
		logger.Info("Velero Backup created")
		return backup, nil
	}
	if err != nil {
		r.Recorder.Event(dj, corev1.EventTypeWarning, "CreateVeleroBackup", "Get Velero Backup failed")
		logger.Error(err, "error getting Velero Backup")
		return nil, err
	}
	r.Recorder.Event(dj, corev1.EventTypeNormal, "CreateVeleroBackup", "Velero Backup already exists")
	return backup, nil
}

// CreateVeleroRestore creates a Velero Restore resource
func (r *DisasterJobReconciler) CreateVeleroRestore(ctx context.Context, cli client.Client, dj *disasterv1.DisasterJob, db *disasterv1.DisasterBackup) (*velerov1.Restore, error) {
	logger := logf.FromContext(ctx)
	r.Recorder.Event(dj, corev1.EventTypeNormal, "CreateVeleroRestore", "Start creating Velero Restore")
	restoreName := r.GenRestoreName(dj)
	backupName := r.GenBakcupName(dj)
	restore := &velerov1.Restore{}
	err := cli.Get(ctx, types.NamespacedName{Name: restoreName, Namespace: "velero"}, restore)
	if apierrors.IsNotFound(err) {
		logger.Info("Velero Restore not found")
		restorePVs := true
		restore := &velerov1.Restore{
			ObjectMeta: metav1.ObjectMeta{
				Name:      restoreName,
				Namespace: "velero",
			},
			Spec: velerov1.RestoreSpec{
				BackupName: backupName,
				RestorePVs: &restorePVs,
			},
		}
		err = cli.Create(ctx, restore)
		if err != nil {
			logger.Error(err, "error creating Velero Restore")
			return nil, err
		}
		r.Recorder.Event(dj, corev1.EventTypeNormal, "CreateVeleroRestore", "Velero Restore created successfully")
		logger.Info("Velero Restore created")
		return restore, nil
	}
	if err != nil {
		r.Recorder.Event(dj, corev1.EventTypeWarning, "CreateVeleroRestore", "Get Velero Restore failed")
		logger.Error(err, "error getting Velero Restore")
		return nil, err
	}
	r.Recorder.Event(dj, corev1.EventTypeNormal, "CreateVeleroRestore", "Velero Restore already exists")
	return restore, nil
}

// 创建 change-storage-class-config
func (r *DisasterJobReconciler) CreateChangeStorageClassConfig(ctx context.Context, cli client.Client, oldSC, newSC string) error {
	cm := &corev1.ConfigMap{}
	err := cli.Get(ctx, types.NamespacedName{Name: cm.Name, Namespace: cm.Namespace}, cm)
	if apierrors.IsNotFound(err) {
		logger := logf.FromContext(ctx)
		logger.Info("ConfigMap not found")
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      ChangeStorageClassConfig,
				Namespace: "velero",
				Labels: map[string]string{
					"velero.io/plugin-config":        "",
					"velero.io/change-storage-class": "RestoreItemAction",
				},
			},
			Data: map[string]string{
				oldSC: newSC,
			},
		}
		err = cli.Create(ctx, cm)
		if err != nil {
			logger.Error(err, "error creating ConfigMap")
			return err
		}
		logger.Info("ConfigMap created")
	}
	if err != nil {
		logger := logf.FromContext(ctx)
		logger.Error(err, "error getting ConfigMap")
		return err
	}
	return nil
}

// 删除 change-storage-class-config
func (r *DisasterJobReconciler) DeleteChangeStorageClassConfig(ctx context.Context, cli client.Client) error {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ChangeStorageClassConfig,
			Namespace: "velero",
		},
	}
	return cli.Delete(ctx, cm)
}

// 创建 change-image-name-config
func (r *DisasterJobReconciler) CreateChangeImageNameConfig(ctx context.Context, cli client.Client, maps []string) error {
	logger := logf.FromContext(ctx)
	cm := &corev1.ConfigMap{}
	err := cli.Get(ctx, types.NamespacedName{Name: cm.Name, Namespace: cm.Namespace}, cm)
	if apierrors.IsNotFound(err) {
		logger.Info("ConfigMap not found")
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      ChangeImageNameConfig,
				Namespace: "velero",
				Labels: map[string]string{
					"velero.io/plugin-config":     "",
					"velero.io/change-image-name": "RestoreItemAction",
				},
			},
			Data: map[string]string{},
		}
		for i, m := range maps {
			cm.Data[strconv.Itoa(i)] = m
		}
		err = cli.Create(ctx, cm)
		if err != nil {
			logger.Error(err, "error creating ConfigMap")
			return err
		}
		logger.Info("ConfigMap created")
	}
	return nil
}

// 删除 change-image-name-config
func (r *DisasterJobReconciler) DeleteChangeImageNameConfig(ctx context.Context, cli client.Client) error {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ChangeImageNameConfig,
			Namespace: "velero",
		},
	}
	return cli.Delete(ctx, cm)
}
