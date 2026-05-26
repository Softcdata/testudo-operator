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
	. "github.com/softcdata/testudo-operator/pkg/metadata"
	"github.com/softcdata/testudo-operator/pkg/tools"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// DisasterBackupReconciler reconciles a DisasterBackup object
type DisasterBackupReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=testudo.softcdata.com,resources=disasterbackups,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=testudo.softcdata.com,resources=disasterbackups/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=testudo.softcdata.com,resources=disasterbackups/finalizers,verbs=update
// +kubebuilder:rbac:groups=velero.io,resources=backups,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the DisasterBackup object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/reconcile
func (r *DisasterBackupReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)
	logger.Info("DisasterBackupReconciler")

	db := &disasterv1.DisasterBackup{}
	err := r.Get(ctx, req.NamespacedName, db)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("DisasterBackup not found")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "error getting DisasterBackup")
		return ctrl.Result{}, err
	}
	logger = logger.WithValues(TraceIDKey, db.Annotations[AnnotationTraceID])
	ctx = context.WithValue(ctx, TraceIDKey, db.Annotations[AnnotationTraceID])

	if changed, err := r.syncDependencyLabels(ctx, db); err != nil {
		logger.Error(err, "failed to sync dependency labels")
		return ctrl.Result{}, err
	} else if changed {
		if err := r.Update(ctx, db); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	defer func() {
		// db.Status.UpdateTime = metav1.Now()
		err = r.Status().Update(ctx, db)
		if err != nil {
			logger.Error(err, "unable to update DisasterBackup status")
		}
	}()

	if db.Status.Phase == "" {
		db.Status.Phase = disasterv1.DisasterBackupPhasePending
	}
	drc := &disasterv1.DisasterConfig{}
	err = r.Get(ctx, types.NamespacedName{Name: db.Spec.DisasterConfig, Namespace: db.Namespace}, drc)
	if err != nil {
		db.Status.Phase = disasterv1.DisasterBackupPhaseFailed
		logger.Error(err, "error getting DisasterConfig")
		return ctrl.Result{RequeueAfter: 3 * time.Second}, nil
	}

	// 获取 rest config
	srcRestConfig, err := r.getRestConfigByClusterName(ctx, drc.Spec.SourceCluster)
	if err != nil {
		db.Status.Phase = disasterv1.DisasterBackupPhaseFailed
		logger.Error(err, "error getting rest config for source cluster")
		return ctrl.Result{RequeueAfter: 3 * time.Second}, nil
	}

	// 使用正确的 Discovery 客户端
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(srcRestConfig)
	if err != nil {
		db.Status.Phase = disasterv1.DisasterBackupPhaseFailed
		logger.Error(err, "error creating discovery client for source cluster")
		return ctrl.Result{RequeueAfter: 3 * time.Second}, nil
	}

	// 获取支持命名空间的 API 资源列表
	apiResources, err := GetNamespaceAPIResources(discoveryClient, db.Spec.Namespace)
	if err != nil {
		db.Status.Phase = disasterv1.DisasterBackupPhaseFailed
		logger.Error(err, "error getting namespace resources")
		return ctrl.Result{RequeueAfter: 3 * time.Second}, nil
	}

	// logger.Info("apiResources", "apiResources", apiResources)

	// 创建动态客户端
	dynamicClient, err := dynamic.NewForConfig(srcRestConfig)
	if err != nil {
		db.Status.Phase = disasterv1.DisasterBackupPhaseFailed
		logger.Error(err, "error creating dynamic client for source cluster")
		return ctrl.Result{RequeueAfter: 3 * time.Second}, nil
	}

	// 获取指定命名空间下的资源
	resources, err := GetResourcesInNamespace(dynamicClient, apiResources, db.Spec.Namespace)
	if err != nil {
		db.Status.Phase = disasterv1.DisasterBackupPhaseFailed
		logger.Error(err, "error getting resources in namespace")
		return ctrl.Result{RequeueAfter: 3 * time.Second}, nil
	}
	logger.Info("resources", "resources", resources)

	db.Status.Resources = map[string][]disasterv1.Resources{
		db.Spec.Namespace: resources,
	}
	db.Status.Phase = disasterv1.DisasterBackupPhaseReady
	return ctrl.Result{RequeueAfter: 3 * time.Minute}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *DisasterBackupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&disasterv1.DisasterBackup{}).
		WithEventFilter(IgnoreStatusUpdatesPredicate{}). // 应用事件过滤器
		Named("disasterbackup").
		Complete(r)
}

