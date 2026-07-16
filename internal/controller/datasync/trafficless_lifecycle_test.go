package datasync

import (
	"context"
	"testing"
	"time"

	"github.com/go-logr/logr"
	ctrlcommon "github.com/softcdata/testudo-operator/internal/controller"
	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	"github.com/softcdata/testudo-operator/pkg/metadata"
	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newDataSyncTrafficlessLifecycleScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add corev1 scheme: %v", err)
	}
	if err := disasterv1.AddToScheme(scheme); err != nil {
		t.Fatalf("add disaster scheme: %v", err)
	}
	if err := velerov1.AddToScheme(scheme); err != nil {
		t.Fatalf("add Velero scheme: %v", err)
	}
	return scheme
}

func newTrafficlessCleanupDataSync() *disasterv1.DataSync {
	return &disasterv1.DataSync{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "data-sync-a",
			Namespace: "management",
			UID:       types.UID("data-sync-a-uid"),
		},
	}
}

func newTrafficlessCleanupInstance() (*disasterv1.DisasterInstance, *disasterv1.DisasterConfig) {
	return &disasterv1.DisasterInstance{
			Spec: disasterv1.DisasterInstanceSpec{Namespaces: []string{"workload"}},
		}, &disasterv1.DisasterConfig{
			Spec: disasterv1.DisasterConfigSpec{SourceCluster: "source", TargetCluster: "target"},
		}
}

func TestTrafficlessCleanupSelectsOnlyExactDataSyncOwner(t *testing.T) {
	ctx := context.Background()
	scheme := newDataSyncTrafficlessLifecycleScheme(t)
	ds := newTrafficlessCleanupDataSync()
	instance, config := newTrafficlessCleanupInstance()
	restoreName := "rec-ds-data-sync-a-123456"

	other := newTrafficlessCleanupDataSync()
	other.UID = types.UID("another-data-sync-uid")
	owned := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "owned", Namespace: "workload", Labels: dataSyncTrafficlessLabels(ds, restoreName)},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "worker", Image: "busybox:1.36"}}},
	}
	otherOwner := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "other-owner", Namespace: "workload", Labels: dataSyncTrafficlessLabels(other, "rec-ds-other-123456")},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "worker", Image: "busybox:1.36"}}},
	}
	normalBusybox := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "normal-busybox", Namespace: "workload"},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "worker", Image: "busybox:1.36"}}},
	}
	target := fake.NewClientBuilder().WithScheme(scheme).WithObjects(owned, otherOwner, normalBusybox).Build()
	reconciler := &DataSyncReconciler{
		Client:              fake.NewClientBuilder().WithScheme(scheme).Build(),
		Scheme:              scheme,
		TargetClientFactory: &ctrlcommon.MockClientFactory{MockClient: target},
	}

	result, err := reconciler.reconcileTrafficlessPodCleanup(ctx, logr.Discard(), ds, config, instance, restoreName, trafficlessCleanupBeforeRestore)
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if result.Done {
		t.Fatal("a successful Delete request must not be treated as cleanup confirmation")
	}
	for _, name := range []string{"normal-busybox", "other-owner"} {
		if err := target.Get(ctx, types.NamespacedName{Namespace: "workload", Name: name}, &corev1.Pod{}); err != nil {
			t.Fatalf("unrelated Pod %s was removed: %v", name, err)
		}
	}
	if err := target.Get(ctx, types.NamespacedName{Namespace: "workload", Name: "owned"}, &corev1.Pod{}); !apierrors.IsNotFound(err) {
		t.Fatalf("expected exact owner Pod to be deleted, got %v", err)
	}

	result, err = reconciler.reconcileTrafficlessPodCleanup(ctx, logr.Discard(), ds, config, instance, restoreName, trafficlessCleanupBeforeRestore)
	if err != nil {
		t.Fatalf("confirm cleanup: %v", err)
	}
	if !result.Done {
		t.Fatalf("expected cleanup confirmation after the selector became empty: %#v", result)
	}
}

func TestTrafficlessCleanupFailsClosedForUnscopedLegacyPod(t *testing.T) {
	ctx := context.Background()
	scheme := newDataSyncTrafficlessLifecycleScheme(t)
	ds := newTrafficlessCleanupDataSync()
	instance, config := newTrafficlessCleanupInstance()
	legacy := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "legacy-trafficless",
			Namespace: "workload",
			Labels:    map[string]string{"trafficless": "true"},
		},
	}
	target := fake.NewClientBuilder().WithScheme(scheme).WithObjects(legacy).Build()
	reconciler := &DataSyncReconciler{
		Client:              fake.NewClientBuilder().WithScheme(scheme).Build(),
		Scheme:              scheme,
		TargetClientFactory: &ctrlcommon.MockClientFactory{MockClient: target},
	}

	_, err := reconciler.reconcileTrafficlessPodCleanup(ctx, logr.Discard(), ds, config, instance, "rec-ds-a", trafficlessCleanupBeforeRestore)
	if err == nil {
		t.Fatal("expected unscoped trafficless Pod to fail closed")
	}
	reason, message := trafficlessLifecycleErrorDetails(err, "")
	if reason != dataSyncReasonTrafficlessCleanupAmbiguous || message == "" {
		t.Fatalf("unexpected lifecycle error: reason=%q message=%q", reason, message)
	}
	if err := target.Get(ctx, types.NamespacedName{Namespace: "workload", Name: legacy.Name}, &corev1.Pod{}); err != nil {
		t.Fatalf("legacy Pod must not be deleted: %v", err)
	}
}

