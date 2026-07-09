package apprestore

import (
	"context"
	"testing"
	"time"

	controller "github.com/softcdata/testudo-operator/internal/controller"
	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newVeleroRestartTestClient(t *testing.T, objs ...ctrlclient.Object) ctrlclient.Client {
	t.Helper()

	scheme := runtime.NewScheme()
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add appsv1 scheme: %v", err)
	}
	if err := velerov1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add velero scheme: %v", err)
	}
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
}

func TestRestartVeleroDeployment_UpdatesRestartAnnotation(t *testing.T) {
	origCooldown := veleroRestartCooldown
	veleroRestartCooldown = 2 * time.Minute
	t.Cleanup(func() { veleroRestartCooldown = origCooldown })

	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      veleroDeploymentDefaultName,
			Namespace: controller.VeleroNamespace,
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "velero"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "velero"},
				},
			},
		},
	}
	cli := newVeleroRestartTestClient(t, deploy)

	restarted, err := restartVeleroDeployment(context.Background(), cli)
	if err != nil {
		t.Fatalf("restartVeleroDeployment returned error: %v", err)
	}
	if !restarted {
		t.Fatalf("expected restarted=true")
	}

	updated := &appsv1.Deployment{}
	if err := cli.Get(context.Background(), types.NamespacedName{Name: veleroDeploymentDefaultName, Namespace: controller.VeleroNamespace}, updated); err != nil {
		t.Fatalf("failed to get updated deployment: %v", err)
	}
	got := updated.Spec.Template.Annotations[veleroRestartAtAnnotationKey]
	if got == "" {
		t.Fatalf("expected %s annotation to be set", veleroRestartAtAnnotationKey)
	}
	if _, err := time.Parse(time.RFC3339, got); err != nil {
		t.Fatalf("expected restart annotation RFC3339 timestamp, got %q: %v", got, err)
	}
}

func TestRestartVeleroDeployment_CooldownSkips(t *testing.T) {
	origCooldown := veleroRestartCooldown
	veleroRestartCooldown = 10 * time.Minute
	t.Cleanup(func() { veleroRestartCooldown = origCooldown })

	restartedAt := time.Now().UTC().Add(-1 * time.Minute).Format(time.RFC3339)
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      veleroDeploymentDefaultName,
			Namespace: controller.VeleroNamespace,
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "velero"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      map[string]string{"app": "velero"},
					Annotations: map[string]string{veleroRestartAtAnnotationKey: restartedAt},
				},
			},
		},
	}
	cli := newVeleroRestartTestClient(t, deploy)

	restarted, err := restartVeleroDeployment(context.Background(), cli)
	if err != nil {
		t.Fatalf("restartVeleroDeployment returned error: %v", err)
	}
	if restarted {
		t.Fatalf("expected restarted=false during cooldown")
	}

	updated := &appsv1.Deployment{}
	if err := cli.Get(context.Background(), types.NamespacedName{Name: veleroDeploymentDefaultName, Namespace: controller.VeleroNamespace}, updated); err != nil {
		t.Fatalf("failed to get updated deployment: %v", err)
	}
	if got := updated.Spec.Template.Annotations[veleroRestartAtAnnotationKey]; got != restartedAt {
		t.Fatalf("expected restart annotation unchanged, got=%q want=%q", got, restartedAt)
	}
}

func TestHasRunningVeleroOperations_DetectsRunningPVR(t *testing.T) {
	pvr := &velerov1.PodVolumeRestore{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pvr-running",
			Namespace: controller.VeleroNamespace,
		},
		Status: velerov1.PodVolumeRestoreStatus{
			Phase: velerov1.PodVolumeRestorePhaseInProgress,
		},
	}
	cli := newVeleroRestartTestClient(t, pvr)

	running, err := hasRunningVeleroOperations(context.Background(), cli)
	if err != nil {
		t.Fatalf("hasRunningVeleroOperations returned error: %v", err)
	}
	if !running {
		t.Fatalf("expected running operation to be detected")
	}
}

