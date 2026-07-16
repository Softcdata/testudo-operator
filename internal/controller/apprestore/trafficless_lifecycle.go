package apprestore

import (
	"context"
	"fmt"
	"strings"
	"time"

	ctrlcommon "github.com/softcdata/testudo-operator/internal/controller"
	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	"github.com/softcdata/testudo-operator/pkg/helper"
	. "github.com/softcdata/testudo-operator/pkg/metadata"
	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	annotationDataSyncTrafficlessTerminationRequestedAt = "testudo.softcdata.com/datasync-trafficless-termination-requested-at"
	annotationDataSyncTrafficlessTerminationReason      = "testudo.softcdata.com/datasync-trafficless-termination-reason"
	annotationDataSyncTrafficlessTerminationMessage     = "testudo.softcdata.com/datasync-trafficless-termination-message"
	annotationDataSyncTrafficlessTerminationOutcome     = "testudo.softcdata.com/datasync-trafficless-termination-outcome"
	annotationDataSyncTrafficlessTerminationRestoreName = "testudo.softcdata.com/datasync-trafficless-termination-restore-name"

	dataSyncTrafficlessTerminationOutcomeFail  = "fail"
	dataSyncTrafficlessTerminationOutcomeRetry = "retry"

	dataSyncTrafficlessReasonTargetRuntimeNotReady = "TargetVeleroRuntimeNotReady"
	dataSyncTrafficlessReasonPodUnschedulable      = "TrafficlessPodUnschedulable"
	dataSyncTrafficlessReasonPodImagePullFailed    = "TrafficlessPodImagePullFailed"
	dataSyncTrafficlessReasonPodMountFailed        = "TrafficlessPodMountFailed"
	dataSyncTrafficlessReasonPodRuntimeFailed      = "TrafficlessPodRuntimeFailed"
	dataSyncTrafficlessReasonNodeAgentUnavailable  = "NodeAgentUnavailable"
	dataSyncTrafficlessReasonTerminationTimeout    = "VeleroRestoreTerminationTimeout"
)

func isDataSyncTrafficlessAppRestore(appRestore *disasterv1.AppRestore) bool {
	return appRestore != nil && appRestore.Labels != nil &&
		strings.TrimSpace(appRestore.Labels[LabelTrafficlessLifecycle]) == TrafficlessLifecycleDataSync
}

func dataSyncTrafficlessPodSelector(appRestore *disasterv1.AppRestore) (client.MatchingLabels, error) {
	if !isDataSyncTrafficlessAppRestore(appRestore) {
		return nil, nil
	}
	selector := client.MatchingLabels{}
	for _, key := range []string{
		LabelCleanupManagedBy,
		LabelCleanupOwnerToken,
		LabelCleanupRelation,
		LabelCleanupStrategy,
		LabelTrafficlessRun,
	} {
		value := strings.TrimSpace(appRestore.Labels[key])
		if value == "" {
			return nil, fmt.Errorf("DataSync trafficless AppRestore is missing required label %s", key)
		}
		selector[key] = value
	}
	return selector, nil
}

func dataSyncTrafficlessObservationTimeout(appRestore *disasterv1.AppRestore, cfg RestoreRuntimeConfig) time.Duration {
	timeout := resolveRestoreInProgressTimeout(appRestore, cfg)
	if cfg.PodVolumeRestorePendingMaxWait > 0 && (timeout <= 0 || timeout > cfg.PodVolumeRestorePendingMaxWait) {
		timeout = cfg.PodVolumeRestorePendingMaxWait
	}
	if timeout <= 0 {
		return 10 * time.Minute
	}
	return timeout
}

