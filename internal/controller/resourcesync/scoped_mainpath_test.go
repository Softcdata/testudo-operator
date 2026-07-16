package resourcesync

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/go-logr/logr/funcr"
	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestBuildAppBackupSpec_ProjectsScopedSelection(t *testing.T) {
	r := &ResourceSyncReconciler{}
	instance := &disasterv1.DisasterInstance{
		Spec: disasterv1.DisasterInstanceSpec{
			Namespaces: []string{"app"},
			LabelSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"base": "true"},
			},
			RestorePolicy: &disasterv1.RestorePolicy{
				ResourceSelection: &disasterv1.RestoreResourceSelectionPolicy{
					IncludedNamespaces:               []string{"app-dr"},
					ExcludedNamespaces:               []string{"skip-me"},
					IncludedNamespaceScopedResources: []string{"deployments.apps", "persistentvolumeclaims", "services"},
					ExcludedNamespaceScopedResources: []string{"secrets"},
					IncludedClusterScopedResources:   []string{"clusterroles.rbac.authorization.k8s.io", "customresourcedefinitions.apiextensions.k8s.io"},
					ExcludedClusterScopedResources:   []string{"clusterrolebindings.rbac.authorization.k8s.io"},
					LabelSelector:                    &metav1.LabelSelector{MatchLabels: map[string]string{"fine": "true"}},
				},
			},
		},
	}
	config := &disasterv1.DisasterConfig{
		Spec: disasterv1.DisasterConfigSpec{
			SourceCluster:     "cluster-a",
			TargetCluster:     "cluster-b",
			StorageRepository: "repo-a",
		},
	}

	spec := r.buildAppBackupSpec(instance, config)

	if !equalStrings(spec.Template.IncludedNamespaces, []string{"app-dr"}) {
		t.Fatalf("unexpected included namespaces: %v", spec.Template.IncludedNamespaces)
	}
	if !equalStrings(spec.Template.ExcludedNamespaces, []string{"velero", "kube-system", "skip-me"}) {
		t.Fatalf("unexpected excluded namespaces: %v", spec.Template.ExcludedNamespaces)
	}
	if spec.Template.LabelSelector == nil || spec.Template.LabelSelector.MatchLabels["fine"] != "true" {
		t.Fatalf("expected scoped label selector to override base selector, got %#v", spec.Template.LabelSelector)
	}
	if !equalStrings(spec.Template.IncludedNamespaceScopedResources, []string{"deployments.apps", "services"}) {
		t.Fatalf("unexpected included namespace scoped resources: %v", spec.Template.IncludedNamespaceScopedResources)
	}
	if !equalStrings(spec.Template.ExcludedNamespaceScopedResources, []string{"secrets", "pods", "persistentvolumeclaims", "persistentvolumes"}) {
		t.Fatalf("unexpected excluded namespace scoped resources: %v", spec.Template.ExcludedNamespaceScopedResources)
	}
	if !equalStrings(spec.Template.IncludedClusterScopedResources, []string{"clusterroles.rbac.authorization.k8s.io", "customresourcedefinitions.apiextensions.k8s.io"}) {
		t.Fatalf("unexpected included cluster scoped resources: %v", spec.Template.IncludedClusterScopedResources)
	}
	if !equalStrings(spec.Template.ExcludedClusterScopedResources, []string{"clusterrolebindings.rbac.authorization.k8s.io"}) {
		t.Fatalf("unexpected excluded cluster scoped resources: %v", spec.Template.ExcludedClusterScopedResources)
	}
}