func TestHasRunningVeleroOperations_CompletedObjectsAreNotRunning(t *testing.T) {
	backup := &velerov1.Backup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "backup-complete",
			Namespace: controller.VeleroNamespace,
		},
		Status: velerov1.BackupStatus{Phase: velerov1.BackupPhaseCompleted},
	}
	restore := &velerov1.Restore{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "restore-complete",
			Namespace: controller.VeleroNamespace,
		},
		Status: velerov1.RestoreStatus{Phase: velerov1.RestorePhaseCompleted},
	}
	pvb := &velerov1.PodVolumeBackup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pvb-complete",
			Namespace: controller.VeleroNamespace,
		},
		Status: velerov1.PodVolumeBackupStatus{Phase: velerov1.PodVolumeBackupPhaseCompleted},
	}
	pvr := &velerov1.PodVolumeRestore{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pvr-complete",
			Namespace: controller.VeleroNamespace,
		},
		Status: velerov1.PodVolumeRestoreStatus{Phase: velerov1.PodVolumeRestorePhaseCompleted},
	}
	cli := newVeleroRestartTestClient(t, backup, restore, pvb, pvr)

	running, err := hasRunningVeleroOperations(context.Background(), cli)
	if err != nil {
		t.Fatalf("hasRunningVeleroOperations returned error: %v", err)
	}
	if running {
		t.Fatalf("expected completed operations not to be considered running")
	}
}

func TestTryRestartVeleroAfterStall_SkipsStartupTransient(t *testing.T) {
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      veleroDeploymentDefaultName,
			Namespace: controller.VeleroNamespace,
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "velero"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "velero"},
				},
			},
		},
	}
	cli := newVeleroRestartTestClient(t, deploy)
	reconciler := &AppRestoreReconciler{Recorder: record.NewFakeRecorder(10)}
	appRestore := &disasterv1.AppRestore{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "apprestore-startup-transient",
			Namespace: "disaster-system",
		},
	}

	reconciler.tryRestartVeleroAfterStall(context.Background(), cli, appRestore, restoreStallTypeStartupTransient, "restore-a")

	updated := &appsv1.Deployment{}
	if err := cli.Get(context.Background(), types.NamespacedName{Name: veleroDeploymentDefaultName, Namespace: controller.VeleroNamespace}, updated); err != nil {
		t.Fatalf("failed to get deployment: %v", err)
	}
	if got := updated.Spec.Template.Annotations[veleroRestartAtAnnotationKey]; got != "" {
		t.Fatalf("expected velero restart annotation to stay empty, got %q", got)
	}
}

func TestTryRestartVeleroAfterStall_SkipsWhenOperationsRunning(t *testing.T) {
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      veleroDeploymentDefaultName,
			Namespace: controller.VeleroNamespace,
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "velero"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "velero"},
				},
			},
		},
	}
	restore := &velerov1.Restore{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "other-restore",
			Namespace: controller.VeleroNamespace,
		},
		Status: velerov1.RestoreStatus{Phase: velerov1.RestorePhaseInProgress},
	}
	cli := newVeleroRestartTestClient(t, deploy, restore)
	reconciler := &AppRestoreReconciler{Recorder: record.NewFakeRecorder(10)}
	appRestore := &disasterv1.AppRestore{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "apprestore-running-ops",
			Namespace: "disaster-system",
		},
	}

	reconciler.tryRestartVeleroAfterStall(context.Background(), cli, appRestore, restoreStallTypeEmptyStatus, "restore-a")

	updated := &appsv1.Deployment{}
	if err := cli.Get(context.Background(), types.NamespacedName{Name: veleroDeploymentDefaultName, Namespace: controller.VeleroNamespace}, updated); err != nil {
		t.Fatalf("failed to get deployment: %v", err)
	}
	if got := updated.Spec.Template.Annotations[veleroRestartAtAnnotationKey]; got != "" {
		t.Fatalf("expected velero restart annotation to stay empty, got %q", got)
	}
}
