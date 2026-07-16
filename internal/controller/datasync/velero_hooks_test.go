package datasync

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	ctrlcommon "github.com/softcdata/testudo-operator/internal/controller"
	restorebuilder "github.com/softcdata/testudo-operator/internal/controller/restore"
	"github.com/softcdata/testudo-operator/internal/controller/scheduler"
	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	corev1 "k8s.io/api/core/v1"
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
	originalSelector := &metav1.LabelSelector{MatchLabels: map[string]string{"app": "hook-di"}}
	hooks.Resources[0].IncludedNamespaces = []string{"app"}
	hooks.Resources[0].LabelSelector = originalSelector
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
	if len(spec.Template.Hooks.Resources) != 1 {
		t.Fatalf("expected one data restore hook resource, got %#v", spec.Template.Hooks.Resources)
	}
	gotResource := spec.Template.Hooks.Resources[0]
	markerKey := restorebuilder.DataRestoreHookMarkerLabelKey(0)
	if !reflect.DeepEqual(gotResource.LabelSelector, &metav1.LabelSelector{MatchLabels: map[string]string{markerKey: "true"}}) {
		t.Fatalf("expected data restore exec hook selector to use marker label, got %#v", gotResource.LabelSelector)
	}
	if !reflect.DeepEqual(gotResource.PostHooks, hooks.Resources[0].PostHooks) {
		t.Fatalf("expected data restore exec hook parameters to be preserved, got %#v", gotResource.PostHooks)
	}
	if !hasMarkerPatchRule(spec.ResourceModifierRules, markerKey, originalSelector, []string{"app"}) {
		t.Fatalf("expected marker modifier rule for original hook selector, got %#v", spec.ResourceModifierRules)
	}
}

func TestPrepareTrafficlessDataRestoreHooks_LeavesInitOnlyHooksUntouched(t *testing.T) {
	initHook := velerov1.RestoreResourceHook{
		Init: &velerov1.InitRestoreHook{
			Timeout: metav1.Duration{Duration: time.Minute},
		},
	}
	selector := &metav1.LabelSelector{MatchLabels: map[string]string{"app": "hook-di"}}
	hooks := &velerov1.RestoreHooks{
		Resources: []velerov1.RestoreResourceHookSpec{{
			Name:               "init-only",
			IncludedNamespaces: []string{"app"},
			IncludedResources:  []string{"pods"},
			LabelSelector:      selector,
			PostHooks:          []velerov1.RestoreResourceHook{initHook},
		}},
	}

	rewritten, markerRules := restorebuilder.PrepareTrafficlessDataRestoreHooks(hooks, []string{"app"}, nil)
	if len(markerRules) != 0 {
		t.Fatalf("expected init-only hook not to create marker rules, got %#v", markerRules)
	}
	if !reflect.DeepEqual(rewritten, hooks) {
		t.Fatalf("expected init-only hooks unchanged, got %#v", rewritten)
	}
}

func TestPrepareTrafficlessDataRestoreHooks_SplitsMixedInitAndExecHooks(t *testing.T) {
	selector := &metav1.LabelSelector{MatchLabels: map[string]string{"app": "hook-di"}}
	hooks := &velerov1.RestoreHooks{
		Resources: []velerov1.RestoreResourceHookSpec{{
			Name:               "mixed",
			IncludedNamespaces: []string{"app"},
			IncludedResources:  []string{"pods"},
			LabelSelector:      selector,
			PostHooks: []velerov1.RestoreResourceHook{
				{Init: &velerov1.InitRestoreHook{Timeout: metav1.Duration{Duration: time.Minute}}},
				{Exec: &velerov1.ExecRestoreHook{Command: []string{"/bin/hook", "after"}}},
			},
		}},
	}

	rewritten, markerRules := restorebuilder.PrepareTrafficlessDataRestoreHooks(hooks, []string{"app"}, nil)
	if len(markerRules) != 1 {
		t.Fatalf("expected one marker rule, got %#v", markerRules)
	}
	if len(rewritten.Resources) != 2 {
		t.Fatalf("expected mixed hook to be split into init and exec resources, got %#v", rewritten.Resources)
	}
	if !reflect.DeepEqual(rewritten.Resources[0].LabelSelector, selector) {
		t.Fatalf("expected init resource selector unchanged, got %#v", rewritten.Resources[0].LabelSelector)
	}
	if len(rewritten.Resources[0].PostHooks) != 1 || rewritten.Resources[0].PostHooks[0].Init == nil || rewritten.Resources[0].PostHooks[0].Exec != nil {
		t.Fatalf("expected first split resource to contain init hook only, got %#v", rewritten.Resources[0].PostHooks)
	}
	markerKey := restorebuilder.DataRestoreHookMarkerLabelKey(0)
	if !reflect.DeepEqual(rewritten.Resources[1].LabelSelector, &metav1.LabelSelector{MatchLabels: map[string]string{markerKey: "true"}}) {
		t.Fatalf("expected exec resource selector to use marker label, got %#v", rewritten.Resources[1].LabelSelector)
	}
	if len(rewritten.Resources[1].PostHooks) != 1 || rewritten.Resources[1].PostHooks[0].Exec == nil || rewritten.Resources[1].PostHooks[0].Init != nil {
		t.Fatalf("expected second split resource to contain exec hook only, got %#v", rewritten.Resources[1].PostHooks)
	}
}

