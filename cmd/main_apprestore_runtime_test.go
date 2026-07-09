package main

import (
	"testing"
	"time"

	"github.com/softcdata/testudo-operator/internal/controller/apprestore"
)

func TestLoadAppRestoreRuntimeOptions_FromEnv(t *testing.T) {
	t.Setenv("APPRESTORE_IN_PROGRESS_MAX_WAIT", "3h")
	t.Setenv("APPRESTORE_UNKNOWN_MAX_WAIT", "45m")
	t.Setenv("APPRESTORE_PROGRESS_COMPLETE_GRACE", "30m")
	t.Setenv("APPRESTORE_STARTUP_GRACE", "10m")
	t.Setenv("APPRESTORE_MISSING_GRACE", "2m")
	t.Setenv("APPRESTORE_EMPTY_STATUS_GRACE", "4m")
	t.Setenv("APPRESTORE_PVR_PENDING_MAX_WAIT", "15m")
	t.Setenv("APPRESTORE_RETRY_LIMIT", "2")
	t.Setenv("APPRESTORE_RETRY_LIMIT_PROGRESS", "3")
	t.Setenv("APPRESTORE_RETRY_LIMIT_STARTUP", "4")
	t.Setenv("APPRESTORE_RETRY_LIMIT_MISSING", "5")
	t.Setenv("APPRESTORE_RETRY_LIMIT_EMPTY", "6")
	t.Setenv("APPRESTORE_RETRY_BACKOFF", "25s")

	cfg := apprestore.NewRestoreRuntimeConfig(loadAppRestoreRuntimeOptions()...)
	if cfg.RestoreInProgressMaxWaitDefault != 3*time.Hour {
		t.Fatalf("expected RestoreInProgressMaxWaitDefault=3h, got %s", cfg.RestoreInProgressMaxWaitDefault)
	}
	if cfg.RestoreUnknownMaxWaitDefault != 45*time.Minute {
		t.Fatalf("expected RestoreUnknownMaxWaitDefault=45m, got %s", cfg.RestoreUnknownMaxWaitDefault)
	}
	if cfg.ProgressCompleteGrace != 30*time.Minute {
		t.Fatalf("expected ProgressCompleteGrace=30m, got %s", cfg.ProgressCompleteGrace)
	}
	if cfg.StartupGrace != 10*time.Minute {
		t.Fatalf("expected StartupGrace=10m, got %s", cfg.StartupGrace)
	}
	if cfg.MissingGrace != 2*time.Minute {
		t.Fatalf("expected MissingGrace=2m, got %s", cfg.MissingGrace)
	}
	if cfg.EmptyStatusGrace != 4*time.Minute {
		t.Fatalf("expected EmptyStatusGrace=4m, got %s", cfg.EmptyStatusGrace)
	}
	if cfg.PodVolumeRestorePendingMaxWait != 15*time.Minute {
		t.Fatalf("expected PodVolumeRestorePendingMaxWait=15m, got %s", cfg.PodVolumeRestorePendingMaxWait)
	}
	if cfg.AutoRetryLimit != 2 {
		t.Fatalf("expected AutoRetryLimit=2, got %d", cfg.AutoRetryLimit)
	}
	if cfg.AutoRetryLimitProgress != 3 {
		t.Fatalf("expected AutoRetryLimitProgress=3, got %d", cfg.AutoRetryLimitProgress)
	}
	if cfg.AutoRetryLimitStartup != 4 {
		t.Fatalf("expected AutoRetryLimitStartup=4, got %d", cfg.AutoRetryLimitStartup)
	}
	if cfg.AutoRetryLimitMissing != 5 {
		t.Fatalf("expected AutoRetryLimitMissing=5, got %d", cfg.AutoRetryLimitMissing)
	}
	if cfg.AutoRetryLimitEmpty != 6 {
		t.Fatalf("expected AutoRetryLimitEmpty=6, got %d", cfg.AutoRetryLimitEmpty)
	}
	if cfg.RetryBackoff != 25*time.Second {
		t.Fatalf("expected RetryBackoff=25s, got %s", cfg.RetryBackoff)
	}
}

func TestLoadAppRestoreRuntimeOptions_InvalidEnvFallsBackToDefaults(t *testing.T) {
	t.Setenv("APPRESTORE_MISSING_GRACE", "bad-duration")
	t.Setenv("APPRESTORE_RETRY_LIMIT_PROGRESS", "bad-int")

	cfg := apprestore.NewRestoreRuntimeConfig(loadAppRestoreRuntimeOptions()...)
	if cfg.MissingGrace != 90*time.Second {
		t.Fatalf("expected MissingGrace default 90s, got %s", cfg.MissingGrace)
	}
	if cfg.AutoRetryLimitProgress != 1 {
		t.Fatalf("expected AutoRetryLimitProgress default 1, got %d", cfg.AutoRetryLimitProgress)
	}
}
