package disastergroup

import (
	"context"
	"testing"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newGroupTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := scheme.AddToScheme(s); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	if err := disasterv1.AddToScheme(s); err != nil {
		t.Fatalf("add disaster scheme: %v", err)
	}
	return s
}

func findCondition(conds []metav1.Condition, condType string) *metav1.Condition {
	for i := range conds {
		if conds[i].Type == condType {
			return &conds[i]
		}
	}
	return nil
}

func TestReconcileSetsGroupErrorWhenInstanceMissing(t *testing.T) {
	ctx := context.Background()
	s := newGroupTestScheme(t)

	group := &disasterv1.DisasterGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "group-a",
			Namespace: "default",
		},
		Spec: disasterv1.DisasterGroupSpec{
			Levels: [][]string{{"missing-inst"}},
		},
	}

	c := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(group).
		WithStatusSubresource(group).
		Build()

	r := &DisasterGroupReconciler{
		Client: c,
		Scheme: s,
		Log:    ctrl.Log.WithName("test-disastergroup"),
	}

	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: group.Name, Namespace: group.Namespace}}
	if _, err := r.Reconcile(ctx, req); err != nil {
		if !apierrors.IsConflict(err) {
			t.Fatalf("reconcile failed: %v", err)
		}
		if _, retryErr := r.Reconcile(ctx, req); retryErr != nil {
			t.Fatalf("reconcile retry after conflict failed: %v", retryErr)
		}
	}

	updated := &disasterv1.DisasterGroup{}
	if err := c.Get(ctx, types.NamespacedName{Name: group.Name, Namespace: group.Namespace}, updated); err != nil {
		t.Fatalf("get updated group failed: %v", err)
	}

	if updated.Status.Reason != groupReasonInstanceNotFound {
		t.Fatalf("unexpected reason: got %q want %q", updated.Status.Reason, groupReasonInstanceNotFound)
	}
	if updated.Status.Message == "" {
		t.Fatalf("expected non-empty message")
	}
	if updated.Status.TotalInstances != 1 || updated.Status.ReadyInstances != 0 {
		t.Fatalf("unexpected aggregate status: total=%d ready=%d", updated.Status.TotalInstances, updated.Status.ReadyInstances)
	}

	errCond := findCondition(updated.Status.Conditions, "Error")
	if errCond == nil {
		t.Fatalf("expected Error condition")
	}
	if errCond.Status != metav1.ConditionTrue {
		t.Fatalf("unexpected Error condition status: %s", errCond.Status)
	}
	if errCond.Reason != groupReasonInstanceNotFound {
		t.Fatalf("unexpected Error condition reason: %q", errCond.Reason)
	}
}

func TestReconcileClearsGroupErrorWhenMembersHealthy(t *testing.T) {
	ctx := context.Background()
	s := newGroupTestScheme(t)

	instance := &disasterv1.DisasterInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "inst-a",
			Namespace: "default",
		},
		Status: disasterv1.DisasterInstanceStatus{
			FsmState: disasterv1.FsmStateProtected,
		},
	}

	group := &disasterv1.DisasterGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "group-a",
			Namespace: "default",
		},
		Spec: disasterv1.DisasterGroupSpec{
			Levels: [][]string{{"inst-a"}},
		},
		Status: disasterv1.DisasterGroupStatus{
			Reason:  groupReasonInstanceFailed,
			Message: "instances in Failed state: inst-a",
			Conditions: []metav1.Condition{
				{
					Type:    "Error",
					Status:  metav1.ConditionTrue,
					Reason:  groupReasonInstanceFailed,
					Message: "instances in Failed state: inst-a",
				},
			},
		},
	}

	c := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(instance, group).
		WithStatusSubresource(instance, group).
		Build()

	r := &DisasterGroupReconciler{
		Client: c,
		Scheme: s,
		Log:    ctrl.Log.WithName("test-disastergroup"),
	}

	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: group.Name, Namespace: group.Namespace}}
	if _, err := r.Reconcile(ctx, req); err != nil {
		if !apierrors.IsConflict(err) {
			t.Fatalf("reconcile failed: %v", err)
		}
		if _, retryErr := r.Reconcile(ctx, req); retryErr != nil {
			t.Fatalf("reconcile retry after conflict failed: %v", retryErr)
		}
	}

	updated := &disasterv1.DisasterGroup{}
	if err := c.Get(ctx, types.NamespacedName{Name: group.Name, Namespace: group.Namespace}, updated); err != nil {
		t.Fatalf("get updated group failed: %v", err)
	}

	if updated.Status.Reason != "" || updated.Status.Message != "" {
		t.Fatalf("expected stale error cleared, got reason=%q message=%q", updated.Status.Reason, updated.Status.Message)
	}
	if updated.Status.TotalInstances != 1 || updated.Status.ReadyInstances != 1 {
		t.Fatalf("unexpected aggregate status: total=%d ready=%d", updated.Status.TotalInstances, updated.Status.ReadyInstances)
	}

	errCond := findCondition(updated.Status.Conditions, "Error")
	if errCond == nil {
		t.Fatalf("expected Error condition")
	}
	if errCond.Status != metav1.ConditionFalse {
		t.Fatalf("expected Error condition false, got %s", errCond.Status)
	}
	if errCond.Reason != "Healthy" {
		t.Fatalf("unexpected healthy reason: %q", errCond.Reason)
	}
}

