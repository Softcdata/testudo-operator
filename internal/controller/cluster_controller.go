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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	stdruntime "runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	semver "github.com/blang/semver/v4"
	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	"github.com/softcdata/testudo-operator/pkg/helper"
	platformlicense "github.com/softcdata/testudo-operator/pkg/license"
	. "github.com/softcdata/testudo-operator/pkg/metadata"
	"github.com/softcdata/testudo-operator/pkg/tools"
	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/yaml"
)

const (
	veleroCompatibilityFailedEventReason = "VeleroCompatibilityFailed"

	clusterReasonVeleroVersionIncompatible    = "VeleroVersionIncompatible"
	clusterReasonVeleroCRDVersionIncompatible = "VeleroCRDVersionIncompatible"
	clusterReasonVeleroCRDCheckFailed         = "VeleroCRDCheckFailed"
	clusterReasonVeleroRuntimeNotReady        = "VeleroRuntimeNotReady"
	clusterReasonVeleroStatusSyncPending      = "VeleroStatusSyncPending"

	veleroSupportedVersionMin = "1.17.0"
	veleroSupportedVersionMax = "1.18.0"
	veleroRequiredCRDVersion  = "v1"

	defaultVeleroChartPath  = "./velero-11.1.1.tgz"
	defaultVeleroValuesPath = "./velero.values.yaml"
)

var requiredVeleroCRDNames = []string{
	"backups.velero.io",
	"restores.velero.io",
	"serverstatusrequests.velero.io",
	"backupstoragelocations.velero.io",
}

// ClusterReconciler reconciles a Cluster object
type ClusterReconciler struct {
	client.Client
	Scheme                  *runtime.Scheme
	Recorder                record.EventRecorder
	CommandExecutor         CommandExecutor
	BSL                     BSL
	ClientFactory           func(config *rest.Config, options client.Options) (client.Client, error)
	KubeClientFactory       func(c *rest.Config) (kubernetes.Interface, error)
	ForceVeleroNotInstalled bool // For testing only
	ManagementNamespace     string
	LicenseGateEnabled      bool
	LicenseNamespace        string
	LicenseCAPath           string
	LicenseVerifier         *platformlicense.Verifier
	LicenseAcceptanceLock   sync.Mutex
	VeleroChartPath         string
	VeleroValuesPath        string
}

func clearClusterReason(cluster *disasterv1.Cluster, reason string) {
	if cluster.Status.Reason != reason {
		return
	}
	cluster.Status.Reason = ""
	cluster.Status.Message = ""
}

func markClusterNotReady(cluster *disasterv1.Cluster, reason string, err error) {
	cluster.Status.Status = disasterv1.ClusterStatusNotReady
	if cluster.Status.Reason == "TokenExpired" {
		return
	}
	cluster.Status.Reason = reason
	cluster.Status.Message = err.Error()
}

func refreshClusterTokenExpiration(cluster *disasterv1.Cluster, now time.Time) bool {
	if strings.TrimSpace(cluster.Spec.Token) == "" {
		cluster.Status.TokenExpiration = nil
		clearClusterReason(cluster, "TokenExpired")
		return false
	}

	exp, err := helper.ParseTokenExpiration(cluster.Spec.Token)
	if err != nil || exp == nil {
		// Non-JWT / opaque tokens can still be valid auth inputs. Only clear stale expiry state.
		cluster.Status.TokenExpiration = nil
		clearClusterReason(cluster, "TokenExpired")
		return false
	}

	metaTime := metav1.NewTime(*exp)
	cluster.Status.TokenExpiration = &metaTime
	if now.After(*exp) {
		cluster.Status.Status = disasterv1.ClusterStatusNotReady
		cluster.Status.Reason = "TokenExpired"
		cluster.Status.Message = fmt.Sprintf("Token expired at %s", exp.Format(time.RFC3339))
		return true
	}

	clearClusterReason(cluster, "TokenExpired")
	return false
}

func veleroWaitGenerationMarker(generation int64) string {
	return fmt.Sprintf("spec generation %d", generation)
}

func isClusterWaitingForCurrentVeleroStatus(cluster *disasterv1.Cluster) bool {
	switch cluster.Status.Reason {
	case clusterReasonVeleroRuntimeNotReady, clusterReasonVeleroStatusSyncPending:
		return strings.Contains(cluster.Status.Message, veleroWaitGenerationMarker(cluster.Generation))
	default:
		return false
	}
}

func canReuseObservedVeleroVersion(cluster *disasterv1.Cluster, wasReady bool) bool {
	if cluster.Status.ObservedGeneration != cluster.Generation {
		return false
	}

	version := strings.TrimSpace(cluster.Status.VeleroVersion)
	if version == "" || version == "-" {
		return false
	}

	if wasReady {
		return true
	}

	if cluster.Status.Reason != clusterReasonVeleroStatusSyncPending {
		return false
	}

	return strings.Contains(cluster.Status.Message, veleroWaitGenerationMarker(cluster.Generation))
}

func shouldReconcileExistingVeleroInstall(cluster *disasterv1.Cluster) bool {
	if cluster.Status.ObservedGeneration != 0 || cluster.Spec.VeleroInstall == nil {
		return false
	}

	if strings.TrimSpace(cluster.Spec.VeleroInstall.ImageRegistry) != "" {
		return true
	}

	if cluster.Spec.VeleroInstall.RegistryCredentialSecretRef != nil && strings.TrimSpace(cluster.Spec.VeleroInstall.RegistryCredentialSecretRef.Name) != "" {
		return true
	}

	return false
}

func podReady(pod *corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

func podWaitingReason(statuses []corev1.ContainerStatus) string {
	for _, status := range statuses {
		if status.State.Waiting != nil && strings.TrimSpace(status.State.Waiting.Reason) != "" {
			return status.State.Waiting.Reason
		}
		if status.State.Terminated != nil && strings.TrimSpace(status.State.Terminated.Reason) != "" {
			return status.State.Terminated.Reason
		}
	}
	return ""
}

func hasTerminatedContainerReason(statuses []corev1.ContainerStatus, reason string) bool {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return false
	}
	for _, status := range statuses {
		if status.State.Terminated != nil && strings.TrimSpace(status.State.Terminated.Reason) == reason {
			return true
		}
	}
	return false
}

func isTerminalVeleroRuntimePod(pod corev1.Pod) bool {
	if pod.DeletionTimestamp != nil {
		return true
	}
	if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
		return true
	}
	if strings.TrimSpace(pod.Status.Reason) == "Evicted" {
		return true
	}
	return hasTerminatedContainerReason(pod.Status.InitContainerStatuses, "ContainerStatusUnknown") ||
		hasTerminatedContainerReason(pod.Status.ContainerStatuses, "ContainerStatusUnknown")
}

func summarizeVeleroPodIssue(pod corev1.Pod) string {
	if pod.Status.Phase == corev1.PodSucceeded {
		return ""
	}
	if pod.Status.Phase == corev1.PodRunning && podReady(&pod) {
		return ""
	}
	reason := podWaitingReason(pod.Status.InitContainerStatuses)
	if reason == "" {
		reason = podWaitingReason(pod.Status.ContainerStatuses)
	}
	if reason == "" {
		reason = strings.TrimSpace(pod.Status.Reason)
	}
	if reason == "" {
		reason = string(pod.Status.Phase)
	}
	if reason == "" {
		reason = "NotReady"
	}
	return fmt.Sprintf("pod %s %s", pod.Name, reason)
}

func isVeleroRuntimePod(pod corev1.Pod) bool {
	if pod.Labels != nil {
		switch strings.TrimSpace(pod.Labels["name"]) {
		case "velero", "node-agent":
			return true
		}
		if strings.TrimSpace(pod.Labels["app.kubernetes.io/name"]) == "velero" {
			return true
		}
		if strings.TrimSpace(pod.Labels["role"]) == "node-agent" {
			return true
		}
	}

	return strings.HasPrefix(pod.Name, "velero-") || strings.HasPrefix(pod.Name, "node-agent-")
}

func diagnoseVeleroStatusPending(ctx context.Context, cli client.Client, generation int64) (string, string) {
	details := make([]string, 0, 4)
	runtimeNotReady := false

	if cli != nil {
		deployment := &appsv1.Deployment{}
		if err := cli.Get(ctx, types.NamespacedName{Name: "velero", Namespace: VeleroNamespace}, deployment); err == nil && deployment.Name != "" {
			if deployment.Status.ReadyReplicas < 1 || deployment.Status.AvailableReplicas < 1 {
				runtimeNotReady = true
				details = append(details, fmt.Sprintf(
					"deployment velero ready=%d available=%d unavailable=%d",
					deployment.Status.ReadyReplicas,
					deployment.Status.AvailableReplicas,
					deployment.Status.UnavailableReplicas,
				))
			}
		}

		daemonSet := &appsv1.DaemonSet{}
		if err := cli.Get(ctx, types.NamespacedName{Name: "node-agent", Namespace: VeleroNamespace}, daemonSet); err == nil && daemonSet.Name != "" {
			if daemonSet.Status.DesiredNumberScheduled > 0 && daemonSet.Status.NumberReady < daemonSet.Status.DesiredNumberScheduled {
				runtimeNotReady = true
				details = append(details, fmt.Sprintf(
					"daemonset node-agent ready=%d desired=%d",
					daemonSet.Status.NumberReady,
					daemonSet.Status.DesiredNumberScheduled,
				))
			}
		}

		podList := &corev1.PodList{}
		if err := cli.List(ctx, podList, client.InNamespace(VeleroNamespace)); err == nil {
			for _, pod := range podList.Items {
				if !isVeleroRuntimePod(pod) {
					continue
				}
				if isTerminalVeleroRuntimePod(pod) {
					continue
				}
				summary := summarizeVeleroPodIssue(pod)
				if summary == "" {
					continue
				}
				runtimeNotReady = true
				details = append(details, summary)
				if len(details) >= 4 {
					break
				}
			}
		}
	}

	if runtimeNotReady {
		message := fmt.Sprintf("waiting for Velero runtime to become ready for %s", veleroWaitGenerationMarker(generation))
		if len(details) > 0 {
			message = fmt.Sprintf("%s: %s", message, strings.Join(details, "; "))
		}
		return clusterReasonVeleroRuntimeNotReady, message
	}

	return clusterReasonVeleroStatusSyncPending, fmt.Sprintf(
		"waiting for Velero server status request to be processed for %s",
		veleroWaitGenerationMarker(generation),
	)
}

