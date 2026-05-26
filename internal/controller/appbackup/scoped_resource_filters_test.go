package appbackup

import (
	"context"
	"testing"
	"time"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestCreateVeleroBackup_PropagatesScopedResourceFilters(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := disasterv1.AddToScheme(scheme); err != nil {
		t.Fatalf("add disaster scheme: %v", err)
	}
	if err := velerov1.AddToScheme(scheme); err != nil {
		t.Fatalf("add velero scheme: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add corev1 scheme: %v", err)
	}

	cli := fake.NewClientBuilder().WithScheme(scheme).Build()
	reconciler := &AppBackupReconciler{Recorder: record.NewFakeRecorder(16)}
	appBackup := buildScopedTemplateAppBackup("", "ab-scoped-backup")

	backup, created, err := reconciler.CreateVeleroBackup(
		context.Background(),
		cli,
		appBackup,
		"repo-target",
		"backup-fixed",
	)
	if err != nil {
		t.Fatalf("CreateVeleroBackup returned error: %v", err)
	}
	if !created {
		t.Fatalf("expected created=true")
	}
	if backup == nil {
		t.Fatalf("expected created backup object")
	}

	if !equalStringSlice(backup.Spec.IncludedNamespaceScopedResources, appBackup.Spec.Template.IncludedNamespaceScopedResources) {
		t.Fatalf("includedNamespaceScopedResources mismatch: got %v want %v", backup.Spec.IncludedNamespaceScopedResources, appBackup.Spec.Template.IncludedNamespaceScopedResources)
	}
	if !equalStringSlice(backup.Spec.ExcludedNamespaceScopedResources, appBackup.Spec.Template.ExcludedNamespaceScopedResources) {
		t.Fatalf("excludedNamespaceScopedResources mismatch: got %v want %v", backup.Spec.ExcludedNamespaceScopedResources, appBackup.Spec.Template.ExcludedNamespaceScopedResources)
	}
	if !equalStringSlice(backup.Spec.IncludedClusterScopedResources, appBackup.Spec.Template.IncludedClusterScopedResources) {
		t.Fatalf("includedClusterScopedResources mismatch: got %v want %v", backup.Spec.IncludedClusterScopedResources, appBackup.Spec.Template.IncludedClusterScopedResources)
	}
	if !equalStringSlice(backup.Spec.ExcludedClusterScopedResources, appBackup.Spec.Template.ExcludedClusterScopedResources) {
		t.Fatalf("excludedClusterScopedResources mismatch: got %v want %v", backup.Spec.ExcludedClusterScopedResources, appBackup.Spec.Template.ExcludedClusterScopedResources)
	}

	stored := &velerov1.Backup{}
	if err := cli.Get(context.Background(), types.NamespacedName{Name: "backup-fixed", Namespace: "velero"}, stored); err != nil {
		t.Fatalf("get stored backup: %v", err)
	}
	if !equalStringSlice(stored.Spec.IncludedClusterScopedResources, appBackup.Spec.Template.IncludedClusterScopedResources) {
		t.Fatalf("stored backup includedClusterScopedResources mismatch: got %v want %v", stored.Spec.IncludedClusterScopedResources, appBackup.Spec.Template.IncludedClusterScopedResources)
	}
}

func TestCreateVeleroSchedule_PropagatesScopedResourceFilters(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := disasterv1.AddToScheme(scheme); err != nil {
		t.Fatalf("add disaster scheme: %v", err)
	}
	if err := velerov1.AddToScheme(scheme); err != nil {
		t.Fatalf("add velero scheme: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add corev1 scheme: %v", err)
	}

	cli := fake.NewClientBuilder().WithScheme(scheme).Build()
	reconciler := &AppBackupReconciler{Recorder: record.NewFakeRecorder(16)}
	appBackup := buildScopedTemplateAppBackup("*/5 * * * *", "ab-scoped-schedule")
	appBackup.Spec.Template.TTL = metav1.Duration{Duration: 720 * time.Hour}

	schedule, created, err := reconciler.CreateVeleroSchedule(context.Background(), cli, appBackup, "repo-target")
	if err != nil {
		t.Fatalf("CreateVeleroSchedule returned error: %v", err)
	}
	if !created {
		t.Fatalf("expected created=true")
	}
	if schedule == nil {
		t.Fatalf("expected created schedule object")
	}

	if !equalStringSlice(schedule.Spec.Template.IncludedNamespaceScopedResources, appBackup.Spec.Template.IncludedNamespaceScopedResources) {
		t.Fatalf("schedule includedNamespaceScopedResources mismatch: got %v want %v", schedule.Spec.Template.IncludedNamespaceScopedResources, appBackup.Spec.Template.IncludedNamespaceScopedResources)
	}
	if !equalStringSlice(schedule.Spec.Template.ExcludedNamespaceScopedResources, appBackup.Spec.Template.ExcludedNamespaceScopedResources) {
		t.Fatalf("schedule excludedNamespaceScopedResources mismatch: got %v want %v", schedule.Spec.Template.ExcludedNamespaceScopedResources, appBackup.Spec.Template.ExcludedNamespaceScopedResources)
	}
	if !equalStringSlice(schedule.Spec.Template.IncludedClusterScopedResources, appBackup.Spec.Template.IncludedClusterScopedResources) {
		t.Fatalf("schedule includedClusterScopedResources mismatch: got %v want %v", schedule.Spec.Template.IncludedClusterScopedResources, appBackup.Spec.Template.IncludedClusterScopedResources)
	}
	if !equalStringSlice(schedule.Spec.Template.ExcludedClusterScopedResources, appBackup.Spec.Template.ExcludedClusterScopedResources) {
		t.Fatalf("schedule excludedClusterScopedResources mismatch: got %v want %v", schedule.Spec.Template.ExcludedClusterScopedResources, appBackup.Spec.Template.ExcludedClusterScopedResources)
	}
	if schedule.Spec.Template.TTL.Duration != 720*time.Hour {
		t.Fatalf("schedule ttl mismatch: got %s want %s", schedule.Spec.Template.TTL.Duration, 720*time.Hour)
	}
}

func buildScopedTemplateAppBackup(schedule string, name string) *disasterv1.AppBackup {
	return &disasterv1.AppBackup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "disaster-system",
			UID:       types.UID(name + "-uid"),
		},
		Spec: disasterv1.AppBackupSpec{
			Cluster:  "cluster-a",
			Schedule: schedule,
			Template: velerov1.BackupSpec{
				StorageLocation:                  "repo-origin",
				IncludedNamespaceScopedResources: []string{"deployments.apps", "services"},
				ExcludedNamespaceScopedResources: []string{"secrets"},
				IncludedClusterScopedResources:   []string{"nodes", "clusterroles"},
				ExcludedClusterScopedResources:   []string{"clusterrolebindings"},
			},
		},
	}
}

func equalStringSlice(a []string, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