func TestBuildAppBackupSpec_ScopedWithoutClusterIncludeDisablesClusterBackup(t *testing.T) {
	r := &ResourceSyncReconciler{}
	instance := &disasterv1.DisasterInstance{
		Spec: disasterv1.DisasterInstanceSpec{
			Namespaces: []string{"app"},
			RestorePolicy: &disasterv1.RestorePolicy{
				ResourceSelection: &disasterv1.RestoreResourceSelectionPolicy{
					IncludedNamespaceScopedResources: []string{"deployments.apps"},
					ExcludedClusterScopedResources:   []string{"clusterroles.rbac.authorization.k8s.io"},
				},
			},
		},
	}
	config := &disasterv1.DisasterConfig{
		Spec: disasterv1.DisasterConfigSpec{SourceCluster: "cluster-a"},
	}

	spec := r.buildAppBackupSpec(instance, config)
	if spec.Template.IncludedClusterScopedResources != nil {
		t.Fatalf("expected no cluster includes, got %v", spec.Template.IncludedClusterScopedResources)
	}
	if !equalStrings(spec.Template.ExcludedClusterScopedResources, []string{"*"}) {
		t.Fatalf("expected cluster backup disabled with wildcard exclude, got %v", spec.Template.ExcludedClusterScopedResources)
	}
}

func TestBuildAppBackupSpec_ScopedClusterIncludeWithoutExcludesKeepsNilExcludedClusterScopedResources(t *testing.T) {
	r := &ResourceSyncReconciler{}
	instance := &disasterv1.DisasterInstance{
		Spec: disasterv1.DisasterInstanceSpec{
			Namespaces: []string{"app"},
			RestorePolicy: &disasterv1.RestorePolicy{
				ResourceSelection: &disasterv1.RestoreResourceSelectionPolicy{
					IncludedClusterScopedResources: []string{"clusterroles.rbac.authorization.k8s.io"},
				},
			},
		},
	}
	config := &disasterv1.DisasterConfig{
		Spec: disasterv1.DisasterConfigSpec{SourceCluster: "cluster-a"},
	}

	spec := r.buildAppBackupSpec(instance, config)
	if !equalStrings(spec.Template.IncludedClusterScopedResources, []string{"clusterroles.rbac.authorization.k8s.io"}) {
		t.Fatalf("unexpected included cluster scoped resources: %v", spec.Template.IncludedClusterScopedResources)
	}
	if spec.Template.ExcludedClusterScopedResources != nil {
		t.Fatalf("expected nil excluded cluster scoped resources, got %#v", spec.Template.ExcludedClusterScopedResources)
	}
}

func TestBuildClusterAppRestoreSpec_UsesNoneAndClusterKinds(t *testing.T) {
	r := &ResourceSyncReconciler{}
	rs := &disasterv1.ResourceSync{ObjectMeta: metav1.ObjectMeta{Name: "rs-demo", Namespace: "default"}}
	instance := &disasterv1.DisasterInstance{
		Spec: disasterv1.DisasterInstanceSpec{
			Namespaces: []string{"app"},
			RestorePolicy: &disasterv1.RestorePolicy{
				Execution: &disasterv1.RestoreExecutionPolicy{
					ExistingResourcePolicy: "update",
				},
				ResourceSelection: &disasterv1.RestoreResourceSelectionPolicy{
					IncludedNamespaceScopedResources: []string{"deployments.apps"},
					IncludedClusterScopedResources:   []string{"clusterroles.rbac.authorization.k8s.io"},
					ExcludedClusterScopedResources:   []string{"clusterrolebindings.rbac.authorization.k8s.io"},
				},
			},
		},
	}
	config := &disasterv1.DisasterConfig{
		Spec: disasterv1.DisasterConfigSpec{
			SourceCluster:     "cluster-a",
			TargetCluster:     "cluster-b",
			StorageRepository: "repo-a",
		},
	}

	spec, _, err := r.buildClusterAppRestoreSpec(context.Background(), rs, config, instance, "backup-001")
	if err != nil {
		t.Fatalf("buildClusterAppRestoreSpec returned error: %v", err)
	}
	if spec.Template.ExistingResourcePolicy != velerov1.PolicyTypeNone {
		t.Fatalf("expected cluster phase policy=none, got %s", spec.Template.ExistingResourcePolicy)
	}
	if spec.Template.IncludeClusterResources == nil || !*spec.Template.IncludeClusterResources {
		t.Fatalf("expected includeClusterResources=true, got %v", spec.Template.IncludeClusterResources)
	}
	if !equalStrings(spec.Template.IncludedResources, []string{"clusterroles.rbac.authorization.k8s.io"}) {
		t.Fatalf("unexpected cluster phase included resources: %v", spec.Template.IncludedResources)
	}
	if !equalStrings(spec.Template.ExcludedResources, []string{"clusterrolebindings.rbac.authorization.k8s.io"}) {
		t.Fatalf("unexpected cluster phase excluded resources: %v", spec.Template.ExcludedResources)
	}
}

