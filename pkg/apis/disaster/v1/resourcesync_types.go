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

// ResourceSync 状态常量
const (
	ResourceSyncStateReady      = "Ready"      // 就绪状态
	ResourceSyncStateInProgress = "InProgress" // 同步进行中
	ResourceSyncStateFailed     = "Failed"     // 同步失败
)

// StandbyModifierConfig 定义如何修改资源以实现 Standby 模式
type StandbyModifierConfig struct {
	// ScaleToZero 是否在恢复时将副本数设为 0
	// +optional
	// +kubebuilder:default=true
	ScaleToZero bool `json:"scaleToZero,omitempty"`

	// OriginalReplicaAnnotation 存储原始副本数的注解键
	// +optional
	// +kubebuilder:default="testudo.softcdata.com/original-replica-count"
	OriginalReplicaAnnotation string `json:"originalReplicaAnnotation,omitempty"`
}

// ResourceSyncSpec 定义 ResourceSync 的期望状态
type ResourceSyncSpec struct {
	// Instance 父级 DisasterInstance 的名称
	// +required
	Instance string `json:"instance"`

	// Trigger 定义同步的触发方式
	// +optional
	Trigger TriggerSpec `json:"trigger,omitempty"`

	// Paused 指示是否暂停同步
	// +optional
	Paused bool `json:"paused,omitempty"`

	// StandbyModifier 定义如何修改资源以实现 Standby 模式
	// +optional
	StandbyModifier *StandbyModifierConfig `json:"standbyModifier,omitempty"`

	// ExcludeResources 需要排除的资源类型列表
	// +optional
	ExcludeResources []string `json:"excludeResources,omitempty"`
}

// ResourceSyncStatus 定义 ResourceSync 的观测状态
type ResourceSyncStatus struct {
	// State 当前同步状态
	// +optional
	State string `json:"state,omitempty"`

	// Reason 机器可读错误码
	// +optional
	Reason string `json:"reason,omitempty"`

	// Message 人类可读错误描述
	// +optional
	Message string `json:"message,omitempty"`

	// LastSyncTime 上次同步时间
	// +optional
	LastSyncTime *metav1.Time `json:"lastSyncTime,omitempty"`

	// LastBackupName 上次创建的 AppBackup 名称
	// +optional
	LastBackupName string `json:"lastBackupName,omitempty"`

	// LastRestoreName 上次创建的 AppRestore 名称
	// +optional
	LastRestoreName string `json:"lastRestoreName,omitempty"`

	// LastClusterRestoreName 上次创建或观测到的 cluster phase AppRestore 名称
	// +optional
	LastClusterRestoreName string `json:"lastClusterRestoreName,omitempty"`

	// ClusterRestoreStatus 记录 cluster phase 的 restore 状态
	// +optional
	ClusterRestoreStatus AppRestorePhase `json:"clusterRestoreStatus,omitempty"`

	// LastNamespaceRestoreName 上次创建或观测到的 namespaced phase AppRestore 名称
	// +optional
	LastNamespaceRestoreName string `json:"lastNamespaceRestoreName,omitempty"`

	// NamespaceRestoreStatus 记录 namespaced phase 的 restore 状态
	// +optional
	NamespaceRestoreStatus AppRestorePhase `json:"namespaceRestoreStatus,omitempty"`

	// History 记录最近的同步历史
	// +optional
	History []SyncHistoryRecord `json:"history,omitempty"`

	// Conditions 最新的观测状态
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// SyncHistoryRecord 定义单次同步的历史记录
type SyncHistoryRecord struct {
	// StartTime 同步开始时间
	// +optional
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// CompletionTime 同步结束时间
	// +optional
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`

	// Duration 同步耗时
	// +optional
	Duration string `json:"duration,omitempty"`

	// BackupName 关联的 AppBackup 名称
	// +optional
	BackupName string `json:"backupName,omitempty"`

	// RestoreName 关联的 AppRestore 名称 (如果有)
	// +optional
	RestoreName string `json:"restoreName,omitempty"`

	// BackupResourceCount 备份资源数
	// +optional
	BackupResourceCount int `json:"backupResourceCount,omitempty"`

	// RestoreResourceCount 恢复资源数
	// +optional
	RestoreResourceCount int `json:"restoreResourceCount,omitempty"`

	// Status 同步结果状态 (Completed/Failed)
	// +optional
	Status string `json:"status,omitempty"`
}

// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=rs
// +kubebuilder:printcolumn:name="Instance",type="string",JSONPath=".spec.instance",description="父级 DisasterInstance"
// +kubebuilder:printcolumn:name="State",type="string",JSONPath=".status.state",description="当前状态"
// +kubebuilder:printcolumn:name="LastSync",type="date",JSONPath=".status.lastSyncTime",description="上次同步时间"
// +kubebuilder:printcolumn:name="Paused",type="boolean",JSONPath=".spec.paused",description="是否暂停"

// ResourceSync 是资源同步的 API Schema
// 管理 Kubernetes 资源同步（构建"资源骨架"）
type ResourceSync struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ResourceSyncSpec   `json:"spec,omitempty"`
	Status ResourceSyncStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ResourceSyncList 包含 ResourceSync 列表
type ResourceSyncList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ResourceSync `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ResourceSync{}, &ResourceSyncList{})
}
