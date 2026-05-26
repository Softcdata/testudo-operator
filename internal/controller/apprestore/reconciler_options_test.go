package apprestore

import (
	"testing"
	"time"

	controller "github.com/softcdata/testudo-operator/internal/controller"
	"github.com/softcdata/testudo-operator/pkg/helper"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestNewAppRestoreReconciler_Defaults(t *testing.T) {
	scheme := newPVRTestScheme(t)
	cli := fake.NewClientBuilder().WithScheme(scheme).Build()

	r := NewAppRestoreReconciler(cli, scheme, nil)
	if r == nil {
		t.Fatalf("expected reconciler, got nil")
	}
	if r.Client == nil {
		t.Fatalf("expected client to be set")
	}
	if r.ClientFactory == nil {
		t.Fatalf("expected default client factory to be set")
	}
	if r.StatsHelper == nil {
		t.Fatalf("expected default stats helper to be set")
	}
	cfg := r.restoreRuntimeConfig()
	if cfg.AutoRetryLimit != 1 {
		t.Fatalf("expected default auto retry limit 1, got %d", cfg.AutoRetryLimit)
	}
}

func TestNewAppRestoreReconciler_WithOptions(t *testing.T) {
	scheme := newPVRTestScheme(t)
	cli := fake.NewClientBuilder().WithScheme(scheme).Build()

	mockClientFactory := &controller.MockClientFactory{}
	mockStats := helper.NewStatisticsHelper(cli)

	r := NewAppRestoreReconciler(
		cli,
		scheme,
		nil,
		WithClientFactory(mockClientFactory),
		WithStatsHelper(mockStats),
		WithRestoreRuntime(
			WithProgressCompleteGrace(2*time.Minute),
			WithAutoRetryLimit(2),
		),
	)

	if r.ClientFactory != mockClientFactory {
		t.Fatalf("expected client factory option to be applied")
	}
	if r.StatsHelper != mockStats {
		t.Fatalf("expected stats helper option to be applied")
	}
	cfg := r.restoreRuntimeConfig()
	if cfg.ProgressCompleteGrace != 2*time.Minute {
		t.Fatalf("expected progress complete grace to be 2m, got %s", cfg.ProgressCompleteGrace)
	}
	if cfg.AutoRetryLimit != 2 {
		t.Fatalf("expected auto retry limit to be 2, got %d", cfg.AutoRetryLimit)
	}
}
