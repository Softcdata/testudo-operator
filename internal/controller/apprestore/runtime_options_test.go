package apprestore

import (
	"testing"
	"time"

	. "github.com/softcdata/testudo-operator/internal/controller"
)

func TestNewRestoreRuntimeConfig_Defaults(t *testing.T) {
	cfg := NewRestoreRuntimeConfig()

	if cfg.RestoreInProgressMaxWaitDefault != RestorePhaseInProgressMaxWait {
		t.Fatalf("unexpected RestoreInProgressMaxWaitDefault: %s", cfg.RestoreInProgressMaxWaitDefault)
	}
	if cfg.RestoreUnknownMaxWaitDefault != RestorePhaseUnknownMaxWait {
		t.Fatalf("unexpected RestoreUnknownMaxWaitDefault: %s", cfg.RestoreUnknownMaxWaitDefault)
	}
	if cfg.ProgressCompleteGrace != 5*time.Minute {
		t.Fatalf("unexpected ProgressCompleteGrace: %s", cfg.ProgressCompleteGrace)
	}
	if cfg.StartupGrace != 5*time.Minute {
		t.Fatalf("unexpected StartupGrace: %s", cfg.StartupGrace)
	}
	if cfg.MissingGrace != 90*time.Second {
		t.Fatalf("unexpected MissingGrace: %s", cfg.MissingGrace)
	}
	if cfg.EmptyStatusGrace != 5*time.Minute {
		t.Fatalf("unexpected EmptyStatusGrace: %s", cfg.EmptyStatusGrace)
	}
	if cfg.PodVolumeRestorePendingMaxWait != 10*time.Minute {
		t.Fatalf("unexpected PodVolumeRestorePendingMaxWait: %s", cfg.PodVolumeRestorePendingMaxWait)
	}
	if cfg.RetryBackoff != 15*time.Second {
		t.Fatalf("unexpected RetryBackoff: %s", cfg.RetryBackoff)
	}
	if cfg.AutoRetryLimit != 1 {
		t.Fatalf("unexpected AutoRetryLimit: %d", cfg.AutoRetryLimit)
	}
	if cfg.AutoRetryLimitProgress != 1 {
		t.Fatalf("unexpected AutoRetryLimitProgress: %d", cfg.AutoRetryLimitProgress)
	}
	if cfg.AutoRetryLimitStartup != 1 {
		t.Fatalf("unexpected AutoRetryLimitStartup: %d", cfg.AutoRetryLimitStartup)
	}
	if cfg.AutoRetryLimitMissing != 2 {
		t.Fatalf("unexpected AutoRetryLimitMissing: %d", cfg.AutoRetryLimitMissing)
	}
	if cfg.AutoRetryLimitEmpty != 2 {
		t.Fatalf("unexpected AutoRetryLimitEmpty: %d", cfg.AutoRetryLimitEmpty)
	}
}

