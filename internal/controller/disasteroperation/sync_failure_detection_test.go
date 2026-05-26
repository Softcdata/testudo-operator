package disasteroperation

import (
	"testing"
	"time"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestHasResourceSyncFailureSince_StateFailedWithRecentLastSync(t *testing.T) {
	triggeredAt := time.Now().Add(-1 * time.Minute)
	rs := &disasterv1.ResourceSync{
		Status: disasterv1.ResourceSyncStatus{
			State:        disasterv1.ResourceSyncStateFailed,
			LastSyncTime: &metav1.Time{Time: time.Now()},
		},
	}

	failed, reason := hasResourceSyncFailureSince(rs, triggeredAt)
	if !failed {
		t.Fatalf("expected failed=true")
	}
	if reason == "" {
		t.Fatalf("expected non-empty reason")
	}
}

func TestHasResourceSyncFailureSince_ConditionFailureAfterTrigger(t *testing.T) {
	triggeredAt := time.Now().Add(-1 * time.Minute)
	rs := &disasterv1.ResourceSync{
		Status: disasterv1.ResourceSyncStatus{
			State: disasterv1.ResourceSyncStateInProgress,
			Conditions: []metav1.Condition{
				{
					Type:               "BuildRestoreSpecFailed",
					Status:             metav1.ConditionTrue,
					LastTransitionTime: metav1.Time{Time: time.Now()},
					Reason:             "BuildRestoreSpecFailed",
					Message:            "build apprestore failed",
				},
			},
		},
	}

	failed, reason := hasResourceSyncFailureSince(rs, triggeredAt)
	if !failed {
		t.Fatalf("expected failed=true")
	}
	if reason != "build apprestore failed" {
		t.Fatalf("unexpected reason: %s", reason)
	}
}

func TestHasResourceSyncFailureSince_OldFailureIgnored(t *testing.T) {
	triggeredAt := time.Now()
	old := time.Now().Add(-10 * time.Minute)
	rs := &disasterv1.ResourceSync{
		Status: disasterv1.ResourceSyncStatus{
			State: disasterv1.ResourceSyncStateFailed,
			Conditions: []metav1.Condition{
				{
					Type:               "BuildRestoreSpecFailed",
					Status:             metav1.ConditionTrue,
					LastTransitionTime: metav1.Time{Time: old},
					Reason:             "BuildRestoreSpecFailed",
					Message:            "old failure",
				},
			},
			LastSyncTime: &metav1.Time{Time: old},
		},
	}

	failed, _ := hasResourceSyncFailureSince(rs, triggeredAt)
	if failed {
		t.Fatalf("expected failed=false for stale failure")
	}
}
