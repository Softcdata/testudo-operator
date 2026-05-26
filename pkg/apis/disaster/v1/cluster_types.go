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

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

type PhaseType string
type VeleroStatus string

// ImageSource defines an available image registry source on a cluster.
// It is referenced by DisasterInstance image rewrite mappings.
type ImageSource struct {
	// Name is a readable alias and must be unique within one Cluster.
	Name string `json:"name"`
	// Registry is the registry host/prefix, e.g. harbor.prod.local.
	Registry string `json:"registry"`
}

// VeleroInstallSpec defines cluster-scoped Velero installation settings.
type VeleroInstallSpec struct {
	// ImageRegistry is the registry/repository prefix used for Velero install images.
	// Example: harbor.customer.local/disaster
	// +optional
	ImageRegistry string `json:"imageRegistry,omitempty"`
	// RegistryCredentialSecretRef references the management-plane dockerconfigjson Secret.
	// +optional
	RegistryCredentialSecretRef *corev1.LocalObjectReference `json:"registryCredentialSecretRef,omitempty"`
}

const (
	ClustreStatusPending  StatusType = "Pending"
	ClusterStatusReady    StatusType = "Ready"
	ClusterStatusNotReady StatusType = "NotReady"
	ClusterStatusDeleting StatusType = "Deleting"
)

// ClusterSpec defines the desired state of Cluster
type ClusterSpec struct {
	// +optional
	Token string `json:"token"`
	// +optional
	Endpoint string `json:"endpoint"`
	// +optional
	KubeConfig []byte `json:"kubeConfig,omitempty"`
	// ImageSources defines reusable registry aliases for this cluster.
	// +optional
	ImageSources []ImageSource `json:"imageSources,omitempty"`
	// VeleroInstall defines Velero install-specific image registry settings.
	// +optional
	VeleroInstall *VeleroInstallSpec `json:"veleroInstall,omitempty"`
}

// ClusterStatus defines the observed state of Cluster.
type ClusterStatus struct {
	Status        StatusType `json:"status"`
	Endpoint      string     `json:"endpoint"`
	K8SVersion    string     `json:"k8sVersion"`
	VeleroVersion string     `json:"veleroVersion"`
	// +optional
	Reason string `json:"reason,omitempty"`
	// +optional
	Message   string `json:"message,omitempty"`
	NodeCount int    `json:"nodeCount,omitempty"`
	// NamespaceCount is the number of non-system namespaces tracked by the cluster backup statistics.
	NamespaceCount int `json:"namespaceCount,omitempty"`
	// ResourceTotalCount is the total number of namespace-scoped backup-scope resources across tracked namespaces.
	ResourceTotalCount int `json:"resourceTotalCount,omitempty"`
	// NamespaceStats records the backup-scope resource count per tracked namespace.
	NamespaceStats map[string]int `json:"namespaceStats,omitempty"`
	// WorkloadNamespaceCount is the number of namespaces that contain running Deployment/StatefulSet workloads
	WorkloadNamespaceCount int `json:"workloadNamespaceCount,omitempty"`
	// WorkloadTotalCount is the total number of namespace-scoped backup-scope resources across tracked namespaces that contain running Deployment/StatefulSet workloads.
	WorkloadTotalCount int `json:"workloadTotalCount,omitempty"`
	// WorkloadNamespaceStats records the backup-scope resource count per tracked namespace for namespaces that contain running Deployment/StatefulSet workloads.
	WorkloadNamespaceStats map[string]int `json:"workloadNamespaceStats,omitempty"`

	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// ObservedMetadataHash records the hash of the metadata (labels/annotations) that was last processed
	// +optional
	ObservedMetadataHash string `json:"observedMetadataHash,omitempty"`

	// LastCheckTime records the time when the stats were last collected
	LastCheckTime *metav1.Time `json:"lastCheckTime,omitempty"`

	// TokenExpiration records the expiration time of the token if it is a JWT
	// +optional
	TokenExpiration *metav1.Time `json:"tokenExpiration,omitempty"`

	// LastEventPhase records the last event phase emitted, used to avoid duplicate events
	// +optional
	LastEventPhase string `json:"lastEventPhase,omitempty"`

	// ReadyTimestamp records when the cluster became Ready, used for Duration calculation
	// +optional
	ReadyTimestamp *metav1.Time `json:"readyTimestamp,omitempty"`
}

// +genclient
// +genclient:nonNamespaced
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=cluster
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.status",description="Cluster phase"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Velero",type="string",JSONPath=".status.veleroVersion",description="Velero version"
// +kubebuilder:printcolumn:name="Version",type="string",JSONPath=".status.k8sVersion",description="Cluster version"
// +kubebuilder:printcolumn:name="Endpoint",type="string",JSONPath=".status.endpoint",description="Cluster Serever"

// Cluster is the Schema for the clusters API
type Cluster struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of Cluster
	// +required
	Spec ClusterSpec `json:"spec"`

	// status defines the observed state of Cluster
	// +optional
	Status ClusterStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// ClusterList contains a list of Cluster
type ClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Cluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Cluster{}, &ClusterList{})
}
