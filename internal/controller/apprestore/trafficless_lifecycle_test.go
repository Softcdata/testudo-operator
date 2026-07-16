package apprestore

import (
	"context"
	"testing"
	"time"

	controller "github.com/softcdata/testudo-operator/internal/controller"
	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	"github.com/softcdata/testudo-operator/pkg/metadata"
	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newTrafficlessLifecycleAppRestore(name string) *disasterv1.AppRestore {
	return &disasterv1.AppRestore{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "management",
			UID:       types.UID(name + "-uid"),
			Labels: map[string]string{
				metadata.LabelTrafficlessLifecycle: metadata.TrafficlessLifecycleDataSync,
				metadata.LabelCleanupManagedBy:     metadata.LabelCleanupManagedByValueOperator,
				metadata.LabelCleanupOwnerToken:    "owner-token",
				metadata.LabelCleanupRelation:      metadata.CleanupRelationDataSyncTrafficlessPod,
				metadata.LabelCleanupStrategy:      metadata.CleanupStrategyDelete,
				metadata.LabelTrafficlessRun:       "run-token",
			},
		},
		Spec: disasterv1.AppRestoreSpec{
			Cluster:  "target",
			Template: velerov1.RestoreSpec{IncludedNamespaces: []string{"workload"}},
		},
		Status: disasterv1.AppRestoreStatus{Status: disasterv1.PhaseRestoring},
	}
}

func newTrafficlessLifecycleScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := newPVRTestScheme(t)
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatalf("add apps scheme: %v", err)
	}
	return scheme
}

func dataSyncTrafficlessPodForTest(appRestore *disasterv1.AppRestore, name string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Namespace:         "workload",
			CreationTimestamp: metav1.NewTime(time.Now().Add(-15 * time.Minute)),
			Labels: map[string]string{
				metadata.LabelCleanupManagedBy:  appRestore.Labels[metadata.LabelCleanupManagedBy],
				metadata.LabelCleanupOwnerToken: appRestore.Labels[metadata.LabelCleanupOwnerToken],
				metadata.LabelCleanupRelation:   appRestore.Labels[metadata.LabelCleanupRelation],
				metadata.LabelCleanupStrategy:   appRestore.Labels[metadata.LabelCleanupStrategy],
				metadata.LabelTrafficlessRun:    appRestore.Labels[metadata.LabelTrafficlessRun],
			},
		},
	}
}

func TestRestoringHandler_DataSyncTrafficlessPodFailureWaitsForRestoreDeletion(t *testing.T) {
	ctx := context.Background()
	scheme := newTrafficlessLifecycleScheme(t)
	appRestore := newTrafficlessLifecycleAppRestore("trafficless-pod-failure")
	restore := &velerov1.Restore{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "res-" + appRestore.Name,
			Namespace:         controller.VeleroNamespace,
			CreationTimestamp: metav1.NewTime(time.Now().Add(-20 * time.Minute)),
		},
		Status: velerov1.RestoreStatus{Phase: velerov1.RestorePhaseInProgress},
	}
	pod := dataSyncTrafficlessPodForTest(appRestore, "unschedulable")
	pod.Status.Conditions = []corev1.PodCondition{{
		Type:    corev1.PodScheduled,
		Status:  corev1.ConditionFalse,
		Reason:  "Unschedulable",
		Message: "0/3 nodes are available",
	}}
	target := fake.NewClientBuilder().WithScheme(scheme).WithObjects(restore, pod).Build()
	management := fake.NewClientBuilder().WithScheme(scheme).WithObjects(appRestore).Build()
	reconciler := &AppRestoreReconciler{
		Client:        management,
		Scheme:        scheme,
		Recorder:      record.NewFakeRecorder(20),
		ClientFactory: &controller.MockClientFactory{MockClient: target},
	}

	phase, _, err := (&RestoringHandler{}).Handle(ctx, reconciler, appRestore)
	if err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	if phase != disasterv1.PhaseRestoring {
		t.Fatalf("expected termination confirmation to remain Restoring, got %s", phase)
	}
	if got := appRestore.Annotations[annotationDataSyncTrafficlessTerminationReason]; got != dataSyncTrafficlessReasonPodUnschedulable {
		t.Fatalf("expected preserved pod reason, got %q", got)
	}
	if err := target.Get(ctx, types.NamespacedName{Name: restore.Name, Namespace: restore.Namespace}, &velerov1.Restore{}); err == nil {
		t.Fatal("expected first reconcile to request Velero Restore deletion")
	}

	phase, _, err = (&RestoringHandler{}).Handle(ctx, reconciler, appRestore)
	if err != nil {
		t.Fatalf("deletion confirmation reconcile: %v", err)
	}
	if phase != disasterv1.PhaseFailed {
		t.Fatalf("expected final failure after NotFound confirmation, got %s", phase)
	}
	if appRestore.Status.Reason != dataSyncTrafficlessReasonPodUnschedulable {
		t.Fatalf("expected final preserved reason, got %q", appRestore.Status.Reason)
	}
}

