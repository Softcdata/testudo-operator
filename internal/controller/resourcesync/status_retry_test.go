package resourcesync

import (
	"context"
	"errors"
	"testing"
	"time"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func TestUpdateResourceSyncStatusWithRetry_RetriesOnConflict(t *testing.T) {
	s := runtime.NewScheme()
	if err := scheme.AddToScheme(s); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	if err := disasterv1.AddToScheme(s); err != nil {
		t.Fatalf("add disaster scheme: %v", err)
	}

	rs := &disasterv1.ResourceSync{
		ObjectMeta: metav1.ObjectMeta{Name: "rs-retry", Namespace: "default"},
		Status: disasterv1.ResourceSyncStatus{
			State: disasterv1.ResourceSyncStateReady,
		},
	}

	statusUpdateCalls := 0
	c := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(rs).
		WithStatusSubresource(rs).
		WithInterceptorFuncs(interceptor.Funcs{
			SubResourceUpdate: func(ctx context.Context, cli client.Client, subResourceName string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
				if subResourceName == "status" {
					statusUpdateCalls++
					if statusUpdateCalls == 1 {
						return apierrors.NewConflict(
							schema.GroupResource{Group: disasterv1.GroupVersion.Group, Resource: "resourcesyncs"},
							obj.GetName(),
							errors.New("simulated conflict"),
						)
					}
				}
				return cli.SubResource(subResourceName).Update(ctx, obj, opts...)
			},
		}).
		Build()

	r := &ResourceSyncReconciler{Client: c, Scheme: s}
	if err := r.updateResourceSyncStatusWithRetry(context.Background(), rs, func(latest *disasterv1.ResourceSync) bool {
		latest.Status.State = disasterv1.ResourceSyncStateInProgress
		return true
	}); err != nil {
		t.Fatalf("updateResourceSyncStatusWithRetry returned error: %v", err)
	}

	if statusUpdateCalls != 2 {
		t.Fatalf("expected 2 status update attempts, got %d", statusUpdateCalls)
	}
	if rs.Status.State != disasterv1.ResourceSyncStateInProgress {
		t.Fatalf("expected local status to be updated to InProgress, got %s", rs.Status.State)
	}

	updated := &disasterv1.ResourceSync{}
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(rs), updated); err != nil {
		t.Fatalf("get updated resourcesync: %v", err)
	}
	if updated.Status.State != disasterv1.ResourceSyncStateInProgress {
		t.Fatalf("expected persisted status to be InProgress, got %s", updated.Status.State)
	}
}

func TestAppendResourceSyncHistory_UpsertsExistingCycle(t *testing.T) {
	start := metav1.NewTime(time.Now().Add(-2 * time.Minute))
	earlierCompletion := metav1.NewTime(time.Now().Add(-1 * time.Minute))
	laterCompletion := metav1.NewTime(time.Now())

	rs := &disasterv1.ResourceSync{
		Status: disasterv1.ResourceSyncStatus{
			History: []disasterv1.SyncHistoryRecord{
				{
					StartTime:            &start,
					CompletionTime:       &earlierCompletion,
					BackupName:           "backup-1",
					RestoreName:          "restore-1",
					BackupResourceCount:  7,
					RestoreResourceCount: 3,
					Status:               string(disasterv1.PhaseFailed),
				},
			},
		},
	}

	appendResourceSyncHistory(rs, "backup-1", "restore-1", 9, 8, &start, laterCompletion, disasterv1.PhaseSucceeded)

	if len(rs.Status.History) != 1 {
		t.Fatalf("expected one history record after upsert, got %d", len(rs.Status.History))
	}
	record := rs.Status.History[0]
	if record.Status != string(disasterv1.PhaseSucceeded) {
		t.Fatalf("expected history status Succeeded, got %s", record.Status)
	}
	if record.BackupResourceCount != 9 || record.RestoreResourceCount != 8 {
		t.Fatalf("unexpected resource counts: backup=%d restore=%d", record.BackupResourceCount, record.RestoreResourceCount)
	}
	if record.CompletionTime == nil || !record.CompletionTime.Equal(&laterCompletion) {
		t.Fatalf("expected completion time to be updated")
	}
}
