package datasync

import (
	"testing"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
)

func TestBuildAppBackupSpec_UsesParallelFilesUploadEnv(t *testing.T) {
	t.Setenv(appBackupParallelFilesUploadEnv, "1")

	spec := (&DataSyncReconciler{}).buildAppBackupSpec(
		&disasterv1.DisasterInstance{
			Spec: disasterv1.DisasterInstanceSpec{
				Config:     "test-config",
				Namespaces: []string{"app-ns"},
			},
		},
		&disasterv1.DisasterConfig{
			Spec: disasterv1.DisasterConfigSpec{
				SourceCluster:     "cluster-a",
				TargetCluster:     "cluster-b",
				StorageRepository: "repo-a",
			},
		},
	)

	if spec.Template.UploaderConfig == nil {
		t.Fatalf("expected uploaderConfig to be set")
	}
	if got := spec.Template.UploaderConfig.ParallelFilesUpload; got != 1 {
		t.Fatalf("expected parallelFilesUpload=1, got %d", got)
	}
}

func TestBuildAppBackupSpec_IgnoresInvalidParallelFilesUploadEnv(t *testing.T) {
	t.Setenv(appBackupParallelFilesUploadEnv, "0")

	spec := (&DataSyncReconciler{}).buildAppBackupSpec(
		&disasterv1.DisasterInstance{
			Spec: disasterv1.DisasterInstanceSpec{
				Config:     "test-config",
				Namespaces: []string{"app-ns"},
			},
		},
		&disasterv1.DisasterConfig{
			Spec: disasterv1.DisasterConfigSpec{
				SourceCluster:     "cluster-a",
				TargetCluster:     "cluster-b",
				StorageRepository: "repo-a",
			},
		},
	)

	if spec.Template.UploaderConfig != nil {
		t.Fatalf("expected uploaderConfig to stay nil for invalid env, got %#v", spec.Template.UploaderConfig)
	}
}
