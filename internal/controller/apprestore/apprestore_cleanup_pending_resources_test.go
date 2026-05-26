package apprestore

import (
	"context"
	"testing"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newPendingCleanupTestClient(t *testing.T, objs ...ctrlclient.Object) ctrlclient.Client {
	t.Helper()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatalf("add apps scheme: %v", err)
	}
	if err := batchv1.AddToScheme(scheme); err != nil {
		t.Fatalf("add batch scheme: %v", err)
	}
	if err := disasterv1.AddToScheme(scheme); err != nil {
		t.Fatalf("add disaster scheme: %v", err)
	}
	if err := velerov1.AddToScheme(scheme); err != nil {
		t.Fatalf("add velero scheme: %v", err)
	}
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
}

func int32Ptr(v int32) *int32 {
	return &v
}

func TestCleanupPendingRestoredResources_DeploymentDeletionBehavior(t *testing.T) {
	ctx := context.Background()
	restoreName := "res-e2e-case"
	ns := "app-ns"

	appRestore := &disasterv1.AppRestore{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ar-cleanup-test",
			Namespace: "default",
		},
		Spec: disasterv1.AppRestoreSpec{
			Cluster: "target-cluster",
			Template: velerov1.RestoreSpec{
				IncludedNamespaces: []string{ns},
			},
		},
	}

	deployScaledZero := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "deploy-scaled-zero",
			Namespace: ns,
			Labels: map[string]string{
				"velero.io/restore-name": restoreName,
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(0),
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "scaled-zero"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "scaled-zero"}},
			},
		},
		Status: appsv1.DeploymentStatus{
			ReadyReplicas: 0,
		},
	}

	deployPending := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "deploy-pending",
			Namespace: ns,
			Labels: map[string]string{
				"velero.io/restore-name": restoreName,
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(1),
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "pending"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "pending"}},
			},
		},
		Status: appsv1.DeploymentStatus{
			ReadyReplicas: 0,
		},
	}

	deployReady := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "deploy-ready",
			Namespace: ns,
			Labels: map[string]string{
				"velero.io/restore-name": restoreName,
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(1),
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "ready"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "ready"}},
			},
		},
		Status: appsv1.DeploymentStatus{
			ReadyReplicas: 1,
		},
	}

	deployNoRestoreLabel := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "deploy-no-restore-label",
			Namespace: ns,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(1),
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "no-label"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "no-label"}},
			},
		},
		Status: appsv1.DeploymentStatus{
			ReadyReplicas: 0,
		},
	}

	cli := newPendingCleanupTestClient(
		t,
		appRestore,
		deployScaledZero,
		deployPending,
		deployReady,
		deployNoRestoreLabel,
	)
	reconciler := &AppRestoreReconciler{
		Recorder: record.NewFakeRecorder(10),
	}

	if err := reconciler.cleanupPendingRestoredResources(ctx, cli, appRestore, restoreName); err != nil {
		t.Fatalf("cleanupPendingRestoredResources returned error: %v", err)
	}

	// replicas=0 的 Deployment 不应被删
	stillScaledZero := &appsv1.Deployment{}
	if err := cli.Get(ctx, ctrlclient.ObjectKey{Name: deployScaledZero.Name, Namespace: ns}, stillScaledZero); err != nil {
		t.Fatalf("expected scaled-zero deployment still exists, got error: %v", err)
	}

	// replicas>0 且 ready=0 的 Deployment 会被删
	deletedPending := &appsv1.Deployment{}
	if err := cli.Get(ctx, ctrlclient.ObjectKey{Name: deployPending.Name, Namespace: ns}, deletedPending); !apierrors.IsNotFound(err) {
		t.Fatalf("expected pending deployment deleted, got err=%v", err)
	}

	// ready=1 的 Deployment 不应被删
	stillReady := &appsv1.Deployment{}
	if err := cli.Get(ctx, ctrlclient.ObjectKey{Name: deployReady.Name, Namespace: ns}, stillReady); err != nil {
		t.Fatalf("expected ready deployment still exists, got error: %v", err)
	}

	// 无 restore label 的 Deployment 不应受影响
	stillNoLabel := &appsv1.Deployment{}
	if err := cli.Get(ctx, ctrlclient.ObjectKey{Name: deployNoRestoreLabel.Name, Namespace: ns}, stillNoLabel); err != nil {
		t.Fatalf("expected no-label deployment still exists, got error: %v", err)
	}
}
