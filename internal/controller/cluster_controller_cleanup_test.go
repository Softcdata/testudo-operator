package controller

import (
	"context"
	"fmt"
	"testing"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	platformlicense "github.com/softcdata/testudo-operator/pkg/license"
	. "github.com/softcdata/testudo-operator/pkg/metadata"
	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func buildCleanupTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := corev1.AddToScheme(s); err != nil {
		t.Fatalf("add corev1 scheme failed: %v", err)
	}
	if err := appsv1.AddToScheme(s); err != nil {
		t.Fatalf("add appsv1 scheme failed: %v", err)
	}
	if err := rbacv1.AddToScheme(s); err != nil {
		t.Fatalf("add rbac scheme failed: %v", err)
	}
	if err := apiextensionsv1.AddToScheme(s); err != nil {
		t.Fatalf("add apiextensions scheme failed: %v", err)
	}
	if err := velerov1.AddToScheme(s); err != nil {
		t.Fatalf("add velero scheme failed: %v", err)
	}
	if err := disasterv1.AddToScheme(s); err != nil {
		t.Fatalf("add disaster scheme failed: %v", err)
	}
	return s
}

func minimalKubeConfigForTest() []byte {
	return []byte(`apiVersion: v1
kind: Config
clusters:
- name: test
  cluster:
    server: https://127.0.0.1:6443
contexts:
- name: test
  context:
    cluster: test
    user: test
current-context: test
users:
- name: test
  user:
    token: dummy
`)
}

func TestCleanupVeleroResiduals_RemovesNamespaceCRDAndRBAC(t *testing.T) {
	ctx := context.Background()
	scheme := buildCleanupTestScheme(t)

	backup := &velerov1.Backup{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "backup-1",
			Namespace:  VeleroNamespace,
			Finalizers: []string{"finalizers.velero.io/backup-finalizer"},
		},
	}
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: VeleroNamespace},
	}
	crd := &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "backups.velero.io",
			Finalizers: []string{"cleanup.apiextensions.k8s.io"},
		},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "velero.io",
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Plural:   "backups",
				Singular: "backup",
				Kind:     "Backup",
			},
			Scope: "Namespaced",
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
				{
					Name:    "v1",
					Served:  true,
					Storage: true,
					Schema: &apiextensionsv1.CustomResourceValidation{
						OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{Type: "object"},
					},
				},
			},
		},
	}
	role := &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "velero-upgrade-crds"}}
	roleBinding := &rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "velero-upgrade-crds"}}

	cli := fakeclient.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ns, backup, crd, role, roleBinding).
		Build()

	r := &ClusterReconciler{Scheme: scheme}
	if err := r.cleanupVeleroResiduals(ctx, cli); err != nil {
		t.Fatalf("cleanupVeleroResiduals failed: %v", err)
	}

	if err := cli.Get(ctx, types.NamespacedName{Name: VeleroNamespace}, &corev1.Namespace{}); !apierrors.IsNotFound(err) {
		t.Fatalf("velero namespace should be deleted, got err=%v", err)
	}
	if err := cli.Get(ctx, types.NamespacedName{Name: "backups.velero.io"}, &apiextensionsv1.CustomResourceDefinition{}); !apierrors.IsNotFound(err) {
		t.Fatalf("velero crd should be deleted, got err=%v", err)
	}
	if err := cli.Get(ctx, types.NamespacedName{Name: "velero-upgrade-crds"}, &rbacv1.ClusterRole{}); !apierrors.IsNotFound(err) {
		t.Fatalf("velero clusterrole should be deleted, got err=%v", err)
	}
	if err := cli.Get(ctx, types.NamespacedName{Name: "velero-upgrade-crds"}, &rbacv1.ClusterRoleBinding{}); !apierrors.IsNotFound(err) {
		t.Fatalf("velero clusterrolebinding should be deleted, got err=%v", err)
	}
	if err := cli.Get(ctx, types.NamespacedName{Name: "backup-1", Namespace: VeleroNamespace}, &velerov1.Backup{}); !apierrors.IsNotFound(err) {
		t.Fatalf("velero backup should be deleted, got err=%v", err)
	}
}

