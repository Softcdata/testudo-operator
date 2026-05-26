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

const (
	DisasterConfigStatusPending  StatusType = "Pending"
	DisasterConfigStatusReady    StatusType = "Ready"
	DisasterConfigStatusNotReady StatusType = "NotReady"
	DisasterConfigStatusError    StatusType = "Error"
)

type ResourcesSyncPolicy string

const (
	ResourcesSyncPolicyDefault ResourcesSyncPolicy = "default"
	ResourcesSyncPolicyIgnore  ResourcesSyncPolicy = "ignore"
)

// DisasterConfigSpec defines the desired state of DisasterConfig
type DisasterConfigSpec struct {
	// SourceCluster is the name of the source cluster (Cluster CR)
	// +required
	SourceCluster string `json:"sourceCluster"`

	// TargetCluster is the name of the target cluster (Cluster CR)
	// +required
	TargetCluster string `json:"targetCluster"`

	// StorageRepository is the name of the storage repository (StorageRepository CR)
	// +required
	StorageRepository string `json:"storageRepository"`

	// DataSyncPolicy is the name of the DisasterPolicy for data synchronization
	// The referenced policy must have type=DataSync
	// +optional
	DataSyncPolicy string `json:"dataSyncPolicy,omitempty"`

	// ResourceSyncPolicy is the name of the DisasterPolicy for resource synchronization
	// The referenced policy must have type=ResourceSync
	// +optional
	ResourceSyncPolicy string `json:"resourceSyncPolicy,omitempty"`

	// DataSyncType specifies the data synchronization type
	// Options: "fsb" (File System Backup), "snapshot", "none", "external"
	// +optional
	// +kubebuilder:default="fsb"
	DataSyncType string `json:"dataSyncType,omitempty"`

	// ImageRewrite defines image source mapping strategy for restore workflows.
	// +optional
	ImageRewrite *ImageRewriteConfig `json:"imageRewrite,omitempty"`

	// ========== Legacy fields (deprecated, kept for backward compatibility) ==========

	// Deprecated: Use DataSyncPolicy instead
	// +optional
	DataSyncInterval int `json:"dataSyncInterval,omitempty"`

	// Deprecated: Use ResourceSyncPolicy instead
	// +optional
	ResourcesSyncInterval int `json:"resourcesSyncInterval,omitempty"`

	// Deprecated: Not used in V2
	// +optional
	ResourcesSyncPolicy int `json:"resourcesSyncPolicy,omitempty"`
}

// DisasterConfigStatus defines the observed state of DisasterConfig.
type DisasterConfigStatus struct {
	Status  StatusType `json:"status,omitempty"` // 灾备配置状态
	Reason  string     `json:"reason,omitempty"`
	Message string     `json:"message,omitempty"`
}

// +genclient
// +genclient:nonNamespaced
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=dc
// +kubebuilder:printcolumn:name="SourceCluster",type="string",JSONPath=".spec.sourceCluster",description="灾备源集群名称"
// +kubebuilder:printcolumn:name="TargetCluster",type="string",JSONPath=".spec.targetCluster",description="灾备目标集群名称"
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.status",description="灾备配置状态"

// DisasterConfig is the Schema for the disasterconfigs API
type DisasterConfig struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of DisasterConfig
	// +required
	Spec DisasterConfigSpec `json:"spec"`

	// status defines the observed state of DisasterConfig
	// +optional
	Status DisasterConfigStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// DisasterConfigList contains a list of DisasterConfig
type DisasterConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DisasterConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DisasterConfig{}, &DisasterConfigList{})
}
