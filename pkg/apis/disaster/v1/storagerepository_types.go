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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type StatusType string
type StorageRepositoryAddressingStyle string

const (
	StorageRepositoryStatusAvailable   StatusType = "Available"
	StorageRepositoryStatusUnavailable StatusType = "Unavailable"

	StorageRepositoryAddressingStylePathStyle          StorageRepositoryAddressingStyle = "PathStyle"
	StorageRepositoryAddressingStyleVirtualHostedStyle StorageRepositoryAddressingStyle = "VirtualHostedStyle"
	StorageRepositoryCASecretKey                                                        = "ca.crt"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// StorageRepositorySpec defines the desired state of StorageRepository
type StorageRepositorySpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	// The following markers will use OpenAPI v3 schema to validate the value
	// More info: https://book.kubebuilder.io/reference/markers/crd-validation.html

	StorageType string `json:"storageType"`
	Bucket      string `json:"bucket"`
	Region      string `json:"region"`
	Endpoint    string `json:"endpoint"`
	AccessKey   string `json:"accessKey"`
	SecretKey   string `json:"secretKey"`
	// AddressingStyle controls the S3 bucket addressing mode used by validation and runtime.
	// +kubebuilder:default=PathStyle
	// +kubebuilder:validation:Enum=PathStyle;VirtualHostedStyle
	// +optional
	AddressingStyle StorageRepositoryAddressingStyle `json:"addressingStyle,omitempty"`
	// CASecretRef references a Secret in the same namespace whose `ca.crt` entry contains a custom CA bundle.
	// +optional
	CASecretRef *corev1.LocalObjectReference `json:"caSecretRef,omitempty"`
	// QuotaBytes defines the storage limit
	// +optional
	QuotaBytes int64 `json:"quotaBytes,omitempty"`
}

func (s StorageRepositorySpec) GetAddressingStyle() StorageRepositoryAddressingStyle {
	if s.AddressingStyle == "" {
		return StorageRepositoryAddressingStylePathStyle
	}
	return s.AddressingStyle
}

func (s StorageRepositorySpec) UsesPathStyle() bool {
	return s.GetAddressingStyle() != StorageRepositoryAddressingStyleVirtualHostedStyle
}

// StorageRepositoryStatus defines the observed state of StorageRepository.
type StorageRepositoryStatus struct {
	Status StatusType `json:"status"`

	// LastCheckTime records the time when the storage was last checked
	LastCheckTime *metav1.Time `json:"lastCheckTime,omitempty"`

	// Reason indicates the reason for the current status
	Reason string `json:"reason,omitempty"`

	// Message provides a detailed message about the current status
	Message string `json:"message,omitempty"`

	// LastEventPhase records the last event phase emitted, used to avoid duplicate events
	// +optional
	LastEventPhase string `json:"lastEventPhase,omitempty"`

	// ReadyTimestamp records when the storage became Available, used for Duration calculation
	// +optional
	ReadyTimestamp *metav1.Time `json:"readyTimestamp,omitempty"`

	// ObservedGeneration records the generation that was last processed
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// ObservedMetadataHash records the hash of the metadata (labels/annotations) that was last processed
	// +optional
	ObservedMetadataHash string `json:"observedMetadataHash,omitempty"`

	// UsedSpaceBytes records the total bytes used by backup objects in the storage repository
	// +optional
	UsedSpaceBytes int64 `json:"usedSpaceBytes,omitempty"`

	// TotalBackupCount records the physical backup objects count inside S3 based on backups/ prefix
	// +optional
	TotalBackupCount int64 `json:"totalBackupCount,omitempty"`
}

// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.status",description="StorageRepository Status"

// StorageRepository is the Schema for the storagerepositories API
type StorageRepository struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of StorageRepository
	// +required
	Spec StorageRepositorySpec `json:"spec"`

	// status defines the observed state of StorageRepository
	// +optional
	Status StorageRepositoryStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// StorageRepositoryList contains a list of StorageRepository
type StorageRepositoryList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []StorageRepository `json:"items"`
}

func init() {
	SchemeBuilder.Register(&StorageRepository{}, &StorageRepositoryList{})
}
