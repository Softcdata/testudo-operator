package appbackup

import (
	"time"

	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const backupStartClockSkewTolerance = 5 * time.Second

func normalizedBackupStartTimestamp(backup *velerov1.Backup) *metav1.Time {
	if backup == nil {
		return nil
	}

	if backup.Status.StartTimestamp == nil {
		if backup.CreationTimestamp.IsZero() {
			return nil
		}
		created := backup.CreationTimestamp
		return &created
	}

	started := *backup.Status.StartTimestamp
	if backup.CreationTimestamp.IsZero() || !started.Time.Before(backup.CreationTimestamp.Time) {
		return &started
	}

	if backup.CreationTimestamp.Time.Sub(started.Time) > backupStartClockSkewTolerance {
		return &started
	}

	created := backup.CreationTimestamp
	return &created
}
