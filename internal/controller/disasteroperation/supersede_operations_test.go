package disasteroperation

import (
	"context"
	"strings"
	"testing"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func buildSupersedeTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := disasterv1.AddToScheme(s); err != nil {
		t.Fatalf("add disaster scheme: %v", err)
	}
	return s
}

func TestSupersedeInFlightOperations_InstanceCancelSupersedesFailover(t *testing.T) {
	ctx := context.Background()
	s := buildSupersedeTestScheme(t)

	oldFailover := &disasterv1.DisasterOperation{
		ObjectMeta: metav1.ObjectMeta{Name: "failover-old", Namespace: "default"},
		Spec: disasterv1.DisasterOperationSpec{
			InstanceName:  "inst-a",
			OperationType: disasterv1.OperationTypeFailover,
		},
		Status: disasterv1.DisasterOperationStatus{
			State:       disasterv1.OperationStateRunning,
			CurrentStep: "FinalSync",
			Steps: []disasterv1.StepStatus{{
				Name:  "FinalSync",
				State: "Running",
			}},
		},
	}
	newCancel := &disasterv1.DisasterOperation{
		ObjectMeta: metav1.ObjectMeta{Name: "cancel-new", Namespace: "default"},
		Spec: disasterv1.DisasterOperationSpec{
			InstanceName:  "inst-a",
			OperationType: disasterv1.OperationTypeCancel,
		},
		Status: disasterv1.DisasterOperationStatus{State: disasterv1.OperationStateRunning},
	}
	unrelated := &disasterv1.DisasterOperation{
		ObjectMeta: metav1.ObjectMeta{Name: "failover-other", Namespace: "default"},
		Spec: disasterv1.DisasterOperationSpec{
			InstanceName:  "inst-b",
			OperationType: disasterv1.OperationTypeFailover,
		},
		Status: disasterv1.DisasterOperationStatus{State: disasterv1.OperationStateRunning},
	}

	cli := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(oldFailover, newCancel, unrelated).
		WithStatusSubresource(oldFailover, newCancel, unrelated).
		Build()

	r := &DisasterOperationReconciler{Client: cli, Scheme: s}
	if err := r.supersedeInFlightOperations(ctx, newCancel); err != nil {
		t.Fatalf("supersede failed: %v", err)
	}

	gotOld := &disasterv1.DisasterOperation{}
	if err := cli.Get(ctx, clientKey("default", "failover-old"), gotOld); err != nil {
		t.Fatalf("get old failover: %v", err)
	}
	if gotOld.Status.State != disasterv1.OperationStateFailed {
		t.Fatalf("expected old failover failed, got %s", gotOld.Status.State)
	}
	if gotOld.Status.Reason != operationReasonSuperseded {
		t.Fatalf("expected reason %s, got %s", operationReasonSuperseded, gotOld.Status.Reason)
	}
	if !strings.Contains(gotOld.Status.Message, "cancel-new") {
		t.Fatalf("expected supersede message contains new op name, got %s", gotOld.Status.Message)
	}
	if len(gotOld.Status.Steps) == 0 || gotOld.Status.Steps[0].State != "Failed" {
		t.Fatalf("expected running step to be marked failed")
	}

	gotOther := &disasterv1.DisasterOperation{}
	if err := cli.Get(ctx, clientKey("default", "failover-other"), gotOther); err != nil {
		t.Fatalf("get unrelated op: %v", err)
	}
	if gotOther.Status.State != disasterv1.OperationStateRunning {
		t.Fatalf("expected unrelated op keep running, got %s", gotOther.Status.State)
	}
}

func TestSupersedeInFlightOperations_GroupCancelSupersedesGroupFailover(t *testing.T) {
	ctx := context.Background()
	s := buildSupersedeTestScheme(t)

	oldGroupFailover := &disasterv1.DisasterOperation{
		ObjectMeta: metav1.ObjectMeta{Name: "failover-group-old", Namespace: "default"},
		Spec: disasterv1.DisasterOperationSpec{
			GroupName:     "group-a",
			OperationType: disasterv1.OperationTypeFailover,
		},
		Status: disasterv1.DisasterOperationStatus{State: disasterv1.OperationStateRunning},
	}
	newGroupCancel := &disasterv1.DisasterOperation{
		ObjectMeta: metav1.ObjectMeta{Name: "cancel-group-new", Namespace: "default"},
		Spec: disasterv1.DisasterOperationSpec{
			GroupName:     "group-a",
			OperationType: disasterv1.OperationTypeCancel,
		},
		Status: disasterv1.DisasterOperationStatus{State: disasterv1.OperationStateRunning},
	}

	cli := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(oldGroupFailover, newGroupCancel).
		WithStatusSubresource(oldGroupFailover, newGroupCancel).
		Build()

	r := &DisasterOperationReconciler{Client: cli, Scheme: s}
	if err := r.supersedeInFlightOperations(ctx, newGroupCancel); err != nil {
		t.Fatalf("supersede failed: %v", err)
	}

	gotOld := &disasterv1.DisasterOperation{}
	if err := cli.Get(ctx, clientKey("default", "failover-group-old"), gotOld); err != nil {
		t.Fatalf("get old group failover: %v", err)
	}
	if gotOld.Status.State != disasterv1.OperationStateFailed {
		t.Fatalf("expected old group failover failed, got %s", gotOld.Status.State)
	}
	if gotOld.Status.Reason != operationReasonSuperseded {
		t.Fatalf("expected reason %s, got %s", operationReasonSuperseded, gotOld.Status.Reason)
	}
}

func TestSupersedeInFlightOperations_FailoverDoesNotSupersedeOthers(t *testing.T) {
	ctx := context.Background()
	s := buildSupersedeTestScheme(t)

	oldFailover := &disasterv1.DisasterOperation{
		ObjectMeta: metav1.ObjectMeta{Name: "failover-old", Namespace: "default"},
		Spec: disasterv1.DisasterOperationSpec{
			InstanceName:  "inst-a",
			OperationType: disasterv1.OperationTypeFailover,
		},
		Status: disasterv1.DisasterOperationStatus{State: disasterv1.OperationStateRunning},
	}
	newFailover := &disasterv1.DisasterOperation{
		ObjectMeta: metav1.ObjectMeta{Name: "failover-new", Namespace: "default"},
		Spec: disasterv1.DisasterOperationSpec{
			InstanceName:  "inst-a",
			OperationType: disasterv1.OperationTypeFailover,
		},
		Status: disasterv1.DisasterOperationStatus{State: disasterv1.OperationStateRunning},
	}

	cli := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(oldFailover, newFailover).
		WithStatusSubresource(oldFailover, newFailover).
		Build()

	r := &DisasterOperationReconciler{Client: cli, Scheme: s}
	if err := r.supersedeInFlightOperations(ctx, newFailover); err != nil {
		t.Fatalf("supersede failed: %v", err)
	}

	gotOld := &disasterv1.DisasterOperation{}
	if err := cli.Get(ctx, clientKey("default", "failover-old"), gotOld); err != nil {
		t.Fatalf("get old failover: %v", err)
	}
	if gotOld.Status.State != disasterv1.OperationStateRunning {
		t.Fatalf("expected old failover keep running for non cancel/undo, got %s", gotOld.Status.State)
	}
}

func clientKey(namespace, name string) client.ObjectKey {
	return client.ObjectKey{Namespace: namespace, Name: name}
}