// +kubebuilder:rbac:groups=testudo.softcdata.com,resources=clusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=testudo.softcdata.com,resources=clusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=testudo.softcdata.com,resources=clusters/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Cluster object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/reconcile
func (r *ClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)
	// when delete	cluster

	cluster := &disasterv1.Cluster{}
	err := r.Get(ctx, req.NamespacedName, cluster)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("cluster not found")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "error getting cluster")
		return ctrl.Result{}, err
	}
	logger = logger.WithValues(TraceIDKey, cluster.Annotations[AnnotationTraceID])
	ctx = context.WithValue(ctx, TraceIDKey, cluster.Annotations[AnnotationTraceID])
	defer func() {
		// If the object is being deleted and has no finalizer, it's likely gone or going, so skip status update
		if !cluster.ObjectMeta.DeletionTimestamp.IsZero() && !controllerutil.ContainsFinalizer(cluster, LabelClusterFinalizer) {
			return
		}
		// cluster.Status.UpdateTime = metav1.Now()
		err = r.updateClusterStatusWithRetry(ctx, cluster)
		if err != nil {
			// Ignore "not found" error if we just deleted it
			if apierrors.IsNotFound(err) {
				return
			}
			logger.Error(err, "unable to update cluster status")
		}
	}()

	// Handle Deletion
	if !cluster.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.handleDelete(ctx, cluster)
	}

	// Sync dependency labels
	if r.syncDependencyLabels(cluster) {
		if err := r.Update(ctx, cluster); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Add Finalizer if not present
	if !controllerutil.ContainsFinalizer(cluster, LabelClusterFinalizer) {
		controllerutil.AddFinalizer(cluster, LabelClusterFinalizer)
		if err := r.Update(ctx, cluster); err != nil {
			return ctrl.Result{}, err
		}
		// Emit 创建集群 Started event
		traceID := cluster.Annotations[AnnotationTraceID]
		user := cluster.Annotations[AnnotationUser]
		if user == "" {
			user = "system"
		}
		helper.ReportTaskStartedWithClient(ctx, r.Client, r.Scheme, cluster,
			fmt.Sprintf("创建集群 %s", cluster.Name), "-", user, traceID, "开始创建集群")
	} else {
		// Calculate metadata hash
		currentMetadataHash := r.calculateMetadataHash(cluster)

		// Check for Spec change OR Metadata change
		specChanged := cluster.Generation > cluster.Status.ObservedGeneration && cluster.Status.ObservedGeneration > 0
		metadataChanged := currentMetadataHash != cluster.Status.ObservedMetadataHash && cluster.Status.ObservedMetadataHash != ""

		if specChanged || metadataChanged {
			// Emit 编辑集群 Started event
			traceID := cluster.Annotations[AnnotationTraceID]
			user := cluster.Annotations[AnnotationUser]
			if user == "" {
				user = "system"
			}
			helper.ReportTaskStartedWithClient(ctx, r.Client, r.Scheme, cluster,
				fmt.Sprintf("编辑集群 %s", cluster.Name), "-", user, traceID, "开始更新集群配置")
		}
	}

	// 记录初始状态，用于区分"首次创建/恢复"和"定期检查"
	// 参考 storagerepository_controller.go:145 的实现
	wasReady := cluster.Status.Status == disasterv1.ClusterStatusReady

	if cluster.Status.Status == "" && cluster.Status.Status != disasterv1.ClustreStatusPending {
		cluster.Status.Status = disasterv1.ClustreStatusPending
		return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
	}

	if cluster.Status.VeleroVersion == "" {
		cluster.Status.VeleroVersion = "-"
	}

	licenseAccepted, err := r.ensureClusterLicenseAccepted(ctx, cluster)
	if err != nil {
		cluster.Status.Status = disasterv1.ClusterStatusNotReady
		cluster.Status.Reason = platformlicense.ReasonLicenseInvalid
		cluster.Status.Message = err.Error()
		logger.Error(err, "cluster license gate failed")
		helper.ReportDiagnosticEvent(r.Recorder, cluster, corev1.EventTypeWarning, platformlicense.ReasonLicenseInvalid, err.Error())
		return ctrl.Result{}, err
	}
	if !licenseAccepted {
		return ctrl.Result{RequeueAfter: 1 * time.Minute}, nil
	}

	// Check Token Expiration
	if refreshClusterTokenExpiration(cluster, time.Now()) {
		helper.ReportDiagnosticEvent(r.Recorder, cluster, corev1.EventTypeWarning, "TokenExpired", cluster.Status.Message)
		// If we are relying on this token (no kubeconfig), this is critical.
		// Even if KubeConfig is present, an expired token in Spec is worth flagging.
		// We continue execution, but the status is set to NotReady.
		// Subsequent checks might fail or succeed if KubeConfig is valid.
	}

	// TODO support token if kubeconfig is empty
	// create clientset by token

	var clientConfig *rest.Config
	if len(cluster.Spec.KubeConfig) > 0 {
		clientConfig, err = tools.GetRestConfig(cluster.Spec.KubeConfig)
		if err != nil {
			cluster.Status.Status = disasterv1.ClusterStatusNotReady
			logger.Error(err, "error getting rest config for source cluster")
			helper.ReportDiagnosticEvent(r.Recorder, cluster, corev1.EventTypeWarning, "ConfigError", err.Error())
			return ctrl.Result{}, err
		}
	} else if cluster.Spec.Token != "" && cluster.Spec.Endpoint != "" {
		clientConfig, err = tools.GetRestConfigFromToken(cluster.Spec.Endpoint, cluster.Spec.Token)
		if err != nil {
			cluster.Status.Status = disasterv1.ClusterStatusNotReady
			cluster.Status.Reason = "ConfigError"
			cluster.Status.Message = err.Error()
			logger.Error(err, "error getting rest config from token")
			helper.ReportDiagnosticEvent(r.Recorder, cluster, corev1.EventTypeWarning, "ConfigError", err.Error())
			return ctrl.Result{}, err
		}
	} else {
		err = fmt.Errorf("neither kubeconfig nor token/endpoint provided")
		cluster.Status.Status = disasterv1.ClusterStatusNotReady
		cluster.Status.Reason = "InvalidSpec"
		cluster.Status.Message = err.Error()
		logger.Error(err, "invalid cluster spec")
		helper.ReportDiagnosticEvent(r.Recorder, cluster, corev1.EventTypeWarning, "InvalidSpec", err.Error())
		return ctrl.Result{}, nil
	}

	if clientConfig != nil {
		clientConfig.QPS = 50
		clientConfig.Burst = 100
	}

	cluster.Status.Endpoint = clientConfig.Host
	kubeClientFactory := r.KubeClientFactory
	if kubeClientFactory == nil {
		kubeClientFactory = func(c *rest.Config) (kubernetes.Interface, error) {
			return kubernetes.NewForConfig(c)
		}
	}
	clientset, err := kubeClientFactory(clientConfig)
	if err != nil {
		cluster.Status.Status = disasterv1.ClusterStatusNotReady
		cluster.Status.Reason = "ClientError"
		cluster.Status.Message = err.Error()
		logger.Error(err, "unable to create client from kubeconfig")
		helper.ReportDiagnosticEvent(r.Recorder, cluster, corev1.EventTypeWarning, "ClientError", err.Error())
		return ctrl.Result{}, err
	}

	remoteCli, err := r.newTargetClusterClient(clientConfig)
	if err != nil {
		cluster.Status.Status = disasterv1.ClusterStatusNotReady
		cluster.Status.Reason = "ClientError"
		cluster.Status.Message = err.Error()
		logger.Error(err, "unable to create remote client from kubeconfig")
		helper.ReportDiagnosticEvent(r.Recorder, cluster, corev1.EventTypeWarning, "ClientError", err.Error())
		return ctrl.Result{}, err
	}

	// Process ensure-storage signal (Storage Connectivity Check)
	if storageName, ok := cluster.Annotations[AnnotationEnsureStorage]; ok {
		sourceCluster := strings.TrimSpace(cluster.Annotations[AnnotationEnsureStorageSourceCluster])
		bslClusterName := cluster.Name
		if sourceCluster != "" {
			bslClusterName = sourceCluster
		}
		logger.Info("Processing ensure-storage signal", "storage", storageName, "sourceCluster", sourceCluster, "bslCluster", bslClusterName)

		// 2. Fetch StorageRepository
		sr := &disasterv1.StorageRepository{}
		if err := r.Get(ctx, types.NamespacedName{Name: storageName, Namespace: r.managementNamespace()}, sr); err != nil {
			logger.Error(err, "failed to get StorageRepository for signal", "name", storageName)
			if apierrors.IsNotFound(err) {
				// Invalid storage name, remove annotation to stop loop
				delete(cluster.Annotations, AnnotationEnsureStorage)
				delete(cluster.Annotations, AnnotationEnsureStorageSourceCluster)
				helper.ReportDiagnosticEvent(r.Recorder, cluster, corev1.EventTypeWarning, "EnsureStorageFailed", fmt.Sprintf("StorageRepository %s not found", storageName))
				if updateErr := r.Update(ctx, cluster); updateErr != nil {
					return ctrl.Result{}, updateErr
				}
				// Keep periodic checks alive even after processing one-shot ensure-storage signal.
				return ctrl.Result{RequeueAfter: 1 * time.Minute}, nil
			}
			return ctrl.Result{}, err
		}

		// 3. Apply BSL
		bslName := fmt.Sprintf("%s-%s", storageName, bslClusterName)
		if err := r.BSL.ApplyStorageRepository(ctx, r.Client, remoteCli, sr, bslName, bslClusterName); err != nil {
			logger.Error(err, "failed to apply BSL for signal")
			return ctrl.Result{}, err
		}

		// 4. Remove Annotation (Ack)
		delete(cluster.Annotations, AnnotationEnsureStorage)
		delete(cluster.Annotations, AnnotationEnsureStorageSourceCluster)
		if err := r.Update(ctx, cluster); err != nil {
			return ctrl.Result{}, err
		}
		logger.Info("Successfully processed ensure-storage signal", "storage", storageName, "sourceCluster", sourceCluster, "bslCluster", bslClusterName)
		// Continue periodic health/stats checks after one-shot ensure-storage handling.
		return ctrl.Result{RequeueAfter: 1 * time.Minute}, nil
	}

	logger.Info("Checking if Velero is installed")
	installed, err := r.IsVeleroInstalled(ctx, remoteCli)
	logger.Info("IsVeleroInstalled result", "installed", installed, "error", err)
	if err != nil {
		markClusterNotReady(cluster, "CheckVeleroFailed", err)
		logger.Error(err, "error checking velero installation")
		helper.ReportDiagnosticEvent(r.Recorder, cluster, corev1.EventTypeWarning, "CheckVeleroFailed", err.Error())
		return ctrl.Result{}, err
	}

	if !installed {
		logger.Info("Installing Velero")
		traceID := cluster.Annotations[AnnotationTraceID]
		user := cluster.Annotations["testudo.softcdata.com/user"]
		if user == "" {
			user = "system"
		}
		helper.ReportTaskProgressWithClient(ctx, r.Client, r.Scheme, cluster, fmt.Sprintf("创建集群 %s", cluster.Name), "-", user, traceID, "检测到 Velero 未安装，开始安装流程...")

		err := r.InstallVeleroInCluster(ctx, cluster)
		if err != nil {
			cluster.Status.Status = disasterv1.ClusterStatusNotReady
			cluster.Status.Reason = "InstallVeleroFailed"
			cluster.Status.Message = err.Error()
			logger.Error(err, "error installing velero")
			helper.ReportDiagnosticEvent(r.Recorder, cluster, corev1.EventTypeWarning, "InstallVeleroFailed", err.Error())
			helper.ReportTaskFinishedWithClient(ctx, r.Client, r.Scheme, cluster,
				fmt.Sprintf("创建集群 %s", cluster.Name), "-", helper.TaskStatusFailed,
				&cluster.CreationTimestamp, nil, user, traceID, "Velero安装失败: "+err.Error(), cluster.Status.Reason)
			return ctrl.Result{}, err
		}
		helper.ReportDiagnosticEvent(r.Recorder, cluster, corev1.EventTypeNormal, "InstallVeleroSuccess", "Velero installed successfully")
	} else {
		if err := r.syncVeleroRegistrySecretToTargetCluster(ctx, cluster, remoteCli); err != nil {
			cluster.Status.Status = disasterv1.ClusterStatusNotReady
			cluster.Status.Reason = "VeleroRegistrySecretSyncFailed"
			cluster.Status.Message = err.Error()
			logger.Error(err, "error syncing velero registry secret to target cluster")
			helper.ReportDiagnosticEvent(r.Recorder, cluster, corev1.EventTypeWarning, "VeleroRegistrySecretSyncFailed", err.Error())
			return ctrl.Result{}, err
		}

		needsInitialInstallReconcile := shouldReconcileExistingVeleroInstall(cluster)
		needsSpecUpgrade := cluster.Generation > cluster.Status.ObservedGeneration && cluster.Status.ObservedGeneration > 0 && !isClusterWaitingForCurrentVeleroStatus(cluster)
		if needsInitialInstallReconcile || needsSpecUpgrade {
			logMessage := "Reconciling existing Velero install for initial cluster configuration"
			errMessage := "error reconciling existing velero install for initial cluster configuration"
			if needsSpecUpgrade {
				logMessage = "Reconciling Velero install after cluster spec change"
				errMessage = "error upgrading velero after cluster spec change"
			}
			logger.Info(logMessage)
			if err := r.InstallVeleroInCluster(ctx, cluster); err != nil {
				cluster.Status.Status = disasterv1.ClusterStatusNotReady
				cluster.Status.Reason = "InstallVeleroFailed"
				cluster.Status.Message = err.Error()
				logger.Error(err, errMessage)
				helper.ReportDiagnosticEvent(r.Recorder, cluster, corev1.EventTypeWarning, "InstallVeleroFailed", err.Error())
				return ctrl.Result{}, err
			}
		}
	}

	version, err := clientset.Discovery().ServerVersion()
	if err != nil {
		markClusterNotReady(cluster, "VersionCheckFailed", err)
		logger.Error(err, "unable to get cluster version")
		helper.ReportDiagnosticEvent(r.Recorder, cluster, corev1.EventTypeWarning, "VersionCheckFailed", err.Error())
		return ctrl.Result{}, err
	}
	cluster.Status.K8SVersion = version.GitVersion
	logger.Info("cluster version", "version", version)

	//获取节点数量
	nodes, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		markClusterNotReady(cluster, "ListNodesFailed", err)
		logger.Error(err, "unable to list nodes")
		helper.ReportDiagnosticEvent(r.Recorder, cluster, corev1.EventTypeWarning, "ListNodesFailed", err.Error())
		return ctrl.Result{}, err
	}
	cluster.Status.NodeCount = len(nodes.Items)
	cli := remoteCli

	veleroVersion, err := r.checkVeleroVersion(ctx, cli, cluster)
	if err != nil {
		markClusterNotReady(cluster, "VeleroVersionCheckFailed", err)
		cluster.Status.VeleroVersion = "-"
		logger.Error(err, "error checking velero version")
		helper.ReportDiagnosticEvent(r.Recorder, cluster, corev1.EventTypeWarning, "VeleroVersionCheckFailed", err.Error())
		return ctrl.Result{}, err
	}
	if veleroVersion == "" {
		pendingReason, pendingMessage := diagnoseVeleroStatusPending(ctx, cli, cluster.Generation)
		if canReuseObservedVeleroVersion(cluster, wasReady) {
			veleroVersion = strings.TrimSpace(cluster.Status.VeleroVersion)
		} else {
			cluster.Status.Status = disasterv1.ClusterStatusNotReady
			cluster.Status.Reason = pendingReason
			cluster.Status.Message = pendingMessage
			return ctrl.Result{RequeueAfter: 3 * time.Second}, nil
		}
	}
	cluster.Status.VeleroVersion = veleroVersion

	runtimeReason, runtimeMessage := diagnoseVeleroStatusPending(ctx, cli, cluster.Generation)
	if runtimeReason == clusterReasonVeleroRuntimeNotReady {
		cluster.Status.Status = disasterv1.ClusterStatusNotReady
		cluster.Status.Reason = runtimeReason
		cluster.Status.Message = runtimeMessage
		return ctrl.Result{RequeueAfter: 3 * time.Second}, nil
	}

	if reason, message := r.checkVeleroCompatibility(ctx, cli, veleroVersion); reason != "" {
		cluster.Status.Status = disasterv1.ClusterStatusNotReady
		cluster.Status.Reason = reason
		cluster.Status.Message = message
		helper.ReportDiagnosticEvent(r.Recorder, cluster, corev1.EventTypeWarning, veleroCompatibilityFailedEventReason, message)
		r.reportCreateClusterFailedForCompatibility(ctx, cluster, reason, message)
		return ctrl.Result{}, fmt.Errorf("%s: %s", reason, message)
	}

	cluster.Status.Status = disasterv1.ClusterStatusReady
	cluster.Status.Reason = ""
	cluster.Status.Message = ""

	// 创建集群完成事件（仅在状态从 非Ready -> Ready 转变时发射）
	// 如果之前已经是 Ready（wasReady=true），说明是定期检查，不发射事件
	if !wasReady && cluster.Status.ObservedGeneration == 0 {
		traceID := cluster.Annotations[AnnotationTraceID]
		user := cluster.Annotations[AnnotationUser]
		if user == "" {
			user = "system"
		}
		now := metav1.Now()
		cluster.Status.ReadyTimestamp = &now
		helper.ReportTaskFinishedWithClient(ctx, r.Client, r.Scheme, cluster,
			fmt.Sprintf("创建集群 %s", cluster.Name), "-", helper.TaskStatusSuccess,
			&cluster.CreationTimestamp, &now, user, traceID, "集群创建完成")
		cluster.Status.LastEventPhase = string(disasterv1.ClusterStatusReady)
	} else {
		currentMetadataHash := r.calculateMetadataHash(cluster)
		specChanged := cluster.Generation > cluster.Status.ObservedGeneration && cluster.Status.ObservedGeneration > 0
		metadataChanged := currentMetadataHash != cluster.Status.ObservedMetadataHash && cluster.Status.ObservedMetadataHash != ""

		if specChanged || metadataChanged {
			// Emit 编辑集群 Finished event
			traceID := cluster.Annotations[AnnotationTraceID]
			user := cluster.Annotations[AnnotationUser]
			if user == "" {
				user = "system"
			}
			now := metav1.Now()
			helper.ReportTaskFinishedWithClient(ctx, r.Client, r.Scheme, cluster,
				fmt.Sprintf("编辑集群 %s", cluster.Name), "-", helper.TaskStatusSuccess,
				&now, &now, user, traceID, "集群配置更新完成")
		}
	}

	// 更新 ObservedGeneration 和 ObservedMetadataHash
	cluster.Status.ObservedGeneration = cluster.Generation
	cluster.Status.ObservedMetadataHash = r.calculateMetadataHash(cluster)

	if handled, err := r.processRefreshClusterStatsSignal(ctx, cli, clientset.Discovery(), cluster); err != nil {
		logger.Error(err, "failed to process refresh-cluster-stats signal")
		return ctrl.Result{}, err
	} else if handled {
		return ctrl.Result{RequeueAfter: 1 * time.Minute}, nil
	}

	// Collect Stats
	if err := r.collectClusterStats(ctx, cli, clientset.Discovery(), cluster); err != nil {
		logger.Error(err, "failed to collect cluster stats")
		helper.ReportDiagnosticEvent(r.Recorder, cluster, corev1.EventTypeWarning, "CollectStatsFailed", err.Error())
	} else {
		// Update Labels
		if cluster.Labels == nil {
			cluster.Labels = make(map[string]string)
		}
		cluster.Labels[LabelClusterName] = cluster.Name
		cluster.Labels[LabelClusterNamespaceCount] = strconv.Itoa(cluster.Status.NamespaceCount)
		cluster.Labels[LabelClusterResourceTotalCount] = strconv.Itoa(cluster.Status.ResourceTotalCount)
		cluster.Labels[LabelClusterWorkloadNamespaceCount] = strconv.Itoa(cluster.Status.WorkloadNamespaceCount)
		cluster.Labels[LabelClusterWorkloadTotalCount] = strconv.Itoa(cluster.Status.WorkloadTotalCount)

		if err := r.updateClusterLabelsWithRetry(ctx, cluster, clusterOwnedStatsLabels(cluster)); err != nil {
			logger.Error(err, "unable to update cluster labels")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{RequeueAfter: 1 * time.Minute}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.BSL == nil {
		r.BSL = &DefaultBSL{}
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&disasterv1.Cluster{}).
		WithEventFilter(IgnoreStatusUpdatesPredicate{}). // 应用事件过滤器
		Named("cluster").
		Complete(r)
}

// check velero version
func (r *ClusterReconciler) checkVeleroVersion(ctx context.Context, cli client.Client, cluster *disasterv1.Cluster) (string, error) {
	logger := logf.FromContext(ctx)
	ssrName := fmt.Sprintf("disaster-cluster-operator-%s", cluster.Name)
	ssr := &velerov1.ServerStatusRequest{}
	time.Sleep(100 * time.Millisecond)
	err := cli.Get(ctx, types.NamespacedName{Name: ssrName, Namespace: VeleroNamespace}, ssr)
	if err != nil {
		if apierrors.IsNotFound(err) {
			ssr := &velerov1.ServerStatusRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      ssrName,
					Namespace: VeleroNamespace,
				},
			}
			err := cli.Create(ctx, ssr)
			if err != nil {
				logger.Error(err, "error creating ServerStatusRequest")
				return "", err
			}
			logger.Info("velero server status request created", "name", ssr.Name)
			return "", err
		}
		logger.Error(err, "error getting ServerStatusRequest")
		return "", err
	}
	return ssr.Status.ServerVersion, nil
}

