package controller

import (
	"testing"
	"time"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestResolveAppBackupTimeout(t *testing.T) {
	if got := ResolveAppBackupTimeout(nil); got != nil {
		t.Fatalf("expected nil timeout for nil instance, got %#v", got)
	}

	instance := &disasterv1.DisasterInstance{}
	if got := ResolveAppBackupTimeout(instance); got != nil {
		t.Fatalf("expected nil timeout for zero instance timeout, got %#v", got)
	}

	instance.Spec.OperationTimeoutMinutes = 45
	got := ResolveAppBackupTimeout(instance)
	if got == nil {
		t.Fatal("expected timeout to be populated")
	}
	if got.Duration != 45*time.Minute {
		t.Fatalf("expected 45m timeout, got %s", got.Duration)
	}
}

func TestAppBackupSpecNeedsUpdateIncludesTimeout(t *testing.T) {
	current := disasterv1.AppBackupSpec{
		Cluster: "cluster-a",
		Template: velerov1.BackupSpec{
			IncludedNamespaces: []string{"app"},
		},
	}
	desired := current
	desired.Timeout = &metav1.Duration{Duration: time.Minute}

	if !AppBackupSpecNeedsUpdate(current, desired) {
		t.Fatal("expected timeout mismatch to require update")
	}

	current.Timeout = &metav1.Duration{Duration: time.Minute}
	if AppBackupSpecNeedsUpdate(current, desired) {
		t.Fatal("expected identical specs to not require update")
	}
}
