package datasync

import (
	"context"
	"crypto/md5"
	"fmt"
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

func TestBackupStartedForDataSyncRunUsesLastSyncBoundary(t *testing.T) {
	lastSync := &metav1.Time{Time: time.Date(2026, 5, 14, 1, 38, 40, 0, time.UTC)}
	trigger := time.Date(2026, 5, 14, 2, 0, 0, 0, time.UTC)

	if !backupStartedForDataSyncRun(&metav1.Time{Time: trigger.Add(-time.Second)}, lastSync) {
		t.Fatalf("expected backup after last sync to match even if it starts before trigger")
	}
	if backupStartedForDataSyncRun(&metav1.Time{Time: lastSync.Time}, lastSync) {
		t.Fatalf("expected backup at the previous sync boundary to not match")
	}
}

func TestDataSyncCurrentActionMatchesBackupNameDespiteClockSkew(t *testing.T) {
	actionAt := time.Date(2026, 5, 14, 2, 20, 0, 0, time.UTC)
	lastSync := &metav1.Time{Time: actionAt.Add(-time.Hour)}
	appBackupName := "ds-dr-ds-app-instances-config-1778722633339"
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

func TestHandleRestore_SetHistoryOnBuildRestoreSpecFailed(t *testing.T) {
	s := runtime.NewScheme()
	if err := scheme.AddToScheme(s); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	if err := disasterv1.AddToScheme(s); err != nil {
		t.Fatalf("add disaster scheme: %v", err)
	}

	ds := &disasterv1.DataSync{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ds-test",
			Namespace: "default",
		},
		Status: disasterv1.DataSyncStatus{
			State: disasterv1.DataSyncStateInProgress,
		},
	}
	backup := &disasterv1.AppBackup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ds-ds-test",
			Namespace: "default",
		},
		Status: disasterv1.AppBackupStatus{
			History: []disasterv1.BackupRecord{
				{
					Name: "backup-1",
					VeleroStatus: &velerov1.BackupStatus{
						Progress: &velerov1.BackupProgress{
							ItemsBackedUp: 9,
						},
					},
					StartTimestamp: &metav1.Time{Time: time.Now().Add(-3 * time.Minute)},
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
			RestorePolicy: &disasterv1.RestorePolicy{
				UseUnifiedDirectionResolver: boolPtr(false),
				ModifierRules: []disasterv1.RestoreModifierRule{
					{
						ID:      "rule-ds-test",
						Mode:    disasterv1.RestoreModifierModeVeleroNative,
						ApplyTo: []disasterv1.RestoreModifierApplyTarget{disasterv1.RestoreModifierApplyDataSync},
						Conditions: disasterv1.Conditions{
							GroupResource: "deployments.apps",
							Namespaces:    []string{"app"},
						},
						VeleroRule: &disasterv1.RestoreModifierVeleroRule{
							Patches: []disasterv1.JSONPatch{
								{
									Operation: "add",
									Path:      "/metadata/annotations/patched-by",
									Value:     "test",
								},
							},
						},
					},
				},
			},
		},
	}
	cfg := &disasterv1.DisasterConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "cfg-test"},
		Spec: disasterv1.DisasterConfigSpec{
			SourceCluster: "src-test",
			TargetCluster: "dst-test",
		},
	}
	sourceCluster := &disasterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "src-test"},
		Spec: disasterv1.ClusterSpec{
			Endpoint: "https://127.0.0.1:65530",
			Token:    "dummy-token",
		},
	}
	targetCluster := &disasterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "dst-test"},
		Spec: disasterv1.ClusterSpec{
			Endpoint: "https://127.0.0.1:65531",
			Token:    "dummy-token",
		},
	}

	c := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(ds, backup, instance, cfg, sourceCluster, targetCluster).
		WithStatusSubresource(ds, backup).
		Build()

	r := &DataSyncReconciler{
		Client:   c,
		Scheme:   s,
		Recorder: record.NewFakeRecorder(10),
	}

	if _, err := r.handleRestore(context.Background(), logr.Discard(), ds, cfg, instance, "backup-1"); err != nil {
		t.Fatalf("handleRestore returned error: %v", err)
	}

	updated := &disasterv1.DataSync{}
	if err := c.Get(context.Background(), types.NamespacedName{Name: "ds-test", Namespace: "default"}, updated); err != nil {
		t.Fatalf("get updated datasync: %v", err)
	}
	if updated.Status.State != disasterv1.DataSyncStateFailed {
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
	if record.BackupResourceCount != 9 {
		t.Fatalf("expected backup resource count 9, got %d", record.BackupResourceCount)
	}
}

