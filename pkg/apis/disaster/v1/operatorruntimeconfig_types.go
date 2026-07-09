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

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

const (
	OperatorRuntimeConfigConditionReady   = "Ready"
	OperatorRuntimeConfigConditionInvalid = "Invalid"
)

// OperatorRuntimeConfigSpec defines hot-reloadable operator runtime knobs.
// Range validation is intentionally handled by the controller so semantically
// invalid objects can be persisted, rejected from activation, and reflected in status.
type OperatorRuntimeConfigSpec struct {
	BackupRuntime    *BackupRuntimeConfigSpec    `json:"backupRuntime,omitempty"`
	RestoreRuntime   *RestoreRuntimeConfigSpec   `json:"restoreRuntime,omitempty"`
	OperationRuntime *OperationRuntimeConfigSpec `json:"operationRuntime,omitempty"`
	InstanceRuntime  *InstanceRuntimeConfigSpec  `json:"instanceRuntime,omitempty"`
	SyncRuntime      *SyncRuntimeConfigSpec      `json:"syncRuntime,omitempty"`
	StorageRuntime   *StorageRuntimeConfigSpec   `json:"storageRuntime,omitempty"`
	ClusterRuntime   *ClusterRuntimeConfigSpec   `json:"clusterRuntime,omitempty"`
}

type BackupRuntimeConfigSpec struct {
	InProgressMaxWait *metav1.Duration `json:"inProgressMaxWait,omitempty"`
	UnknownMaxWait    *metav1.Duration `json:"unknownMaxWait,omitempty"`
	PollInterval      *metav1.Duration `json:"pollInterval,omitempty"`
}

type RestoreRuntimeConfigSpec struct {
	InProgressMaxWait           *metav1.Duration `json:"inProgressMaxWait,omitempty"`
	UnknownMaxWait              *metav1.Duration `json:"unknownMaxWait,omitempty"`
	InProgressPollInterval      *metav1.Duration `json:"inProgressPollInterval,omitempty"`
	UnknownPollInterval         *metav1.Duration `json:"unknownPollInterval,omitempty"`
	ProgressCompleteGrace       *metav1.Duration `json:"progressCompleteGrace,omitempty"`
	StartupGrace                *metav1.Duration `json:"startupGrace,omitempty"`
	MissingGrace                *metav1.Duration `json:"missingGrace,omitempty"`
	EmptyStatusGrace            *metav1.Duration `json:"emptyStatusGrace,omitempty"`
	PodVolumeRestorePendingWait *metav1.Duration `json:"podVolumeRestorePendingMaxWait,omitempty"`
	RetryBackoff                *metav1.Duration `json:"retryBackoff,omitempty"`
	RetryLimit                  *int32           `json:"retryLimit,omitempty"`
	RetryLimitProgress          *int32           `json:"retryLimitProgress,omitempty"`
	RetryLimitStartup           *int32           `json:"retryLimitStartup,omitempty"`
	RetryLimitMissing           *int32           `json:"retryLimitMissing,omitempty"`
	RetryLimitEmpty             *int32           `json:"retryLimitEmpty,omitempty"`
}

type OperationRuntimeConfigSpec struct {
	DefaultTimeoutMinutes *int32           `json:"defaultTimeoutMinutes,omitempty"`
	StepStartRequeue      *metav1.Duration `json:"stepStartRequeue,omitempty"`
	StepRunningRequeue    *metav1.Duration `json:"stepRunningRequeue,omitempty"`
	DefaultRetryInterval  *metav1.Duration `json:"defaultRetryInterval,omitempty"`
}

type InstanceRuntimeConfigSpec struct {
	TransitionWatchdogTimeout    *metav1.Duration `json:"transitionWatchdogTimeout,omitempty"`
	MinTransitionWatchdogTimeout *metav1.Duration `json:"minTransitionWatchdogTimeout,omitempty"`
	InitializingRequeue          *metav1.Duration `json:"initializingRequeue,omitempty"`
	SteadyRequeue                *metav1.Duration `json:"steadyRequeue,omitempty"`
	FailedRequeue                *metav1.Duration `json:"failedRequeue,omitempty"`
}

type SyncRuntimeConfigSpec struct {
	SchedulerUpdateTimeout  *metav1.Duration `json:"schedulerUpdateTimeout,omitempty"`
	BackupObserveRequeue    *metav1.Duration `json:"backupObserveRequeue,omitempty"`
	BackupInProgressRequeue *metav1.Duration `json:"backupInProgressRequeue,omitempty"`
	HistoryMissingRequeue   *metav1.Duration `json:"historyMissingRequeue,omitempty"`
	RestoreObserveRequeue   *metav1.Duration `json:"restoreObserveRequeue,omitempty"`
	HistoryRetention        *int32           `json:"historyRetention,omitempty"`
}

type StorageRuntimeConfigSpec struct {
	RequeueInterval *metav1.Duration `json:"requeueInterval,omitempty"`
}

type ClusterRuntimeConfigSpec struct {
	ReconcileInterval         *metav1.Duration `json:"reconcileInterval,omitempty"`
	DeletionRetryInterval     *metav1.Duration `json:"deletionRetryInterval,omitempty"`
	VeleroInstallTimeout      *metav1.Duration `json:"veleroInstallTimeout,omitempty"`
	VeleroZombieLockThreshold *metav1.Duration `json:"veleroZombieLockThreshold,omitempty"`
}

type OperatorRuntimeConfigStatus struct {
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	ActiveGeneration   int64              `json:"activeGeneration,omitempty"`
	LastActivatedTime  *metav1.Time       `json:"lastActivatedTime,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=operatorruntimeconfigs,scope=Namespaced,shortName=orc
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Invalid",type="string",JSONPath=".status.conditions[?(@.type=='Invalid')].status"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type OperatorRuntimeConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   OperatorRuntimeConfigSpec   `json:"spec,omitempty"`
	Status OperatorRuntimeConfigStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type OperatorRuntimeConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []OperatorRuntimeConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(&OperatorRuntimeConfig{}, &OperatorRuntimeConfigList{})
}
