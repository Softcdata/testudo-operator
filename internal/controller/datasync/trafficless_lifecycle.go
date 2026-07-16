package datasync

import (
	"context"
	"crypto/md5"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	ctrlcommon "github.com/softcdata/testudo-operator/internal/controller"
	runtimecfg "github.com/softcdata/testudo-operator/internal/controller/runtimeconfig"
	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	"github.com/softcdata/testudo-operator/pkg/metadata"
	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	annotationTrafficlessCleanupBeforeStartedAt = "testudo.softcdata.com/datasync-trafficless-cleanup-before-started-at"
	annotationTrafficlessCleanupAfterStartedAt  = "testudo.softcdata.com/datasync-trafficless-cleanup-after-started-at"
	trafficlessLabelKey                         = "trafficless"
	trafficlessLabelValue                       = "true"
	dataSyncClusterRoleSource                   = "source"

	dataSyncReasonSourceVeleroRuntimeNotReady = "SourceVeleroRuntimeNotReady"
	dataSyncReasonTargetVeleroRuntimeNotReady = "TargetVeleroRuntimeNotReady"
	dataSyncReasonTrafficlessCleanupAmbiguous = "TrafficlessCleanupAmbiguous"
	dataSyncReasonTrafficlessCleanupTimeout   = "TrafficlessCleanupTimeout"
	dataSyncReasonTrafficlessCleanupFailed    = "TrafficlessCleanupFailed"
	dataSyncReasonTargetPVCNotReady           = "TargetPVCNotReady"
)

type trafficlessLifecycleError struct {
	Reason  string
	Message string
}

func (e *trafficlessLifecycleError) Error() string {
	if e == nil {
		return ""
	}
	if e.Reason == "" {
		return e.Message
	}
	if e.Message == "" {
		return e.Reason
	}
	return fmt.Sprintf("%s: %s", e.Reason, e.Message)
}

func newTrafficlessLifecycleError(reason, message string) error {
	return &trafficlessLifecycleError{Reason: reason, Message: message}
}

func trafficlessLifecycleErrorDetails(err error, fallbackReason string) (string, string) {
	if err == nil {
		return fallbackReason, ""
	}
	if lifecycleErr, ok := err.(*trafficlessLifecycleError); ok {
		return lifecycleErr.Reason, lifecycleErr.Message
	}
	return fallbackReason, err.Error()
}

func dataSyncRestoreName(dataSync *disasterv1.DataSync, backupName string) string {
	dsName := dataSync.Name
	if len(dsName) > 20 {
		dsName = dsName[:20]
	}
	backupHash := fmt.Sprintf("%x", md5Sum(backupName))[:6]
	return fmt.Sprintf("rec-ds-%s-%s", dsName, backupHash)
}

// md5Sum is kept local so run naming can be shared without widening the controller API.
func md5Sum(value string) [16]byte {
	return md5.Sum([]byte(value))
}

func dataSyncTrafficlessOwnerUID(dataSync *disasterv1.DataSync) string {
	if dataSync == nil {
		return ""
	}
	if uid := strings.TrimSpace(string(dataSync.UID)); uid != "" {
		return uid
	}
	// Kubernetes-assigned UIDs are always present in production. The deterministic
	// fallback keeps direct unit construction safe and never broadens a selector.
	return dataSync.Namespace + "/" + dataSync.Name
}

func dataSyncTrafficlessOwnerToken(dataSync *disasterv1.DataSync) string {
	return metadata.BuildDependencyToken(dataSyncTrafficlessOwnerUID(dataSync))
}

func dataSyncTrafficlessRunIdentifier(restoreName string) string {
	return metadata.NormalizeRelationCode(restoreName)
}

func dataSyncTrafficlessLabels(dataSync *disasterv1.DataSync, restoreName string) map[string]string {
	labels := map[string]string{
		trafficlessLabelKey:                trafficlessLabelValue,
		metadata.LabelTrafficlessRun:       dataSyncTrafficlessRunIdentifier(restoreName),
		metadata.LabelTrafficlessLifecycle: metadata.TrafficlessLifecycleDataSync,
	}
	labels, _ = metadata.EnsureCleanupLabels(labels, metadata.CleanupDescriptor{
		OwnerUID:     dataSyncTrafficlessOwnerUID(dataSync),
		RelationCode: metadata.CleanupRelationDataSyncTrafficlessPod,
		Strategy:     metadata.CleanupStrategyDelete,
	})
	return labels
}

