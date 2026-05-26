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

// DisasterGroupPolicy 定义容灾组执行策略
type DisasterGroupPolicy struct {
	// FailPolicy 失败处理策略: "Continue" (忽略错误继续执行) / "Stop" (停止执行)
	// +optional
	FailPolicy string `json:"failPolicy,omitempty"`

	// TimeoutMin 单个 Level 的超时时间(分钟)。超过则根据 FailPolicy 处理。
	// +optional
	TimeoutMin int `json:"timeoutMin,omitempty"`

	// Parallelism 全局最大并行实例数。0 表示无限制。
	// +optional
	Parallelism int `json:"parallelism,omitempty"`

	// RetryPolicy 默认重试策略
	// +optional
	RetryPolicy *RetryPolicy `json:"retryPolicy,omitempty"`
}

// DisasterGroupSpec 定义 DisasterGroup 的期望状态
type DisasterGroupSpec struct {
	// Levels 定义切换顺序，二维数组。
	// Index 0 (Level 1) 最先执行。同一 Level 内并行执行。
	Levels [][]string `json:"levels"`

	// Policy 策略配置
	// +optional
	Policy DisasterGroupPolicy `json:"policy,omitempty"`
}

// DisasterGroupStatus 定义 DisasterGroup 的观测状态
type DisasterGroupStatus struct {
	// TotalInstances 组内实例总数
	// +optional
	TotalInstances int `json:"totalInstances"`

	// ReadyInstances 处于 Protected 状态的实例数
	// +optional
	ReadyInstances int `json:"readyInstances"`

	// Reason 机器可读错误码
	// +optional
	Reason string `json:"reason,omitempty"`

	// Message 人类可读错误描述
	// +optional
	Message string `json:"message,omitempty"`

	// Conditions 聚合状态 (如 GroupReady, GroupFailingOver)
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=dg
// +kubebuilder:printcolumn:name="Total",type="integer",JSONPath=".status.totalInstances",description="实例总数"
// +kubebuilder:printcolumn:name="Ready",type="integer",JSONPath=".status.readyInstances",description="就绪实例数"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// DisasterGroup 是容灾组的 API Schema
// 它定义了一组 DisasterInstance 及其切换顺序
type DisasterGroup struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DisasterGroupSpec   `json:"spec,omitempty"`
	Status DisasterGroupStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// DisasterGroupList 包含 DisasterGroup 列表
type DisasterGroupList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DisasterGroup `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DisasterGroup{}, &DisasterGroupList{})
}