func TestNewRestoreRuntimeConfig_WithOptions(t *testing.T) {
	cfg := NewRestoreRuntimeConfig(
		WithRestoreInProgressMaxWaitDefault(30*time.Minute),
		WithRestoreUnknownMaxWaitDefault(45*time.Minute),
		WithProgressCompleteGrace(2*time.Minute),
		WithStartupGrace(3*time.Minute),
		WithMissingGrace(100*time.Second),
		WithEmptyStatusGrace(6*time.Minute),
		WithPodVolumeRestorePendingMaxWait(4*time.Minute),
		WithRetryBackoff(20*time.Second),
		WithAutoRetryLimit(2),
		WithAutoRetryLimitProgress(3),
		WithAutoRetryLimitStartup(4),
		WithAutoRetryLimitMissing(5),
		WithAutoRetryLimitEmpty(6),
	)

	if cfg.RestoreInProgressMaxWaitDefault != 30*time.Minute {
		t.Fatalf("unexpected RestoreInProgressMaxWaitDefault: %s", cfg.RestoreInProgressMaxWaitDefault)
	}
	if cfg.RestoreUnknownMaxWaitDefault != 45*time.Minute {
		t.Fatalf("unexpected RestoreUnknownMaxWaitDefault: %s", cfg.RestoreUnknownMaxWaitDefault)
	}
	if cfg.ProgressCompleteGrace != 2*time.Minute {
		t.Fatalf("unexpected ProgressCompleteGrace: %s", cfg.ProgressCompleteGrace)
	}
	if cfg.StartupGrace != 3*time.Minute {
		t.Fatalf("unexpected StartupGrace: %s", cfg.StartupGrace)
	}
	if cfg.MissingGrace != 100*time.Second {
		t.Fatalf("unexpected MissingGrace: %s", cfg.MissingGrace)
	}
	if cfg.EmptyStatusGrace != 6*time.Minute {
		t.Fatalf("unexpected EmptyStatusGrace: %s", cfg.EmptyStatusGrace)
	}
	if cfg.PodVolumeRestorePendingMaxWait != 4*time.Minute {
		t.Fatalf("unexpected PodVolumeRestorePendingMaxWait: %s", cfg.PodVolumeRestorePendingMaxWait)
	}
	if cfg.RetryBackoff != 20*time.Second {
		t.Fatalf("unexpected RetryBackoff: %s", cfg.RetryBackoff)
	}
	if cfg.AutoRetryLimit != 2 {
		t.Fatalf("unexpected AutoRetryLimit: %d", cfg.AutoRetryLimit)
	}
	if cfg.AutoRetryLimitProgress != 3 {
		t.Fatalf("unexpected AutoRetryLimitProgress: %d", cfg.AutoRetryLimitProgress)
	}
	if cfg.AutoRetryLimitStartup != 4 {
		t.Fatalf("unexpected AutoRetryLimitStartup: %d", cfg.AutoRetryLimitStartup)
	}
	if cfg.AutoRetryLimitMissing != 5 {
		t.Fatalf("unexpected AutoRetryLimitMissing: %d", cfg.AutoRetryLimitMissing)
	}
	if cfg.AutoRetryLimitEmpty != 6 {
		t.Fatalf("unexpected AutoRetryLimitEmpty: %d", cfg.AutoRetryLimitEmpty)
	}
}

