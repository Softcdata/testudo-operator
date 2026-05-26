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

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	"github.com/softcdata/testudo-operator/pkg/helper"
	. "github.com/softcdata/testudo-operator/pkg/metadata"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	ConfigFinalizer       = "testudo.softcdata.com/config-finalizer"
	configRequeueInterval = 10 * time.Second

	configReasonSourceClusterNotFound        = "SourceClusterNotFound"
	configReasonTargetClusterNotFound        = "TargetClusterNotFound"
	configReasonStorageRepositoryNotFound    = "StorageRepositoryNotFound"
	configReasonClusterNotReady              = "ClusterNotReady"
	configReasonQueryDependencyFailed        = "QueryDependencyFailed"
	configReasonUpdateConfigFailed           = "UpdateConfigFailed"
	configReasonApplyStorageRepositoryFailed = "ApplyStorageRepositoryFailed"
)

// DisasterConfigReconciler reconciles a DisasterConfig object
type DisasterConfigReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=testudo.softcdata.com,resources=disasterconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=testudo.softcdata.com,resources=disasterconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=testudo.softcdata.com,resources=disasterconfigs/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *DisasterConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)

	dc := &disasterv1.DisasterConfig{}
	err := r.Get(ctx, req.NamespacedName, dc)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("DisasterConfig not found")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "error getting DisasterConfig")
		return ctrl.Result{RequeueAfter: configRequeueInterval}, nil
	}
	logger = logger.WithValues(TraceIDKey, dc.Annotations[AnnotationTraceID])
	ctx = context.WithValue(ctx, TraceIDKey, dc.Annotations[AnnotationTraceID])

	// 处理删除逻辑
	if !dc.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.handleDelete(ctx, dc)
	}

	// 添加 Finalizer
	if !controllerutil.ContainsFinalizer(dc, ConfigFinalizer) {
		controllerutil.AddFinalizer(dc, ConfigFinalizer)
		if err := r.Update(ctx, dc); err != nil {
			logger.Error(err, "failed to add finalizer")
			return ctrl.Result{RequeueAfter: configRequeueInterval}, nil
		}
		// 重新排队以进行正常的 Reconcile
		return ctrl.Result{Requeue: true}, nil
	}

	// Sync dependency labels
	if changed, err := r.syncDependencyLabels(ctx, dc); err != nil {
		logger.Error(err, "failed to sync dependency labels")
		return ctrl.Result{RequeueAfter: configRequeueInterval}, nil
	} else if changed {
		if err := r.Update(ctx, dc); err != nil {
			logger.Error(err, "failed to update dependency labels")
			return ctrl.Result{RequeueAfter: configRequeueInterval}, nil
		}
		return ctrl.Result{Requeue: true}, nil
	}

	defer func() {
		err = r.Status().Update(ctx, dc)
		if err != nil {
			logger.Error(err, "unable to update DisasterConfig status")
		}
	}()

	if dc.Status.Status == "" {
		dc.Status.Status = disasterv1.DisasterConfigStatusPending
		return ctrl.Result{Requeue: true}, nil
	}

	sourceCluster, err := r.getCluster(ctx, dc.Spec.SourceCluster)
	if err != nil {
		if apierrors.IsNotFound(err) {
			dc.Status.Status = disasterv1.DisasterConfigStatusError
			helper.SetStatusError(&dc.Status, configReasonSourceClusterNotFound, fmt.Sprintf("source cluster %q not found", dc.Spec.SourceCluster))
			logger.Info("source cluster not found")
			return ctrl.Result{RequeueAfter: configRequeueInterval}, nil
		}
		logger.Error(err, "error getting source cluster")
		dc.Status.Status = disasterv1.DisasterConfigStatusError
		helper.SetStatusError(&dc.Status, configReasonQueryDependencyFailed, fmt.Sprintf("failed to query source cluster %q: %v", dc.Spec.SourceCluster, err))
		return ctrl.Result{RequeueAfter: configRequeueInterval}, nil
	}
	if dc.Spec.StorageRepository == "" {
		dc.Spec.StorageRepository = "default"
		err := r.Update(ctx, dc)
		if err != nil {
			logger.Error(err, "error update disaster config")
			dc.Status.Status = disasterv1.DisasterConfigStatusError
			helper.SetStatusError(&dc.Status, configReasonUpdateConfigFailed, fmt.Sprintf("failed to update disaster config default storage repository: %v", err))
			return ctrl.Result{RequeueAfter: configRequeueInterval}, nil
		}
	}

	if sourceCluster.Status.Status != disasterv1.ClusterStatusReady {
		dc.Status.Status = disasterv1.DisasterConfigStatusNotReady
		logger.Info("source cluster is not ready")
		helper.SetStatusError(&dc.Status, configReasonClusterNotReady, fmt.Sprintf("source cluster %q is not ready, current status: %s", sourceCluster.Name, sourceCluster.Status.Status))
		return ctrl.Result{RequeueAfter: configRequeueInterval}, nil
	}

	targetCluster, err := r.getCluster(ctx, dc.Spec.TargetCluster)
	if err != nil {
		if apierrors.IsNotFound(err) {
			dc.Status.Status = disasterv1.DisasterConfigStatusError
			logger.Info("target cluster not found")
			helper.SetStatusError(&dc.Status, configReasonTargetClusterNotFound, fmt.Sprintf("target cluster %q not found", dc.Spec.TargetCluster))
			return ctrl.Result{RequeueAfter: configRequeueInterval}, nil
		}
		logger.Error(err, "error getting target cluster")
		dc.Status.Status = disasterv1.DisasterConfigStatusError
		helper.SetStatusError(&dc.Status, configReasonQueryDependencyFailed, fmt.Sprintf("failed to query target cluster %q: %v", dc.Spec.TargetCluster, err))
		return ctrl.Result{RequeueAfter: configRequeueInterval}, nil
	}
	if targetCluster.Status.Status != disasterv1.ClusterStatusReady {
		dc.Status.Status = disasterv1.DisasterConfigStatusNotReady
		helper.SetStatusError(&dc.Status, configReasonClusterNotReady, fmt.Sprintf("target cluster %q is not ready, current status: %s", targetCluster.Name, targetCluster.Status.Status))
		logger.Info("target cluster is not ready")
		return ctrl.Result{RequeueAfter: configRequeueInterval}, nil
	}

	sr := &disasterv1.StorageRepository{}
	err = r.Get(ctx, types.NamespacedName{Name: dc.Spec.StorageRepository, Namespace: ManagementNamespace()}, sr)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("storage repository not found")
			dc.Status.Status = disasterv1.DisasterConfigStatusError
			helper.SetStatusError(&dc.Status, configReasonStorageRepositoryNotFound, fmt.Sprintf("storage repository %q not found", dc.Spec.StorageRepository))
			return ctrl.Result{RequeueAfter: configRequeueInterval}, nil
		}
		logger.Error(err, "error getting storage repository")
		dc.Status.Status = disasterv1.DisasterConfigStatusError
		helper.SetStatusError(&dc.Status, configReasonQueryDependencyFailed, fmt.Sprintf("failed to query storage repository %q: %v", dc.Spec.StorageRepository, err))
		return ctrl.Result{RequeueAfter: configRequeueInterval}, nil
	}

	// install bsl to source and target cluster
	bslName := sr.Name + "-" + sourceCluster.Name
	err = r.ApplyStorageRepository(ctx, sourceCluster, sr, bslName, sourceCluster.Name)
	if err != nil {
		logger.Error(err, "error applying storage repository")
		dc.Status.Status = disasterv1.DisasterConfigStatusError
		helper.SetStatusError(&dc.Status, configReasonApplyStorageRepositoryFailed, fmt.Sprintf("failed to apply storage repository %q to source cluster %q: %v", sr.Name, sourceCluster.Name, err))
		return ctrl.Result{RequeueAfter: configRequeueInterval}, nil
	}
	err = r.ApplyStorageRepository(ctx, targetCluster, sr, bslName, sourceCluster.Name)
	if err != nil {
		logger.Error(err, "error applying storage repository")
		dc.Status.Status = disasterv1.DisasterConfigStatusError
		helper.SetStatusError(&dc.Status, configReasonApplyStorageRepositoryFailed, fmt.Sprintf("failed to apply storage repository %q to target cluster %q: %v", sr.Name, targetCluster.Name, err))
		return ctrl.Result{RequeueAfter: configRequeueInterval}, nil
	}

	logger.Info("disaster config is ready")
	dc.Status.Status = disasterv1.DisasterConfigStatusReady
	helper.ClearStatusError(&dc.Status)

	return ctrl.Result{RequeueAfter: configRequeueInterval}, nil
}

