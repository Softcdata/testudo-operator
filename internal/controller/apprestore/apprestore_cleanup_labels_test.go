package apprestore

import (
	"context"
	"testing"

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

func newAppRestoreTestScheme(t *testing.T) *runtime.Scheme {
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

func TestCreateVeleroRestoreWritesCleanupLabels(t *testing.T) {
	ctx := context.Background()
	scheme := newAppRestoreTestScheme(t)
	cli := fake.NewClientBuilder().WithScheme(scheme).Build()
	reconciler := &AppRestoreReconciler{
		Scheme:   scheme,
		Recorder: record.NewFakeRecorder(10),
	}

	appRestore := &disasterv1.AppRestore{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "restore-cleanup-test",
			Namespace: "default",
			UID:       types.UID("apprestore-cleanup-uid"),
			Annotations: map[string]string{
				AnnotationTraceID: "trace-restore-cleanup",
			},
		},
		Spec: disasterv1.AppRestoreSpec{
			Cluster: "cluster-a",
			Template: velerov1.RestoreSpec{
				BackupName: "backup-a",
			},
			ResourceModifierRules: []disasterv1.ResourceModifierRule{{
				Conditions: disasterv1.Conditions{GroupResource: "deployments.apps"},
				Patches:    []disasterv1.JSONPatch{{Operation: "replace", Path: "/spec/replicas", Value: "1"}},
			}},
		},
	}

	if err := reconciler.createVeleroRestore(ctx, cli, appRestore); err != nil {
		t.Fatalf("createVeleroRestore returned error: %v", err)
	}

	ownerToken := BuildDependencyToken(string(appRestore.UID))
	restore := &velerov1.Restore{}
	restoreName := reconciler.GenRestoreName(appRestore)
	if err := cli.Get(ctx, ctrlclient.ObjectKey{Name: restoreName, Namespace: controller.VeleroNamespace}, restore); err != nil {
		t.Fatalf("get created restore: %v", err)
	}
	if got := restore.Labels[LabelCleanupOwnerToken]; got != ownerToken {
		t.Fatalf("unexpected restore cleanup owner token: got %q want %q", got, ownerToken)
	}
	if got := restore.Labels[LabelCleanupRelation]; got != "finalizer.veleroRestore" {
		t.Fatalf("unexpected restore cleanup relation: %q", got)
	}
	if got := restore.Labels[LabelCleanupStrategy]; got != CleanupStrategyDelete {
		t.Fatalf("unexpected restore cleanup strategy: %q", got)
	}

	cmManager := NewConfigMapManager(cli)
	cmName := cmManager.generateConfigMapName(appRestore)
	configMap := &corev1.ConfigMap{}
	if err := cli.Get(ctx, ctrlclient.ObjectKey{Name: cmName, Namespace: controller.VeleroNamespace}, configMap); err != nil {
		t.Fatalf("get created configmap: %v", err)
	}
	if got := configMap.Labels[LabelCleanupOwnerToken]; got != ownerToken {
		t.Fatalf("unexpected configmap cleanup owner token: got %q want %q", got, ownerToken)
	}
	if got := configMap.Labels[LabelCleanupRelation]; got != "finalizer.resourceModifierConfigMap" {
		t.Fatalf("unexpected configmap cleanup relation: %q", got)
	}
	if got := configMap.Labels[LabelCleanupStrategy]; got != CleanupStrategyDelete {
		t.Fatalf("unexpected configmap cleanup strategy: %q", got)
	}
	if restore.Spec.ResourceModifier == nil || restore.Spec.ResourceModifier.Name != cmName {
		t.Fatalf("expected restore resourceModifier to reference configmap %q", cmName)
	}
}

func TestGetVeleroRestoreBackfillsCleanupLabels(t *testing.T) {
	ctx := context.Background()
	scheme := newAppRestoreTestScheme(t)
	cli := fake.NewClientBuilder().WithScheme(scheme).Build()
	reconciler := &AppRestoreReconciler{
		Scheme:   scheme,
		Recorder: record.NewFakeRecorder(10),
	}

	appRestore := &disasterv1.AppRestore{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "restore-backfill-test",
			Namespace: "default",
			UID:       types.UID("apprestore-backfill-uid"),
			Annotations: map[string]string{
				AnnotationTraceID: "trace-restore-backfill",
			},
		},
		Spec: disasterv1.AppRestoreSpec{
			Cluster: "cluster-a",
			Template: velerov1.RestoreSpec{
				BackupName: "backup-a",
			},
		},
	}

	// Create a legacy restore resource without cleanup labels.
	restoreName := "res-" + appRestore.Name
	restore := &velerov1.Restore{
		ObjectMeta: metav1.ObjectMeta{
			Name:      restoreName,
			Namespace: controller.VeleroNamespace,
			Labels: map[string]string{
				LabelAppRestoreName: appRestore.Name,
				LabelAppRestoreUID:  string(appRestore.UID),
				AnnotationTraceID:   appRestore.Annotations[AnnotationTraceID],
			},
		},
		Spec: appRestore.Spec.Template,
	}
	if err := cli.Create(ctx, restore); err != nil {
		t.Fatalf("create legacy restore: %v", err)
	}

	got, err := reconciler.getVeleroRestore(ctx, cli, appRestore)
	if err != nil {
		t.Fatalf("getVeleroRestore returned error: %v", err)
	}
	if got == nil {
		t.Fatalf("expected restore to be found")
	}

	// The helper should have backfilled cleanup labels.
	stored := &velerov1.Restore{}
	if err := cli.Get(ctx, ctrlclient.ObjectKey{Name: restoreName, Namespace: controller.VeleroNamespace}, stored); err != nil {
		t.Fatalf("get stored restore: %v", err)
	}

	ownerToken := BuildDependencyToken(string(appRestore.UID))
	if got := stored.Labels[LabelCleanupOwnerToken]; got != ownerToken {
		t.Fatalf("unexpected restore cleanup owner token: got %q want %q", got, ownerToken)
	}
	if got := stored.Labels[LabelCleanupRelation]; got != "finalizer.veleroRestore" {
		t.Fatalf("unexpected restore cleanup relation: %q", got)
	}
	if got := stored.Labels[LabelCleanupStrategy]; got != CleanupStrategyDelete {
		t.Fatalf("unexpected restore cleanup strategy: %q", got)
	}
	if got := stored.Labels[LabelCleanupManagedBy]; got != LabelCleanupManagedByValueOperator {
		t.Fatalf("unexpected restore cleanup managed-by: %q", got)
	}
}
