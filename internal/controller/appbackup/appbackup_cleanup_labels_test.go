package appbackup

import (
	"context"
	"testing"
	"time"

	"github.com/softcdata/testudo-operator/internal/controller"
	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	. "github.com/softcdata/testudo-operator/pkg/metadata"
	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newAppBackupTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	if err := disasterv1.AddToScheme(scheme); err != nil {
		t.Fatalf("add disaster scheme: %v", err)
	}
	if err := velerov1.AddToScheme(scheme); err != nil {
		t.Fatalf("add velero scheme: %v", err)
	}
	return scheme
}

func TestCreateVeleroScheduleUpdatesExistingScheduleAndTTL(t *testing.T) {
	ctx := context.Background()
	scheme := newAppBackupTestScheme(t)
	reconciler := &AppBackupReconciler{
		Scheme:   scheme,
		Recorder: record.NewFakeRecorder(10),
	}
	appBackup := &disasterv1.AppBackup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "schedule-update-test",
			Namespace: "default",
			UID:       types.UID("appbackup-schedule-update-uid"),
		},
		Spec: disasterv1.AppBackupSpec{
			Cluster:  "cluster-a",
			Schedule: "0 0 * * *",
			Template: velerov1.BackupSpec{
				IncludedNamespaces: []string{"demo-ns"},
				TTL:                metav1.Duration{Duration: 24 * time.Hour},
			},
		},
	}
	cli := fake.NewClientBuilder().WithScheme(scheme).Build()

	schedule, created, err := reconciler.CreateVeleroSchedule(ctx, cli, appBackup, "repo-a")
	if err != nil {
		t.Fatalf("CreateVeleroSchedule returned error: %v", err)
	}
	if !created {
		t.Fatalf("expected initial schedule to be created")
	}

	appBackup.Spec.Schedule = "0 2 * * *"
	appBackup.Spec.Template.TTL = metav1.Duration{Duration: 72 * time.Hour}
	appBackup.Spec.Paused = true

	_, created, err = reconciler.CreateVeleroSchedule(ctx, cli, appBackup, "repo-a")
	if err != nil {
		t.Fatalf("CreateVeleroSchedule update returned error: %v", err)
	}
	if created {
		t.Fatalf("expected existing schedule to be updated, not created")
	}

	stored := &velerov1.Schedule{}
	if err := cli.Get(ctx, ctrlclient.ObjectKey{Name: schedule.Name, Namespace: controller.VeleroNamespace}, stored); err != nil {
		t.Fatalf("get updated schedule: %v", err)
	}
	if stored.Spec.Schedule != "CRON_TZ=Asia/Shanghai 0 2 * * *" {
		t.Fatalf("expected schedule to be updated, got %q", stored.Spec.Schedule)
	}
	if stored.Spec.Template.TTL.Duration != 72*time.Hour {
		t.Fatalf("expected ttl to be updated, got %s", stored.Spec.Template.TTL.Duration)
	}
	if !stored.Spec.Paused {
		t.Fatalf("expected schedule to be paused")
	}
}

func TestCreateVeleroBackupWritesCleanupLabels(t *testing.T) {
	ctx := context.Background()
	scheme := newAppBackupTestScheme(t)
	cli := fake.NewClientBuilder().WithScheme(scheme).Build()
	reconciler := &AppBackupReconciler{
		Scheme:   scheme,
		Recorder: record.NewFakeRecorder(10),
	}

	appBackup := &disasterv1.AppBackup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "backup-cleanup-test",
			Namespace: "default",
			UID:       types.UID("appbackup-cleanup-uid"),
			Labels: map[string]string{
				LabelAppBackupType: "Manual",
			},
		},
		Spec: disasterv1.AppBackupSpec{
			Cluster: "cluster-a",
			Template: velerov1.BackupSpec{
				IncludedNamespaces: []string{"demo-ns"},
			},
		},
	}

	backup, created, err := reconciler.CreateVeleroBackup(ctx, cli, appBackup, "repo-a", "")
	if err != nil {
		t.Fatalf("CreateVeleroBackup returned error: %v", err)
	}
	if !created {
		t.Fatalf("expected backup to be created")
	}

	stored := &velerov1.Backup{}
	if err := cli.Get(ctx, ctrlclient.ObjectKey{Name: backup.Name, Namespace: controller.VeleroNamespace}, stored); err != nil {
		t.Fatalf("get created backup: %v", err)
	}

	ownerToken := BuildDependencyToken(string(appBackup.UID))
	if got := stored.Labels[LabelCleanupOwnerToken]; got != ownerToken {
		t.Fatalf("unexpected cleanup owner token: got %q want %q", got, ownerToken)
	}
	if got := stored.Labels[LabelCleanupRelation]; got != "finalizer.veleroBackup" {
		t.Fatalf("unexpected cleanup relation: %q", got)
	}
	if got := stored.Labels[LabelCleanupStrategy]; got != CleanupStrategyDeleteRequest {
		t.Fatalf("unexpected cleanup strategy: %q", got)
	}
	if got := stored.Labels[LabelCleanupManagedBy]; got != LabelCleanupManagedByValueOperator {
		t.Fatalf("unexpected cleanup managed-by: %q", got)
	}
}