func TestHandleRestore_TreatsPartiallyFailedAppRestoreAsFailed(t *testing.T) {
	s := runtime.NewScheme()
	if err := scheme.AddToScheme(s); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	if err := disasterv1.AddToScheme(s); err != nil {
		t.Fatalf("add disaster scheme: %v", err)
	}

	backupName := "backup-partial"
	backupHash := fmt.Sprintf("%x", md5.Sum([]byte(backupName)))[:6]
	restoreName := fmt.Sprintf("rec-ds-%s-%s", "ds-test", backupHash)
	start := metav1.NewTime(time.Now().Add(-4 * time.Minute))

	ds := &disasterv1.DataSync{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ds-test",
			Namespace: "default",
		},
		Status: disasterv1.DataSyncStatus{
			State: disasterv1.DataSyncStateInProgress,
		},
	}
	backup := &disasterv1.AppBackup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ds-ds-test",
			Namespace: "default",
		},
		Status: disasterv1.AppBackupStatus{
			History: []disasterv1.BackupRecord{{
				Name: backupName,
				VeleroStatus: &velerov1.BackupStatus{
					Progress: &velerov1.BackupProgress{ItemsBackedUp: 11},
				},
				StartTimestamp: &start,
			}},
		},
	}
	restore := &disasterv1.AppRestore{
		ObjectMeta: metav1.ObjectMeta{
			Name:      restoreName,
			Namespace: "default",
		},
		Status: disasterv1.AppRestoreStatus{
			Status: disasterv1.PhasePartiallyFailed,
			RestoreStatus: velerov1.RestoreStatus{
				Phase:    velerov1.RestorePhasePartiallyFailed,
				Progress: &velerov1.RestoreProgress{ItemsRestored: 5},
			},
			Reason:  "RestorePartiallyFailed",
			Message: "恢复部分失败: errors=1 warnings=0",
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
			SourceCluster:     "src-test",
			TargetCluster:     "dst-test",
			StorageRepository: "repo-test",
		},
	}

	c := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(ds, backup, restore, instance, cfg).
		WithStatusSubresource(ds, backup, restore).
		Build()

	r := &DataSyncReconciler{
		Client:   c,
		Scheme:   s,
		Recorder: record.NewFakeRecorder(10),
	}

	if _, err := r.handleRestore(context.Background(), logr.Discard(), ds, cfg, instance, backupName); err != nil {
		t.Fatalf("handleRestore returned error: %v", err)
	}

	updated := &disasterv1.DataSync{}
	if err := c.Get(context.Background(), types.NamespacedName{Name: "ds-test", Namespace: "default"}, updated); err != nil {
		t.Fatalf("get updated datasync: %v", err)
	}
	if updated.Status.State != disasterv1.DataSyncStateFailed {
		t.Fatalf("expected state Failed, got %s", updated.Status.State)
	}
	if updated.Status.Reason != dataSyncReasonRestoreFailed {
		t.Fatalf("expected reason %s, got %s", dataSyncReasonRestoreFailed, updated.Status.Reason)
	}
	if len(updated.Status.History) != 1 {
		t.Fatalf("expected one history record, got %d", len(updated.Status.History))
	}
	record := updated.Status.History[0]
	if record.Status != string(disasterv1.PhasePartiallyFailed) {
		t.Fatalf("expected history status PartiallyFailed, got %s", record.Status)
	}
	if record.BackupResourceCount != 11 || record.RestoreResourceCount != 5 {
		t.Fatalf("unexpected history resource counts: backup=%d restore=%d", record.BackupResourceCount, record.RestoreResourceCount)
	}
}

func boolPtr(v bool) *bool { return &v }