func TestUninstallVelero_ReleaseNotFoundStillCleanupResiduals(t *testing.T) {
	ctx := context.Background()
	scheme := buildCleanupTestScheme(t)

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: VeleroNamespace},
	}
	crd := &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: "backups.velero.io"},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "velero.io",
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Plural:   "backups",
				Singular: "backup",
				Kind:     "Backup",
			},
			Scope: "Namespaced",
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
				{
					Name:    "v1",
					Served:  true,
					Storage: true,
					Schema: &apiextensionsv1.CustomResourceValidation{
						OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{Type: "object"},
					},
				},
			},
		},
	}
	role := &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "velero-upgrade-crds"}}
	cli := fakeclient.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ns, crd, role).
		Build()

	r := &ClusterReconciler{
		Scheme:          scheme,
		CommandExecutor: &MockCommandExecutor{ReturnError: fmt.Errorf("release: not found")},
		ClientFactory: func(config *rest.Config, options client.Options) (client.Client, error) {
			return cli, nil
		},
	}

	cluster := &disasterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "c-cleanup"},
		Spec: disasterv1.ClusterSpec{
			KubeConfig: minimalKubeConfigForTest(),
		},
	}
	if err := r.uninstallVelero(ctx, cluster); err != nil {
		t.Fatalf("uninstallVelero should ignore release-not-found and continue cleanup, got err=%v", err)
	}

	if err := cli.Get(ctx, types.NamespacedName{Name: VeleroNamespace}, &corev1.Namespace{}); !apierrors.IsNotFound(err) {
		t.Fatalf("velero namespace should be deleted, got err=%v", err)
	}
	if err := cli.Get(ctx, types.NamespacedName{Name: "backups.velero.io"}, &apiextensionsv1.CustomResourceDefinition{}); !apierrors.IsNotFound(err) {
		t.Fatalf("velero crd should be deleted, got err=%v", err)
	}
	if err := cli.Get(ctx, types.NamespacedName{Name: "velero-upgrade-crds"}, &rbacv1.ClusterRole{}); !apierrors.IsNotFound(err) {
		t.Fatalf("velero clusterrole should be deleted, got err=%v", err)
	}
}

func TestRemoveFinalizersAndDeleteList_IgnoreNoMatch(t *testing.T) {
	r := &ClusterReconciler{}
	mockClient := &MockClient{
		MockList: func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
			return &meta.NoKindMatchError{
				GroupKind: schema.GroupKind{Group: "velero.io", Kind: "Backup"},
			}
		},
	}
	if err := r.removeFinalizersAndDeleteList(context.Background(), mockClient, &velerov1.BackupList{}, client.InNamespace(VeleroNamespace)); err != nil {
		t.Fatalf("removeFinalizersAndDeleteList should ignore no-match error, got=%v", err)
	}
}

func TestDeleteVeleroNamespacedCRs_BulkDeletesHighVolumeResources(t *testing.T) {
	r := &ClusterReconciler{}
	ctx := context.Background()

	var listedBackup, listedDeleteBackupRequest int
	var deletedBackup, deletedDeleteBackupRequest int

	mockClient := &MockClient{
		MockList: func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
			switch list.(type) {
			case *velerov1.BackupList:
				listedBackup++
			case *velerov1.DeleteBackupRequestList:
				listedDeleteBackupRequest++
			}
			return nil
		},
		MockDeleteAllOf: func(ctx context.Context, obj client.Object, opts ...client.DeleteAllOfOption) error {
			switch obj.(type) {
			case *velerov1.Backup:
				deletedBackup++
			case *velerov1.DeleteBackupRequest:
				deletedDeleteBackupRequest++
			}
			return nil
		},
	}

	if err := r.deleteVeleroNamespacedCRs(ctx, mockClient); err != nil {
		t.Fatalf("deleteVeleroNamespacedCRs failed: %v", err)
	}

	if listedBackup == 0 {
		t.Fatalf("expected backup resources to be listed for finalizer cleanup")
	}
	if deletedBackup == 0 {
		t.Fatalf("expected backup resources to be deleted via collection delete")
	}
	if listedDeleteBackupRequest != 0 {
		t.Fatalf("expected deletebackuprequests to skip per-object listing, got list calls=%d", listedDeleteBackupRequest)
	}
	if deletedDeleteBackupRequest == 0 {
		t.Fatalf("expected deletebackuprequests to use bulk delete")
	}
}