func dataSyncTrafficlessAppRestoreLabels(dataSync *disasterv1.DataSync, restoreName string) map[string]string {
	labels := map[string]string{
		metadata.LabelAppResourceOrigin:    metadata.AppResourceOriginDisasterInstance,
		metadata.LabelAppResourceOwnerKind: metadata.AppResourceOwnerKindDataSync,
		metadata.LabelAppResourceOwnerName: dataSync.Name,
	}
	for key, value := range dataSyncTrafficlessLabels(dataSync, restoreName) {
		if key == trafficlessLabelKey {
			continue
		}
		labels[key] = value
	}
	return labels
}

func isDataSyncTrafficlessLifecycleRestore(restore *disasterv1.AppRestore) bool {
	return restore != nil && restore.Labels != nil &&
		strings.TrimSpace(restore.Labels[metadata.LabelTrafficlessLifecycle]) == metadata.TrafficlessLifecycleDataSync
}

type trafficlessCleanupScope string

const (
	trafficlessCleanupBeforeRestore trafficlessCleanupScope = "before"
	trafficlessCleanupAfterRestore  trafficlessCleanupScope = "after"
)

type trafficlessCleanupResult struct {
	Done            bool
	MetadataChanged bool
	Message         string
}

func (scope trafficlessCleanupScope) annotationKey() string {
	if scope == trafficlessCleanupAfterRestore {
		return annotationTrafficlessCleanupAfterStartedAt
	}
	return annotationTrafficlessCleanupBeforeStartedAt
}

func trafficlessCleanupTimeout() time.Duration {
	timeout := runtimecfg.SnapshotCurrent().RestoreRuntime.PodVolumeRestorePendingWait
	if timeout <= 0 {
		return 10 * time.Minute
	}
	return timeout
}

func trafficlessCleanupRequeueAfter() time.Duration {
	requeue := runtimecfg.SnapshotCurrent().RestoreRuntime.InProgressPollInterval
	if requeue <= 0 {
		return 5 * time.Second
	}
	return requeue
}