func TestReconcileSetsGroupErrorWhenInstanceReasonPresent(t *testing.T) {
	ctx := context.Background()
	s := newGroupTestScheme(t)

	instance := &disasterv1.DisasterInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "inst-a",
			Namespace: "default",
		},
		Status: disasterv1.DisasterInstanceStatus{
			FsmState: disasterv1.FsmStateProtected,
			Reason:   "DataSyncFailed",
			Message:  "periodic sync failed",
		},
	}

	group := &disasterv1.DisasterGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "group-a",
			Namespace: "default",
		},
		Spec: disasterv1.DisasterGroupSpec{
			Levels: [][]string{{"inst-a"}},
		},
	}

	c := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(instance, group).
		WithStatusSubresource(instance, group).
		Build()

	r := &DisasterGroupReconciler{
		Client: c,
		Scheme: s,
		Log:    ctrl.Log.WithName("test-disastergroup"),
	}

	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: group.Name, Namespace: group.Namespace}}
	if _, err := r.Reconcile(ctx, req); err != nil {
		if !apierrors.IsConflict(err) {
			t.Fatalf("reconcile failed: %v", err)
		}
		if _, retryErr := r.Reconcile(ctx, req); retryErr != nil {
			t.Fatalf("reconcile retry after conflict failed: %v", retryErr)
		}
	}

	updated := &disasterv1.DisasterGroup{}
	if err := c.Get(ctx, types.NamespacedName{Name: group.Name, Namespace: group.Namespace}, updated); err != nil {
		t.Fatalf("get updated group failed: %v", err)
	}

	if updated.Status.Reason != groupReasonInstanceFailed {
		t.Fatalf("unexpected reason: got %q want %q", updated.Status.Reason, groupReasonInstanceFailed)
	}
	if updated.Status.Message == "" {
		t.Fatalf("expected non-empty message")
	}
	if updated.Status.ReadyInstances != 1 {
		t.Fatalf("ready instances should still count protected members, got %d", updated.Status.ReadyInstances)
	}
}

func TestReconcileSetsGroupErrorWhenInstanceConfigError(t *testing.T) {
	ctx := context.Background()
	s := newGroupTestScheme(t)

	instance := &disasterv1.DisasterInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "inst-a",
			Namespace: "default",
		},
		Status: disasterv1.DisasterInstanceStatus{
			FsmState: disasterv1.FsmStateConfigError,
			Reason:   "ConfigNotReady",
			Message:  "DisasterConfig cfg-a status=NotReady",
		},
	}

	group := &disasterv1.DisasterGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "group-a",
			Namespace: "default",
		},
		Spec: disasterv1.DisasterGroupSpec{
			Levels: [][]string{{"inst-a"}},
		},
	}

	c := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(instance, group).
		WithStatusSubresource(instance, group).
		Build()

	r := &DisasterGroupReconciler{
		Client: c,
		Scheme: s,
		Log:    ctrl.Log.WithName("test-disastergroup"),
	}

	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: group.Name, Namespace: group.Namespace}}
	if _, err := r.Reconcile(ctx, req); err != nil {
		if !apierrors.IsConflict(err) {
			t.Fatalf("reconcile failed: %v", err)
		}
		if _, retryErr := r.Reconcile(ctx, req); retryErr != nil {
			t.Fatalf("reconcile retry after conflict failed: %v", retryErr)
		}
	}

	updated := &disasterv1.DisasterGroup{}
	if err := c.Get(ctx, types.NamespacedName{Name: group.Name, Namespace: group.Namespace}, updated); err != nil {
		t.Fatalf("get updated group failed: %v", err)
	}

	if updated.Status.Reason != groupReasonInstanceFailed {
		t.Fatalf("unexpected reason: got %q want %q", updated.Status.Reason, groupReasonInstanceFailed)
	}
	if updated.Status.ReadyInstances != 0 {
		t.Fatalf("config error member should not be ready, got %d", updated.Status.ReadyInstances)
	}
}