func (r *ClusterReconciler) checkVeleroCompatibility(ctx context.Context, cli client.Client, veleroVersion string) (reason string, message string) {
	if msg := checkVeleroVersionCompatibility(veleroVersion); msg != "" {
		return clusterReasonVeleroVersionIncompatible, msg
	}
	return checkVeleroCRDCompatibility(ctx, cli)
}

func checkVeleroVersionCompatibility(veleroVersion string) string {
	actual, err := semver.ParseTolerant(veleroVersion)
	if err != nil {
		return fmt.Sprintf("velero version incompatible: expected >=%s,<%s, actual %q (parse error: %v)", veleroSupportedVersionMin, veleroSupportedVersionMax, veleroVersion, err)
	}
	min := semver.MustParse(veleroSupportedVersionMin)
	max := semver.MustParse(veleroSupportedVersionMax)
	if actual.LT(min) || !actual.LT(max) {
		return fmt.Sprintf("velero version incompatible: expected >=%s,<%s, actual %s", veleroSupportedVersionMin, veleroSupportedVersionMax, veleroVersion)
	}
	return ""
}

func checkVeleroCRDCompatibility(ctx context.Context, cli client.Client) (reason string, message string) {
	for _, crdName := range requiredVeleroCRDNames {
		crd := &apiextensionsv1.CustomResourceDefinition{}
		err := cli.Get(ctx, types.NamespacedName{Name: crdName}, crd)
		if err != nil {
			if apierrors.IsNotFound(err) {
				return clusterReasonVeleroCRDVersionIncompatible, fmt.Sprintf("velero crd incompatible: %s not found", crdName)
			}
			return clusterReasonVeleroCRDCheckFailed, fmt.Sprintf("failed to validate velero crd compatibility: get %s: %v", crdName, err)
		}

		versionFound := false
		versionServed := false
		versionStorage := false
		for _, ver := range crd.Spec.Versions {
			if ver.Name != veleroRequiredCRDVersion {
				continue
			}
			versionFound = true
			versionServed = ver.Served
			versionStorage = ver.Storage
			break
		}
		if !versionFound {
			return clusterReasonVeleroCRDVersionIncompatible, fmt.Sprintf("velero crd incompatible: %s requires version %s", crdName, veleroRequiredCRDVersion)
		}
		if !versionServed {
			return clusterReasonVeleroCRDVersionIncompatible, fmt.Sprintf("velero crd incompatible: %s requires served version %s", crdName, veleroRequiredCRDVersion)
		}
		if !versionStorage {
			return clusterReasonVeleroCRDVersionIncompatible, fmt.Sprintf("velero crd incompatible: %s requires storage version %s", crdName, veleroRequiredCRDVersion)
		}
	}
	return "", ""
}

