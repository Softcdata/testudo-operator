package resourcesync

import (
	"context"
	"testing"
	"time"

	"github.com/go-logr/logr"
	ctrlcommon "github.com/softcdata/testudo-operator/internal/controller"
	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestBackupStartedForResourceSyncRunUsesLastSyncBoundary(t *testing.T) {
	lastSync := &metav1.Time{Time: time.Date(2026, 5, 14, 1, 38, 40, 0, time.UTC)}
	trigger := time.Date(2026, 5, 14, 2, 0, 0, 0, time.UTC)

	if !backupStartedForResourceSyncRun(&metav1.Time{Time: trigger.Add(-time.Second)}, lastSync) {
		t.Fatalf("expected backup after last sync to match even if it starts before trigger")
	}
	if backupStartedForResourceSyncRun(&metav1.Time{Time: lastSync.Time}, lastSync) {
		t.Fatalf("expected backup at the previous sync boundary to not match")
	}
}

func TestResourceSyncCurrentActionMatchesBackupNameDespiteClockSkew(t *testing.T) {
	actionAt := time.Date(2026, 5, 14, 2, 20, 0, 0, time.UTC)
	lastSync := &metav1.Time{Time: actionAt.Add(-time.Hour)}
	appBackupName := "rs-dr-rs-app-instances-config-1778722633339"
	backupName := ctrlcommon.GenVeleroBackupName(appBackupName, actionAt)

	appBackup := &disasterv1.AppBackup{
		Spec: disasterv1.AppBackupSpec{
			Action: &disasterv1.BackupAction{
				Type:      "Backup",
				RequestAt: metav1.NewTime(actionAt),
			},
		},
		Status: disasterv1.AppBackupStatus{
			History: []disasterv1.BackupRecord{{
				Name:           backupName,
				StartTimestamp: &metav1.Time{Time: actionAt.Add(-time.Second)},
			}},
		},
	}

	expected, ok := ctrlcommon.CurrentBackupActionVeleroBackupName(appBackupName, appBackup, lastSync)
	if !ok {
		t.Fatalf("expected current backup action to produce a backup name")
	}
	rec, found := ctrlcommon.FindBackupRecordByName(appBackup.Status.History, expected)
	if !found {
		t.Fatalf("expected backup record to match by generated name")
	}
	if !rec.StartTimestamp.Time.Before(actionAt) {
		t.Fatalf("test setup expected skewed start timestamp before action time")
	}
}

func TestHandleRestore_SetLastSyncTimeOnBuildRestoreSpecFailed(t *testing.T) {
	s := runtime.NewScheme()
	if err := scheme.AddToScheme(s); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	if err := disasterv1.AddToScheme(s); err != nil {
		t.Fatalf("add disaster scheme: %v", err)
	}

	rs := &disasterv1.ResourceSync{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rs-test",
			Namespace: "default",
		},
		Status: disasterv1.ResourceSyncStatus{
			State: disasterv1.ResourceSyncStateInProgress,
		},
	}
	backup := &disasterv1.AppBackup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rs-rs-test",
			Namespace: "default",
		},
		Status: disasterv1.AppBackupStatus{
			History: []disasterv1.BackupRecord{
				{
					Name: "backup-1",
					VeleroStatus: &velerov1.BackupStatus{
						Progress: &velerov1.BackupProgress{
							ItemsBackedUp: 7,
						},
					},
					StartTimestamp: &metav1.Time{Time: time.Now().Add(-2 * time.Minute)},
				},
			},
		},
	}
	instance := &disasterv1.DisasterInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "inst-test",
			Namespace: "default",
		},
		Spec: disasterv1.DisasterInstanceSpec{
			Namespaces: []string{"app"},
		},
	}
	cfg := &disasterv1.DisasterConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "cfg-test"},
		Spec: disasterv1.DisasterConfigSpec{
			SourceCluster: "not-exist-src",
			TargetCluster: "not-exist-dst",
		},
	}

	c := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(rs, backup, instance, cfg).
		WithStatusSubresource(rs, backup).
		Build()

	r := &ResourceSyncReconciler{
		Client:   c,
		Scheme:   s,
		Recorder: record.NewFakeRecorder(10),
	}

	if _, err := r.handleRestore(context.Background(), logr.Discard(), rs, cfg, instance, "backup-1"); err != nil {
		t.Fatalf("handleRestore returned error: %v", err)
	}

	updated := &disasterv1.ResourceSync{}
	if err := c.Get(context.Background(), types.NamespacedName{Name: "rs-test", Namespace: "default"}, updated); err != nil {
		t.Fatalf("get updated resourcesync: %v", err)
	}
	if updated.Status.State != disasterv1.ResourceSyncStateFailed {
		t.Fatalf("expected state Failed, got %s", updated.Status.State)
	}
	if updated.Status.LastSyncTime == nil {
		t.Fatalf("expected LastSyncTime to be set on BuildRestoreSpecFailed")
	}
	if len(updated.Status.Conditions) == 0 {
		t.Fatalf("expected failure condition recorded")
	}
	if len(updated.Status.History) != 1 {
		t.Fatalf("expected one failure history record, got %d", len(updated.Status.History))
	}
	record := updated.Status.History[0]
	if record.Status != string(disasterv1.PhaseFailed) {
		t.Fatalf("expected history status Failed, got %s", record.Status)
	}
	if record.BackupName != "backup-1" {
		t.Fatalf("expected backup name backup-1, got %s", record.BackupName)
	}
	if record.BackupResourceCount != 7 {
		t.Fatalf("expected backup resource count 7, got %d", record.BackupResourceCount)
	}
}
