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

// DataSync 状态常量
const (
	DataSyncStateReady      = "Ready"      // 就绪状态
	DataSyncStateInProgress = "InProgress" // 同步进行中
	DataSyncStateFailed     = "Failed"     // 同步失败
)

// TriggerSpec 定义同步的触发方式
type TriggerSpec struct {
	// Schedule 定时调度表达式（从 DisasterPolicy 继承）
	// 只读字段，由引用的策略填充
	// +optional
	Schedule string `json:"schedule,omitempty"`

	// Manual 手动触发时间戳
	// 设置为当前时间以触发立即同步
	// +optional
	Manual string `json:"manual,omitempty"`
}

// TrafficlessConfig 定义 Trafficless Restore 的配置
// 创建不接收流量的"隐形 Pod"来接收 FSB 数据
type TrafficlessConfig struct {
	// Image 隐形 Pod 使用的容器镜像（默认: busybox:1.36）
	// +optional
	// +kubebuilder:default="busybox:1.36"
	Image string `json:"image,omitempty"`

	// Command 隐形 Pod 的启动命令（默认: ["sleep", "3600"]）
	// +optional
	Command []string `json:"command,omitempty"`

	// RemoveLabels 需要移除的标签列表，防止 Pod 接收服务流量
	// +optional
	RemoveLabels []string `json:"removeLabels,omitempty"`
}

// DataSyncSpec 定义 DataSync 的期望状态
type DataSyncSpec struct {
	// Instance 父级 DisasterInstance 的名称
	// +required
	Instance string `json:"instance"`

	// Trigger 定义同步的触发方式
	// +optional
	Trigger TriggerSpec `json:"trigger,omitempty"`

	// Paused 指示是否暂停同步
	// +optional
	Paused bool `json:"paused,omitempty"`

	// TrafficlessConfig 定义 Trafficless Restore 的设置
	// +optional
	TrafficlessConfig *TrafficlessConfig `json:"trafficlessConfig,omitempty"`
}

// TrafficlessPodStatus 表示隐形 Pod 的状态
type TrafficlessPodStatus struct {
	// Name Pod 名称
	Name string `json:"name"`
	// Namespace Pod 所在命名空间
	Namespace string `json:"namespace"`
	// PVCName 该 Pod 挂载的 PVC 名称
	PVCName string `json:"pvcName,omitempty"`
	// Phase Pod 阶段（Running, Completed 等）
	Phase string `json:"phase,omitempty"`
}

// DataSyncStatus 定义 DataSync 的观测状态
type DataSyncStatus struct {
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

	// TrafficlessPods 当前隐形 Pod 列表
	// +optional
	TrafficlessPods []TrafficlessPodStatus `json:"trafficlessPods,omitempty"`

	// History 记录最近的同步历史
	// +optional
	History []SyncHistoryRecord `json:"history,omitempty"`

	// Conditions 最新的观测状态
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=ds
// +kubebuilder:printcolumn:name="Instance",type="string",JSONPath=".spec.instance",description="父级 DisasterInstance"
// +kubebuilder:printcolumn:name="State",type="string",JSONPath=".status.state",description="当前状态"
// +kubebuilder:printcolumn:name="LastSync",type="date",JSONPath=".status.lastSyncTime",description="上次同步时间"
// +kubebuilder:printcolumn:name="Paused",type="boolean",JSONPath=".spec.paused",description="是否暂停"

// DataSync 是数据同步的 API Schema
// 使用 Trafficless Restore 方案管理 PVC 数据同步
type DataSync struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DataSyncSpec   `json:"spec,omitempty"`
	Status DataSyncStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// DataSyncList 包含 DataSync 列表
type DataSyncList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DataSync `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DataSync{}, &DataSyncList{})
}