func (r *ClusterReconciler) reportCreateClusterFailedForCompatibility(ctx context.Context, cluster *disasterv1.Cluster, reason, message string) {
	// 仅在创建集群首轮失败时发射结束事件，避免后续重试事件风暴。
	if cluster.Status.ObservedGeneration != 0 {
		return
	}
	if cluster.Status.LastEventPhase == string(disasterv1.ClusterStatusNotReady) {
		return
	}
	traceID := cluster.Annotations[AnnotationTraceID]
	user := cluster.Annotations[AnnotationUser]
	if user == "" {
		user = "system"
	}
	helper.ReportTaskFinishedWithClient(ctx, r.Client, r.Scheme, cluster,
		fmt.Sprintf("创建集群 %s", cluster.Name), "-", helper.TaskStatusFailed,
		&cluster.CreationTimestamp, nil, user, traceID, message, reason)
	cluster.Status.LastEventPhase = string(disasterv1.ClusterStatusNotReady)
}

func (r *ClusterReconciler) syncDependencyLabels(cluster *disasterv1.Cluster) bool {
	if cluster.Labels == nil {
		cluster.Labels = make(map[string]string)
	}
	_, _, tokenChanged := EnsureDependencyTokenLabel(cluster.Labels, string(cluster.UID))
	_, depChanged := RebuildDependencyToLabels(cluster.Labels, nil)
	return tokenChanged || depChanged
}

func (r *ClusterReconciler) updateClusterStatusWithRetry(ctx context.Context, cluster *disasterv1.Cluster) error {
	if cluster == nil {
		return nil
	}

	desiredStatus := cluster.Status
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		latest := &disasterv1.Cluster{}
		if err := r.Get(ctx, client.ObjectKeyFromObject(cluster), latest); err != nil {
			return err
		}
		latest.Status = desiredStatus
		if err := r.Status().Update(ctx, latest); err != nil {
			return err
		}
		cluster.Status = latest.Status
		cluster.ResourceVersion = latest.ResourceVersion
		return nil
	})
}

func (r *ClusterReconciler) updateClusterLabelsWithRetry(ctx context.Context, cluster *disasterv1.Cluster, ownedLabels map[string]string) error {
	if cluster == nil || len(ownedLabels) == 0 {
		return nil
	}

	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		latest := &disasterv1.Cluster{}
		if err := r.Get(ctx, client.ObjectKeyFromObject(cluster), latest); err != nil {
			return err
		}
		if latest.Labels == nil {
			latest.Labels = make(map[string]string)
		}
		if cluster.Labels == nil {
			cluster.Labels = make(map[string]string)
		}
		for key, value := range ownedLabels {
			latest.Labels[key] = value
			cluster.Labels[key] = value
		}
		if err := r.Update(ctx, latest); err != nil {
			return err
		}
		cluster.ResourceVersion = latest.ResourceVersion
		return nil
	})
}

func (r *ClusterReconciler) updateClusterMetadataWithRetry(ctx context.Context, cluster *disasterv1.Cluster, mutate func(*disasterv1.Cluster)) error {
	if cluster == nil || mutate == nil {
		return nil
	}

	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		latest := &disasterv1.Cluster{}
		if err := r.Get(ctx, client.ObjectKeyFromObject(cluster), latest); err != nil {
			return err
		}
		mutate(latest)
		if err := r.Update(ctx, latest); err != nil {
			return err
		}
		cluster.ObjectMeta = latest.ObjectMeta
		cluster.ResourceVersion = latest.ResourceVersion
		return nil
	})
}

func clusterOwnedStatsLabels(cluster *disasterv1.Cluster) map[string]string {
	return map[string]string{
		LabelClusterName:                   cluster.Name,
		LabelClusterNamespaceCount:         strconv.Itoa(cluster.Status.NamespaceCount),
		LabelClusterResourceTotalCount:     strconv.Itoa(cluster.Status.ResourceTotalCount),
		LabelClusterWorkloadNamespaceCount: strconv.Itoa(cluster.Status.WorkloadNamespaceCount),
		LabelClusterWorkloadTotalCount:     strconv.Itoa(cluster.Status.WorkloadTotalCount),
	}
}

var clusterStatsFallbackNamespacedResourceGVKs = []schema.GroupVersionKind{
	{Group: "apps", Version: "v1", Kind: "Deployment"},
	{Group: "apps", Version: "v1", Kind: "StatefulSet"},
	{Group: "apps", Version: "v1", Kind: "DaemonSet"},
	{Group: "batch", Version: "v1", Kind: "Job"},
	{Group: "batch", Version: "v1", Kind: "CronJob"},
	{Group: "", Version: "v1", Kind: "Service"},
	{Group: "", Version: "v1", Kind: "ConfigMap"},
	{Group: "", Version: "v1", Kind: "Secret"},
	{Group: "", Version: "v1", Kind: "ServiceAccount"},
	{Group: "networking.k8s.io", Version: "v1", Kind: "Ingress"},
	{Group: "networking.k8s.io", Version: "v1", Kind: "NetworkPolicy"},
	{Group: "policy", Version: "v1", Kind: "PodDisruptionBudget"},
	{Group: "autoscaling", Version: "v2", Kind: "HorizontalPodAutoscaler"},
	{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "Role"},
	{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "RoleBinding"},
}

var clusterStatsExcludedResourceKeys = map[string]struct{}{
	"pods":                            {},
	"persistentvolumeclaims":          {},
	"persistentvolumes":               {},
	"events":                          {},
	"events.events.k8s.io":            {},
	"leases.coordination.k8s.io":      {},
	"endpoints":                       {},
	"endpointslices.discovery.k8s.io": {},
	"controllerrevisions.apps":        {},
}

type clusterNamespaceStatsSnapshot struct {
	TrackedNamespaces []string
	NamespaceStats    map[string]int
	ResourceTotal     int
}

func clusterNamespacedResourceGVKs() []schema.GroupVersionKind {
	return append([]schema.GroupVersionKind(nil), clusterStatsFallbackNamespacedResourceGVKs...)
}

func clusterStatsTrackedNamespaceNames(nsList *corev1.NamespaceList) []string {
	if nsList == nil || len(nsList.Items) == 0 {
		return nil
	}
	namespaces := make([]string, 0, len(nsList.Items))
	for _, ns := range nsList.Items {
		if isClusterStatsNamespaceExcluded(ns.Name) {
			continue
		}
		namespaces = append(namespaces, ns.Name)
	}
	sort.Strings(namespaces)
	return namespaces
}

func isClusterStatsNamespaceExcluded(namespace string) bool {
	switch strings.TrimSpace(namespace) {
	case "", VeleroNamespace, "kube-system":
		return true
	default:
		return false
	}
}

func clusterStatsResourceKey(resource, group string) string {
	resource = strings.ToLower(strings.TrimSpace(resource))
	group = strings.ToLower(strings.TrimSpace(group))
	if group == "" {
		return resource
	}
	return resource + "." + group
}

func isClusterStatsResourceExcluded(resource, group string) bool {
	if _, exists := clusterStatsExcludedResourceKeys[clusterStatsResourceKey(resource, group)]; exists {
		return true
	}
	_, exists := clusterStatsExcludedResourceKeys[strings.ToLower(strings.TrimSpace(resource))]
	return exists
}

func supportsListVerb(verbs metav1.Verbs) bool {
	for _, verb := range verbs {
		if verb == "list" {
			return true
		}
	}
	return false
}

func clusterBackupableNamespacedResourceGVKs(disco discovery.DiscoveryInterface) []schema.GroupVersionKind {
	if disco == nil {
		return append([]schema.GroupVersionKind(nil), clusterStatsFallbackNamespacedResourceGVKs...)
	}

	resourceLists, err := disco.ServerPreferredNamespacedResources()
	if err != nil && len(resourceLists) == 0 {
		return append([]schema.GroupVersionKind(nil), clusterStatsFallbackNamespacedResourceGVKs...)
	}

	seen := make(map[schema.GroupVersionKind]struct{})
	out := make([]schema.GroupVersionKind, 0, len(clusterStatsFallbackNamespacedResourceGVKs))

	for _, resourceList := range resourceLists {
		gv, err := schema.ParseGroupVersion(resourceList.GroupVersion)
		if err != nil {
			continue
		}
		for _, resource := range resourceList.APIResources {
			if !resource.Namespaced || strings.Contains(resource.Name, "/") || !supportsListVerb(resource.Verbs) {
				continue
			}
			if isClusterStatsResourceExcluded(resource.Name, gv.Group) {
				continue
			}

			gvk := schema.GroupVersionKind{Group: gv.Group, Version: gv.Version, Kind: resource.Kind}
			if _, exists := seen[gvk]; exists {
				continue
			}
			seen[gvk] = struct{}{}
			out = append(out, gvk)
		}
	}

	if len(out) == 0 {
		return append([]schema.GroupVersionKind(nil), clusterStatsFallbackNamespacedResourceGVKs...)
	}

	sort.Slice(out, func(i, j int) bool {
		left := out[i]
		right := out[j]
		if left.Group != right.Group {
			return left.Group < right.Group
		}
		if left.Version != right.Version {
			return left.Version < right.Version
		}
		return left.Kind < right.Kind
	})

	return out
}