func TestBuildAppRestoreSpec_ScopedNamespacePhaseUsesUpdateAndMandatoryExclusions(t *testing.T) {
	s := runtime.NewScheme()
	if err := scheme.AddToScheme(s); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	if err := disasterv1.AddToScheme(s); err != nil {
		t.Fatalf("add disaster scheme: %v", err)
	}

	sourceCluster := &disasterv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "cluster-a"}}
	targetCluster := &disasterv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "cluster-b"}}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(sourceCluster, targetCluster).Build()
	r := &ResourceSyncReconciler{Client: c, Scheme: s}
	rs := &disasterv1.ResourceSync{ObjectMeta: metav1.ObjectMeta{Name: "rs-demo", Namespace: "default"}}
	instance := &disasterv1.DisasterInstance{
		Spec: disasterv1.DisasterInstanceSpec{
			Namespaces: []string{"app"},
			RestorePolicy: &disasterv1.RestorePolicy{
				Execution: &disasterv1.RestoreExecutionPolicy{
					ExistingResourcePolicy: "none",
				},
				ResourceSelection: &disasterv1.RestoreResourceSelectionPolicy{
					IncludedNamespaceScopedResources: []string{"deployments.apps", "persistentvolumeclaims"},
					ExcludedNamespaceScopedResources: []string{"configmaps"},
					IncludedClusterScopedResources:   []string{"clusterroles.rbac.authorization.k8s.io"},
				},
			},
		},
	}
	config := &disasterv1.DisasterConfig{
		Spec: disasterv1.DisasterConfigSpec{
			SourceCluster:     "cluster-a",
			TargetCluster:     "cluster-b",
			StorageRepository: "repo-a",
		},
	}

	spec, _, err := r.buildAppRestoreSpec(context.Background(), rs, config, instance, "backup-001")
	if err != nil {
		t.Fatalf("buildAppRestoreSpec returned error: %v", err)
	}
	if spec.Template.ExistingResourcePolicy != velerov1.PolicyTypeUpdate {
		t.Fatalf("expected namespace phase policy=update, got %s", spec.Template.ExistingResourcePolicy)
	}
	if spec.Template.IncludeClusterResources == nil || *spec.Template.IncludeClusterResources {
		t.Fatalf("expected includeClusterResources=false, got %v", spec.Template.IncludeClusterResources)
	}
	if !equalStrings(spec.Template.IncludedResources, []string{"deployments.apps"}) {
		t.Fatalf("unexpected namespace phase included resources: %v", spec.Template.IncludedResources)
	}
	if !equalStrings(spec.Template.ExcludedResources, []string{"configmaps", "pods", "persistentvolumeclaims", "persistentvolumes"}) {
		t.Fatalf("unexpected namespace phase excluded resources: %v", spec.Template.ExcludedResources)
	}
	if hasResourceSyncPatchPath(spec.ResourceModifierRules, "/spec/nodeName") ||
		hasResourceSyncPatchPath(spec.ResourceModifierRules, "/spec/nodeSelector") ||
		hasResourceSyncPatchPath(spec.ResourceModifierRules, "/spec/affinity") {
		t.Fatalf("ResourceSync restore must not include DataSync-only scheduling cleanup: %#v", spec.ResourceModifierRules)
	}
}