func (r *AppRestoreReconciler) detectDataSyncTrafficlessPodIssue(
	ctx context.Context,
	cli client.Client,
	appRestore *disasterv1.AppRestore,
	cfg RestoreRuntimeConfig,
) (reason, message string, err error) {
	selector, err := dataSyncTrafficlessPodSelector(appRestore)
	if err != nil {
		return dataSyncTrafficlessReasonPodRuntimeFailed, err.Error(), nil
	}
	if len(selector) == 0 {
		return "", "", nil
	}

	namespaces, err := r.getTargetNamespaces(ctx, appRestore)
	if err != nil {
		return "", "", err
	}
	if len(namespaces) == 0 {
		namespaces = append([]string(nil), appRestore.Spec.Template.IncludedNamespaces...)
	}

	observationTimeout := dataSyncTrafficlessObservationTimeout(appRestore, cfg)
	for _, namespace := range namespaces {
		namespace = strings.TrimSpace(namespace)
		if namespace == "" {
			continue
		}
		podList := &corev1.PodList{}
		if err := cli.List(ctx, podList, client.InNamespace(namespace), selector); err != nil {
			return "", "", err
		}
		for i := range podList.Items {
			pod := &podList.Items[i]
			if podFailureReason, podFailureMessage := trafficlessPodFailure(pod); podFailureReason != "" {
				if trafficlessPodFailureExceededWindow(pod, observationTimeout) {
					return podFailureReason, podFailureMessage, nil
				}
			}

			if pod.Spec.NodeName == "" || !trafficlessPodFailureExceededWindow(pod, observationTimeout) {
				continue
			}
			ready, detail, err := dataSyncTrafficlessNodeAgentReady(ctx, cli, pod.Spec.NodeName)
			if err != nil {
				return "", "", err
			}
			if !ready {
				return dataSyncTrafficlessReasonNodeAgentUnavailable,
					fmt.Sprintf("trafficless Pod %s/%s is scheduled on node %s without a Ready node-agent: %s", pod.Namespace, pod.Name, pod.Spec.NodeName, detail),
					nil
			}
		}
	}

	return "", "", nil
}

func trafficlessPodFailureExceededWindow(pod *corev1.Pod, timeout time.Duration) bool {
	if pod == nil || timeout <= 0 {
		return true
	}
	if pod.CreationTimestamp.IsZero() {
		return true
	}
	return time.Since(pod.CreationTimestamp.Time) > timeout
}

func trafficlessPodFailure(pod *corev1.Pod) (string, string) {
	if pod == nil {
		return "", ""
	}
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodScheduled && condition.Status == corev1.ConditionFalse &&
			strings.EqualFold(condition.Reason, "Unschedulable") {
			return dataSyncTrafficlessReasonPodUnschedulable,
				fmt.Sprintf("trafficless Pod %s/%s is Unschedulable: %s", pod.Namespace, pod.Name, condition.Message)
		}
	}

	if reason, message := trafficlessContainerFailure(pod, pod.Status.InitContainerStatuses, true); reason != "" {
		return reason, message
	}
	if reason, message := trafficlessContainerFailure(pod, pod.Status.ContainerStatuses, false); reason != "" {
		return reason, message
	}

	podReason := strings.TrimSpace(pod.Status.Reason)
	podMessage := strings.TrimSpace(pod.Status.Message)
	if strings.EqualFold(podReason, "FailedMount") || strings.Contains(strings.ToLower(podMessage), "failedmount") || strings.Contains(strings.ToLower(podMessage), "mountvolume") {
		return dataSyncTrafficlessReasonPodMountFailed,
			fmt.Sprintf("trafficless Pod %s/%s mount failed: %s %s", pod.Namespace, pod.Name, podReason, podMessage)
	}
	if pod.Status.Phase == corev1.PodFailed {
		return dataSyncTrafficlessReasonPodRuntimeFailed,
			fmt.Sprintf("trafficless Pod %s/%s failed: %s %s", pod.Namespace, pod.Name, podReason, podMessage)
	}
	return "", ""
}

func trafficlessContainerFailure(pod *corev1.Pod, statuses []corev1.ContainerStatus, initContainer bool) (string, string) {
	containerType := "container"
	if initContainer {
		containerType = "initContainer"
	}
	for _, status := range statuses {
		if waiting := status.State.Waiting; waiting != nil {
			reason := strings.TrimSpace(waiting.Reason)
			message := fmt.Sprintf("trafficless Pod %s/%s %s %s is waiting: %s %s", pod.Namespace, pod.Name, containerType, status.Name, reason, strings.TrimSpace(waiting.Message))
			switch reason {
			case "ImagePullBackOff", "ErrImagePull", "RegistryUnavailable":
				return dataSyncTrafficlessReasonPodImagePullFailed, message
			case "CreateContainerConfigError", "FailedMount":
				return dataSyncTrafficlessReasonPodMountFailed, message
			case "CrashLoopBackOff", "RunContainerError", "CreateContainerError", "InvalidImageName":
				return dataSyncTrafficlessReasonPodRuntimeFailed, message
			}
			if strings.Contains(strings.ToLower(reason+" "+waiting.Message), "mount") {
				return dataSyncTrafficlessReasonPodMountFailed, message
			}
		}
		if terminated := status.State.Terminated; terminated != nil && terminated.ExitCode != 0 {
			return dataSyncTrafficlessReasonPodRuntimeFailed,
				fmt.Sprintf("trafficless Pod %s/%s %s %s terminated exitCode=%d reason=%s message=%s", pod.Namespace, pod.Name, containerType, status.Name, terminated.ExitCode, terminated.Reason, terminated.Message)
		}
	}
	return "", ""
}

