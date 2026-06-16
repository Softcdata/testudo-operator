package restore

import (
	"reflect"
	"testing"
	"time"

	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestBuildAppRestoreSpec_DataRestoreProjectsHooks(t *testing.T) {
	hooks := &velerov1.RestoreHooks{
		Resources: []velerov1.RestoreResourceHookSpec{
			{
				Name:              "post-restore",
				IncludedResources: []string{"pods"},
				PostHooks: []velerov1.RestoreResourceHook{
					{
						Exec: &velerov1.ExecRestoreHook{
							Command:     []string{"/usr/local/bin/dr-hook", "after-restore"},
							ExecTimeout: metav1.Duration{Duration: 2 * time.Minute},
						},
					},
				},
			},
		},
	}

	spec := BuildAppRestoreSpec(BuilderConfig{
		RestoreType:        RestoreTypeData,
		BackupSource:       "ds-demo",
		BackupName:         "backup-001",
		TargetCluster:      "cluster-b",
		SourceCluster:      "cluster-a",
		StorageRepository:  "repo-main",
		IncludedNamespaces: []string{"demo"},
		DataRestoreHooks:   hooks,
	})

	if !reflect.DeepEqual(spec.Template.Hooks, *hooks) {
		t.Fatalf("expected data restore hooks to be projected, got %#v", spec.Template.Hooks)
	}
}
