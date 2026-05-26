package controller

import (
	"context"
	"testing"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newDisasterConfigTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := scheme.AddToScheme(s); err != nil {
		t.Fatalf("add core scheme failed: %v", err)
	}
	if err := disasterv1.AddToScheme(s); err != nil {
		t.Fatalf("add disaster scheme failed: %v", err)
	}
	return s
}

func TestDisasterConfigReconcileSetsSourceClusterNotFound(t *testing.T) {
	ctx := context.Background()
	s := newDisasterConfigTestScheme(t)

	cfg := &disasterv1.DisasterConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "cfg-a",
			Finalizers: []string{ConfigFinalizer},
		},
		Spec: disasterv1.DisasterConfigSpec{
			SourceCluster:     "missing-source",
			TargetCluster:     "target",
			StorageRepository: "repo",
		},
		Status: disasterv1.DisasterConfigStatus{
			Status: disasterv1.DisasterConfigStatusPending,
		},
	}

	c := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(cfg).
		WithStatusSubresource(cfg).
		Build()

	r := &DisasterConfigReconciler{
		Client: c,
		Scheme: s,
	}

	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: cfg.Name}}); err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}

	updated := &disasterv1.DisasterConfig{}
	if err := c.Get(ctx, types.NamespacedName{Name: cfg.Name}, updated); err != nil {
		t.Fatalf("get updated config failed: %v", err)
	}

	if updated.Status.Status != disasterv1.DisasterConfigStatusError {
		t.Fatalf("unexpected status: got %q want %q", updated.Status.Status, disasterv1.DisasterConfigStatusError)
	}
	if updated.Status.Reason != configReasonSourceClusterNotFound {
		t.Fatalf("unexpected reason: got %q want %q", updated.Status.Reason, configReasonSourceClusterNotFound)
	}
	if updated.Status.Message == "" {
		t.Fatalf("expected non-empty message")
	}
}