func collectClusterNamespaceStatsSnapshot(ctx context.Context, cli client.Client, disco discovery.DiscoveryInterface) (clusterNamespaceStatsSnapshot, error) {
	nsList := &corev1.NamespaceList{}
	if err := cli.List(ctx, nsList); err != nil {
		return clusterNamespaceStatsSnapshot{}, err
	}

	trackedNamespaces := clusterStatsTrackedNamespaceNames(nsList)
	stats := make(map[string]int, len(trackedNamespaces))
	if len(trackedNamespaces) == 0 {
		return clusterNamespaceStatsSnapshot{
			TrackedNamespaces: trackedNamespaces,
			NamespaceStats:    stats,
		}, nil
	}

	trackedSet := make(map[string]struct{}, len(trackedNamespaces))
	for _, namespace := range trackedNamespaces {
		trackedSet[namespace] = struct{}{}
		stats[namespace] = 0
	}

	for _, gvk := range clusterBackupableNamespacedResourceGVKs(disco) {
		list := &metav1.PartialObjectMetadataList{}
		list.SetGroupVersionKind(gvk)
		if err := cli.List(ctx, list); err != nil {
			continue
		}
		for _, item := range list.Items {
			if _, exists := trackedSet[item.Namespace]; !exists {
				continue
			}
			stats[item.Namespace]++
		}
	}

	total := 0
	for _, namespace := range trackedNamespaces {
		total += stats[namespace]
	}

	return clusterNamespaceStatsSnapshot{
		TrackedNamespaces: trackedNamespaces,
		NamespaceStats:    stats,
		ResourceTotal:     total,
	}, nil
}

func applyNamespaceStatsSnapshot(cluster *disasterv1.Cluster, snapshot clusterNamespaceStatsSnapshot) {
	cluster.Status.NamespaceCount = len(snapshot.TrackedNamespaces)
	cluster.Status.NamespaceStats = make(map[string]int, len(snapshot.NamespaceStats))
	for _, namespace := range snapshot.TrackedNamespaces {
		cluster.Status.NamespaceStats[namespace] = snapshot.NamespaceStats[namespace]
	}
	cluster.Status.ResourceTotalCount = snapshot.ResourceTotal
}

func applyWorkloadNamespaceStatsSnapshot(cluster *disasterv1.Cluster, snapshot clusterNamespaceStatsSnapshot, runningNamespaces map[string]struct{}) {
	cluster.Status.WorkloadNamespaceStats = make(map[string]int)
	cluster.Status.WorkloadNamespaceCount = 0
	cluster.Status.WorkloadTotalCount = 0

	for _, namespace := range snapshot.TrackedNamespaces {
		if _, exists := runningNamespaces[namespace]; !exists {
			continue
		}
		count := snapshot.NamespaceStats[namespace]
		cluster.Status.WorkloadNamespaceStats[namespace] = count
		cluster.Status.WorkloadNamespaceCount++
		cluster.Status.WorkloadTotalCount += count
	}
}

func listRunningWorkloadNamespaces(ctx context.Context, cli client.Client) (map[string]struct{}, error) {
	namespaces := make(map[string]struct{})

	deployments := &appsv1.DeploymentList{}
	if err := cli.List(ctx, deployments); err != nil {
		return nil, err
	}
	for _, deployment := range deployments.Items {
		if deployment.Status.ReadyReplicas > 0 || deployment.Status.AvailableReplicas > 0 {
			namespaces[deployment.Namespace] = struct{}{}
		}
	}

	statefulSets := &appsv1.StatefulSetList{}
	if err := cli.List(ctx, statefulSets); err != nil {
		return nil, err
	}
	for _, sts := range statefulSets.Items {
		if sts.Status.ReadyReplicas > 0 {
			namespaces[sts.Namespace] = struct{}{}
		}
	}

	return namespaces, nil
}

func (r *ClusterReconciler) managementNamespace() string {
	if ns := strings.TrimSpace(r.ManagementNamespace); ns != "" {
		return ns
	}
	return ManagementNamespace()
}

func resolveVeleroAssetPath(explicitPath, defaultPath string) string {
	candidates := make([]string, 0, 3)
	if p := strings.TrimSpace(explicitPath); p != "" {
		candidates = append(candidates, p)
	} else {
		candidates = append(candidates, defaultPath)
	}
	if _, file, _, ok := stdruntime.Caller(0); ok && file != "" {
		base := filepath.Dir(file)
		candidates = append(candidates, filepath.Join(base, "..", "..", strings.TrimPrefix(defaultPath, "./")))
	}
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate) == "" {
			continue
		}
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	if len(candidates) > 0 {
		return candidates[0]
	}
	return defaultPath
}

func (r *ClusterReconciler) veleroChartPath() string {
	return resolveVeleroAssetPath(r.VeleroChartPath, defaultVeleroChartPath)
}

func (r *ClusterReconciler) veleroValuesPath() string {
	return resolveVeleroAssetPath(r.VeleroValuesPath, defaultVeleroValuesPath)
}

func (r *ClusterReconciler) buildClusterAccess(cluster *disasterv1.Cluster) ([]byte, *rest.Config, error) {
	if len(cluster.Spec.KubeConfig) > 0 {
		clientConfig, err := tools.GetRestConfig(cluster.Spec.KubeConfig)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to parse kubeconfig: %w", err)
		}
		return cluster.Spec.KubeConfig, clientConfig, nil
	}
	if cluster.Spec.Token != "" && cluster.Spec.Endpoint != "" {
		kubeConfigBytes, err := tools.GenerateKubeConfigFromToken(cluster.Spec.Endpoint, cluster.Spec.Token)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to generate kubeconfig from token: %w", err)
		}
		clientConfig, err := tools.GetRestConfigFromToken(cluster.Spec.Endpoint, cluster.Spec.Token)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get rest config from token: %w", err)
		}
		return kubeConfigBytes, clientConfig, nil
	}
	return nil, nil, fmt.Errorf("neither kubeconfig nor token/endpoint provided")
}

func (r *ClusterReconciler) writeTempKubeconfig(clusterName, prefix string, kubeConfigBytes []byte) (string, func(), error) {
	tmpFile, err := os.CreateTemp("", fmt.Sprintf("%s-%s-*.yaml", prefix, clusterName))
	if err != nil {
		return "", nil, fmt.Errorf("failed to create temp kubeconfig file: %w", err)
	}
	if _, err := tmpFile.Write(kubeConfigBytes); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpFile.Name())
		return "", nil, fmt.Errorf("failed to write kubeconfig to temp file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpFile.Name())
		return "", nil, fmt.Errorf("failed to close temp kubeconfig file: %w", err)
	}
	return tmpFile.Name(), func() { _ = os.Remove(tmpFile.Name()) }, nil
}

func (r *ClusterReconciler) newTargetClusterClient(clientConfig *rest.Config) (client.Client, error) {
	clientFactory := r.ClientFactory
	if clientFactory == nil {
		clientFactory = client.New
	}
	return clientFactory(clientConfig, client.Options{Scheme: r.Scheme})
}

func veleroRegistryTargetSecretName(clusterName string) string {
	return fmt.Sprintf("velero-regcred-%s", clusterName)
}

func (r *ClusterReconciler) currentVeleroTargetSecretName(cluster *disasterv1.Cluster) string {
	if cluster.Spec.VeleroInstall == nil || cluster.Spec.VeleroInstall.RegistryCredentialSecretRef == nil || strings.TrimSpace(cluster.Spec.VeleroInstall.RegistryCredentialSecretRef.Name) == "" {
		return ""
	}
	return veleroRegistryTargetSecretName(cluster.Name)
}

func normalizeVeleroImageRegistry(registry string) string {
	return strings.Trim(strings.TrimSpace(registry), "/")
}

func joinRegistryRepository(registry, name string) string {
	registry = normalizeVeleroImageRegistry(registry)
	if registry == "" {
		return name
	}
	return fmt.Sprintf("%s/%s", registry, name)
}

func rewriteImageReferenceToRegistry(imageRef, registry string) string {
	registry = normalizeVeleroImageRegistry(registry)
	if imageRef == "" || registry == "" {
		return imageRef
	}

	ref := imageRef
	digest := ""
	if idx := strings.Index(ref, "@"); idx >= 0 {
		digest = ref[idx:]
		ref = ref[:idx]
	}

	tag := ""
	lastSlash := strings.LastIndex(ref, "/")
	lastColon := strings.LastIndex(ref, ":")
	if lastColon > lastSlash {
		tag = ref[lastColon:]
		ref = ref[:lastColon]
	}

	name := ref
	if lastSlash >= 0 {
		name = ref[lastSlash+1:]
	}
	return fmt.Sprintf("%s/%s%s%s", registry, name, tag, digest)
}

func ensureObjectMap(parent map[string]interface{}, key string) map[string]interface{} {
	if existing, ok := parent[key].(map[string]interface{}); ok {
		return existing
	}
	child := make(map[string]interface{})
	parent[key] = child
	return child
}

