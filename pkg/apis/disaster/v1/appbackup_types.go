/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1

import (
	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

const (
	ManualSchedule string = "@manual"
)

const (
	LastBackupStatusInProgress string = "InProgress"
	LastBackupStatusCompleted  string = "Completed"
	LastBackupStatusFailed     string = "Failed"
	LastBackupStatusCanceled   string = "Canceled"
	LastBackupStatusDeleted    string = "Deleted"
	LastBackupStatusUnknown    string = "Unknown"
)

// AppBackupSpec defines the desired state of AppBackup
type AppBackupSpec struct {
	// Cluster is the name of the cluster to backup
	Cluster string `json:"cluster"`

	// Template is the definition of the Backup to be run
	// on the provided schedule
	Template velerov1.BackupSpec `json:"template"`

	// Schedule is a Cron expression defining when to run
	// the Backup. "@manual" is a special schedule that runs once immediately
	Schedule string `json:"schedule"`

	//策略配置名称
	DisasterPolicy string `json:"disasterPolicy,omitempty"`

	// UseOwnerReferencesBackup specifies whether to use
	// OwnerReferences on backups created by this Schedule.
	// +optional
	// +nullable
	UseOwnerReferencesInBackup *bool `json:"useOwnerReferencesInBackup,omitempty"`

	// Paused specifies whether the schedule is paused or not
	// +optional
	Paused bool `json:"paused,omitempty"`

	//跳过首次备份
	// +optional
	SkipImmediately *bool `json:"skipImmediately,omitempty"`

	// Action defines the manual action to be performed.
	// +optional
	Action *BackupAction `json:"action,omitempty"`

	// Timeout defines the maximum duration to wait for the backup to complete.
	// If exceeded, the controller will attempt to cancel/delete the Velero Backup.
	// +optional
	Timeout *metav1.Duration `json:"timeout,omitempty"`
}

type BackupAction struct {
	// Type is the type of the action.
	// Valid values are: "Backup", "Retry", "Cancel", "Delete".
	Type string `json:"type"`

	// TargetBackup specifies the name of the backup to operate on.
	// If empty, the action applies to the latest backup (for Retry/Cancel).
	// For Delete action, this field is required.
	// +optional
	TargetBackup string `json:"targetBackup,omitempty"`

	// RequestAt is the time when the action was requested.
	// The controller will process the action if this timestamp is newer than the one in status.
	RequestAt metav1.Time `json:"requestAt"`
}

// AppBackupStatus defines the observed state of AppBackup.
type AppBackupStatus struct {
	Status         string                  `json:"status,omitempty,omitzero"`
	ScheduleStatus velerov1.ScheduleStatus `json:"scheduleStatus,omitempty,omitzero"`
	BackupStatus   velerov1.BackupStatus   `json:"backupStatus,omitempty,omitzero"`

	// TotalBackups records the total number of backups managed by this AppBackup
	TotalBackups int `json:"totalBackups,omitempty"`

	// History records the list of recent backups
	History []BackupRecord `json:"history,omitempty"`

	// LastAction records the last processed manual action.
	LastAction *BackupAction `json:"lastAction,omitempty"`

	// HasRunInitialBackup records whether the initial one-off backup has been run
	HasRunInitialBackup bool `json:"hasRunInitialBackup,omitempty"`

	// LastBackupStatus records the status of the last backup (e.g., InProgress, Completed, Failed, Canceled)
	LatestBackupStatus string `json:"latestBackupStatus,omitempty"`

	// Reason indicates the reason for the current status
	Reason string `json:"reason,omitempty"`

	// Message provides a detailed message for the current status
	Message string `json:"message,omitempty"`
}

type BackupRecord struct {
	// Name is the name of the Velero Backup
	Name string `json:"name"`

	// Phase is the current phase of the Backup
	Phase string `json:"phase"`

	// ManagedStatus records the high-level status managed by the controller
	// Valid values: InProgress, Completed, Failed, Canceled
	ManagedStatus string `json:"managedStatus,omitempty"`

	// StartTimestamp is the time when the backup was started
	StartTimestamp *metav1.Time `json:"startTimestamp,omitempty"`

	// CompletionTimestamp is the time when the backup completed
	CompletionTimestamp *metav1.Time `json:"completionTimestamp,omitempty"`

	// Errors is the number of errors that occurred during the backup
	Errors int `json:"errors,omitempty"`

	// Warnings is the number of warnings that occurred during the backup
	Warnings int `json:"warnings,omitempty"`

	// Expiration is the time when the backup expires
	Expiration *metav1.Time `json:"expiration,omitempty"`

	// VeleroStatus records the complete status from Velero Backup
	VeleroStatus *velerov1.BackupStatus `json:"veleroStatus,omitempty"`
}

// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// AppBackup is the Schema for the appbackups API
// +kubebuilder:printcolumn:name="Cluster",type="string",JSONPath=".spec.cluster",description="The cluster of the AppBackup"
// +kubebuilder:printcolumn:name="TotalBackups",type="integer",JSONPath=".status.totalBackups",description="The total number of backups managed by this AppBackup"
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.status",description="The status of the AppBackup"
// +kubebuilder:printcolumn:name="LatestBackupStatus",type="string",JSONPath=".status.latestBackupStatus",description="The status of the latest backup"
type AppBackup struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of AppBackup
	// +required
	Spec AppBackupSpec `json:"spec"`

	// status defines the observed state of AppBackup
	// +optional
	Status AppBackupStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// AppBackupList contains a list of AppBackup
type AppBackupList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AppBackup `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AppBackup{}, &AppBackupList{})
}
