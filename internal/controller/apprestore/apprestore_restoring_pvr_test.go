package apprestore

import (
	"context"
	"strings"
	"testing"
	"time"

	controller "github.com/softcdata/testudo-operator/internal/controller"
	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newPVRTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add corev1 scheme: %v", err)
	}
	if err := disasterv1.AddToScheme(scheme); err != nil {
		t.Fatalf("add disasterv1 scheme: %v", err)
	}
	if err := velerov1.AddToScheme(scheme); err != nil {
		t.Fatalf("add velero scheme: %v", err)
	}
	return scheme
}

func TestDetectPodVolumeRestoreIssue_Failed(t *testing.T) {
	scheme := newPVRTestScheme(t)
	restoreName := "res-apprestore-failed"
	pvr := &velerov1.PodVolumeRestore{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "pvr-failed-1",
			Namespace:         controller.VeleroNamespace,
			CreationTimestamp: metav1.NewTime(time.Now().Add(-2 * time.Minute)),
			Labels: map[string]string{
				"velero.io/restore-name": restoreName,
			},
		},
		Status: velerov1.PodVolumeRestoreStatus{
			Phase:   velerov1.PodVolumeRestorePhaseFailed,
			Message: "mkdir /xxx/.velero: no such file or directory",
		},
	}

	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pvr).Build()
	reconciler := &AppRestoreReconciler{}

	cfg := reconciler.restoreRuntimeConfig()
	reason, message, err := reconciler.detectPodVolumeRestoreIssue(context.Background(), cli, restoreName, time.Hour, cfg.PodVolumeRestorePendingMaxWait)
	if err != nil {
		t.Fatalf("detectPodVolumeRestoreIssue returned error: %v", err)
	}
	if reason != appRestoreReasonPVRFailed {
		t.Fatalf("unexpected reason: got %q", reason)
	}
	if !strings.Contains(message, "pvr-failed-1") {
		t.Fatalf("message should contain failed pvr name, got: %q", message)
	}
	if !strings.Contains(message, "no such file or directory") {
		t.Fatalf("message should contain failure detail, got: %q", message)
	}
}

func TestDetectPodVolumeRestoreIssue_Stalled(t *testing.T) {
	scheme := newPVRTestScheme(t)
	restoreName := "res-apprestore-stalled"
	pvr := &velerov1.PodVolumeRestore{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "pvr-stalled-1",
			Namespace:         controller.VeleroNamespace,
			CreationTimestamp: metav1.NewTime(time.Now().Add(-20 * time.Minute)),
			Labels: map[string]string{
				"velero.io/restore-name": restoreName,
			},
		},
		// status.phase intentionally empty (stuck pending)
	}

	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pvr).Build()
	reconciler := &AppRestoreReconciler{}

	cfg := reconciler.restoreRuntimeConfig()
	reason, message, err := reconciler.detectPodVolumeRestoreIssue(context.Background(), cli, restoreName, time.Hour, cfg.PodVolumeRestorePendingMaxWait)
	if err != nil {
		t.Fatalf("detectPodVolumeRestoreIssue returned error: %v", err)
	}
	if reason != appRestoreReasonPVRStalled {
		t.Fatalf("unexpected reason: got %q", reason)
	}
	if !strings.Contains(message, "pvr-stalled-1") {
		t.Fatalf("message should contain stalled pvr name, got: %q", message)
	}
}