func (r *ClusterReconciler) buildVeleroValuesFile(cluster *disasterv1.Cluster, targetPullSecretName string) (string, func(), error) {
	baseValues, err := os.ReadFile(r.veleroValuesPath())
	if err != nil {
		return "", nil, fmt.Errorf("failed to read velero values file: %w", err)
	}

	values := make(map[string]interface{})
	if err := yaml.Unmarshal(baseValues, &values); err != nil {
		return "", nil, fmt.Errorf("failed to parse velero values file: %w", err)
	}

	imageRegistry := ""
	if cluster.Spec.VeleroInstall != nil {
		imageRegistry = normalizeVeleroImageRegistry(cluster.Spec.VeleroInstall.ImageRegistry)
	}

	imageValues := ensureObjectMap(values, "image")
	if imageRegistry != "" {
		imageValues["repository"] = joinRegistryRepository(imageRegistry, "velero")
	}
	if targetPullSecretName != "" {
		imageValues["imagePullSecrets"] = []string{targetPullSecretName}
	} else {
		imageValues["imagePullSecrets"] = []string{}
	}

	kubectlValues := ensureObjectMap(values, "kubectl")
	kubectlImageValues := ensureObjectMap(kubectlValues, "image")
	if imageRegistry != "" {
		kubectlImageValues["repository"] = joinRegistryRepository(imageRegistry, "kubectl")
	}

	if imageRegistry != "" {
		if initContainers, ok := values["initContainers"].([]interface{}); ok {
			for i := range initContainers {
				containerMap, ok := initContainers[i].(map[string]interface{})
				if !ok {
					continue
				}
				imageRef, _ := containerMap["image"].(string)
				if imageRef == "" {
					continue
				}
				containerMap["image"] = rewriteImageReferenceToRegistry(imageRef, imageRegistry)
			}
		}
	}

	renderedValues, err := yaml.Marshal(values)
	if err != nil {
		return "", nil, fmt.Errorf("failed to render velero values overlay: %w", err)
	}

	tmpFile, err := os.CreateTemp("", fmt.Sprintf("velero-values-%s-*.yaml", cluster.Name))
	if err != nil {
		return "", nil, fmt.Errorf("failed to create temp velero values file: %w", err)
	}
	if _, err := tmpFile.Write(renderedValues); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpFile.Name())
		return "", nil, fmt.Errorf("failed to write temp velero values file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpFile.Name())
		return "", nil, fmt.Errorf("failed to close temp velero values file: %w", err)
	}
	return tmpFile.Name(), func() { _ = os.Remove(tmpFile.Name()) }, nil
}

func (r *ClusterReconciler) ensureNamespace(ctx context.Context, cli client.Client, namespace string) error {
	ns := &corev1.Namespace{}
	err := cli.Get(ctx, types.NamespacedName{Name: namespace}, ns)
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return err
	}
	return cli.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}})
}

func (r *ClusterReconciler) syncVeleroRegistrySecretToTargetCluster(ctx context.Context, cluster *disasterv1.Cluster, destClient client.Client) error {
	if cluster.Spec.VeleroInstall == nil || cluster.Spec.VeleroInstall.RegistryCredentialSecretRef == nil || strings.TrimSpace(cluster.Spec.VeleroInstall.RegistryCredentialSecretRef.Name) == "" {
		return r.deleteTargetVeleroRegistrySecret(ctx, destClient, cluster.Name)
	}

	secretName := strings.TrimSpace(cluster.Spec.VeleroInstall.RegistryCredentialSecretRef.Name)
	sourceSecret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{Name: secretName, Namespace: r.managementNamespace()}, sourceSecret); err != nil {
		return fmt.Errorf("failed to get management-plane velero registry secret %s/%s: %w", r.managementNamespace(), secretName, err)
	}
	if sourceSecret.Type != corev1.SecretTypeDockerConfigJson {
		return fmt.Errorf("management-plane velero registry secret %s/%s is not dockerconfigjson", sourceSecret.Namespace, sourceSecret.Name)
	}

	dockerConfig, ok := sourceSecret.Data[corev1.DockerConfigJsonKey]
	if !ok || len(dockerConfig) == 0 {
		return fmt.Errorf("management-plane velero registry secret %s/%s is missing %s", sourceSecret.Namespace, sourceSecret.Name, corev1.DockerConfigJsonKey)
	}

	if err := r.ensureNamespace(ctx, destClient, VeleroNamespace); err != nil {
		return fmt.Errorf("failed to ensure namespace %s: %w", VeleroNamespace, err)
	}

	targetSecretName := veleroRegistryTargetSecretName(cluster.Name)
	targetSecret := &corev1.Secret{}
	err := destClient.Get(ctx, types.NamespacedName{Name: targetSecretName, Namespace: VeleroNamespace}, targetSecret)
	if apierrors.IsNotFound(err) {
		return destClient.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      targetSecretName,
				Namespace: VeleroNamespace,
			},
			Type: corev1.SecretTypeDockerConfigJson,
			Data: map[string][]byte{
				corev1.DockerConfigJsonKey: append([]byte(nil), dockerConfig...),
			},
		})
	}
	if err != nil {
		return err
	}

	targetSecret.Type = corev1.SecretTypeDockerConfigJson
	if targetSecret.Data == nil {
		targetSecret.Data = make(map[string][]byte)
	}
	targetSecret.Data[corev1.DockerConfigJsonKey] = append([]byte(nil), dockerConfig...)
	return destClient.Update(ctx, targetSecret)
}

func (r *ClusterReconciler) deleteTargetVeleroRegistrySecret(ctx context.Context, destClient client.Client, clusterName string) error {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      veleroRegistryTargetSecretName(clusterName),
			Namespace: VeleroNamespace,
		},
	}
	err := destClient.Delete(ctx, secret)
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

func (r *ClusterReconciler) cleanupVeleroRegistrySecretOnDelete(ctx context.Context, cluster *disasterv1.Cluster) error {
	_, clientConfig, err := r.buildClusterAccess(cluster)
	if err != nil {
		return err
	}
	destClient, err := r.newTargetClusterClient(clientConfig)
	if err != nil {
		return fmt.Errorf("failed to create target cluster client for velero registry secret cleanup: %w", err)
	}
	return r.deleteTargetVeleroRegistrySecret(ctx, destClient, cluster.Name)
}

// IsVeleroInstalled checks if Velero is installed by verifying CRD availability
// This is more reliable than checking for Deployment existence, as CRDs are the core indicator of Velero installation
func (r *ClusterReconciler) IsVeleroInstalled(ctx context.Context, cli client.Client) (bool, error) {
	if r.ForceVeleroNotInstalled {
		return false, nil
	}

	// Check if Velero Backup CRD is available by attempting to list
	backupList := &velerov1.BackupList{}
	err := cli.List(ctx, backupList, client.Limit(1))
	if err != nil {
		if meta.IsNoMatchError(err) {
			// CRD not found, Velero is not installed
			return false, nil
		}
		// Other errors (permission, connection) also indicate Velero is not properly accessible
		return false, err
	}

	// CRD is accessible, now check deployment
	deployment := &appsv1.Deployment{}
	err = cli.Get(ctx, types.NamespacedName{Name: "velero", Namespace: VeleroNamespace}, deployment)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Deployment not found, Velero is not fully installed
			return false, nil
		}
		// Other errors (permission, connection) also indicate Velero is not properly accessible
		return false, err
	}

	// Check node-agent DaemonSet (required for Restic/Kopia)
	daemonSet := &appsv1.DaemonSet{}
	err = cli.Get(ctx, types.NamespacedName{Name: "node-agent", Namespace: VeleroNamespace}, daemonSet)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// DaemonSet not found
			return false, nil
		}
		return false, err
	}

	return true, nil
}

func (r *ClusterReconciler) InstallVeleroInCluster(ctx context.Context, cluster *disasterv1.Cluster) error {
	logger := logf.FromContext(ctx).WithName("velero-installer")
	logger.Info("Starting Velero installation", "cluster", cluster.Name)

	traceID := cluster.Annotations[AnnotationTraceID]
	user := cluster.Annotations[AnnotationUser]
	if user == "" {
		user = "system"
	}

	// 第一步：准备 kubeconfig 文件
	logger.Info("[1/5] Preparing target cluster kubeconfig")
	kubeConfigBytes, clientConfig, err := r.buildClusterAccess(cluster)
	if err != nil {
		return err
	}
	kubeconfigPath, cleanupKubeconfig, err := r.writeTempKubeconfig(cluster.Name, "velero-install", kubeConfigBytes)
	if err != nil {
		return err
	}
	defer cleanupKubeconfig()
	logger.Info("[1/5] Kubeconfig prepared", "endpoint", clientConfig.Host)
	helper.ReportTaskProgressWithClient(ctx, r.Client, r.Scheme, cluster, fmt.Sprintf("创建集群 %s", cluster.Name), "-", user, traceID, "目标集群 Kubeconfig 准备就绪")

	// 第二步：创建目标集群客户端
	logger.Info("[2/5] Creating target cluster client")
	destClient, err := r.newTargetClusterClient(clientConfig)
	if err != nil {
		return fmt.Errorf("failed to create client for target cluster: %w", err)
	}
	logger.Info("[2/5] Target cluster client created successfully")

	if err := r.syncVeleroRegistrySecretToTargetCluster(ctx, cluster, destClient); err != nil {
		return fmt.Errorf("failed to sync velero registry secret to target cluster: %w", err)
	}

	valuesPath, cleanupValues, err := r.buildVeleroValuesFile(cluster, r.currentVeleroTargetSecretName(cluster))
	if err != nil {
		return err
	}
	defer cleanupValues()

	// 第三步：清理僵尸 Helm 锁（可选步骤，失败不阻断流程）
	logger.Info("[3/5] Checking and cleaning up zombie Helm locks")
	if cleanupErr := CleanupZombieHelmLocks(ctx, destClient, VeleroNamespace, "velero"); cleanupErr != nil {
		logger.Error(cleanupErr, "[3/5] Failed to cleanup zombie Helm locks, continuing anyway")
		// 继续安装 - 这是一个尽力而为的清理操作
	} else {
		logger.Info("[3/5] Zombie Helm locks check completed")
	}

	// 第四步：手动安装 Velero CRDs（绕过 Helm Hook Job 的镜像拉取问题）
	logger.Info("[4/5] Installing Velero CRDs manually")
	if crdErr := EnsureVeleroCRDs(ctx, destClient); crdErr != nil {
		return fmt.Errorf("failed to ensure Velero CRDs: %w", crdErr)
	}
	logger.Info("[4/5] Velero CRDs installed successfully")
	helper.ReportTaskProgressWithClient(ctx, r.Client, r.Scheme, cluster, fmt.Sprintf("创建集群 %s", cluster.Name), "-", user, traceID, "Velero CRD 安装成功")

	// 第五步：执行 Helm 安装（使用 --no-hooks 跳过 CRD 安装 Job）
	logger.Info("[5/5] Running Helm upgrade --install")
	helper.ReportTaskProgressWithClient(ctx, r.Client, r.Scheme, cluster, fmt.Sprintf("创建集群 %s", cluster.Name), "-", user, traceID, "开始执行 Helm 安装 Velero...")
	if r.CommandExecutor == nil {
		r.CommandExecutor = &DefaultCommandExecutor{}
	}
	err = r.CommandExecutor.Run("helm", "upgrade", "velero", r.veleroChartPath(),
		"--install",
		"--create-namespace",
		"--cleanup-on-fail",
		"--no-hooks", // 跳过 CRD 安装 Hook，因为已手动安装
		"--timeout", "10m",
		"-n", VeleroNamespace,
		"-f", valuesPath,
		"--kubeconfig", kubeconfigPath,
	)

	if err != nil {
		return fmt.Errorf("failed to install velero via helm: %w", err)
	}

	logger.Info("Velero installation completed", "cluster", cluster.Name)
	helper.ReportTaskProgressWithClient(ctx, r.Client, r.Scheme, cluster, fmt.Sprintf("创建集群 %s", cluster.Name), "-", user, traceID, "Velero 安装完成，等待状态同步...")
	return nil
}

