package apprestore

import (
	"context"
	"strings"
	"testing"
	"time"

	controller "github.com/softcdata/testudo-operator/internal/controller"
	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func TestRestoringHandler_DoesNotSucceedWhenUpdateRestoreHasWarnings(t *testing.T) {
	reconciler, mockTargetClient, mgmtClient := newRestoringConvergenceReconciler(t)
	appRestore := newRestoringTestAppRestore("apprestore-update-warning")
	appRestore.Spec.Template.ExistingResourcePolicy = velerov1.PolicyTypeUpdate

	restoreName := reconciler.GenRestoreName(appRestore)
	now := metav1.NewTime(time.Now())
	restore := &velerov1.Restore{
		ObjectMeta: metav1.ObjectMeta{
			Name:      restoreName,
			Namespace: controller.VeleroNamespace,
		},
		Status: velerov1.RestoreStatus{
			Phase:               velerov1.RestorePhaseCompleted,
			Warnings:            1,
			StartTimestamp:      &now,
			CompletionTimestamp: &now,
		},
	}

	mockTargetClient.MockGet = func(ctx context.Context, key ctrlclient.ObjectKey, obj ctrlclient.Object, opts ...ctrlclient.GetOption) error {
		if target, ok := obj.(*velerov1.Restore); ok {
			restore.DeepCopyInto(target)
			return nil
		}
		return mgmtClient.Get(ctx, key, obj, opts...)
	}

	nextPhase, _, err := (&RestoringHandler{}).Handle(context.Background(), reconciler, appRestore)
	if err != nil {
		t.Fatalf("RestoringHandler.Handle returned error: %v", err)
	}
	if nextPhase == disasterv1.PhaseSucceeded {
		t.Fatalf("expected warning-bearing update restore not to succeed")
	}
	if nextPhase != disasterv1.PhasePartiallyFailed {
		t.Fatalf("expected next phase PartiallyFailed, got %q", nextPhase)
	}
	if appRestore.Status.Reason != appRestoreReasonCompletedWithWarnings {
		t.Fatalf("expected warning reason, got %q", appRestore.Status.Reason)
	}
	if !strings.Contains(appRestore.Status.Message, "warnings=1") {
		t.Fatalf("expected warning count in message, got %q", appRestore.Status.Message)
	}
}

func TestRestoringHandler_PreservesNonePolicySkipWhenRestoreHasWarnings(t *testing.T) {
	reconciler, mockTargetClient, mgmtClient := newRestoringConvergenceReconciler(t)
	appRestore := newRestoringTestAppRestore("apprestore-none-warning")
	appRestore.Spec.Template.ExistingResourcePolicy = velerov1.PolicyTypeNone

	restoreName := reconciler.GenRestoreName(appRestore)
	restore := &velerov1.Restore{
		ObjectMeta: metav1.ObjectMeta{
			Name:      restoreName,
			Namespace: controller.VeleroNamespace,
		},
		Status: velerov1.RestoreStatus{
			Phase:    velerov1.RestorePhaseCompleted,
			Warnings: 1,
		},
	}

	mockTargetClient.MockGet = func(ctx context.Context, key ctrlclient.ObjectKey, obj ctrlclient.Object, opts ...ctrlclient.GetOption) error {
		if target, ok := obj.(*velerov1.Restore); ok {
			restore.DeepCopyInto(target)
			return nil
		}
		return mgmtClient.Get(ctx, key, obj, opts...)
	}

	nextPhase, _, err := (&RestoringHandler{}).Handle(context.Background(), reconciler, appRestore)
	if err != nil {
		t.Fatalf("RestoringHandler.Handle returned error: %v", err)
	}
	if nextPhase != disasterv1.PhaseSucceeded {
		t.Fatalf("expected none policy warning to preserve skip success, got %q", nextPhase)
	}
}

func TestRestoringHandler_DoesNotSucceedWhenUpdateRestoreHasErrors(t *testing.T) {
	reconciler, mockTargetClient, mgmtClient := newRestoringConvergenceReconciler(t)
	appRestore := newRestoringTestAppRestore("apprestore-update-error")
	appRestore.Spec.Template.ExistingResourcePolicy = velerov1.PolicyTypeUpdate

	restoreName := reconciler.GenRestoreName(appRestore)
	restore := &velerov1.Restore{
		ObjectMeta: metav1.ObjectMeta{Name: restoreName, Namespace: controller.VeleroNamespace},
		Status: velerov1.RestoreStatus{
			Phase:  velerov1.RestorePhaseCompleted,
			Errors: 1,
		},
	}
	mockTargetClient.MockGet = func(ctx context.Context, key ctrlclient.ObjectKey, obj ctrlclient.Object, opts ...ctrlclient.GetOption) error {
		if target, ok := obj.(*velerov1.Restore); ok {
			restore.DeepCopyInto(target)
			return nil
		}
		return mgmtClient.Get(ctx, key, obj, opts...)
	}

	nextPhase, _, err := (&RestoringHandler{}).Handle(context.Background(), reconciler, appRestore)
	if err != nil {
		t.Fatalf("RestoringHandler.Handle returned error: %v", err)
	}
	if nextPhase != disasterv1.PhasePartiallyFailed {
		t.Fatalf("expected next phase PartiallyFailed, got %q", nextPhase)
	}
	if !strings.Contains(appRestore.Status.Message, "errors=1 warnings=0") {
		t.Fatalf("expected error count in message, got %q", appRestore.Status.Message)
	}
	if appRestore.Status.Reason != appRestoreReasonCompletedWithErrors {
		t.Fatalf("expected error reason, got %q", appRestore.Status.Reason)
	}
}