func TestHandleRestore_CreatesClusterPhaseBeforeNamespacePhase(t *testing.T) {
	s := runtime.NewScheme()
	if err := scheme.AddToScheme(s); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	if err := disasterv1.AddToScheme(s); err != nil {
		t.Fatalf("add disaster scheme: %v", err)
	}

	rs := &disasterv1.ResourceSync{
		ObjectMeta: metav1.ObjectMeta{Name: "rs-demo", Namespace: "default"},
		Status: disasterv1.ResourceSyncStatus{
			State:          disasterv1.ResourceSyncStateInProgress,
			LastBackupName: "backup-001",
		},
	}
	instance := &disasterv1.DisasterInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "inst-demo", Namespace: "default"},
		Spec: disasterv1.DisasterInstanceSpec{
			Namespaces: []string{"app"},
			RestorePolicy: &disasterv1.RestorePolicy{
				ResourceSelection: &disasterv1.RestoreResourceSelectionPolicy{
					IncludedNamespaceScopedResources: []string{"deployments.apps"},
					IncludedClusterScopedResources:   []string{"clusterroles.rbac.authorization.k8s.io"},
				},
			},
		},
	}
	config := &disasterv1.DisasterConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "cfg-demo", Namespace: "default"},
		Spec: disasterv1.DisasterConfigSpec{
			SourceCluster:     "cluster-a",
			TargetCluster:     "cluster-b",
			StorageRepository: "repo-a",
		},
	}
	sourceCluster := &disasterv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "cluster-a"}}
	targetCluster := &disasterv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "cluster-b"}}
	appBackup := &disasterv1.AppBackup{
		ObjectMeta: metav1.ObjectMeta{Name: "rs-rs-demo", Namespace: "default"},
		Status: disasterv1.AppBackupStatus{
			History: []disasterv1.BackupRecord{{
				Name:           "backup-001",
				StartTimestamp: &metav1.Time{Time: time.Now().Add(-time.Minute)},
			}},
		},
	}

	c := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(rs, instance, config, sourceCluster, targetCluster, appBackup).
		WithStatusSubresource(rs, appBackup).
		Build()

	r := &ResourceSyncReconciler{
		Client:   c,
		Scheme:   s,
		Recorder: record.NewFakeRecorder(10),
	}

	if _, err := r.handleRestore(context.Background(), logr.Discard(), rs, config, instance, "backup-001"); err != nil {
		t.Fatalf("first handleRestore returned error: %v", err)
	}

	clusterRestoreName := resourceSyncRestoreName(rs.Name, "backup-001", resourceSyncRestorePhaseCluster)
	clusterRestore := &disasterv1.AppRestore{}
	if err := c.Get(context.Background(), types.NamespacedName{Name: clusterRestoreName, Namespace: "default"}, clusterRestore); err != nil {
		t.Fatalf("expected cluster phase restore to be created: %v", err)
	}

	updatedRS := &disasterv1.ResourceSync{}
	if err := c.Get(context.Background(), types.NamespacedName{Name: "rs-demo", Namespace: "default"}, updatedRS); err != nil {
		t.Fatalf("get updated resourcesync: %v", err)
	}
	if updatedRS.Status.LastClusterRestoreName != clusterRestoreName {
		t.Fatalf("expected lastClusterRestoreName=%s, got %s", clusterRestoreName, updatedRS.Status.LastClusterRestoreName)
	}
	if updatedRS.Status.LastNamespaceRestoreName != "" {
		t.Fatalf("expected namespace phase not started yet, got %s", updatedRS.Status.LastNamespaceRestoreName)
	}
}