func (r *ClusterReconciler) collectClusterStats(ctx context.Context, cli client.Client, disco discovery.DiscoveryInterface, cluster *disasterv1.Cluster) error {
	snapshot, err := collectClusterNamespaceStatsSnapshot(ctx, cli, disco)
	if err != nil {
		return err
	}
	runningNamespaces, err := listRunningWorkloadNamespaces(ctx, cli)
	if err != nil {
		return err
	}
	applyNamespaceStatsSnapshot(cluster, snapshot)
	applyWorkloadNamespaceStatsSnapshot(cluster, snapshot, runningNamespaces)
	now := metav1.Now()
	cluster.Status.LastCheckTime = &now
	return nil
}

func (r *ClusterReconciler) collectNamespaceStats(ctx context.Context, cli client.Client, disco discovery.DiscoveryInterface, cluster *disasterv1.Cluster) error {
	snapshot, err := collectClusterNamespaceStatsSnapshot(ctx, cli, disco)
	if err != nil {
		return err
	}
	applyNamespaceStatsSnapshot(cluster, snapshot)
	return nil
}

func (r *ClusterReconciler) collectWorkloadNamespaceStats(ctx context.Context, cli client.Client, disco discovery.DiscoveryInterface, cluster *disasterv1.Cluster) error {
	snapshot, err := collectClusterNamespaceStatsSnapshot(ctx, cli, disco)
	if err != nil {
		return err
	}
	runningNamespaces, err := listRunningWorkloadNamespaces(ctx, cli)
	if err != nil {
		return err
	}
	applyWorkloadNamespaceStatsSnapshot(cluster, snapshot, runningNamespaces)
	return nil
}

func (r *ClusterReconciler) processRefreshClusterStatsSignal(ctx context.Context, cli client.Client, disco discovery.DiscoveryInterface, cluster *disasterv1.Cluster) (bool, error) {
	if cluster == nil || cluster.Annotations == nil {
		return false, nil
	}

	refreshType := strings.TrimSpace(cluster.Annotations[AnnotationRefreshClusterStats])
	if refreshType == "" {
		return false, nil
	}

	var err error
	switch ClusterStatsRefreshType(refreshType) {
	case ClusterStatsRefreshTypeNamespaceStats:
		err = r.collectNamespaceStats(ctx, cli, disco, cluster)
	case ClusterStatsRefreshTypeWorkloadNamespaceStats:
		err = r.collectWorkloadNamespaceStats(ctx, cli, disco, cluster)
	case ClusterStatsRefreshTypeAll:
		err = r.collectClusterStats(ctx, cli, disco, cluster)
	default:
		return true, r.clearRefreshClusterStatsSignal(ctx, cluster)
	}
	if err != nil {
		return true, err
	}

	now := metav1.Now()
	cluster.Status.LastCheckTime = &now
	if err := r.updateClusterStatusWithRetry(ctx, cluster); err != nil {
		return true, err
	}
	if err := r.updateClusterLabelsWithRetry(ctx, cluster, clusterOwnedStatsLabels(cluster)); err != nil {
		return true, err
	}
	if err := r.clearRefreshClusterStatsSignal(ctx, cluster); err != nil {
		return true, err
	}
	return true, nil
}

func (r *ClusterReconciler) clearRefreshClusterStatsSignal(ctx context.Context, cluster *disasterv1.Cluster) error {
	return r.updateClusterMetadataWithRetry(ctx, cluster, func(latest *disasterv1.Cluster) {
		if latest.Annotations == nil {
			return
		}
		delete(latest.Annotations, AnnotationRefreshClusterStats)
		if len(latest.Annotations) == 0 {
			latest.Annotations = nil
		}
	})
}