func TestDetectDataSyncTrafficlessPodIssueClassifiesContainerFailures(t *testing.T) {
	appRestore := newTrafficlessLifecycleAppRestore("trafficless-classifier")
	cases := []struct {
		name   string
		status corev1.ContainerStatus
		init   bool
		want   string
	}{
		{name: "image pull", status: corev1.ContainerStatus{Name: "main", State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff"}}}, want: dataSyncTrafficlessReasonPodImagePullFailed},
		{name: "mount config", status: corev1.ContainerStatus{Name: "main", State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CreateContainerConfigError"}}}, want: dataSyncTrafficlessReasonPodMountFailed},
		{name: "init crash loop", status: corev1.ContainerStatus{Name: "init", State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}}}, init: true, want: dataSyncTrafficlessReasonPodRuntimeFailed},
		{name: "sidecar failed", status: corev1.ContainerStatus{Name: "sidecar", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 1, Reason: "Error"}}}, want: dataSyncTrafficlessReasonPodRuntimeFailed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pod := dataSyncTrafficlessPodForTest(appRestore, tc.name)
			if tc.init {
				pod.Status.InitContainerStatuses = []corev1.ContainerStatus{tc.status}
			} else {
				pod.Status.ContainerStatuses = []corev1.ContainerStatus{tc.status}
			}
			reason, message := trafficlessPodFailure(pod)
			if reason != tc.want {
				t.Fatalf("expected %s, got %s (%s)", tc.want, reason, message)
			}
		})
	}
}

func TestDetectDataSyncTrafficlessPodIssueReportsMissingNodeAgent(t *testing.T) {
	ctx := context.Background()
	scheme := newTrafficlessLifecycleScheme(t)
	appRestore := newTrafficlessLifecycleAppRestore("trafficless-node-agent")
	pod := dataSyncTrafficlessPodForTest(appRestore, "scheduled")
	pod.Spec.NodeName = "node-a"
	target := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		pod,
		&appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{Name: "node-agent", Namespace: controller.VeleroNamespace},
			Status:     appsv1.DaemonSetStatus{DesiredNumberScheduled: 1, NumberReady: 1},
		},
	).Build()
	reconciler := &AppRestoreReconciler{}

	reason, message, err := reconciler.detectDataSyncTrafficlessPodIssue(ctx, target, appRestore, reconciler.restoreRuntimeConfig())
	if err != nil {
		t.Fatalf("detect node-agent: %v", err)
	}
	if reason != dataSyncTrafficlessReasonNodeAgentUnavailable || message == "" {
		t.Fatalf("expected node-agent diagnostic, got reason=%q message=%q", reason, message)
	}
}

