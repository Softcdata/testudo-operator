package appbackup

import (
	"context"
	"testing"
	"time"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	. "github.com/softcdata/testudo-operator/pkg/metadata"
	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestApplyAutoBackupPolicyCopiesTTLAndSchedule(t *testing.T) {
	ttl := metav1.Duration{Duration: 720 * time.Hour}
	appBackup := &disasterv1.AppBackup{
		Spec: disasterv1.AppBackupSpec{
			Schedule: "*/5 * * * *",
			Template: velerov1.BackupSpec{
				TTL: metav1.Duration{Duration: 24 * time.Hour},
			},
		},
	}
	policy := &disasterv1.DisasterPolicy{
		ObjectMeta: metav1.ObjectMeta{UID: types.UID("policy-uid")},
		Spec: disasterv1.DisasterPolicySpec{
			Type:     disasterv1.PolicyTypeAutoBackup,
			Schedule: "0 2 * * *",
			State:    disasterv1.PolicyStateDisabled,
			TTL:      &ttl,
		},
	}

	applyAutoBackupPolicy(appBackup, policy)

	if appBackup.Spec.Schedule != "0 2 * * *" {
		t.Fatalf("expected schedule from policy, got %q", appBackup.Spec.Schedule)
	}
	if appBackup.Spec.Template.TTL.Duration != 720*time.Hour {
		t.Fatalf("expected ttl from policy, got %s", appBackup.Spec.Template.TTL.Duration)
	}
	if !appBackup.Spec.Paused {
		t.Fatalf("expected disabled policy to pause AppBackup schedule")
	}
	if appBackup.Annotations[AnnotationAppBackupManualPaused] != "false" {
		t.Fatalf("expected disabled policy pause to be marked as policy-derived")
	}
	if appBackup.Labels[LabelDisasterPolicyUID] != "policy-uid" {
		t.Fatalf("expected policy uid label, got %q", appBackup.Labels[LabelDisasterPolicyUID])
	}
}

func TestApplyAutoBackupPolicyKeepsExistingTTLWhenPolicyTTLUnset(t *testing.T) {
	appBackup := &disasterv1.AppBackup{
		Spec: disasterv1.AppBackupSpec{
			Template: velerov1.BackupSpec{
				TTL: metav1.Duration{Duration: 24 * time.Hour},
			},
		},
	}
	policy := &disasterv1.DisasterPolicy{
		Spec: disasterv1.DisasterPolicySpec{
			Type:     disasterv1.PolicyTypeAutoBackup,
			Schedule: "0 2 * * *",
		},
	}

	applyAutoBackupPolicy(appBackup, policy)

	if appBackup.Spec.Template.TTL.Duration != 24*time.Hour {
		t.Fatalf("expected existing ttl to be preserved, got %s", appBackup.Spec.Template.TTL.Duration)
	}
}

func TestApplyAutoBackupPolicyPreservesManualPauseForEnabledPolicy(t *testing.T) {
	appBackup := &disasterv1.AppBackup{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				AnnotationAppBackupManualPaused: "true",
			},
		},
		Spec: disasterv1.AppBackupSpec{
			Paused: true,
		},
	}
	policy := &disasterv1.DisasterPolicy{
		Spec: disasterv1.DisasterPolicySpec{
			Type:     disasterv1.PolicyTypeAutoBackup,
			Schedule: "0 2 * * *",
			State:    disasterv1.PolicyStateEnabled,
		},
	}

	applyAutoBackupPolicy(appBackup, policy)

	if !appBackup.Spec.Paused {
		t.Fatalf("expected manual pause to remain effective while policy is enabled")
	}
}

func TestApplyAutoBackupPolicyClearsPolicyDerivedPauseWhenPolicyEnabled(t *testing.T) {
	appBackup := &disasterv1.AppBackup{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				AnnotationAppBackupManualPaused: "false",
			},
		},
		Spec: disasterv1.AppBackupSpec{
			Paused: true,
		},
	}
	policy := &disasterv1.DisasterPolicy{
		Spec: disasterv1.DisasterPolicySpec{
			Type:     disasterv1.PolicyTypeAutoBackup,
			Schedule: "0 2 * * *",
			State:    disasterv1.PolicyStateEnabled,
		},
	}

	applyAutoBackupPolicy(appBackup, policy)

	if appBackup.Spec.Paused {
		t.Fatalf("expected policy-derived pause to clear when policy is enabled")
	}
}

func TestMapAppBackupsForAutoBackupPolicy(t *testing.T) {
	ctx := context.Background()
	scheme := newAppBackupTestScheme(t)
	policy := &disasterv1.DisasterPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "auto-pol", Namespace: "default"},
		Spec: disasterv1.DisasterPolicySpec{
			Type: disasterv1.PolicyTypeAutoBackup,
		},
	}
	nonAutoPolicy := &disasterv1.DisasterPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "sync-pol", Namespace: "default"},
		Spec: disasterv1.DisasterPolicySpec{
			Type: disasterv1.PolicyTypeDataSync,
		},
	}
	matching := &disasterv1.AppBackup{
		ObjectMeta: metav1.ObjectMeta{Name: "matching", Namespace: "default"},
		Spec: disasterv1.AppBackupSpec{
			DisasterPolicy: "auto-pol",
		},
	}
	otherPolicy := &disasterv1.AppBackup{
		ObjectMeta: metav1.ObjectMeta{Name: "other-policy", Namespace: "default"},
		Spec: disasterv1.AppBackupSpec{
			DisasterPolicy: "other-pol",
		},
	}
	otherNamespace := &disasterv1.AppBackup{
		ObjectMeta: metav1.ObjectMeta{Name: "other-namespace", Namespace: "other"},
		Spec: disasterv1.AppBackupSpec{
			DisasterPolicy: "auto-pol",
		},
	}
	reconciler := &AppBackupReconciler{
		Client: fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(policy, nonAutoPolicy, matching, otherPolicy, otherNamespace).
			Build(),
	}

	requests := reconciler.mapAppBackupsForAutoBackupPolicy(ctx, policy)
	if len(requests) != 1 {
		t.Fatalf("expected one request, got %d: %#v", len(requests), requests)
	}
	if requests[0].Name != "matching" || requests[0].Namespace != "default" {
		t.Fatalf("unexpected request: %#v", requests[0])
	}

	if got := reconciler.mapAppBackupsForAutoBackupPolicy(ctx, nonAutoPolicy); len(got) != 0 {
		t.Fatalf("expected non-AutoBackup policy to enqueue no requests, got %#v", got)
	}
}