func (r *ClusterReconciler) handleDelete(ctx context.Context, cluster *disasterv1.Cluster) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)
	logger.Info("Handling cluster deletion")

	// 1. Update Status to Deleting
	if cluster.Status.Status != disasterv1.ClusterStatusDeleting ||
		cluster.Status.Reason != "Deleting" ||
		cluster.Status.Message != "Cluster is being deleted" {
		cluster.Status.Status = disasterv1.ClusterStatusDeleting
		cluster.Status.Reason = "Deleting"
		cluster.Status.Message = "Cluster is being deleted"
		// 删除集群 Started 事件 (仅发射一次)
		if cluster.Status.LastEventPhase != string(disasterv1.ClusterStatusDeleting) {
			traceID := cluster.Annotations[AnnotationTraceID]
			user := cluster.Annotations[AnnotationUser]
			if user == "" {
				user = "system"
			}
			helper.ReportTaskStartedWithClient(ctx, r.Client, r.Scheme, cluster,
				fmt.Sprintf("删除集群 %s", cluster.Name), "-", user, traceID, "开始删除集群")
			cluster.Status.LastEventPhase = string(disasterv1.ClusterStatusDeleting)
		}
		if err := r.updateClusterStatusWithRetry(ctx, cluster); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// 2. Legacy finalizer deletion protection is temporarily disabled.
	//
	// The old behavior blocked finalizer removal whenever the Cluster still had
	// upstream/downstream references discovered by controller-side dependency
	// checks. We are temporarily bypassing that legacy protection so deletion can
	// continue, and will re-introduce the new case-based rules separately.
	/*
		if err := r.checkDependencies(ctx, cluster); err != nil {
			logger.Info("Cluster deletion blocked by dependencies", "reason", err.Error())
			cluster.Status.Reason = "DeletionBlocked"
			cluster.Status.Message = err.Error()
			helper.ReportDiagnosticEvent(r.Recorder, cluster, corev1.EventTypeWarning, "DeletionBlocked", err.Error())
			// Requeue to check again later
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}
	*/

	if r.shouldSkipTargetCleanupForUnacceptedCluster(cluster) {
		message := "Cluster was not accepted by the license gate; skipping target-cluster cleanup"
		logger.Info(message)
		cluster.Status.Message = message
		helper.ReportDiagnosticEvent(r.Recorder, cluster, corev1.EventTypeNormal, "LicenseRejectedClusterCleanupSkipped", message)
	} else {
		// 3. Check if Velero needs to be uninstalled
		if cluster.Annotations[AnnotationUninstallVelero] == "true" {
			logger.Info("Uninstalling Velero from cluster")
			cluster.Status.Message = "Uninstalling Velero..."
			if err := r.uninstallVelero(ctx, cluster); err != nil {
				logger.Error(err, "Failed to uninstall Velero")
				cluster.Status.Reason = "VeleroUninstallFailed"
				cluster.Status.Message = fmt.Sprintf("Failed to uninstall Velero: %v", err)
				helper.ReportDiagnosticEvent(r.Recorder, cluster, corev1.EventTypeWarning, "VeleroUninstallFailed", err.Error())
				return ctrl.Result{RequeueAfter: 10 * time.Second}, nil // Retry
			}
			helper.ReportDiagnosticEvent(r.Recorder, cluster, corev1.EventTypeNormal, "VeleroUninstalled", "Velero uninstalled successfully")
		}

		if err := r.cleanupVeleroRegistrySecretOnDelete(ctx, cluster); err != nil {
			logger.Error(err, "Failed to cleanup velero registry pull secret from cluster")
			cluster.Status.Reason = "VeleroRegistrySecretCleanupFailed"
			cluster.Status.Message = fmt.Sprintf("Failed to cleanup velero registry pull secret: %v", err)
			helper.ReportDiagnosticEvent(r.Recorder, cluster, corev1.EventTypeWarning, "VeleroRegistrySecretCleanupFailed", err.Error())
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}
	}

	// 4. 发射删除完成事件（必须在移除 Finalizer 之前！）
	traceID := cluster.Annotations[AnnotationTraceID]
	user := cluster.Annotations[AnnotationUser]
	if user == "" {
		user = "system"
	}
	now := metav1.Now()
	helper.ReportTaskFinishedWithClient(ctx, r.Client, r.Scheme, cluster,
		fmt.Sprintf("删除集群 %s", cluster.Name), "-", helper.TaskStatusSuccess,
		cluster.DeletionTimestamp, &now, user, traceID, "集群删除完成")

	// 5. Remove Finalizer
	if controllerutil.ContainsFinalizer(cluster, LabelClusterFinalizer) {
		controllerutil.RemoveFinalizer(cluster, LabelClusterFinalizer)
		if err := r.Update(ctx, cluster); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func (r *ClusterReconciler) shouldSkipTargetCleanupForUnacceptedCluster(cluster *disasterv1.Cluster) bool {
	if cluster == nil || !r.LicenseGateEnabled || isLicenseAccepted(cluster) {
		return false
	}
	if cluster.Status.Reason == platformlicense.ReasonLicenseLimitExceeded {
		return true
	}
	return cluster.Status.ReadyTimestamp == nil &&
		cluster.Status.ObservedGeneration == 0 &&
		strings.TrimSpace(cluster.Status.Endpoint) == "" &&
		strings.TrimSpace(cluster.Status.K8SVersion) == "" &&
		(strings.TrimSpace(cluster.Status.VeleroVersion) == "" || strings.TrimSpace(cluster.Status.VeleroVersion) == "-")
}

func (r *ClusterReconciler) checkDependencies(ctx context.Context, cluster *disasterv1.Cluster) error {
	// Check AppBackups
	appBackups := &disasterv1.AppBackupList{}
	if err := r.List(ctx, appBackups, client.MatchingLabels{LabelAppBackupCluster: cluster.Name}); err != nil {
		return err
	}
	if len(appBackups.Items) > 0 {
		return fmt.Errorf("cluster is in use by %d AppBackups (e.g., %s)", len(appBackups.Items), appBackups.Items[0].Name)
	}

	// Check AppRestores
	appRestores := &disasterv1.AppRestoreList{}
	if err := r.List(ctx, appRestores, client.MatchingLabels{LabelAppRestoreCluster: cluster.Name}); err != nil {
		return err
	}
	if len(appRestores.Items) > 0 {
		return fmt.Errorf("cluster is in use by %d AppRestores (e.g., %s)", len(appRestores.Items), appRestores.Items[0].Name)
	}

	// Check DisasterConfigs (No label yet, so we list all and filter)
	// Optimization: In the future, add labels to DisasterConfig
	disasterConfigs := &disasterv1.DisasterConfigList{}
	if err := r.List(ctx, disasterConfigs); err != nil {
		return err
	}
	for _, dc := range disasterConfigs.Items {
		if dc.Spec.SourceCluster == cluster.Name || dc.Spec.TargetCluster == cluster.Name {
			return fmt.Errorf("cluster is in use by DisasterConfig %s", dc.Name)
		}
	}

	return nil
}

func (r *ClusterReconciler) uninstallVelero(ctx context.Context, cluster *disasterv1.Cluster) error {
	kubeConfigBytes, clientConfig, err := r.buildClusterAccess(cluster)
	if err != nil {
		return err
	}
	kubeconfigPath, cleanupKubeconfig, err := r.writeTempKubeconfig(cluster.Name, "velero-uninstall", kubeConfigBytes)
	if err != nil {
		return err
	}
	defer cleanupKubeconfig()

	// Use CommandExecutor to run helm uninstall
	if r.CommandExecutor == nil {
		r.CommandExecutor = &DefaultCommandExecutor{}
	}

	// helm uninstall velero -n velero --kubeconfig <tmpFile>
	err = r.CommandExecutor.Run(
		"helm", "uninstall", "velero",
		"-n", VeleroNamespace,
		"--ignore-not-found",
		"--no-hooks",
		"--kubeconfig", kubeconfigPath,
	)
	if err != nil {
		// release: not found 仍需继续做残留清理（CR/CRD/RBAC/Namespace）
		if strings.Contains(err.Error(), "release: not found") {
			err = nil
		} else {
			return fmt.Errorf("failed to uninstall velero: %w", err)
		}
	}

	destClient, err := r.newTargetClusterClient(clientConfig)
	if err != nil {
		return fmt.Errorf("failed to create client for velero cleanup: %w", err)
	}

	if err := r.cleanupVeleroResiduals(ctx, destClient); err != nil {
		return fmt.Errorf("failed to cleanup velero residual resources: %w", err)
	}
	return nil
}

func (r *ClusterReconciler) cleanupVeleroResiduals(ctx context.Context, cli client.Client) error {
	logger := logf.FromContext(ctx)

	// 1) 先清理所有 namespaced Velero CR（删除前先移除 finalizer，避免命名空间删除被卡住）
	logger.Info("Cleaning up Velero namespaced resources")
	if err := r.deleteVeleroNamespacedCRs(ctx, cli); err != nil {
		return err
	}

	// 2) 请求删除 velero 命名空间（先请求，后续继续删 CRD/RBAC）
	logger.Info("Deleting Velero namespace")
	if err := r.deleteVeleroNamespace(ctx, cli); err != nil {
		return err
	}

	// 3) 清理 velero.io CRD
	logger.Info("Deleting Velero CRDs")
	if err := r.deleteVeleroCRDs(ctx, cli); err != nil {
		return err
	}

	// 4) 清理 velero 相关集群级 RBAC（主要是 helm hook 遗留）
	logger.Info("Deleting Velero cluster RBAC")
	if err := r.deleteVeleroClusterRBAC(ctx, cli); err != nil {
		return err
	}
	return nil
}

func (r *ClusterReconciler) deleteVeleroNamespacedCRs(ctx context.Context, cli client.Client) error {
	finalizerAwareLists := []client.ObjectList{
		&velerov1.BackupList{},
		&velerov1.RestoreList{},
		&velerov1.ScheduleList{},
		&velerov1.BackupStorageLocationList{},
		&velerov1.BackupRepositoryList{},
		&velerov1.VolumeSnapshotLocationList{},
		&velerov1.PodVolumeBackupList{},
		&velerov1.PodVolumeRestoreList{},
	}

	bulkDeleteOnly := []client.Object{
		&velerov1.DeleteBackupRequest{},
		&velerov1.DownloadRequest{},
		&velerov1.ServerStatusRequest{},
	}

	for _, obj := range bulkDeleteOnly {
		if err := r.deleteCollection(ctx, cli, obj, client.InNamespace(VeleroNamespace)); err != nil {
			return err
		}
	}

	for _, listObj := range finalizerAwareLists {
		if err := r.removeFinalizersAndDeleteList(ctx, cli, listObj, client.InNamespace(VeleroNamespace)); err != nil {
			return err
		}
	}
	return nil
}

func (r *ClusterReconciler) removeFinalizersAndDeleteList(ctx context.Context, cli client.Client, listObj client.ObjectList, opts ...client.ListOption) error {
	if err := cli.List(ctx, listObj, opts...); err != nil {
		// 资源类型不存在（CRD 已删除）或对象不存在，按幂等处理。
		if meta.IsNoMatchError(err) || apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	items, err := meta.ExtractList(listObj)
	if err != nil {
		return err
	}
	for _, item := range items {
		obj, ok := item.(client.Object)
		if !ok {
			continue
		}
		if len(obj.GetFinalizers()) > 0 {
			before := obj.DeepCopyObject()
			obj.SetFinalizers(nil)
			patchBase, ok := before.(client.Object)
			if !ok {
				return fmt.Errorf("failed to cast %T to client.Object for patch", before)
			}
			if err := cli.Patch(ctx, obj, client.MergeFrom(patchBase)); err != nil && !apierrors.IsNotFound(err) {
				// 如果并发删除导致找不到，按幂等处理。
				if meta.IsNoMatchError(err) {
					continue
				}
				return err
			}
		}
	}

	deleteObj, err := objectForList(listObj)
	if err != nil {
		return err
	}
	return r.deleteCollection(ctx, cli, deleteObj, listOptionsToDeleteAllOf(opts)...)
}

func (r *ClusterReconciler) deleteCollection(ctx context.Context, cli client.Client, obj client.Object, opts ...client.DeleteAllOfOption) error {
	deleteOpts := append([]client.DeleteAllOfOption{}, opts...)
	deleteOpts = append(deleteOpts, client.PropagationPolicy(metav1.DeletePropagationBackground))
	if err := cli.DeleteAllOf(ctx, obj, deleteOpts...); err != nil {
		if meta.IsNoMatchError(err) || apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}

func listOptionsToDeleteAllOf(opts []client.ListOption) []client.DeleteAllOfOption {
	deleteOpts := make([]client.DeleteAllOfOption, 0, len(opts))
	for _, opt := range opts {
		if deleteOpt, ok := opt.(client.DeleteAllOfOption); ok {
			deleteOpts = append(deleteOpts, deleteOpt)
		}
	}
	return deleteOpts
}

func objectForList(listObj client.ObjectList) (client.Object, error) {
	switch listObj.(type) {
	case *velerov1.BackupList:
		return &velerov1.Backup{}, nil
	case *velerov1.RestoreList:
		return &velerov1.Restore{}, nil
	case *velerov1.ScheduleList:
		return &velerov1.Schedule{}, nil
	case *velerov1.BackupStorageLocationList:
		return &velerov1.BackupStorageLocation{}, nil
	case *velerov1.BackupRepositoryList:
		return &velerov1.BackupRepository{}, nil
	case *velerov1.VolumeSnapshotLocationList:
		return &velerov1.VolumeSnapshotLocation{}, nil
	case *velerov1.DeleteBackupRequestList:
		return &velerov1.DeleteBackupRequest{}, nil
	case *velerov1.DownloadRequestList:
		return &velerov1.DownloadRequest{}, nil
	case *velerov1.PodVolumeBackupList:
		return &velerov1.PodVolumeBackup{}, nil
	case *velerov1.PodVolumeRestoreList:
		return &velerov1.PodVolumeRestore{}, nil
	case *velerov1.ServerStatusRequestList:
		return &velerov1.ServerStatusRequest{}, nil
	default:
		return nil, fmt.Errorf("unsupported list type for delete collection: %T", listObj)
	}
}

func (r *ClusterReconciler) deleteVeleroNamespace(ctx context.Context, cli client.Client) error {
	ns := &corev1.Namespace{}
	if err := cli.Get(ctx, types.NamespacedName{Name: VeleroNamespace}, ns); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if err := cli.Delete(ctx, ns); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

func (r *ClusterReconciler) deleteVeleroCRDs(ctx context.Context, cli client.Client) error {
	crdList := &apiextensionsv1.CustomResourceDefinitionList{}
	if err := cli.List(ctx, crdList); err != nil {
		return err
	}
	for i := range crdList.Items {
		crd := &crdList.Items[i]
		if !strings.HasSuffix(crd.Name, ".velero.io") {
			continue
		}
		if len(crd.Finalizers) > 0 {
			before := crd.DeepCopy()
			crd.Finalizers = nil
			if err := cli.Patch(ctx, crd, client.MergeFrom(before)); err != nil && !apierrors.IsNotFound(err) {
				return err
			}
		}
		if err := cli.Delete(ctx, crd); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

func (r *ClusterReconciler) deleteVeleroClusterRBAC(ctx context.Context, cli client.Client) error {
	clusterRoles := &rbacv1.ClusterRoleList{}
	if err := cli.List(ctx, clusterRoles); err != nil {
		return err
	}
	for i := range clusterRoles.Items {
		role := &clusterRoles.Items[i]
		if strings.Contains(role.Name, "velero") {
			if err := cli.Delete(ctx, role); err != nil && !apierrors.IsNotFound(err) {
				return err
			}
		}
	}

	clusterRoleBindings := &rbacv1.ClusterRoleBindingList{}
	if err := cli.List(ctx, clusterRoleBindings); err != nil {
		return err
	}
	for i := range clusterRoleBindings.Items {
		binding := &clusterRoleBindings.Items[i]
		if strings.Contains(binding.Name, "velero") {
			if err := cli.Delete(ctx, binding); err != nil && !apierrors.IsNotFound(err) {
				return err
			}
		}
	}
	return nil
}

// calculateMetadataHash calculates a deterministic hash of the filtered metadata (labels & annotations)
func (r *ClusterReconciler) calculateMetadataHash(cluster *disasterv1.Cluster) string {
	// 1. Filter Labels
	labels := make(map[string]string)
	for k, v := range cluster.Labels {
		// Exclude system/controller managed labels
		if k == LabelClusterName ||
			k == LabelClusterNamespaceCount ||
			k == LabelClusterWorkloadNamespaceCount ||
			k == LabelClusterResourceTotalCount ||
			k == LabelClusterWorkloadTotalCount ||
			k == LabelClusterFinalizer {
			continue
		}
		labels[k] = v
	}

	// 2. Filter Annotations
	annotations := make(map[string]string)
	// Only include specific user-facing annotations OR exclude system ones
	// Strategy: Include "testudo.softcdata.com/description" explicitly, or everything except system.
	// Let's include everything except known system/dynamic ones.
	for k, v := range cluster.Annotations {
		if k == AnnotationTraceID ||
			k == AnnotationRefreshClusterStats ||
			k == "kubectl.kubernetes.io/last-applied-configuration" {
			continue
		}
		annotations[k] = v
	}

	// 3. Serialize to map (keys sorted by json.Marshal if it was a struct, but for map we need manual sort or use a struct)
	// Actually, json.Marshal on map sorts keys key alphabetically.
	data := struct {
		Labels      map[string]string `json:"labels"`
		Annotations map[string]string `json:"annotations"`
	}{
		Labels:      labels,
		Annotations: annotations,
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return "" // Should not happen with string map
	}

	// 4. Hash
	hash := sha256.Sum256(jsonData)
	return hex.EncodeToString(hash[:])
}
