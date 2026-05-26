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
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// PolicyType defines the type of the policy
// +kubebuilder:validation:Enum=AutoBackup;DataSync;ResourceSync
type PolicyType string

const (
	PolicyTypeAutoBackup   PolicyType = "AutoBackup"
	PolicyTypeDataSync     PolicyType = "DataSync"
	PolicyTypeResourceSync PolicyType = "ResourceSync"
)

// PolicyState defines the state of the policy
// +kubebuilder:validation:Enum=Enabled;Disabled
// +kubebuilder:default=Enabled
type PolicyState string

const (
	PolicyStateEnabled  PolicyState = "Enabled"
	PolicyStateDisabled PolicyState = "Disabled"
)

// PolicyPhase defines the lifecycle phase of the policy
// +kubebuilder:validation:Enum=Active;Deleting
type PolicyPhase string

const (
	// PolicyPhaseActive indicates the policy is active and functioning normally
	PolicyPhaseActive PolicyPhase = "Active"
	// PolicyPhaseDeleting indicates the policy is being deleted but blocked by dependencies
	PolicyPhaseDeleting PolicyPhase = "Deleting"
)

// DisasterPolicySpec defines the desired state of DisasterPolicy
type DisasterPolicySpec struct {
	// Type specifies the type of the policy
	// +required
	Type PolicyType `json:"type"`

	// Schedule is a Cron expression defining when to run the policy
	// +required
	Schedule string `json:"schedule"`

	// StartTime specifies when the policy should start taking effect
	// +optional
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// TTL defines how long Velero backups created by this AutoBackup policy are retained.
	// Only effective when type is AutoBackup.
	// +optional
	TTL *metav1.Duration `json:"ttl,omitempty"`

	// Description provides a human-readable description of the policy
	// +optional
	Description string `json:"description,omitempty"`

	// State specifies whether the policy is enabled or disabled
	// +required
	State PolicyState `json:"state"`
}

// DisasterPolicyStatus defines the observed state of DisasterPolicy.
type DisasterPolicyStatus struct {
	// Phase indicates the lifecycle phase of the policy (Active, Deleting)
	// +optional
	Phase PolicyPhase `json:"phase,omitempty"`

	// LastExecutionTime records the last time the policy was executed
	LastExecutionTime *metav1.Time `json:"lastExecutionTime,omitempty"`

	// NextExecutionTime records the next scheduled execution time
	NextExecutionTime *metav1.Time `json:"nextExecutionTime,omitempty"`

	// Reason indicates the reason for the current status
	Reason string `json:"reason,omitempty"`

	// Message provides a detailed message about the current status
	Message string `json:"message,omitempty"`

	// ObservedGeneration records the generation that was last processed
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// LastEventPhase records the last event phase for debouncing
	// +optional
	LastEventPhase string `json:"lastEventPhase,omitempty"`

	// LastState records the last observed policy state (Enabled/Disabled)
	// +optional
	LastState PolicyState `json:"lastState,omitempty"`
}

// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Type",type="string",JSONPath=".spec.type",description="The type of the policy"
// +kubebuilder:printcolumn:name="Schedule",type="string",JSONPath=".spec.schedule",description="The schedule of the policy"
// +kubebuilder:printcolumn:name="State",type="string",JSONPath=".spec.state",description="The state of the policy"
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase",description="The lifecycle phase of the policy"
// +kubebuilder:printcolumn:name="LastExecution",type="date",JSONPath=".status.lastExecutionTime",description="The last time the policy was executed"

// DisasterPolicy is the Schema for the disasterpolicies API
type DisasterPolicy struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of DisasterPolicy
	// +required
	Spec DisasterPolicySpec `json:"spec"`

	// status defines the observed state of DisasterPolicy
	// +optional
	Status DisasterPolicyStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// DisasterPolicyList contains a list of DisasterPolicy
type DisasterPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []DisasterPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DisasterPolicy{}, &DisasterPolicyList{})
}