func TestReconcileConfigErrorMemberNotCountedAsReady(t *testing.T) {
	ctx := context.Background()
	s := newGroupTestScheme(t)

	healthy := &disasterv1.DisasterInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "inst-protected",
			Namespace: "default",
		},
		Status: disasterv1.DisasterInstanceStatus{
			FsmState: disasterv1.FsmStateProtected,
		},
	}
	configError := &disasterv1.DisasterInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "inst-config-error",
			Namespace: "default",
		},
		Status: disasterv1.DisasterInstanceStatus{
			FsmState: disasterv1.FsmStateConfigError,
		},
	}

	group := &disasterv1.DisasterGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "group-a",
			Namespace: "default",
		},
		Spec: disasterv1.DisasterGroupSpec{
			Levels: [][]string{{"inst-protected", "inst-config-error"}},
		},
	}

	c := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(healthy, configError, group).
		WithStatusSubresource(healthy, configError, group).
		Build()

	r := &DisasterGroupReconciler{
		Client: c,
		Scheme: s,
		Log:    ctrl.Log.WithName("test-disastergroup"),
	}

	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: group.Name, Namespace: group.Namespace}}
	if _, err := r.Reconcile(ctx, req); err != nil {
		if !apierrors.IsConflict(err) {
			t.Fatalf("reconcile failed: %v", err)
		}
		if _, retryErr := r.Reconcile(ctx, req); retryErr != nil {
			t.Fatalf("reconcile retry after conflict failed: %v", retryErr)
		}
	}

	updated := &disasterv1.DisasterGroup{}
	if err := c.Get(ctx, types.NamespacedName{Name: group.Name, Namespace: group.Namespace}, updated); err != nil {
		t.Fatalf("get updated group failed: %v", err)
	}

	if updated.Status.TotalInstances != 2 {
		t.Fatalf("unexpected total instances: got %d", updated.Status.TotalInstances)
	}
	if updated.Status.ReadyInstances != 1 {
		t.Fatalf("unexpected ready instances: got %d", updated.Status.ReadyInstances)
	}
}

func TestMapInstanceToGroups(t *testing.T) {
	ctx := context.Background()
	s := newGroupTestScheme(t)

	instance := &disasterv1.DisasterInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "inst-a",
			Namespace: "default",
		},
	}

	groupA := &disasterv1.DisasterGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "group-a",
			Namespace: "default",
		},
		Spec: disasterv1.DisasterGroupSpec{
			Levels: [][]string{{"inst-a"}},
		},
	}

	groupB := &disasterv1.DisasterGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "group-b",
			Namespace: "default",
		},
		Spec: disasterv1.DisasterGroupSpec{
			Levels: [][]string{{"inst-b"}},
		},
	}

	c := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(instance, groupA, groupB).
		Build()

	r := &DisasterGroupReconciler{
		Client: c,
		Scheme: s,
		Log:    ctrl.Log.WithName("test-disastergroup"),
	}

	requests := r.mapInstanceToGroups(ctx, instance)
	if len(requests) != 1 {
		t.Fatalf("unexpected requests count: got %d want 1", len(requests))
	}
	if requests[0].NamespacedName.Name != "group-a" || requests[0].NamespacedName.Namespace != "default" {
		t.Fatalf("unexpected request target: %+v", requests[0].NamespacedName)
	}
}

func TestClassifyGroupStatusEvent(t *testing.T) {
	tests := []struct {
		name          string
		ready         int
		total         int
		reason        string
		wantType      string
		wantEventCode string
	}{
		{
			name:          "healthy when all ready and no reason",
			ready:         2,
			total:         2,
			reason:        "",
			wantType:      corev1.EventTypeNormal,
			wantEventCode: "GroupHealthy",
		},
		{
			name:          "degraded when not all ready",
			ready:         1,
			total:         2,
			reason:        "",
			wantType:      corev1.EventTypeWarning,
			wantEventCode: "GroupDegraded",
		},
		{
			name:          "degraded when reason exists",
			ready:         2,
			total:         2,
			reason:        "InstanceNotFound",
			wantType:      corev1.EventTypeWarning,
			wantEventCode: "GroupDegraded",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotType, gotCode := classifyGroupStatusEvent(tt.ready, tt.total, tt.reason)
			if gotType != tt.wantType {
				t.Fatalf("classifyGroupStatusEvent type=%q, want %q", gotType, tt.wantType)
			}
			if gotCode != tt.wantEventCode {
				t.Fatalf("classifyGroupStatusEvent reason=%q, want %q", gotCode, tt.wantEventCode)
			}
		})
	}
}