func (r *DataSyncReconciler) reconcileTrafficlessPodCleanup(
	ctx context.Context,
	log logr.Logger,
	dataSync *disasterv1.DataSync,
	config *disasterv1.DisasterConfig,
	instance *disasterv1.DisasterInstance,
	restoreName string,
	scope trafficlessCleanupScope,
) (trafficlessCleanupResult, error) {
	if dataSync == nil || config == nil || instance == nil {
		return trafficlessCleanupResult{}, newTrafficlessLifecycleError(
			dataSyncReasonTrafficlessCleanupFailed,
			"DataSync cleanup dependencies are incomplete",
		)
	}

	_, target := resolveClusters(instance, config)
	targetClient, err := r.getTargetClusterClient(ctx, target)
	if err != nil {
		return trafficlessCleanupResult{}, newTrafficlessLifecycleError(
			dataSyncReasonTrafficlessCleanupFailed,
			fmt.Sprintf("build target cluster client %s: %v", target, err),
		)
	}

	ownerSelector := client.MatchingLabels{
		metadata.LabelCleanupManagedBy:  metadata.LabelCleanupManagedByValueOperator,
		metadata.LabelCleanupOwnerToken: dataSyncTrafficlessOwnerToken(dataSync),
		metadata.LabelCleanupRelation:   metadata.CleanupRelationDataSyncTrafficlessPod,
		metadata.LabelCleanupStrategy:   metadata.CleanupStrategyDelete,
	}
	if scope == trafficlessCleanupAfterRestore {
		ownerSelector[metadata.LabelTrafficlessRun] = dataSyncTrafficlessRunIdentifier(restoreName)
	}

	matchedPods := make([]corev1.Pod, 0)
	for _, namespace := range instance.Spec.Namespaces {
		namespace = strings.TrimSpace(namespace)
		if namespace == "" {
			continue
		}

		allPods := &corev1.PodList{}
		if err := targetClient.List(ctx, allPods, client.InNamespace(namespace)); err != nil {
			return trafficlessCleanupResult{}, newTrafficlessLifecycleError(
				dataSyncReasonTrafficlessCleanupFailed,
				fmt.Sprintf("list Pods in namespace %s: %v", namespace, err),
			)
		}
		for i := range allPods.Items {
			pod := &allPods.Items[i]
			if pod.Labels[trafficlessLabelKey] != trafficlessLabelValue {
				continue
			}
			if isAmbiguousTrafficlessPod(pod) {
				return trafficlessCleanupResult{}, newTrafficlessLifecycleError(
					dataSyncReasonTrafficlessCleanupAmbiguous,
					fmt.Sprintf("found unscoped trafficless Pod %s/%s; manual review is required", pod.Namespace, pod.Name),
				)
			}
		}

		exactPods := &corev1.PodList{}
		if err := targetClient.List(ctx, exactPods, client.InNamespace(namespace), ownerSelector); err != nil {
			return trafficlessCleanupResult{}, newTrafficlessLifecycleError(
				dataSyncReasonTrafficlessCleanupFailed,
				fmt.Sprintf("list scoped trafficless Pods in namespace %s: %v", namespace, err),
			)
		}
		matchedPods = append(matchedPods, exactPods.Items...)
	}

	annotationKey := scope.annotationKey()
	if len(matchedPods) == 0 {
		result := trafficlessCleanupResult{Done: true, Message: "scoped trafficless Pods are absent"}
		if dataSync.Annotations != nil {
			if _, exists := dataSync.Annotations[annotationKey]; exists {
				delete(dataSync.Annotations, annotationKey)
				result.MetadataChanged = true
			}
		}
		return result, nil
	}

	startedAt, hasStartedAt := trafficlessCleanupStartedAt(dataSync, annotationKey)
	if !hasStartedAt {
		startedAt = time.Now().UTC()
		if dataSync.Annotations == nil {
			dataSync.Annotations = make(map[string]string)
		}
		dataSync.Annotations[annotationKey] = startedAt.Format(time.RFC3339)
	}
	if elapsed := time.Since(startedAt); elapsed > trafficlessCleanupTimeout() {
		return trafficlessCleanupResult{MetadataChanged: !hasStartedAt}, newTrafficlessLifecycleError(
			dataSyncReasonTrafficlessCleanupTimeout,
			fmt.Sprintf("%d scoped trafficless Pod(s) remain after %s during %s cleanup", len(matchedPods), elapsed.Round(time.Second), scope),
		)
	}

	for i := range matchedPods {
		pod := &matchedPods[i]
		if !pod.DeletionTimestamp.IsZero() {
			continue
		}
		log.Info("requesting scoped trafficless Pod deletion", "namespace", pod.Namespace, "pod", pod.Name, "scope", scope)
		if err := targetClient.Delete(ctx, pod); err != nil && !apierrors.IsNotFound(err) {
			return trafficlessCleanupResult{MetadataChanged: !hasStartedAt}, newTrafficlessLifecycleError(
				dataSyncReasonTrafficlessCleanupFailed,
				fmt.Sprintf("delete scoped trafficless Pod %s/%s: %v", pod.Namespace, pod.Name, err),
			)
		}
	}

	return trafficlessCleanupResult{
		Done:            false,
		MetadataChanged: !hasStartedAt,
		Message:         fmt.Sprintf("waiting for %d scoped trafficless Pod(s) to disappear", len(matchedPods)),
	}, nil
}

func isAmbiguousTrafficlessPod(pod *corev1.Pod) bool {
	if pod == nil || pod.Labels[trafficlessLabelKey] != trafficlessLabelValue {
		return false
	}
	for _, key := range []string{
		metadata.LabelCleanupManagedBy,
		metadata.LabelCleanupOwnerToken,
		metadata.LabelCleanupRelation,
		metadata.LabelCleanupStrategy,
		metadata.LabelTrafficlessRun,
	} {
		if strings.TrimSpace(pod.Labels[key]) == "" {
			return true
		}
	}
	return false
}