func dataSyncTrafficlessNodeAgentReady(ctx context.Context, cli client.Client, nodeName string) (bool, string, error) {
	nodeAgent := &appsv1.DaemonSet{}
	if err := cli.Get(ctx, types.NamespacedName{Name: "node-agent", Namespace: ctrlcommon.VeleroNamespace}, nodeAgent); err != nil {
		if apierrors.IsNotFound(err) {
			return false, "DaemonSet velero/node-agent is not found", nil
		}
		return false, "", err
	}
	if nodeAgent.Status.DesiredNumberScheduled < 1 || nodeAgent.Status.NumberReady < nodeAgent.Status.DesiredNumberScheduled {
		return false, fmt.Sprintf("DaemonSet %s/%s ready=%d desired=%d", nodeAgent.Namespace, nodeAgent.Name, nodeAgent.Status.NumberReady, nodeAgent.Status.DesiredNumberScheduled), nil
	}

	pods := &corev1.PodList{}
	if err := cli.List(ctx, pods, client.InNamespace(ctrlcommon.VeleroNamespace)); err != nil {
		return false, "", err
	}
	for i := range pods.Items {
		pod := &pods.Items[i]
		if pod.Spec.NodeName != nodeName || !isNodeAgentPod(pod) || pod.Status.Phase != corev1.PodRunning {
			continue
		}
		for _, condition := range pod.Status.Conditions {
			if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
				return true, "", nil
			}
		}
	}
	return false, "no Ready node-agent Pod found on the scheduled node", nil
}

func isNodeAgentPod(pod *corev1.Pod) bool {
	if pod == nil {
		return false
	}
	if pod.Labels != nil {
		if pod.Labels["name"] == "node-agent" || pod.Labels["role"] == "node-agent" {
			return true
		}
	}
	return strings.HasPrefix(pod.Name, "node-agent-")
}

func dataSyncTrafficlessTerminationPending(appRestore *disasterv1.AppRestore) bool {
	return appRestore != nil && appRestore.Annotations != nil &&
		strings.TrimSpace(appRestore.Annotations[annotationDataSyncTrafficlessTerminationRequestedAt]) != ""
}

func (r *AppRestoreReconciler) beginDataSyncTrafficlessTermination(
	ctx context.Context,
	cli client.Client,
	appRestore *disasterv1.AppRestore,
	restore *velerov1.Restore,
	restoreName string,
	reason string,
	message string,
	outcome string,
	cfg RestoreRuntimeConfig,
) (disasterv1.AppRestorePhase, ctrl.Result, error) {
	if appRestore.Annotations == nil {
		appRestore.Annotations = make(map[string]string)
	}
	if !dataSyncTrafficlessTerminationPending(appRestore) {
		appRestore.Annotations[annotationDataSyncTrafficlessTerminationRequestedAt] = time.Now().UTC().Format(time.RFC3339)
		appRestore.Annotations[annotationDataSyncTrafficlessTerminationReason] = reason
		appRestore.Annotations[annotationDataSyncTrafficlessTerminationMessage] = message
		appRestore.Annotations[annotationDataSyncTrafficlessTerminationOutcome] = outcome
		appRestore.Annotations[annotationDataSyncTrafficlessTerminationRestoreName] = restoreName
		r.Recorder.Event(appRestore, corev1.EventTypeWarning, reason, message)
	}
	if err := requestDataSyncTrafficlessRestoreDelete(ctx, cli, restore); err != nil {
		return disasterv1.PhaseRestoring, ctrl.Result{}, err
	}
	return disasterv1.PhaseRestoring, ctrl.Result{RequeueAfter: cfg.RetryBackoff}, nil
}

func requestDataSyncTrafficlessRestoreDelete(ctx context.Context, cli client.Client, restore *velerov1.Restore) error {
	if cli == nil || restore == nil || restore.Name == "" || !restore.DeletionTimestamp.IsZero() {
		return nil
	}
	if err := cli.Delete(ctx, restore); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete Velero Restore %s/%s: %w", restore.Namespace, restore.Name, err)
	}
	return nil
}