func TestRestoringHandler_FailsWhenPodVolumeRestoreFailed(t *testing.T) {
	scheme := newPVRTestScheme(t)
	mgmtClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	mockTargetClient := &controller.MockClient{
		Client: mgmtClient,
	}
	mockFactory := &controller.MockClientFactory{
		MockClient: mockTargetClient,
	}

	recorder := record.NewFakeRecorder(20)
	reconciler := &AppRestoreReconciler{
		Client:        mgmtClient,
		Scheme:        scheme,
		Recorder:      recorder,
		ClientFactory: mockFactory,
	}

	restorePVsTrue := true
	appRestore := &disasterv1.AppRestore{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "apprestore-pvr-failed",
			Namespace: "disaster-system",
		},
		Spec: disasterv1.AppRestoreSpec{
			Cluster: "cluster-a",
			Template: velerov1.RestoreSpec{
				RestorePVs: &restorePVsTrue,
			},
		},
		Status: disasterv1.AppRestoreStatus{
			Status: disasterv1.PhaseRestoring,
		},
	}

	restoreName := reconciler.GenRestoreName(appRestore)
	mockTargetClient.MockGet = func(ctx context.Context, key ctrlclient.ObjectKey, obj ctrlclient.Object, opts ...ctrlclient.GetOption) error {
		if restore, ok := obj.(*velerov1.Restore); ok {
			restore.Name = restoreName
			restore.Namespace = controller.VeleroNamespace
			restore.CreationTimestamp = metav1.NewTime(time.Now().Add(-5 * time.Minute))
			restore.Status.Phase = velerov1.RestorePhaseInProgress
			return nil
		}
		return mgmtClient.Get(ctx, key, obj, opts...)
	}
	mockTargetClient.MockList = func(ctx context.Context, list ctrlclient.ObjectList, opts ...ctrlclient.ListOption) error {
		if pvrList, ok := list.(*velerov1.PodVolumeRestoreList); ok {
			pvrList.Items = []velerov1.PodVolumeRestore{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pvr-failed-in-handler",
						Namespace: controller.VeleroNamespace,
						Labels: map[string]string{
							"velero.io/restore-name": restoreName,
						},
						CreationTimestamp: metav1.NewTime(time.Now().Add(-1 * time.Minute)),
					},
					Status: velerov1.PodVolumeRestoreStatus{
						Phase:   velerov1.PodVolumeRestorePhaseFailed,
						Message: "data path restore failed",
					},
				},
			}
			return nil
		}
		return mgmtClient.List(ctx, list, opts...)
	}

	handler := &RestoringHandler{}
	nextPhase, _, err := handler.Handle(context.Background(), reconciler, appRestore)
	if err != nil {
		t.Fatalf("RestoringHandler.Handle returned error: %v", err)
	}
	if nextPhase != disasterv1.PhaseFailed {
		t.Fatalf("expected next phase Failed, got %q", nextPhase)
	}
	if appRestore.Status.Reason != appRestoreReasonPVRFailed {
		t.Fatalf("expected reason PodVolumeRestoreFailed, got %q", appRestore.Status.Reason)
	}
	if !strings.Contains(appRestore.Status.Message, "pvr-failed-in-handler") {
		t.Fatalf("expected message to include failed PVR name, got %q", appRestore.Status.Message)
	}
}

func TestRestoringHandler_MapsVeleroPartiallyFailedToAppRestorePartiallyFailed(t *testing.T) {
	scheme := newPVRTestScheme(t)
	mgmtClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	mockTargetClient := &controller.MockClient{
		Client: mgmtClient,
	}
	mockFactory := &controller.MockClientFactory{
		MockClient: mockTargetClient,
	}

	reconciler := &AppRestoreReconciler{
		Client:        mgmtClient,
		Scheme:        scheme,
		Recorder:      record.NewFakeRecorder(20),
		ClientFactory: mockFactory,
	}

	appRestore := &disasterv1.AppRestore{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "apprestore-partial",
			Namespace: "disaster-system",
		},
		Spec: disasterv1.AppRestoreSpec{
			Cluster: "cluster-a",
		},
		Status: disasterv1.AppRestoreStatus{
			Status: disasterv1.PhaseRestoring,
		},
	}

	restoreName := reconciler.GenRestoreName(appRestore)
	mockTargetClient.MockGet = func(ctx context.Context, key ctrlclient.ObjectKey, obj ctrlclient.Object, opts ...ctrlclient.GetOption) error {
		if restore, ok := obj.(*velerov1.Restore); ok {
			restore.Name = restoreName
			restore.Namespace = controller.VeleroNamespace
			restore.CreationTimestamp = metav1.NewTime(time.Now().Add(-2 * time.Minute))
			restore.Status.Phase = velerov1.RestorePhasePartiallyFailed
			restore.Status.Errors = 1
			restore.Status.Warnings = 2
			return nil
		}
		return mgmtClient.Get(ctx, key, obj, opts...)
	}

	handler := &RestoringHandler{}
	nextPhase, _, err := handler.Handle(context.Background(), reconciler, appRestore)
	if err != nil {
		t.Fatalf("RestoringHandler.Handle returned error: %v", err)
	}
	if nextPhase != disasterv1.PhasePartiallyFailed {
		t.Fatalf("expected next phase PartiallyFailed, got %q", nextPhase)
	}
	if appRestore.Status.RestoreStatus.Phase != velerov1.RestorePhasePartiallyFailed {
		t.Fatalf("expected nested restore phase PartiallyFailed, got %q", appRestore.Status.RestoreStatus.Phase)
	}
	if appRestore.Status.Reason != "RestorePartiallyFailed" {
		t.Fatalf("expected reason RestorePartiallyFailed, got %q", appRestore.Status.Reason)
	}
	if !strings.Contains(appRestore.Status.Message, "errors=1 warnings=2") {
		t.Fatalf("expected error counts in message, got %q", appRestore.Status.Message)
	}
}