func TestHandleDelete_PersistsDeletingStatusBeforeCleanup(t *testing.T) {
	ctx := context.Background()
	scheme := buildCleanupTestScheme(t)

	cluster := &disasterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "delete-fast",
			Finalizers: []string{LabelClusterFinalizer},
			Annotations: map[string]string{
				AnnotationUninstallVelero: "true",
			},
		},
		Status: disasterv1.ClusterStatus{
			LastEventPhase: string(disasterv1.ClusterStatusDeleting),
		},
	}

	cli := fakeclient.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&disasterv1.Cluster{}).
		WithObjects(cluster).
		Build()

	if err := cli.Delete(ctx, cluster); err != nil {
		t.Fatalf("delete cluster failed: %v", err)
	}

	latest := &disasterv1.Cluster{}
	if err := cli.Get(ctx, types.NamespacedName{Name: cluster.Name}, latest); err != nil {
		t.Fatalf("get deleting cluster failed: %v", err)
	}

	r := &ClusterReconciler{Client: cli, Scheme: scheme}
	res, err := r.handleDelete(ctx, latest)
	if err != nil {
		t.Fatalf("handleDelete failed: %v", err)
	}
	if !res.Requeue {
		t.Fatalf("expected first delete pass to requeue after persisting status")
	}

	stored := &disasterv1.Cluster{}
	if err := cli.Get(ctx, types.NamespacedName{Name: cluster.Name}, stored); err != nil {
		t.Fatalf("get stored cluster failed: %v", err)
	}
	if stored.Status.Status != disasterv1.ClusterStatusDeleting {
		t.Fatalf("expected status=%s, got=%s", disasterv1.ClusterStatusDeleting, stored.Status.Status)
	}
	if stored.Status.Reason != "Deleting" {
		t.Fatalf("expected reason=Deleting, got=%s", stored.Status.Reason)
	}
	if stored.Status.Message != "Cluster is being deleted" {
		t.Fatalf("unexpected deleting message: %s", stored.Status.Message)
	}
	if len(stored.Finalizers) == 0 {
		t.Fatalf("expected finalizer to remain until cleanup pass runs")
	}
}

func TestHandleDeleteSkipsTargetCleanupForUnacceptedLicenseRejectedCluster(t *testing.T) {
	ctx := context.Background()
	scheme := buildCleanupTestScheme(t)

	cluster := &disasterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "license-rejected-delete",
			Finalizers: []string{LabelClusterFinalizer},
		},
		Status: disasterv1.ClusterStatus{
			Status:  disasterv1.ClusterStatusNotReady,
			Reason:  platformlicense.ReasonLicenseLimitExceeded,
			Message: "cluster license limit exceeded",
		},
	}

	cli := fakeclient.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&disasterv1.Cluster{}).
		WithObjects(cluster).
		Build()

	if err := cli.Delete(ctx, cluster); err != nil {
		t.Fatalf("delete cluster failed: %v", err)
	}

	latest := &disasterv1.Cluster{}
	if err := cli.Get(ctx, types.NamespacedName{Name: cluster.Name}, latest); err != nil {
		t.Fatalf("get deleting cluster failed: %v", err)
	}

	r := &ClusterReconciler{
		Client:             cli,
		Scheme:             scheme,
		Recorder:           record.NewFakeRecorder(20),
		LicenseGateEnabled: true,
	}
	res, err := r.handleDelete(ctx, latest)
	if err != nil {
		t.Fatalf("first handleDelete failed: %v", err)
	}
	if !res.Requeue {
		t.Fatalf("expected first delete pass to requeue after persisting deleting status")
	}

	if err := cli.Get(ctx, types.NamespacedName{Name: cluster.Name}, latest); err != nil {
		t.Fatalf("get second-pass deleting cluster failed: %v", err)
	}
	res, err = r.handleDelete(ctx, latest)
	if err != nil {
		t.Fatalf("second handleDelete should not call target cleanup, got err=%v", err)
	}
	if res.Requeue || res.RequeueAfter != 0 {
		t.Fatalf("expected finalizer release without requeue, got result=%+v", res)
	}

	stored := &disasterv1.Cluster{}
	err = cli.Get(ctx, types.NamespacedName{Name: cluster.Name}, stored)
	if apierrors.IsNotFound(err) {
		return
	}
	if err != nil {
		t.Fatalf("get stored cluster failed: %v", err)
	}
	if controllerutil.ContainsFinalizer(stored, LabelClusterFinalizer) {
		t.Fatalf("expected license-rejected cluster finalizer to be removed, finalizers=%v", stored.Finalizers)
	}
}