func (r *DisasterConfigReconciler) handleDelete(ctx context.Context, dc *disasterv1.DisasterConfig) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(dc, ConfigFinalizer) {
		return ctrl.Result{}, nil
	}

	// Legacy finalizer deletion protection is temporarily disabled.
	//
	// The old behavior blocked finalizer removal while DisasterInstance still
	// referenced the DisasterConfig. We are temporarily bypassing that legacy
	// protection so deletion can proceed, and will re-introduce the new
	// case-based deletion rules separately.
	/*
		hasRef, refName, err := r.checkReferences(ctx, dc)
		if err != nil {
			return ctrl.Result{}, err
		}

		if hasRef {
			msg := fmt.Sprintf("Cannot delete DisasterConfig: referenced by DisasterInstance %s", refName)
			r.Recorder.Event(dc, "Warning", "DeletionBlocked", msg)

			// 更新 Status Reason，提示用户
			dc.Status.Reason = msg
			if err := r.Status().Update(ctx, dc); err != nil {
				return ctrl.Result{}, err
			}

			// Requeue to allow future deletion if reference is removed
			return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
		}
	*/

	// Remove finalizer
	controllerutil.RemoveFinalizer(dc, ConfigFinalizer)
	if err := r.Update(ctx, dc); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *DisasterConfigReconciler) checkReferences(ctx context.Context, dc *disasterv1.DisasterConfig) (bool, string, error) {
	instances := &disasterv1.DisasterInstanceList{}
	if err := r.List(ctx, instances); err != nil {
		return false, "", err
	}

	for _, inst := range instances.Items {
		if inst.Spec.Config == dc.Name {
			return true, inst.Name, nil
		}
	}
	return false, "", nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *DisasterConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&disasterv1.DisasterConfig{}).
		WithEventFilter(IgnoreStatusUpdatesPredicate{}). // 应用事件过滤器
		Named("disasterconfig").
		Complete(r)
}

