package controller

import (
	"crypto/md5"
	"fmt"
	"strconv"
	"time"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const BackupPhaseTimeoutFailed = "TimeoutFailed"

func GenVeleroBackupName(appBackupName string, timestamp time.Time) string {
	name := appBackupName
	if len(name) > 40 {
		hash := fmt.Sprintf("%x", md5.Sum([]byte(name)))[:6]
		name = name[:33] + "-" + hash
	}

	return fmt.Sprintf("bak-%s-%s", name, strconv.FormatInt(timestamp.Unix(), 16))
}

func CurrentBackupActionVeleroBackupName(appBackupName string, appBackup *disasterv1.AppBackup, lastSync *metav1.Time) (string, bool) {
	if appBackup == nil || appBackup.Spec.Action == nil || appBackup.Spec.Action.Type != "Backup" {
		return "", false
	}
	if lastSync != nil && !appBackup.Spec.Action.RequestAt.Time.After(lastSync.Time) {
		return "", false
	}
	return GenVeleroBackupName(appBackupName, appBackup.Spec.Action.RequestAt.Time), true
}

func FindBackupRecordByName(history []disasterv1.BackupRecord, backupName string) (disasterv1.BackupRecord, bool) {
	for _, rec := range history {
		if rec.Name == backupName {
			return rec, true
		}
	}
	return disasterv1.BackupRecord{}, false
}

func BackupRecordFailed(rec disasterv1.BackupRecord) bool {
	switch rec.Phase {
	case string(velerov1.BackupPhaseFailed),
		string(velerov1.BackupPhasePartiallyFailed),
		string(velerov1.BackupPhaseFailedValidation),
		BackupPhaseTimeoutFailed:
		return true
	}
	return rec.ManagedStatus == disasterv1.LastBackupStatusFailed
}

func BackupRecordFailureStatus(rec disasterv1.BackupRecord) string {
	if rec.Phase != "" {
		return rec.Phase
	}
	if rec.ManagedStatus != "" {
		return rec.ManagedStatus
	}
	return disasterv1.LastBackupStatusFailed
}