func TestTrafficlessCleanupWaitsAndTimesOutWhenDeleteDoesNotConverge(t *testing.T) {
	ctx := context.Background()
	scheme := newDataSyncTrafficlessLifecycleScheme(t)
	ds := newTrafficlessCleanupDataSync()
	instance, config := newTrafficlessCleanupInstance()
	restoreName := "rec-ds-data-sync-a-123456"
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "stuck", Namespace: "workload", Labels: dataSyncTrafficlessLabels(ds, restoreName)},
	}
	baseTarget := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod).Build()
	target := &ctrlcommon.MockClient{
		Client: baseTarget,
		MockDelete: func(context.Context, client.Object, ...client.DeleteOption) error {
			return nil
		},
	}
	reconciler := &DataSyncReconciler{
		Client:              fake.NewClientBuilder().WithScheme(scheme).Build(),
		Scheme:              scheme,
		TargetClientFactory: &ctrlcommon.MockClientFactory{MockClient: target},
	}

	result, err := reconciler.reconcileTrafficlessPodCleanup(ctx, logr.Discard(), ds, config, instance, restoreName, trafficlessCleanupAfterRestore)
	if err != nil {
		t.Fatalf("initial cleanup: %v", err)
	}
	if result.Done || !result.MetadataChanged {
		t.Fatalf("expected pending cleanup with persisted start time, got %#v", result)
	}
	ds.Annotations[annotationTrafficlessCleanupAfterStartedAt] = time.Now().Add(-trafficlessCleanupTimeout() - time.Second).UTC().Format(time.RFC3339)

	_, err = reconciler.reconcileTrafficlessPodCleanup(ctx, logr.Discard(), ds, config, instance, restoreName, trafficlessCleanupAfterRestore)
	if err == nil {
		t.Fatal("expected cleanup timeout while Pod remains present")
	}
	reason, _ := trafficlessLifecycleErrorDetails(err, "")
	if reason != dataSyncReasonTrafficlessCleanupTimeout {
		t.Fatalf("expected %s, got %s", dataSyncReasonTrafficlessCleanupTimeout, reason)
	}
}

func TestBuildAppRestoreSpecPropagatesInstanceOperationTimeout(t *testing.T) {
	reconciler := &DataSyncReconciler{}
	ds := newTrafficlessCleanupDataSync()
	instance := &disasterv1.DisasterInstance{
		Spec: disasterv1.DisasterInstanceSpec{
			Namespaces:              []string{"workload"},
			OperationTimeoutMinutes: 180,
		},
	}
	config := &disasterv1.DisasterConfig{Spec: disasterv1.DisasterConfigSpec{SourceCluster: "source", TargetCluster: "target"}}

	spec, _, err := reconciler.buildAppRestoreSpec(context.Background(), ds, config, instance, "backup-001")
	if err != nil {
		t.Fatalf("build AppRestore spec: %v", err)
	}
	if spec.Timeout == nil || spec.Timeout.Duration != 180*time.Minute {
		t.Fatalf("expected AppRestore timeout 180m, got %#v", spec.Timeout)
	}
	labels := dataSyncTrafficlessLabels(ds, dataSyncRestoreName(ds, "backup-001"))
	if labels[metadata.LabelTrafficlessRun] == "" || labels[metadata.LabelCleanupOwnerToken] == "" {
		t.Fatalf("expected owner/run lifecycle labels, got %#v", labels)
	}
}

func TestVerifyDataSyncPodVolumeRestoresRejectsFailedAndStalledPVR(t *testing.T) {
	ctx := context.Background()
	scheme := newDataSyncTrafficlessLifecycleScheme(t)
	instance, config := newTrafficlessCleanupInstance()
	appRestore := &disasterv1.AppRestore{
		ObjectMeta: metav1.ObjectMeta{Name: "restore-a"},
		Spec:       disasterv1.AppRestoreSpec{Timeout: &metav1.Duration{Duration: time.Hour}},
	}
	cases := []struct {
		name  string
		phase velerov1.PodVolumeRestorePhase
		want  string
	}{
		{name: "failed", phase: velerov1.PodVolumeRestorePhaseFailed, want: "PodVolumeRestoreFailed"},
		{name: "in-progress", phase: velerov1.PodVolumeRestorePhaseInProgress, want: "PodVolumeRestoreStalled"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pvr := &velerov1.PodVolumeRestore{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "pvr-" + tc.name,
					Namespace:         ctrlcommon.VeleroNamespace,
					CreationTimestamp: metav1.NewTime(time.Now().Add(-trafficlessCleanupTimeout() - time.Minute)),
					Labels:            map[string]string{velerov1.RestoreNameLabel: "res-" + appRestore.Name},
				},
				Status: velerov1.PodVolumeRestoreStatus{Phase: tc.phase, Message: "volume restore error"},
			}
			target := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pvr).Build()
			reconciler := &DataSyncReconciler{
				Client:              fake.NewClientBuilder().WithScheme(scheme).Build(),
				Scheme:              scheme,
				TargetClientFactory: &ctrlcommon.MockClientFactory{MockClient: target},
			}
			_, err := reconciler.verifyDataSyncPodVolumeRestores(ctx, config, instance, appRestore)
			if err == nil {
				t.Fatalf("expected %s PVR to fail the success gate", tc.name)
			}
			reason, _ := trafficlessLifecycleErrorDetails(err, "")
			if reason != tc.want {
				t.Fatalf("expected %s, got %s", tc.want, reason)
			}
		})
	}
}
