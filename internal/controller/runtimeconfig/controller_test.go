package runtimeconfig

import (
	"context"
	"testing"
	"time"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newRuntimeConfigTestReconciler(t *testing.T, objects ...client.Object) (*Reconciler, client.Client) {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := disasterv1.AddToScheme(scheme); err != nil {
		t.Fatalf("add disaster scheme: %v", err)
	}
	cli := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objects...).
		WithStatusSubresource(&disasterv1.OperatorRuntimeConfig{}).
		Build()
	return &Reconciler{
		Client:    cli,
		Scheme:    scheme,
		Namespace: "disaster-system",
	}, cli
}

func TestReconcileActivatesValidConfigAndUpdatesReadyStatus(t *testing.T) {
	ResetForTest()
	t.Cleanup(ResetForTest)

	cfg := &disasterv1.OperatorRuntimeConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:       SingletonName,
			Namespace:  "disaster-system",
			Generation: 2,
		},
		Spec: disasterv1.OperatorRuntimeConfigSpec{
			RestoreRuntime: &disasterv1.RestoreRuntimeConfigSpec{
				RetryBackoff: durationPtr(30 * time.Second),
			},
		},
	}
	reconciler, cli := newRuntimeConfigTestReconciler(t, cfg)

	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: cfg.Namespace, Name: cfg.Name}}); err != nil {
		t.Fatalf("reconcile valid config: %v", err)
	}
	if got := SnapshotCurrent().RestoreRuntime.RetryBackoff; got != 30*time.Second {
		t.Fatalf("expected active retryBackoff=30s, got %s", got)
	}

	updated := &disasterv1.OperatorRuntimeConfig{}
	if err := cli.Get(context.Background(), client.ObjectKeyFromObject(cfg), updated); err != nil {
		t.Fatalf("get updated config: %v", err)
	}
	ready := meta.FindStatusCondition(updated.Status.Conditions, disasterv1.OperatorRuntimeConfigConditionReady)
	if ready == nil || ready.Status != metav1.ConditionTrue {
		t.Fatalf("expected Ready=True, got %#v", ready)
	}
	if updated.Status.ActiveGeneration != 2 {
		t.Fatalf("expected ActiveGeneration=2, got %d", updated.Status.ActiveGeneration)
	}
}

func TestReconcileInvalidConfigKeepsLastValidSnapshot(t *testing.T) {
	ResetForTest()
	t.Cleanup(ResetForTest)

	startup := DefaultSnapshot()
	startup.RestoreRuntime.RetryBackoff = 20 * time.Second
	SetStartupDefaults(startup)
	valid := startup
	valid.RestoreRuntime.RetryBackoff = 30 * time.Second
	Activate(valid)

	cfg := &disasterv1.OperatorRuntimeConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:       SingletonName,
			Namespace:  "disaster-system",
			Generation: 3,
		},
		Spec: disasterv1.OperatorRuntimeConfigSpec{
			RestoreRuntime: &disasterv1.RestoreRuntimeConfigSpec{
				RetryBackoff: durationPtr(0),
			},
		},
	}
	reconciler, cli := newRuntimeConfigTestReconciler(t, cfg)

	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: cfg.Namespace, Name: cfg.Name}}); err != nil {
		t.Fatalf("reconcile invalid config: %v", err)
	}
	if got := SnapshotCurrent().RestoreRuntime.RetryBackoff; got != 30*time.Second {
		t.Fatalf("expected last valid retryBackoff=30s, got %s", got)
	}

	updated := &disasterv1.OperatorRuntimeConfig{}
	if err := cli.Get(context.Background(), client.ObjectKeyFromObject(cfg), updated); err != nil {
		t.Fatalf("get updated config: %v", err)
	}
	invalid := meta.FindStatusCondition(updated.Status.Conditions, disasterv1.OperatorRuntimeConfigConditionInvalid)
	if invalid == nil || invalid.Status != metav1.ConditionTrue {
		t.Fatalf("expected Invalid=True, got %#v", invalid)
	}
}

func TestReconcileDeleteFallsBackToStartupDefaults(t *testing.T) {
	ResetForTest()
	t.Cleanup(ResetForTest)

	startup := DefaultSnapshot()
	startup.RestoreRuntime.RetryBackoff = 20 * time.Second
	SetStartupDefaults(startup)
	active := startup
	active.RestoreRuntime.RetryBackoff = 30 * time.Second
	Activate(active)

	reconciler, _ := newRuntimeConfigTestReconciler(t)
	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "disaster-system", Name: SingletonName}}); err != nil {
		t.Fatalf("reconcile deleted config: %v", err)
	}
	if got := SnapshotCurrent().RestoreRuntime.RetryBackoff; got != 20*time.Second {
		t.Fatalf("expected startup retryBackoff=20s after delete, got %s", got)
	}
}