func TestDetectPodVolumeRestoreIssueIncludesInProgressOnlyForDataSync(t *testing.T) {
	ctx := context.Background()
	scheme := newTrafficlessLifecycleScheme(t)
	restoreName := "res-trafficless-pvr"
	pvr := &velerov1.PodVolumeRestore{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "pvr-in-progress",
			Namespace:         controller.VeleroNamespace,
			CreationTimestamp: metav1.NewTime(time.Now().Add(-20 * time.Minute)),
			Labels:            map[string]string{velerov1.RestoreNameLabel: restoreName},
		},
		Status: velerov1.PodVolumeRestoreStatus{Phase: velerov1.PodVolumeRestorePhaseInProgress},
	}
	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pvr).Build()
	reconciler := &AppRestoreReconciler{}

	reason, _, err := reconciler.detectPodVolumeRestoreIssueWithInProgress(ctx, cli, restoreName, time.Hour, 10*time.Minute, true)
	if err != nil || reason != appRestoreReasonPVRStalled {
		t.Fatalf("expected DataSync PVR stall, reason=%q err=%v", reason, err)
	}
	reason, _, err = reconciler.detectPodVolumeRestoreIssue(ctx, cli, restoreName, time.Hour, 10*time.Minute)
	if err != nil || reason != "" {
		t.Fatalf("ordinary AppRestore must retain prior InProgress behavior, reason=%q err=%v", reason, err)
	}
}

func TestDataSyncTrafficlessTerminationTimesOutWithoutFinalizerStripping(t *testing.T) {
	ctx := context.Background()
	scheme := newTrafficlessLifecycleScheme(t)
	appRestore := newTrafficlessLifecycleAppRestore("trafficless-termination-timeout")
	appRestore.Spec.Timeout = &metav1.Duration{Duration: time.Minute}
	appRestore.Annotations = map[string]string{
		annotationDataSyncTrafficlessTerminationRequestedAt: time.Now().Add(-2 * time.Minute).UTC().Format(time.RFC3339),
		annotationDataSyncTrafficlessTerminationReason:      appRestoreReasonPVRFailed,
		annotationDataSyncTrafficlessTerminationMessage:     "pvr failed",
		annotationDataSyncTrafficlessTerminationOutcome:     dataSyncTrafficlessTerminationOutcomeFail,
	}
	management := fake.NewClientBuilder().WithScheme(scheme).WithObjects(appRestore).Build()
	reconciler := &AppRestoreReconciler{Client: management, Scheme: scheme, Recorder: record.NewFakeRecorder(20)}
	restore := &velerov1.Restore{ObjectMeta: metav1.ObjectMeta{Name: "res-" + appRestore.Name, Namespace: controller.VeleroNamespace, Finalizers: []string{"velero.io/restore-finalizer"}}}

	phase, _, err := reconciler.reconcileDataSyncTrafficlessTermination(ctx, fake.NewClientBuilder().WithScheme(scheme).Build(), appRestore, restore, nil, restore.Name, reconciler.restoreRuntimeConfig())
	if err != nil {
		t.Fatalf("termination timeout reconcile: %v", err)
	}
	if phase != disasterv1.PhaseFailed || appRestore.Status.Reason != dataSyncTrafficlessReasonTerminationTimeout {
		t.Fatalf("expected termination timeout failure, phase=%s reason=%s", phase, appRestore.Status.Reason)
	}
	if len(restore.Finalizers) != 1 {
		t.Fatalf("DataSync branch must not strip Velero Restore finalizers, got %#v", restore.Finalizers)
	}
}

func TestTrafficlessLifecycleSelectorSkipsOrdinaryAppRestore(t *testing.T) {
	selector, err := dataSyncTrafficlessPodSelector(&disasterv1.AppRestore{})
	if err != nil || selector != nil {
		t.Fatalf("ordinary AppRestore must stay outside lifecycle selector, selector=%#v err=%v", selector, err)
	}
}