func trafficlessCleanupStartedAt(dataSync *disasterv1.DataSync, annotationKey string) (time.Time, bool) {
	if dataSync == nil || dataSync.Annotations == nil {
		return time.Time{}, false
	}
	raw := strings.TrimSpace(dataSync.Annotations[annotationKey])
	if raw == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

func (r *DataSyncReconciler) ensureDataSyncTrafficlessRuntime(
	ctx context.Context,
	clusterName string,
	role string,
	remoteClient client.Client,
) error {
	cluster := &disasterv1.Cluster{}
	if err := r.Get(ctx, types.NamespacedName{Name: clusterName}, cluster); err != nil {
		// Older test and migration paths can inject a client before the management
		// Cluster object exists. A registered Cluster is still authoritative when present.
		if apierrors.IsNotFound(err) {
			return nil
		}
		reason := dataSyncReasonTargetVeleroRuntimeNotReady
		if role == dataSyncClusterRoleSource {
			reason = dataSyncReasonSourceVeleroRuntimeNotReady
		}
		return newTrafficlessLifecycleError(reason, fmt.Sprintf("get %s Cluster %s: %v", role, clusterName, err))
	}

	reason := dataSyncReasonTargetVeleroRuntimeNotReady
	if role == dataSyncClusterRoleSource {
		reason = dataSyncReasonSourceVeleroRuntimeNotReady
	}
	if cluster.Status.Status != disasterv1.ClusterStatusReady {
		message := fmt.Sprintf("%s Cluster %s is %s", role, clusterName, cluster.Status.Status)
		if cluster.Status.Message != "" {
			message = fmt.Sprintf("%s: %s", message, cluster.Status.Message)
		}
		return newTrafficlessLifecycleError(reason, message)
	}

	if remoteClient == nil {
		var err error
		if role == dataSyncClusterRoleSource {
			remoteClient, err = r.getSourceClusterClient(ctx, clusterName)
		} else {
			remoteClient, err = r.getTargetClusterClient(ctx, clusterName)
		}
		if err != nil {
			return newTrafficlessLifecycleError(reason, fmt.Sprintf("build %s cluster client %s: %v", role, clusterName, err))
		}
	}
	if err := ctrlcommon.ValidateTrafficlessVeleroRuntime(ctx, remoteClient); err != nil {
		return newTrafficlessLifecycleError(reason, fmt.Sprintf("%s cluster %s Velero runtime is not ready: %v", role, clusterName, err))
	}
	return nil
}

type targetPVCReadiness struct {
	Ready   bool
	Message string
}

type podVolumeRestoreReadiness struct {
	Ready   bool
	Message string
}

func (r *DataSyncReconciler) verifyDataSyncPodVolumeRestores(
	ctx context.Context,
	config *disasterv1.DisasterConfig,
	instance *disasterv1.DisasterInstance,
	appRestore *disasterv1.AppRestore,
) (podVolumeRestoreReadiness, error) {
	if config == nil || instance == nil || appRestore == nil {
		return podVolumeRestoreReadiness{}, newTrafficlessLifecycleError("PodVolumeRestoreFailed", "PodVolumeRestore verification dependencies are incomplete")
	}
	_, target := resolveClusters(instance, config)
	targetClient, err := r.getTargetClusterClient(ctx, target)
	if err != nil {
		return podVolumeRestoreReadiness{}, newTrafficlessLifecycleError("PodVolumeRestoreFailed", fmt.Sprintf("build target cluster client %s: %v", target, err))
	}

	restoreName := "res-" + appRestore.Name
	pvrs := &velerov1.PodVolumeRestoreList{}
	if err := targetClient.List(ctx, pvrs,
		client.InNamespace(ctrlcommon.VeleroNamespace),
		client.MatchingLabels{velerov1.RestoreNameLabel: restoreName},
	); err != nil {
		return podVolumeRestoreReadiness{}, newTrafficlessLifecycleError("PodVolumeRestoreFailed", fmt.Sprintf("list PodVolumeRestore for %s: %v", restoreName, err))
	}

	stallTimeout := dataSyncRestoreTimeout(appRestore, instance)
	if pvrTimeout := trafficlessCleanupTimeout(); pvrTimeout > 0 && (stallTimeout <= 0 || stallTimeout > pvrTimeout) {
		stallTimeout = pvrTimeout
	}
	for i := range pvrs.Items {
		pvr := &pvrs.Items[i]
		switch pvr.Status.Phase {
		case velerov1.PodVolumeRestorePhaseCompleted:
			continue
		case velerov1.PodVolumeRestorePhaseFailed, velerov1.PodVolumeRestorePhaseCanceled:
			message := fmt.Sprintf("PodVolumeRestore %s is %s", pvr.Name, pvr.Status.Phase)
			if pvr.Status.Message != "" {
				message = fmt.Sprintf("%s: %s", message, pvr.Status.Message)
			}
			return podVolumeRestoreReadiness{}, newTrafficlessLifecycleError("PodVolumeRestoreFailed", message)
		default:
			if elapsed := time.Since(dataSyncPodVolumeRestoreReferenceTime(pvr)); elapsed > stallTimeout {
				return podVolumeRestoreReadiness{}, newTrafficlessLifecycleError(
					"PodVolumeRestoreStalled",
					fmt.Sprintf("PodVolumeRestore %s remains %s for %s", pvr.Name, pvr.Status.Phase, elapsed.Round(time.Second)),
				)
			}
			return podVolumeRestoreReadiness{Message: fmt.Sprintf("waiting for PodVolumeRestore %s phase=%s", pvr.Name, pvr.Status.Phase)}, nil
		}
	}
	return podVolumeRestoreReadiness{Ready: true}, nil
}

func dataSyncPodVolumeRestoreReferenceTime(pvr *velerov1.PodVolumeRestore) time.Time {
	if pvr == nil {
		return time.Now()
	}
	if pvr.Status.StartTimestamp != nil {
		return pvr.Status.StartTimestamp.Time
	}
	if pvr.Status.AcceptedTimestamp != nil {
		return pvr.Status.AcceptedTimestamp.Time
	}
	if pvr.CreationTimestamp.IsZero() {
		return time.Now()
	}
	return pvr.CreationTimestamp.Time
}

func (r *DataSyncReconciler) verifyDataSyncTargetPVCsReady(
	ctx context.Context,
	config *disasterv1.DisasterConfig,
	instance *disasterv1.DisasterInstance,
) (targetPVCReadiness, error) {
	source, target := resolveClusters(instance, config)
	sourceClient, err := r.getSourceClusterClient(ctx, source)
	if err != nil {
		return targetPVCReadiness{}, newTrafficlessLifecycleError(dataSyncReasonTargetPVCNotReady, fmt.Sprintf("build source cluster client %s: %v", source, err))
	}
	plan, err := r.discoverDataSyncVolumePlan(ctx, sourceClient, instance)
	if err != nil {
		return targetPVCReadiness{}, newTrafficlessLifecycleError(dataSyncReasonTargetPVCNotReady, fmt.Sprintf("discover expected PVCs: %v", err))
	}
	if len(plan.PVCs) == 0 {
		return targetPVCReadiness{Ready: true}, nil
	}

	targetClient, err := r.getTargetClusterClient(ctx, target)
	if err != nil {
		return targetPVCReadiness{}, newTrafficlessLifecycleError(dataSyncReasonTargetPVCNotReady, fmt.Sprintf("build target cluster client %s: %v", target, err))
	}
	for _, expected := range plan.PVCs {
		pvc := &corev1.PersistentVolumeClaim{}
		if err := targetClient.Get(ctx, expected, pvc); err != nil {
			if apierrors.IsNotFound(err) {
				return targetPVCReadiness{Message: fmt.Sprintf("target PVC %s/%s is not found", expected.Namespace, expected.Name)}, nil
			}
			return targetPVCReadiness{}, newTrafficlessLifecycleError(dataSyncReasonTargetPVCNotReady, fmt.Sprintf("get target PVC %s/%s: %v", expected.Namespace, expected.Name, err))
		}
		if !pvc.DeletionTimestamp.IsZero() {
			return targetPVCReadiness{Message: fmt.Sprintf("target PVC %s/%s is deleting", pvc.Namespace, pvc.Name)}, nil
		}
		if pvc.Status.Phase != corev1.ClaimBound {
			return targetPVCReadiness{Message: fmt.Sprintf("target PVC %s/%s is %s", pvc.Namespace, pvc.Name, pvc.Status.Phase)}, nil
		}
	}
	return targetPVCReadiness{Ready: true}, nil
}

func dataSyncRestoreTimeout(restore *disasterv1.AppRestore, instance *disasterv1.DisasterInstance) time.Duration {
	if restore != nil && restore.Spec.Timeout != nil && restore.Spec.Timeout.Duration > 0 {
		return restore.Spec.Timeout.Duration
	}
	if instance != nil && instance.Spec.OperationTimeoutMinutes > 0 {
		return time.Duration(instance.Spec.OperationTimeoutMinutes) * time.Minute
	}
	return runtimecfg.SnapshotCurrent().RestoreRuntime.InProgressMaxWait
}