func TestCreateVeleroScheduleWritesCleanupLabels(t *testing.T) {
	ctx := context.Background()
	scheme := newAppBackupTestScheme(t)
	cli := fake.NewClientBuilder().WithScheme(scheme).Build()
	reconciler := &AppBackupReconciler{
		Scheme:   scheme,
		Recorder: record.NewFakeRecorder(10),
	}

	appBackup := &disasterv1.AppBackup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "schedule-cleanup-test",
			Namespace: "default",
			UID:       types.UID("appbackup-schedule-uid"),
			Labels: map[string]string{
				LabelAppBackupType: "Schedule",
			},
		},
		Spec: disasterv1.AppBackupSpec{
			Cluster:  "cluster-a",
			Schedule: "0 0 * * *",
			Template: velerov1.BackupSpec{
				IncludedNamespaces: []string{"demo-ns"},
			},
		},
	}

	schedule, created, err := reconciler.CreateVeleroSchedule(ctx, cli, appBackup, "repo-a")
	if err != nil {
		t.Fatalf("CreateVeleroSchedule returned error: %v", err)
	}
	if !created {
		t.Fatalf("expected schedule to be created")
	}

	stored := &velerov1.Schedule{}
	if err := cli.Get(ctx, ctrlclient.ObjectKey{Name: schedule.Name, Namespace: controller.VeleroNamespace}, stored); err != nil {
		t.Fatalf("get created schedule: %v", err)
	}

	ownerToken := BuildDependencyToken(string(appBackup.UID))
	if got := stored.Labels[LabelCleanupOwnerToken]; got != ownerToken {
		t.Fatalf("unexpected schedule cleanup owner token: got %q want %q", got, ownerToken)
	}
	if got := stored.Labels[LabelCleanupRelation]; got != "finalizer.veleroSchedule" {
		t.Fatalf("unexpected schedule cleanup relation: %q", got)
	}
	if got := stored.Labels[LabelCleanupStrategy]; got != CleanupStrategyDelete {
		t.Fatalf("unexpected schedule cleanup strategy: %q", got)
	}

	if got := stored.Spec.Template.Metadata.Labels[LabelCleanupOwnerToken]; got != ownerToken {
		t.Fatalf("unexpected template cleanup owner token: got %q want %q", got, ownerToken)
	}
	if got := stored.Spec.Template.Metadata.Labels[LabelCleanupRelation]; got != "finalizer.veleroBackup" {
		t.Fatalf("unexpected template cleanup relation: %q", got)
	}
	if got := stored.Spec.Template.Metadata.Labels[LabelCleanupStrategy]; got != CleanupStrategyDeleteRequest {
		t.Fatalf("unexpected template cleanup strategy: %q", got)
	}
}

func TestUpdateVeleroBackupBackfillsCleanupLabels(t *testing.T) {
	ctx := context.Background()
	scheme := newAppBackupTestScheme(t)
	cli := fake.NewClientBuilder().WithScheme(scheme).Build()
	reconciler := &AppBackupReconciler{
		Scheme:   scheme,
		Recorder: record.NewFakeRecorder(10),
	}

	appBackup := &disasterv1.AppBackup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "backup-cleanup-backfill-test",
			Namespace: "default",
			UID:       types.UID("appbackup-cleanup-backfill-uid"),
			Labels: map[string]string{
				LabelAppBackupType: "Manual",
			},
		},
		Spec: disasterv1.AppBackupSpec{
			Cluster: "cluster-a",
			Template: velerov1.BackupSpec{
				IncludedNamespaces: []string{"demo-ns"},
			},
		},
	}

	backup, created, err := reconciler.CreateVeleroBackup(ctx, cli, appBackup, "repo-a", "")
	if err != nil {
		t.Fatalf("CreateVeleroBackup returned error: %v", err)
	}
	if !created {
		t.Fatalf("expected backup to be created")
	}

	stored := &velerov1.Backup{}
	if err := cli.Get(ctx, ctrlclient.ObjectKey{Name: backup.Name, Namespace: controller.VeleroNamespace}, stored); err != nil {
		t.Fatalf("get created backup: %v", err)
	}

	// Simulate a legacy resource that missed cleanup labels.
	for _, key := range []string{LabelCleanupOwnerToken, LabelCleanupRelation, LabelCleanupStrategy, LabelCleanupManagedBy} {
		delete(stored.Labels, key)
	}
	if err := cli.Update(ctx, stored); err != nil {
		t.Fatalf("update backup to remove labels: %v", err)
	}

	_, created2, err := reconciler.CreateVeleroBackup(ctx, cli, appBackup, "repo-a", "")
	if err != nil {
		t.Fatalf("CreateVeleroBackup (existing) returned error: %v", err)
	}
	if created2 {
		t.Fatalf("expected backup to already exist")
	}

	stored2 := &velerov1.Backup{}
	if err := cli.Get(ctx, ctrlclient.ObjectKey{Name: backup.Name, Namespace: controller.VeleroNamespace}, stored2); err != nil {
		t.Fatalf("get updated backup: %v", err)
	}

	ownerToken := BuildDependencyToken(string(appBackup.UID))
	if got := stored2.Labels[LabelCleanupOwnerToken]; got != ownerToken {
		t.Fatalf("unexpected cleanup owner token: got %q want %q", got, ownerToken)
	}
	if got := stored2.Labels[LabelCleanupRelation]; got != "finalizer.veleroBackup" {
		t.Fatalf("unexpected cleanup relation: %q", got)
	}
	if got := stored2.Labels[LabelCleanupStrategy]; got != CleanupStrategyDeleteRequest {
		t.Fatalf("unexpected cleanup strategy: %q", got)
	}
	if got := stored2.Labels[LabelCleanupManagedBy]; got != LabelCleanupManagedByValueOperator {
		t.Fatalf("unexpected cleanup managed-by: %q", got)
	}
}

