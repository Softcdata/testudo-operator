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
	runtime "k8s.io/apimachinery/pkg/runtime"
)

var _ runtime.Object = &AppRestore{}
var _ runtime.Object = &AppRestoreList{}

// AppRestoreSpec defines the desired state of AppRestore
type AppRestoreSpec struct {
	// BackupSource specifies the source of the backup to restore
	BackupSource string `json:"backupSource"`

	// Cluster specifies the target cluster for the restore
	Cluster string `json:"cluster"`

	// Template contains the restore configuration
	Template velerov1.RestoreSpec `json:"template"`

	// SourceCluster specifies the source cluster of the backup.
	// If empty, defaults to the current cluster (same-cluster restore).
	// Required for cross-cluster restore.
	// +optional
	SourceCluster string `json:"sourceCluster,omitempty"`

	// StorageRepository specifies the storage repository name (e.g., storage-1).
	// Required for cross-cluster restore to construct BSL name and path.
	// +optional
	StorageRepository string `json:"storageRepository,omitempty"`

	// Action defines the manual action to be performed.
	// +optional
	Action *RestoreAction `json:"action,omitempty"`

	// ResourceModifierRules defines the rules for resource modification during restore.
	// If provided, the controller will generate a ConfigMap in the Velero namespace based on this content.
	// +optional
	ResourceModifierRules []ResourceModifierRule `json:"resourceModifierRules,omitempty"`

	// Timeout defines the maximum duration to wait for the restore to complete.
	// If exceeded, the controller will attempt to cancel/delete the Velero Restore.
	// +optional
	Timeout *metav1.Duration `json:"timeout,omitempty"`
}

// ResourceModifierRule defines a rule for modifying resources during restore.
type ResourceModifierRule struct {
	// Conditions specifies the conditions that must be met for the patches to be applied.
	Conditions Conditions `json:"conditions"`

	// Patches specifies the JSON patches to apply to the matching resources.
	Patches []JSONPatch `json:"patches"`
}

// Conditions defines the criteria for selecting resources to modify.
type Conditions struct {
	// GroupResource is the group and resource to match (e.g., "persistentvolumeclaims" or "deployments.apps").
	GroupResource string `json:"groupResource"`

	// ResourceNameRegex is a regular expression to match the resource name.
	// +optional
	ResourceNameRegex string `json:"resourceNameRegex,omitempty"`

	// Namespaces is a list of namespaces to match.
	// +optional
	Namespaces []string `json:"namespaces,omitempty"`

	// LabelSelector is a label selector to match resources.
	// +optional
	LabelSelector *metav1.LabelSelector `json:"labelSelector,omitempty"`
}

// JSONPatch defines a JSON patch operation.
type JSONPatch struct {
	// Operation is the type of JSON patch operation (e.g., "replace", "add", "remove").
	Operation string `json:"operation"`

	// Path is the JSON path to the field to modify.
	Path string `json:"path"`

	// Value is the value to set (for "replace" or "add" operations).
	// +optional
	Value string `json:"value,omitempty"`

	// From is the source path (for "move" or "copy" operations).
	// +optional
	From string `json:"from,omitempty"`
}

type RestoreAction struct {
	// Type is the type of the action.
	// Valid values are: "Restore", "Retry".
	Type string `json:"type"`

	// RequestAt is the time when the action was requested.
	RequestAt metav1.Time `json:"requestAt"`
}

// AppRestorePhase defines the phase of an AppRestore
// +kubebuilder:validation:Enum=Pending;Restoring;InProgress;Succeeded;Failed;Cancelled;Initiating;Deleting;Unknown
// +kubebuilder:default=Pending
type AppRestorePhase string

const (
	PhasePending    AppRestorePhase = "Pending"
	PhaseInitiating AppRestorePhase = "Initiating" //若不更新nextphase,则不会更新status,导致第一次无法获取velero restore状态,引入init阶段，更新status后重新进入restoring阶段
	PhaseRestoring  AppRestorePhase = "Restoring"
	PhaseSucceeded  AppRestorePhase = "Succeeded"
	PhaseFailed     AppRestorePhase = "Failed"
	PhaseCancelled  AppRestorePhase = "Cancelled"
	PhaseDeleting   AppRestorePhase = "Deleting"
	PhaseUnknown    AppRestorePhase = "Unknown"
)

// AppRestoreStatus defines the observed state of AppRestore
type AppRestoreStatus struct {
	Status        AppRestorePhase        `json:"status,omitempty"`
	RestoreStatus velerov1.RestoreStatus `json:"restoreStatus,omitempty"`

	// LastAction records the last processed manual action.
	LastAction *RestoreAction `json:"lastAction,omitempty"`

	// Reason indicates the reason for the current status
	Reason string `json:"reason,omitempty"`

	// Message provides a detailed message for the current status
	Message string `json:"message,omitempty"`

	// TargetNamespaces records the list of namespaces that will be restored
	TargetNamespaces []string `json:"targetNamespaces,omitempty"`
}

// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Cluster",type="string",JSONPath=".spec.cluster",description="The target cluster for the AppRestore"
// +kubebuilder:printcolumn:name="BackupSource",type="string",JSONPath=".spec.backupSource",description="The source backup for the AppRestore"
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.status",description="The status of the AppRestore"
// AppRestore is the Schema for the apprestores API
type AppRestore struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec defines the desired state of AppRestore
	// +required
	Spec AppRestoreSpec `json:"spec"`

	// status defines the observed state of AppRestore
	// +optional
	Status AppRestoreStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// AppRestoreList contains a list of AppRestore
type AppRestoreList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AppRestore `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AppRestore{}, &AppRestoreList{})
}