func TestPrepareTrafficlessDataRestoreHooks_UsesIndependentMarkerKeys(t *testing.T) {
	hooks := &velerov1.RestoreHooks{
		Resources: []velerov1.RestoreResourceHookSpec{
			{
				Name:              "first",
				IncludedResources: []string{"pods"},
				PostHooks: []velerov1.RestoreResourceHook{{
					Exec: &velerov1.ExecRestoreHook{Command: []string{"/bin/hook", "first"}},
				}},
			},
			{
				Name:              "second",
				IncludedResources: []string{"pods"},
				PostHooks: []velerov1.RestoreResourceHook{{
					Exec: &velerov1.ExecRestoreHook{Command: []string{"/bin/hook", "second"}},
				}},
			},
		},
	}

	rewritten, markerRules := restorebuilder.PrepareTrafficlessDataRestoreHooks(hooks, []string{"app"}, nil)
	if len(markerRules) != 2 {
		t.Fatalf("expected two marker rules, got %#v", markerRules)
	}
	for idx := range rewritten.Resources {
		markerKey := restorebuilder.DataRestoreHookMarkerLabelKey(idx)
		if !reflect.DeepEqual(rewritten.Resources[idx].LabelSelector, &metav1.LabelSelector{MatchLabels: map[string]string{markerKey: "true"}}) {
			t.Fatalf("expected resource %d selector to use marker key %s, got %#v", idx, markerKey, rewritten.Resources[idx].LabelSelector)
		}
		if markerRules[idx].Patches[0].Path != markerPatchPath(idx) {
			t.Fatalf("unexpected marker patch path for rule %d: %s", idx, markerRules[idx].Patches[0].Path)
		}
	}
}

func TestBuildAppRestoreSpec_WithRestorePolicyKeepsMarkerAfterTrafficlessLabels(t *testing.T) {
	r := &DataSyncReconciler{}
	hooks := sampleRestoreHooks()
	hooks.Resources[0].LabelSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"app": "hook-di"}}
	ds := &disasterv1.DataSync{ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "default"}}
	instance := &disasterv1.DisasterInstance{
		Spec: disasterv1.DisasterInstanceSpec{
			Namespaces:    []string{"app"},
			RestorePolicy: &disasterv1.RestorePolicy{},
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

	labelsIndex := firstPatchIndex(spec.ResourceModifierRules, "/metadata/labels")
	markerIndex := firstPatchIndex(spec.ResourceModifierRules, markerPatchPath(0))
	if labelsIndex < 0 || markerIndex < 0 {
		t.Fatalf("expected trafficless labels and marker patches, got %#v", spec.ResourceModifierRules)
	}
	if labelsIndex >= markerIndex {
		t.Fatalf("expected trafficless labels patch before marker patch, labelsIndex=%d markerIndex=%d rules=%#v", labelsIndex, markerIndex, spec.ResourceModifierRules)
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
	sourceClient := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(&corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: "data", Namespace: "app"},
		}).
		Build()
	syncScheduler, err := scheduler.NewSyncScheduler()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = syncScheduler.Shutdown() }()
	r := &DataSyncReconciler{
		Client:              c,
		Scheme:              s,
		Log:                 ctrl.Log.WithName("test"),
		Recorder:            record.NewFakeRecorder(16),
		Scheduler:           syncScheduler,
		SourceClientFactory: &ctrlcommon.MockClientFactory{MockClient: sourceClient},
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

func hasMarkerPatchRule(
	rules []disasterv1.ResourceModifierRule,
	markerKey string,
	selector *metav1.LabelSelector,
	namespaces []string,
) bool {
	wantPath := "/metadata/labels/" + markerKey
	for idx := 0; idx < 32; idx++ {
		if restorebuilder.DataRestoreHookMarkerLabelKey(idx) == markerKey {
			wantPath = markerPatchPath(idx)
			break
		}
	}
	for _, rule := range rules {
		if rule.Conditions.GroupResource != "pods" {
			continue
		}
		if !reflect.DeepEqual(rule.Conditions.LabelSelector, selector) {
			continue
		}
		if !reflect.DeepEqual(rule.Conditions.Namespaces, namespaces) {
			continue
		}
		for _, patch := range rule.Patches {
			if patch.Operation == "add" && patch.Path == wantPath && patch.Value == `"true"` {
				return true
			}
		}
	}
	return false
}

func markerPatchPath(index int) string {
	return fmt.Sprintf("/metadata/labels/testudo.softcdata.com~1data-restore-hook-%d", index)
}

func firstPatchIndex(rules []disasterv1.ResourceModifierRule, path string) int {
	index := 0
	for _, rule := range rules {
		for _, patch := range rule.Patches {
			if patch.Path == path {
				return index
			}
			index++
		}
	}
	return -1
}