func (r *DisasterConfigReconciler) getCluster(ctx context.Context, name string) (*disasterv1.Cluster, error) {
	cluster := &disasterv1.Cluster{}
	err := r.Get(ctx, types.NamespacedName{Name: name}, cluster)
	if err != nil {
		return nil, err
	}
	return cluster, nil
}

func (r *DisasterConfigReconciler) ApplyStorageRepository(ctx context.Context, cluster *disasterv1.Cluster, sr *disasterv1.StorageRepository, bslName, prefix string) error {
	logger := logf.FromContext(ctx)

	cli, err := GetKubeClientSetWithCluster(ctx, r.Client, r.Scheme, cluster)
	if err != nil {
		logger.Error(err, "unable to get kube client")
		return err
	}

	defaultBSL := &DefaultBSL{}
	return defaultBSL.ApplyStorageRepository(ctx, r.Client, cli, sr, bslName, prefix)
}

func (r *DisasterConfigReconciler) syncDependencyLabels(ctx context.Context, dc *disasterv1.DisasterConfig) (bool, error) {
	if dc.Labels == nil {
		dc.Labels = make(map[string]string)
	}
	_, _, tokenChanged := EnsureDependencyTokenLabel(dc.Labels, string(dc.UID))
	edges := make([]DependencyEdge, 0, 5)

	if dc.Spec.SourceCluster != "" {
		source := &disasterv1.Cluster{}
		if err := r.Get(ctx, types.NamespacedName{Name: dc.Spec.SourceCluster}, source); err == nil {
			edges = append(edges, DependencyEdge{
				TargetToken:  BuildDependencyToken(string(source.UID)),
				RelationCode: "spec.sourceCluster",
			})
		}
	}
	if dc.Spec.TargetCluster != "" {
		target := &disasterv1.Cluster{}
		if err := r.Get(ctx, types.NamespacedName{Name: dc.Spec.TargetCluster}, target); err == nil {
			edges = append(edges, DependencyEdge{
				TargetToken:  BuildDependencyToken(string(target.UID)),
				RelationCode: "spec.targetCluster",
			})
		}
	}
	if dc.Spec.StorageRepository != "" {
		sr := &disasterv1.StorageRepository{}
		if err := r.Get(ctx, types.NamespacedName{Namespace: ManagementNamespace(), Name: dc.Spec.StorageRepository}, sr); err == nil {
			edges = append(edges, DependencyEdge{
				TargetToken:  BuildDependencyToken(string(sr.UID)),
				RelationCode: "spec.storageRepository",
			})
		}
	}

	policyNamespace := dc.Namespace
	if policyNamespace == "" {
		policyNamespace = ManagementNamespace()
	}
	if dc.Spec.DataSyncPolicy != "" {
		policy := &disasterv1.DisasterPolicy{}
		if err := r.Get(ctx, types.NamespacedName{Namespace: policyNamespace, Name: dc.Spec.DataSyncPolicy}, policy); err == nil {
			edges = append(edges, DependencyEdge{
				TargetToken:  BuildDependencyToken(string(policy.UID)),
				RelationCode: "spec.dataSyncPolicy",
			})
		}
	}
	if dc.Spec.ResourceSyncPolicy != "" {
		policy := &disasterv1.DisasterPolicy{}
		if err := r.Get(ctx, types.NamespacedName{Namespace: policyNamespace, Name: dc.Spec.ResourceSyncPolicy}, policy); err == nil {
			edges = append(edges, DependencyEdge{
				TargetToken:  BuildDependencyToken(string(policy.UID)),
				RelationCode: "spec.resourceSyncPolicy",
			})
		}
	}

	_, depChanged := RebuildDependencyToLabels(dc.Labels, edges)
	return tokenChanged || depChanged, nil
}
