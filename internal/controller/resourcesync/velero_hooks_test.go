package resourcesync

import (
	"testing"
	"time"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestBuildAppBackupSpec_DoesNotProjectInstanceVeleroHooks(t *testing.T) {
	r := &ResourceSyncReconciler{}
	instance := &disasterv1.DisasterInstance{
		Spec: disasterv1.DisasterInstanceSpec{
			Namespaces: []string{"app"},
			VeleroHooks: &disasterv1.DisasterVeleroHooks{
				DataBackup: &velerov1.BackupHooks{
					Resources: []velerov1.BackupResourceHookSpec{
						{
							Name:              "should-not-project",
							IncludedResources: []string{"pods"},
							PreHooks: []velerov1.BackupResourceHook{
								{Exec: &velerov1.ExecHook{Command: []string{"true"}, Timeout: metav1.Duration{Duration: 30 * time.Second}}},
							},
						},
					},
				},
			},
		},
	}
	config := &disasterv1.DisasterConfig{
		Spec: disasterv1.DisasterConfigSpec{
			SourceCluster:     "cluster-a",
			TargetCluster:     "cluster-b",
			StorageRepository: "repo-main",
		},
	}

	spec := r.buildAppBackupSpec(instance, config)
	if len(spec.Template.Hooks.Resources) != 0 {
		t.Fatalf("ResourceSync must not project instance dataBackup hooks, got %#v", spec.Template.Hooks)
	}
}
