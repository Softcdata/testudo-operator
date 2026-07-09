package runtimeconfig

import (
	"strings"
	"testing"
	"time"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func durationPtr(v time.Duration) *metav1.Duration {
	return &metav1.Duration{Duration: v}
}

func int32Ptr(v int32) *int32 {
	return &v
}

func TestDefaultSnapshotIsValid(t *testing.T) {
	if errs := Validate(DefaultSnapshot()); len(errs) > 0 {
		t.Fatalf("default snapshot should be valid: %v", errs)
	}
}

func TestMergeSpecRetryLimitFallback(t *testing.T) {
	base := DefaultSnapshot()
	next, errs := MergeSpec(base, disasterv1.OperatorRuntimeConfigSpec{
		RestoreRuntime: &disasterv1.RestoreRuntimeConfigSpec{
			RetryLimit:        int32Ptr(3),
			RetryLimitMissing: int32Ptr(1),
		},
	}, 7)
	if len(errs) > 0 {
		t.Fatalf("unexpected validation errors: %v", errs)
	}
	if next.Generation != 7 {
		t.Fatalf("expected generation 7, got %d", next.Generation)
	}
	if next.RestoreRuntime.RetryLimitProgress != 3 ||
		next.RestoreRuntime.RetryLimitStartup != 3 ||
		next.RestoreRuntime.RetryLimitEmpty != 3 {
		t.Fatalf("generic retryLimit should be inherited by unset per-type limits: %#v", next.RestoreRuntime)
	}
	if next.RestoreRuntime.RetryLimitMissing != 1 {
		t.Fatalf("explicit retryLimitMissing should win, got %d", next.RestoreRuntime.RetryLimitMissing)
	}
}

func TestMergeSpecRejectsSemanticRangeErrors(t *testing.T) {
	_, errs := MergeSpec(DefaultSnapshot(), disasterv1.OperatorRuntimeConfigSpec{
		RestoreRuntime: &disasterv1.RestoreRuntimeConfigSpec{
			RetryBackoff: durationPtr(0),
		},
	}, 1)
	if len(errs) == 0 {
		t.Fatal("expected validation error")
	}
	if got := FormatErrors(errs); !strings.Contains(got, "restoreRuntime.retryBackoff") {
		t.Fatalf("expected field path in error, got %q", got)
	}
}

func TestActiveSnapshotKeepsLastValidUntilReset(t *testing.T) {
	ResetForTest()
	startup := DefaultSnapshot()
	startup.RestoreRuntime.RetryBackoff = 20 * time.Second
	SetStartupDefaults(startup)

	valid, errs := MergeSpec(StartupSnapshot(), disasterv1.OperatorRuntimeConfigSpec{
		RestoreRuntime: &disasterv1.RestoreRuntimeConfigSpec{
			RetryBackoff: durationPtr(30 * time.Second),
		},
	}, 2)
	if len(errs) > 0 {
		t.Fatalf("unexpected validation errors: %v", errs)
	}
	Activate(valid)

	invalid, errs := MergeSpec(StartupSnapshot(), disasterv1.OperatorRuntimeConfigSpec{
		RestoreRuntime: &disasterv1.RestoreRuntimeConfigSpec{
			RetryBackoff: durationPtr(0),
		},
	}, 3)
	if len(errs) == 0 {
		t.Fatal("expected invalid snapshot")
	}
	if invalid.RestoreRuntime.RetryBackoff != 0 {
		t.Fatalf("test setup expected invalid value to be present before activation, got %s", invalid.RestoreRuntime.RetryBackoff)
	}
	if got := SnapshotCurrent().RestoreRuntime.RetryBackoff; got != 30*time.Second {
		t.Fatalf("invalid config should not replace last valid snapshot, got %s", got)
	}

	reset := ResetToStartupDefaults()
	if reset.RestoreRuntime.RetryBackoff != 20*time.Second {
		t.Fatalf("reset should return to startup defaults, got %s", reset.RestoreRuntime.RetryBackoff)
	}
}
