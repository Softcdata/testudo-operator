package datasync

import (
	"context"
	"reflect"
	"testing"
	"time"

	ctrlcommon "github.com/softcdata/testudo-operator/internal/controller"
	"github.com/softcdata/testudo-operator/internal/controller/scheduler"
	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestBuildAppBackupSpec_ProjectsDataBackupHooks(t *testing.T) {
	r := &DataSyncReconciler{}
	hooks := sampleBackupHooks()
	instance := &disasterv1.DisasterInstance{
		Spec: disasterv1.DisasterInstanceSpec{
			Namespaces: []string{"app"},
			VeleroHooks: &disasterv1.DisasterVeleroHooks{
				DataBackup: hooks,
			},
		},
	}
	config := &disasterv1.DisasterConfig{
		Spec: disasterv1.DisasterConfigSpec{
			SourceCluster:     "cluster-a",
			TargetCluster:     "cluster-b",
			StorageRepository: "repo-main",
		},
	}

	spec := r.buildAppBackupSpec(instance, config)
	if !reflect.DeepEqual(spec.Template.Hooks, *hooks) {
		t.Fatalf("expected data backup hooks to be projected, got %#v", spec.Template.Hooks)
	}
}

func TestBuildAppRestoreSpec_ProjectsDataRestoreHooks(t *testing.T) {
	r := &DataSyncReconciler{}
	hooks := sampleRestoreHooks()
	ds := &disasterv1.DataSync{ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "default"}}
	instance := &disasterv1.DisasterInstance{
		Spec: disasterv1.DisasterInstanceSpec{
			Namespaces: []string{"app"},
			VeleroHooks: &disasterv1.DisasterVeleroHooks{
				DataRestore: hooks,
			},
		},
	}
	config := &disasterv1.DisasterConfig{
		Spec: disasterv1.DisasterConfigSpec{
			SourceCluster:     "cluster-a",
			TargetCluster:     "cluster-b",
			StorageRepository: "repo-main",
		},
	}

	spec, _, err := r.buildAppRestoreSpec(context.Background(), ds, config, instance, "backup-001")
	if err != nil {
		t.Fatalf("buildAppRestoreSpec returned error: %v", err)
	}
	if !reflect.DeepEqual(spec.Template.Hooks, *hooks) {
		t.Fatalf("expected data restore hooks to be projected, got %#v", spec.Template.Hooks)
	}
}

func TestReconcile_AlignsExistingAppBackupHooksBeforeNextBackup(t *testing.T) {
	ctx := context.Background()
	s := runtime.NewScheme()
	if err := scheme.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := disasterv1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}

	hooks := sampleBackupHooks()
	ds := &disasterv1.DataSync{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "demo",
			Namespace:  "default",
			Finalizers: []string{dataSyncFinalizer},
		},
		Spec: disasterv1.DataSyncSpec{
			Instance: "instance-a",
		},
		Status: disasterv1.DataSyncStatus{
			State:        disasterv1.DataSyncStateInProgress,
			LastSyncTime: &metav1.Time{Time: time.Now().Add(-time.Hour)},
		},
	}
	instance := &disasterv1.DisasterInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "instance-a", Namespace: "default"},
		Spec: disasterv1.DisasterInstanceSpec{
			Config:     "config-a",
			Namespaces: []string{"app"},
			VeleroHooks: &disasterv1.DisasterVeleroHooks{
				DataBackup: hooks,
			},
		},
	}
	config := &disasterv1.DisasterConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "config-a", Namespace: "default"},
		Spec: disasterv1.DisasterConfigSpec{
			SourceCluster:     "cluster-a",
			TargetCluster:     "cluster-b",
			StorageRepository: "repo-main",
		},
	}
	storage := &disasterv1.StorageRepository{
		ObjectMeta: metav1.ObjectMeta{Name: "repo-main", Namespace: ctrlcommon.ManagementNamespace()},
		Status: disasterv1.StorageRepositoryStatus{
			Status: disasterv1.StorageRepositoryStatusAvailable,
		},
	}
	existingBackup := &disasterv1.AppBackup{
		ObjectMeta: metav1.ObjectMeta{Name: "ds-demo", Namespace: "default"},
		Spec: disasterv1.AppBackupSpec{
			Cluster: "cluster-a",
			Template: velerov1.BackupSpec{
				IncludedNamespaces: []string{"app"},
				StorageLocation:    "repo-main",
			},
		},
	}

	c := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(ds, instance, config, storage, existingBackup).
		WithStatusSubresource(ds).
		Build()
	scheduler, err := scheduler.NewSyncScheduler()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = scheduler.Shutdown() }()
	r := &DataSyncReconciler{
		Client:    c,
		Scheme:    s,
		Log:       ctrl.Log.WithName("test"),
		Recorder:  record.NewFakeRecorder(16),
		Scheduler: scheduler,
	}

	_, err = r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "demo", Namespace: "default"}})
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	updated := &disasterv1.AppBackup{}
	if err := c.Get(ctx, types.NamespacedName{Name: "ds-demo", Namespace: "default"}, updated); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(updated.Spec.Template.Hooks, *hooks) {
		t.Fatalf("expected existing AppBackup hooks to be aligned, got %#v", updated.Spec.Template.Hooks)
	}
	if updated.Spec.Action != nil {
		t.Fatalf("expected reconcile to align template before triggering backup action")
	}
}

func TestSyncHistoryHookStatus(t *testing.T) {
	got := syncHistoryHookStatus(&velerov1.HookStatus{HooksAttempted: 3, HooksFailed: 1})
	if got == nil || got.HooksAttempted != 3 || got.HooksFailed != 1 {
		t.Fatalf("unexpected hook status: %#v", got)
	}
	if syncHistoryHookStatus(nil) != nil {
		t.Fatalf("expected nil input to return nil")
	}
}

func sampleBackupHooks() *velerov1.BackupHooks {
	return &velerov1.BackupHooks{
		Resources: []velerov1.BackupResourceHookSpec{
			{
				Name:              "quiesce",
				IncludedResources: []string{"pods"},
				PreHooks: []velerov1.BackupResourceHook{
					{
						Exec: &velerov1.ExecHook{
							Command: []string{"/usr/local/bin/dr-hook", "pre-backup", "--mode", "quiesce"},
							Timeout: metav1.Duration{Duration: 30 * time.Second},
						},
					},
				},
			},
		},
	}
}

func sampleRestoreHooks() *velerov1.RestoreHooks {
	return &velerov1.RestoreHooks{
		Resources: []velerov1.RestoreResourceHookSpec{
			{
				Name:              "after-restore",
				IncludedResources: []string{"pods"},
				PostHooks: []velerov1.RestoreResourceHook{
					{
						Exec: &velerov1.ExecRestoreHook{
							Command:     []string{"/usr/local/bin/dr-hook", "after-restore"},
							ExecTimeout: metav1.Duration{Duration: time.Minute},
						},
					},
				},
			},
		},
	}
}