func TestHandleRestore_DoesNotRepeatSuccessLogsForUnchangedTerminalPhases(t *testing.T) {
	s := runtime.NewScheme()
	if err := scheme.AddToScheme(s); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	if err := disasterv1.AddToScheme(s); err != nil {
		t.Fatalf("add disaster scheme: %v", err)
	}

	backupName := "backup-001"
	clusterRestoreName := resourceSyncRestoreName("rs-demo", backupName, resourceSyncRestorePhaseCluster)
	namespaceRestoreName := resourceSyncRestoreName("rs-demo", backupName, resourceSyncRestorePhaseNamespace)

	rs := &disasterv1.ResourceSync{
		ObjectMeta: metav1.ObjectMeta{Name: "rs-demo", Namespace: "default"},
		Status: disasterv1.ResourceSyncStatus{
			State:                    disasterv1.ResourceSyncStateInProgress,
			LastBackupName:           backupName,
			LastRestoreName:          namespaceRestoreName,
			LastClusterRestoreName:   clusterRestoreName,
			ClusterRestoreStatus:     disasterv1.PhaseSucceeded,
			LastNamespaceRestoreName: namespaceRestoreName,
			NamespaceRestoreStatus:   disasterv1.PhaseSucceeded,
		},
	}
	instance := &disasterv1.DisasterInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "inst-demo", Namespace: "default"},
		Spec: disasterv1.DisasterInstanceSpec{
			Namespaces: []string{"app"},
			RestorePolicy: &disasterv1.RestorePolicy{
				ResourceSelection: &disasterv1.RestoreResourceSelectionPolicy{
					IncludedNamespaceScopedResources: []string{"deployments.apps"},
					IncludedClusterScopedResources:   []string{"clusterroles.rbac.authorization.k8s.io"},
				},
			},
		},
	}
	config := &disasterv1.DisasterConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "cfg-demo", Namespace: "default"},
		Spec: disasterv1.DisasterConfigSpec{
			SourceCluster:     "cluster-a",
			TargetCluster:     "cluster-b",
			StorageRepository: "repo-a",
		},
	}
	sourceCluster := &disasterv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "cluster-a"}}
	targetCluster := &disasterv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "cluster-b"}}
	appBackup := &disasterv1.AppBackup{
		ObjectMeta: metav1.ObjectMeta{Name: "rs-rs-demo", Namespace: "default"},
		Status: disasterv1.AppBackupStatus{
			History: []disasterv1.BackupRecord{{
				Name:           backupName,
				StartTimestamp: &metav1.Time{Time: time.Now().Add(-time.Minute)},
			}},
		},
	}
	clusterRestore := &disasterv1.AppRestore{
		ObjectMeta: metav1.ObjectMeta{Name: clusterRestoreName, Namespace: "default"},
		Status: disasterv1.AppRestoreStatus{
			Status: disasterv1.PhaseSucceeded,
		},
	}
	namespaceRestore := &disasterv1.AppRestore{
		ObjectMeta: metav1.ObjectMeta{Name: namespaceRestoreName, Namespace: "default"},
		Status: disasterv1.AppRestoreStatus{
			Status: disasterv1.PhaseSucceeded,
		},
	}

	c := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(rs, instance, config, sourceCluster, targetCluster, appBackup, clusterRestore, namespaceRestore).
		WithStatusSubresource(rs, appBackup, clusterRestore, namespaceRestore).
		Build()

	var lines []string
	logger := funcr.New(func(prefix, args string) {
		lines = append(lines, prefix+" "+args)
	}, funcr.Options{})

	r := &ResourceSyncReconciler{
		Client:   c,
		Scheme:   s,
		Recorder: record.NewFakeRecorder(10),
	}

	if _, err := r.handleRestore(context.Background(), logger, rs, config, instance, backupName); err != nil {
		t.Fatalf("handleRestore returned error: %v", err)
	}

	for _, line := range lines {
		if strings.Contains(line, "AppRestore 成功") {
			t.Fatalf("expected no repeated success log, got line: %s", line)
		}
	}
}

func equalStrings(a []string, b []string) bool {
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

func hasResourceSyncPatchPath(rules []disasterv1.ResourceModifierRule, path string) bool {
	for _, rule := range rules {
		for _, patch := range rule.Patches {
			if patch.Path == path {
				return true
			}
		}
	}
	return false
}