func TestNewRestoreRuntimeConfig_InvalidValuesFallback(t *testing.T) {
	cfg := NewRestoreRuntimeConfig(
		WithRestoreInProgressMaxWaitDefault(0),
		WithRestoreUnknownMaxWaitDefault(0),
		WithProgressCompleteGrace(0),
		WithStartupGrace(0),
		WithMissingGrace(0),
		WithEmptyStatusGrace(0),
		WithPodVolumeRestorePendingMaxWait(0),
		WithRetryBackoff(0),
		WithAutoRetryLimit(-1),
		WithAutoRetryLimitProgress(-1),
		WithAutoRetryLimitStartup(-1),
		WithAutoRetryLimitMissing(-1),
		WithAutoRetryLimitEmpty(-1),
	)

	if cfg.RestoreInProgressMaxWaitDefault != RestorePhaseInProgressMaxWait {
		t.Fatalf("expected RestoreInProgressMaxWaitDefault fallback, got %s", cfg.RestoreInProgressMaxWaitDefault)
	}
	if cfg.RestoreUnknownMaxWaitDefault != RestorePhaseUnknownMaxWait {
		t.Fatalf("expected RestoreUnknownMaxWaitDefault fallback, got %s", cfg.RestoreUnknownMaxWaitDefault)
	}
	if cfg.ProgressCompleteGrace != 5*time.Minute {
		t.Fatalf("expected ProgressCompleteGrace fallback, got %s", cfg.ProgressCompleteGrace)
	}
	if cfg.StartupGrace != 5*time.Minute {
		t.Fatalf("expected StartupGrace fallback, got %s", cfg.StartupGrace)
	}
	if cfg.MissingGrace != 90*time.Second {
		t.Fatalf("expected MissingGrace fallback, got %s", cfg.MissingGrace)
	}
	if cfg.EmptyStatusGrace != 5*time.Minute {
		t.Fatalf("expected EmptyStatusGrace fallback, got %s", cfg.EmptyStatusGrace)
	}
	if cfg.PodVolumeRestorePendingMaxWait != 10*time.Minute {
		t.Fatalf("expected PodVolumeRestorePendingMaxWait fallback, got %s", cfg.PodVolumeRestorePendingMaxWait)
	}
	if cfg.RetryBackoff != 15*time.Second {
		t.Fatalf("expected RetryBackoff fallback, got %s", cfg.RetryBackoff)
	}
	if cfg.AutoRetryLimit != 0 {
		t.Fatalf("expected non-negative AutoRetryLimit fallback to 0, got %d", cfg.AutoRetryLimit)
	}
	if cfg.AutoRetryLimitProgress != 0 {
		t.Fatalf("expected non-negative AutoRetryLimitProgress fallback to 0, got %d", cfg.AutoRetryLimitProgress)
	}
	if cfg.AutoRetryLimitStartup != 0 {
		t.Fatalf("expected non-negative AutoRetryLimitStartup fallback to 0, got %d", cfg.AutoRetryLimitStartup)
	}
	if cfg.AutoRetryLimitMissing != 0 {
		t.Fatalf("expected non-negative AutoRetryLimitMissing fallback to 0, got %d", cfg.AutoRetryLimitMissing)
	}
	if cfg.AutoRetryLimitEmpty != 0 {
		t.Fatalf("expected non-negative AutoRetryLimitEmpty fallback to 0, got %d", cfg.AutoRetryLimitEmpty)
	}
}

func TestNewRestoreRuntimeConfig_AutoRetryLimitCompatibilityFallback(t *testing.T) {
	cfg := NewRestoreRuntimeConfig(WithAutoRetryLimit(3))

	if cfg.AutoRetryLimitProgress != 3 {
		t.Fatalf("expected AutoRetryLimitProgress fallback to AutoRetryLimit, got %d", cfg.AutoRetryLimitProgress)
	}
	if cfg.AutoRetryLimitStartup != 3 {
		t.Fatalf("expected AutoRetryLimitStartup fallback to AutoRetryLimit, got %d", cfg.AutoRetryLimitStartup)
	}
	if cfg.AutoRetryLimitMissing != 3 {
		t.Fatalf("expected AutoRetryLimitMissing fallback to AutoRetryLimit, got %d", cfg.AutoRetryLimitMissing)
	}
	if cfg.AutoRetryLimitEmpty != 3 {
		t.Fatalf("expected AutoRetryLimitEmpty fallback to AutoRetryLimit, got %d", cfg.AutoRetryLimitEmpty)
	}
}

func TestNewRestoreRuntimeConfig_AutoRetryLimitWithPerTypeOverride(t *testing.T) {
	cfg := NewRestoreRuntimeConfig(
		WithAutoRetryLimit(2),
		WithAutoRetryLimitMissing(4),
	)

	if cfg.AutoRetryLimitProgress != 2 {
		t.Fatalf("expected AutoRetryLimitProgress to inherit AutoRetryLimit, got %d", cfg.AutoRetryLimitProgress)
	}
	if cfg.AutoRetryLimitStartup != 2 {
		t.Fatalf("expected AutoRetryLimitStartup to inherit AutoRetryLimit, got %d", cfg.AutoRetryLimitStartup)
	}
	if cfg.AutoRetryLimitMissing != 4 {
		t.Fatalf("expected AutoRetryLimitMissing to keep explicit override, got %d", cfg.AutoRetryLimitMissing)
	}
	if cfg.AutoRetryLimitEmpty != 2 {
		t.Fatalf("expected AutoRetryLimitEmpty to inherit AutoRetryLimit, got %d", cfg.AutoRetryLimitEmpty)
	}
}
