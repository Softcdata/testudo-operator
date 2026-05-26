package datasync

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
)

func TestBuildAppRestoreSpec_InjectsPVCVolumeNameCleanupOnFirstInitializingSyncWithoutRestorePolicy(t *testing.T) {
	reconciler := &DataSyncReconciler{}
	ds := &disasterv1.DataSync{}
	instance := &disasterv1.DisasterInstance{
		Spec: disasterv1.DisasterInstanceSpec{
			Namespaces: []string{"app-ns"},
		},
		Status: disasterv1.DisasterInstanceStatus{
			FsmState:         disasterv1.FsmStateInitializing,
			PrimaryCluster:   "cluster-a",
			SecondaryCluster: "cluster-b",
		},
	}
	config := &disasterv1.DisasterConfig{
		Spec: disasterv1.DisasterConfigSpec{
			SourceCluster:     "cluster-a",
			TargetCluster:     "cluster-b",
			StorageRepository: "repo-main",
		},
	}

	spec, _, err := reconciler.buildAppRestoreSpec(context.Background(), ds, config, instance, "backup-001")
	if err != nil {
		t.Fatalf("buildAppRestoreSpec returned error: %v", err)
	}

	if !hasPatch(spec.ResourceModifierRules, "persistentvolumeclaims", "remove", "/spec/volumeName") {
		t.Fatalf("expected pvc volumeName cleanup patch on first initializing sync")
	}
}

func TestBuildAppRestoreSpec_DoesNotInjectPVCVolumeNameCleanupAfterFirstSync(t *testing.T) {
	reconciler := &DataSyncReconciler{}
	now := metav1.NewTime(time.Now())
	ds := &disasterv1.DataSync{
		Status: disasterv1.DataSyncStatus{
			LastSyncTime: &now,
		},
	}
	instance := &disasterv1.DisasterInstance{
		Spec: disasterv1.DisasterInstanceSpec{
			Namespaces: []string{"app-ns"},
		},
		Status: disasterv1.DisasterInstanceStatus{
			FsmState:         disasterv1.FsmStateProtected,
			PrimaryCluster:   "cluster-a",
			SecondaryCluster: "cluster-b",
		},
	}
	config := &disasterv1.DisasterConfig{
		Spec: disasterv1.DisasterConfigSpec{
			SourceCluster:     "cluster-a",
			TargetCluster:     "cluster-b",
			StorageRepository: "repo-main",
		},
	}

	spec, _, err := reconciler.buildAppRestoreSpec(context.Background(), ds, config, instance, "backup-001")
	if err != nil {
		t.Fatalf("buildAppRestoreSpec returned error: %v", err)
	}

	if hasPatch(spec.ResourceModifierRules, "persistentvolumeclaims", "remove", "/spec/volumeName") {
		t.Fatalf("did not expect pvc volumeName cleanup patch after first sync")
	}
}

func TestBuildAppRestoreSpec_InjectsPVCVolumeNameCleanupAsSystemProtectWithRestorePolicy(t *testing.T) {
	reconciler := &DataSyncReconciler{}
	ds := &disasterv1.DataSync{}
	instance := &disasterv1.DisasterInstance{
		Spec: disasterv1.DisasterInstanceSpec{
			Namespaces:    []string{"app-ns"},
			RestorePolicy: &disasterv1.RestorePolicy{},
		},
		Status: disasterv1.DisasterInstanceStatus{
			FsmState:         disasterv1.FsmStateInitializing,
			PrimaryCluster:   "cluster-a",
			SecondaryCluster: "cluster-b",
		},
	}
	config := &disasterv1.DisasterConfig{
		Spec: disasterv1.DisasterConfigSpec{
			SourceCluster:     "cluster-a",
			TargetCluster:     "cluster-b",
			StorageRepository: "repo-main",
		},
	}

	spec, _, err := reconciler.buildAppRestoreSpec(context.Background(), ds, config, instance, "backup-001")
	if err != nil {
		t.Fatalf("buildAppRestoreSpec returned error: %v", err)
	}

	if !hasPatch(spec.ResourceModifierRules, "persistentvolumeclaims", "remove", "/spec/volumeName") {
		t.Fatalf("expected pvc volumeName cleanup patch in system-protect path")
	}
}

func hasPatch(
	rules []disasterv1.ResourceModifierRule,
	groupResource string,
	operation string,
	path string,
) bool {
	for _, rule := range rules {
		if rule.Conditions.GroupResource != groupResource {
			continue
		}
		for _, patch := range rule.Patches {
			if patch.Operation == operation && patch.Path == path {
				return true
			}
		}
	}
	return false
}