func TestUpdateVeleroScheduleBackfillsCleanupLabels(t *testing.T) {
	ctx := context.Background()
	scheme := newAppBackupTestScheme(t)
	cli := fake.NewClientBuilder().WithScheme(scheme).Build()
	reconciler := &AppBackupReconciler{
		Scheme:   scheme,
		Recorder: record.NewFakeRecorder(10),
	}

	appBackup := &disasterv1.AppBackup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "schedule-cleanup-backfill-test",
			Namespace: "default",
			UID:       types.UID("appbackup-schedule-cleanup-backfill-uid"),
			Labels: map[string]string{
				LabelAppBackupType: "Schedule",
			},
		},
		Spec: disasterv1.AppBackupSpec{
			Cluster:  "cluster-a",
			Schedule: "0 0 * * *",
			Template: velerov1.BackupSpec{
				IncludedNamespaces: []string{"demo-ns"},
			},
		},
	}

	schedule, created, err := reconciler.CreateVeleroSchedule(ctx, cli, appBackup, "repo-a")
	if err != nil {
		t.Fatalf("CreateVeleroSchedule returned error: %v", err)
	}
	if !created {
		t.Fatalf("expected schedule to be created")
	}

	stored := &velerov1.Schedule{}
	if err := cli.Get(ctx, ctrlclient.ObjectKey{Name: schedule.Name, Namespace: controller.VeleroNamespace}, stored); err != nil {
		t.Fatalf("get created schedule: %v", err)
	}

	// Simulate legacy resource missing cleanup labels on both schedule and its template.
	for _, key := range []string{LabelCleanupOwnerToken, LabelCleanupRelation, LabelCleanupStrategy, LabelCleanupManagedBy} {
		delete(stored.Labels, key)
		delete(stored.Spec.Template.Metadata.Labels, key)
	}
	if err := cli.Update(ctx, stored); err != nil {
		t.Fatalf("update schedule to remove labels: %v", err)
	}

	_, created2, err := reconciler.CreateVeleroSchedule(ctx, cli, appBackup, "repo-a")
	if err != nil {
		t.Fatalf("CreateVeleroSchedule (existing) returned error: %v", err)
	}
	if created2 {
		t.Fatalf("expected schedule to already exist")
	}

	stored2 := &velerov1.Schedule{}
	if err := cli.Get(ctx, ctrlclient.ObjectKey{Name: schedule.Name, Namespace: controller.VeleroNamespace}, stored2); err != nil {
		t.Fatalf("get updated schedule: %v", err)
	}

	ownerToken := BuildDependencyToken(string(appBackup.UID))
	if got := stored2.Labels[LabelCleanupOwnerToken]; got != ownerToken {
		t.Fatalf("unexpected schedule cleanup owner token: got %q want %q", got, ownerToken)
	}
	if got := stored2.Labels[LabelCleanupRelation]; got != "finalizer.veleroSchedule" {
		t.Fatalf("unexpected schedule cleanup relation: %q", got)
	}
	if got := stored2.Labels[LabelCleanupStrategy]; got != CleanupStrategyDelete {
		t.Fatalf("unexpected schedule cleanup strategy: %q", got)
	}

	// Template should carry backup cleanup labels so Velero-created backups inherit them.
	if got := stored2.Spec.Template.Metadata.Labels[LabelCleanupOwnerToken]; got != ownerToken {
		t.Fatalf("unexpected template cleanup owner token: got %q want %q", got, ownerToken)
	}
	if got := stored2.Spec.Template.Metadata.Labels[LabelCleanupRelation]; got != "finalizer.veleroBackup" {
		t.Fatalf("unexpected template cleanup relation: %q", got)
	}
	if got := stored2.Spec.Template.Metadata.Labels[LabelCleanupStrategy]; got != CleanupStrategyDeleteRequest {
		t.Fatalf("unexpected template cleanup strategy: %q", got)
	}
	if got := stored2.Spec.Template.Metadata.Labels[LabelCleanupManagedBy]; got != LabelCleanupManagedByValueOperator {
		t.Fatalf("unexpected template cleanup managed-by: %q", got)
	}
}
