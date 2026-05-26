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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// StatisticsData defines the counters for backup/restore operations
type StatisticsData struct {
	// Total number of operations
	// +kubebuilder:default=0
	Total int32 `json:"total"`

	// InProgress number of operations
	// +kubebuilder:default=0
	InProgress int32 `json:"inProgress"`

	// Completed number of operations
	// +kubebuilder:default=0
	Completed int32 `json:"completed"`

	// Failed number of operations
	// +kubebuilder:default=0
	Failed int32 `json:"failed"`

	// Canceled number of operations
	// +kubebuilder:default=0
	Canceled int32 `json:"canceled"`

	// Unknown number of operations
	// +kubebuilder:default=0
	Unknown int32 `json:"unknown"`
}

// AssociatedResource defines a resource associated with the statistics
type AssociatedResource struct {
	// APIVersion of the referent.
	// +optional
	APIVersion string `json:"apiVersion,omitempty"`

	// Kind of the referent.
	// +optional
	Kind string `json:"kind,omitempty"`

	// Name of the referent.
	// +optional
	Name string `json:"name,omitempty"`

	// Namespace of the referent.
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// UID of the referent.
	// +optional
	UID types.UID `json:"uid,omitempty"`

	// Role of the resource (e.g., "backup", "restore")
	// +optional
	Role string `json:"role,omitempty"`
}

// StatisticsEvent defines an event in the statistics history
type StatisticsEvent struct {
	// Timestamp of the event
	Timestamp metav1.Time `json:"timestamp"`

	// Type of the event (e.g., "Increment", "Decrement", "Transition", "Sync")
	Type string `json:"type"`

	// Reason for the event
	// +optional
	Reason string `json:"reason,omitempty"`

	// Message describing the event
	// +optional
	Message string `json:"message,omitempty"`

	// Changes applied in this event
	// +optional
	Changes map[string]int32 `json:"changes,omitempty"`
}

// BackupRestoreStatisticsSpec defines the desired state of BackupRestoreStatistics
type BackupRestoreStatisticsSpec struct {
	// ScopeType defines the scope of the statistics
	// +required
	ScopeType ScopeType `json:"scopeType"`

	// ScopeRef defines the reference to the scope object
	// +required
	ScopeRef ScopeReference `json:"scopeRef"`
}

// BackupRestoreStatisticsStatus defines the observed state of BackupRestoreStatistics
type BackupRestoreStatisticsStatus struct {
	// Statistics contains the counters
	Statistics StatisticsData `json:"statistics"`

	// LastUpdateTime is the last time the statistics were updated
	// +optional
	LastUpdateTime *metav1.Time `json:"lastUpdateTime,omitempty"`

	// LastUpdateReason is the reason for the last update
	// +optional
	LastUpdateReason string `json:"lastUpdateReason,omitempty"`

	// AssociatedResources is a list of resources associated with the statistics
	// +optional
	AssociatedResources []AssociatedResource `json:"associatedResources,omitempty"`

	// EventLog is a log of recent events (capped at 100)
	// +optional
	EventLog []StatisticsEvent `json:"eventLog,omitempty"`

	// Conditions represent the latest available observations of an object's state
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=backuprestorestatisticses,scope=Namespaced
// +kubebuilder:printcolumn:name="ScopeType",type="string",JSONPath=".spec.scopeType"
// +kubebuilder:printcolumn:name="Total",type="integer",JSONPath=".status.statistics.total"
// +kubebuilder:printcolumn:name="InProgress",type="integer",JSONPath=".status.statistics.inProgress"
// +kubebuilder:printcolumn:name="Completed",type="integer",JSONPath=".status.statistics.completed"
// +kubebuilder:printcolumn:name="Failed",type="integer",JSONPath=".status.statistics.failed"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// BackupRestoreStatistics is the Schema for the backuprestorestatistics API
type BackupRestoreStatistics struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   BackupRestoreStatisticsSpec   `json:"spec,omitempty"`
	Status BackupRestoreStatisticsStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// BackupRestoreStatisticsList contains a list of BackupRestoreStatistics
type BackupRestoreStatisticsList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []BackupRestoreStatistics `json:"items"`
}

func init() {
	SchemeBuilder.Register(&BackupRestoreStatistics{}, &BackupRestoreStatisticsList{})
}