// 获取集群的 rest config
func (r *DisasterBackupReconciler) getRestConfigByClusterName(ctx context.Context, clsutername string) (*rest.Config, error) {
	cluster := &disasterv1.Cluster{}
	err := r.Get(ctx, types.NamespacedName{Name: clsutername}, cluster)
	if err != nil {
		return nil, err
	}

	if len(cluster.Spec.KubeConfig) > 0 {
		return tools.GetRestConfig(cluster.Spec.KubeConfig)
	} else if cluster.Spec.Token != "" && cluster.Spec.Endpoint != "" {
		return tools.GetRestConfigFromToken(cluster.Spec.Endpoint, cluster.Spec.Token)
	}

	return nil, fmt.Errorf("neither kubeconfig nor token/endpoint provided for cluster %s", clsutername)
}

// 获取支持命名空间的 API 资源列表
func GetNamespaceAPIResources(discovery discovery.DiscoveryInterface, namespace string) ([]*metav1.APIResourceList, error) {
	// 检查 discovery client 是否为 nil
	if discovery == nil {
		return nil, fmt.Errorf("discovery client is nil")
	}

	// 获取所有 API 组资源
	apiResourceLists, err := discovery.ServerPreferredResources()
	if err != nil {
		return nil, fmt.Errorf("failed to get server preferred resources: %w", err)
	}

	var results []*metav1.APIResourceList
	for _, list := range apiResourceLists {
		// 过滤命名空间资源
		if len(list.APIResources) == 0 {
			continue
		}
		newlist := &metav1.APIResourceList{
			GroupVersion: list.GroupVersion,
			APIResources: []metav1.APIResource{},
		}
		for _, resource := range list.APIResources {
			if resource.Namespaced {
				newlist.APIResources = append(newlist.APIResources, resource)
			}
		}
		if len(newlist.APIResources) > 0 {
			results = append(results, newlist)
		}
	}

	return results, nil
}

// 获取指定命名空间下的所有资源
func GetResourcesInNamespace(dynamicClient dynamic.Interface, apiResources []*metav1.APIResourceList, namespace string) ([]disasterv1.Resources, error) {
	var resources []disasterv1.Resources
	for _, apiResource := range apiResources {
		for _, resource := range apiResource.APIResources {
			gv, err := schema.ParseGroupVersion(apiResource.GroupVersion)
			if err != nil {
				// fmt.Println("Error parsing group version:", err)
				return nil, err
			}
			gvr := gv.WithResource(resource.Name)
			resourceClient := dynamicClient.Resource(gvr)
			list, err := resourceClient.Namespace(namespace).List(context.Background(), metav1.ListOptions{})

			if err != nil {
				// fmt.Println("Error listing resources:", err, resource, gvr)
				continue
			}
			if len(list.Items) == 0 {
				continue
			}
			res := disasterv1.Resources{
				TypeMeta: metav1.TypeMeta{},
				Names:    []string{},
			}
			for _, item := range list.Items {
				res.APIVersion = item.GetAPIVersion()
				res.Kind = item.GetKind()
				res.Names = append(res.Names, item.GetName())
			}
			resources = append(resources, res)
		}
	}
	return resources, nil
}

func (r *DisasterBackupReconciler) syncDependencyLabels(ctx context.Context, db *disasterv1.DisasterBackup) (bool, error) {
	if db.Labels == nil {
		db.Labels = make(map[string]string)
	}
	_, _, tokenChanged := EnsureDependencyTokenLabel(db.Labels, string(db.UID))
	edges := make([]DependencyEdge, 0, 1)

	if db.Spec.DisasterConfig != "" {
		config := &disasterv1.DisasterConfig{}
		if err := r.Get(ctx, types.NamespacedName{Name: db.Spec.DisasterConfig, Namespace: db.Namespace}, config); err == nil {
			edges = append(edges, DependencyEdge{
				TargetToken:  BuildDependencyToken(string(config.UID)),
				RelationCode: "spec.disasterConfig",
			})
		}
	}

	_, depChanged := RebuildDependencyToLabels(db.Labels, edges)
	return tokenChanged || depChanged, nil
}