func (r *AppRestoreReconciler) reconcileDataSyncTrafficlessTermination(
	ctx context.Context,
	cli client.Client,
	appRestore *disasterv1.AppRestore,
	restore *velerov1.Restore,
	restoreErr error,
	restoreName string,
	cfg RestoreRuntimeConfig,
) (disasterv1.AppRestorePhase, ctrl.Result, error) {
	if apierrors.IsNotFound(restoreErr) {
		reason := strings.TrimSpace(appRestore.Annotations[annotationDataSyncTrafficlessTerminationReason])
		message := strings.TrimSpace(appRestore.Annotations[annotationDataSyncTrafficlessTerminationMessage])
		outcome := strings.TrimSpace(appRestore.Annotations[annotationDataSyncTrafficlessTerminationOutcome])
		clearDataSyncTrafficlessTermination(appRestore)
		if outcome == dataSyncTrafficlessTerminationOutcomeRetry {
			clearRestoreConvergenceTracking(appRestore)
			appRestore.Status.RestoreStatus = velerov1.RestoreStatus{}
			return disasterv1.PhaseRestoring, ctrl.Result{RequeueAfter: cfg.RetryBackoff}, nil
		}
		if reason == "" {
			reason = "RestoreFailed"
		}
		if message == "" {
			message = fmt.Sprintf("Velero Restore %s was deleted after failure", restoreName)
		}
		appRestore.Status.Reason = reason
		appRestore.Status.Message = message
		r.reportDataSyncTrafficlessTerminationFailure(ctx, appRestore, restore, restoreName, reason, message)
		return disasterv1.PhaseFailed, ctrl.Result{}, nil
	}
	if restoreErr != nil {
		return disasterv1.PhaseRestoring, ctrl.Result{}, restoreErr
	}

	requestedAt, ok := dataSyncTrafficlessTerminationRequestedAt(appRestore)
	if !ok {
		requestedAt = time.Now()
	}
	if elapsed := time.Since(requestedAt); elapsed > resolveRestoreInProgressTimeout(appRestore, cfg) {
		clearDataSyncTrafficlessTermination(appRestore)
		message := fmt.Sprintf("Velero Restore %s is still present %s after deletion was requested", restoreName, elapsed.Round(time.Second))
		appRestore.Status.Reason = dataSyncTrafficlessReasonTerminationTimeout
		appRestore.Status.Message = message
		r.reportDataSyncTrafficlessTerminationFailure(ctx, appRestore, restore, restoreName, dataSyncTrafficlessReasonTerminationTimeout, message)
		return disasterv1.PhaseFailed, ctrl.Result{}, nil
	}
	if err := requestDataSyncTrafficlessRestoreDelete(ctx, cli, restore); err != nil {
		return disasterv1.PhaseRestoring, ctrl.Result{}, err
	}
	return disasterv1.PhaseRestoring, ctrl.Result{RequeueAfter: cfg.RetryBackoff}, nil
}

func dataSyncTrafficlessTerminationRequestedAt(appRestore *disasterv1.AppRestore) (time.Time, bool) {
	if appRestore == nil || appRestore.Annotations == nil {
		return time.Time{}, false
	}
	raw := strings.TrimSpace(appRestore.Annotations[annotationDataSyncTrafficlessTerminationRequestedAt])
	if raw == "" {
		return time.Time{}, false
	}
	requestedAt, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, false
	}
	return requestedAt, true
}

func clearDataSyncTrafficlessTermination(appRestore *disasterv1.AppRestore) {
	if appRestore == nil || appRestore.Annotations == nil {
		return
	}
	for _, key := range []string{
		annotationDataSyncTrafficlessTerminationRequestedAt,
		annotationDataSyncTrafficlessTerminationReason,
		annotationDataSyncTrafficlessTerminationMessage,
		annotationDataSyncTrafficlessTerminationOutcome,
		annotationDataSyncTrafficlessTerminationRestoreName,
	} {
		delete(appRestore.Annotations, key)
	}
}

func (r *AppRestoreReconciler) reportDataSyncTrafficlessTerminationFailure(
	ctx context.Context,
	appRestore *disasterv1.AppRestore,
	restore *velerov1.Restore,
	restoreName string,
	reason string,
	message string,
) {
	user := appRestore.Annotations[AnnotationUser]
	if user == "" {
		user = defaultAppRestoreUser
	}
	triggeredBy := appRestore.Annotations[AnnotationTraceID]
	taskName := fmt.Sprintf("应用恢复 %s 执行恢复 %s", appRestore.Name, restoreName)
	var startTimestamp, completionTimestamp *metav1.Time
	if restore != nil {
		startTimestamp = restore.Status.StartTimestamp
		completionTimestamp = restore.Status.CompletionTimestamp
	}
	helper.ReportTaskFinishedWithClient(
		ctx, r.Client, r.Scheme, appRestore, taskName, appRestore.Spec.Cluster, helper.TaskStatusFailed,
		startTimestamp, completionTimestamp, user, triggeredBy, message, reason,
	)
}
