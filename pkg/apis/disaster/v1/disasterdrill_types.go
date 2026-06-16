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

// DrillState 定义演练的状态
type DrillState string

const (
	// DrillStatePending 创建中，正在自检
	DrillStatePending DrillState = "Pending"
	// DrillStateReady 校验通过，等待用户确认
	DrillStateReady DrillState = "Ready"
	// DrillStateExecuting 执行中 (恢复 + 扩容)
	DrillStateExecuting DrillState = "Executing"
	// DrillStateCompleted 完成
	DrillStateCompleted DrillState = "Completed"
	// DrillStateCleaningUp 清理中
	DrillStateCleaningUp DrillState = "CleaningUp"
	// DrillStateCleanedUp 已清理
	DrillStateCleanedUp DrillState = "CleanedUp"
	// DrillStateFailed 失败
	DrillStateFailed DrillState = "Failed"
)

// RestoreMode 定义恢复模式
type RestoreMode string

const (
	// RestoreModeReuse 复用模式：目标集群=备集群且无命名空间映射，跳过恢复直接扩容
	RestoreModeReuse RestoreMode = "Reuse"
	// RestoreModeFullRestore 完整恢复模式：需要创建 AppRestore 恢复数据
	RestoreModeFullRestore RestoreMode = "FullRestore"
)

// DrillStep 定义演练执行步骤
type DrillStep string

const (
	// DrillStepValidation 校验阶段
	DrillStepValidation DrillStep = "Validation"
	// DrillStepRestore 恢复阶段 (仅完整恢复模式)
	DrillStepRestore DrillStep = "Restore"
	// DrillStepScaleUp 扩容阶段
	DrillStepScaleUp DrillStep = "ScaleUp"
)

// DisasterDrillSpec 定义演练规格
type DisasterDrillSpec struct {
	// InstanceName 关联的容灾实例名称 (与 GroupName 二选一)
	// +optional
	InstanceName string `json:"instanceName,omitempty"`

	// GroupName 关联的容灾组名称 (与 InstanceName 二选一)
	// +optional
	GroupName string `json:"groupName,omitempty"`

	// TargetCluster 演练目标集群 (可选，不指定则使用 Instance 的 secondaryCluster)
	// +optional
	TargetCluster string `json:"targetCluster,omitempty"`

	// NamespaceMapping 命名空间映射 (可选，不指定则使用原始命名空间)
	// 格式: 源命名空间 -> 目标命名空间
	// +optional
	NamespaceMapping map[string]string `json:"namespaceMapping,omitempty"`

	// SkipValidation 跳过前置校验
	// +optional
	SkipValidation bool `json:"skipValidation,omitempty"`

	// Confirmed 用户确认开始执行 (Ready -> Executing)
	// +optional
	Confirmed bool `json:"confirmed,omitempty"`

	// WaitUntilReady 是否等待 Pod 就绪
	// +optional
	WaitUntilReady bool `json:"waitUntilReady,omitempty"`

	// RestorePolicy 演练级 restorePolicy 覆盖配置。
	// 未提供时，默认继承 DisasterInstance 的 restorePolicy；
	// 提供时，演练链路会优先使用该配置中的资源定制化/bulk 修改等策略。
	// +optional
	RestorePolicy *RestorePolicy `json:"restorePolicy,omitempty"`

	// VeleroHooks defines drill-level Velero restore hooks.
	// Only dataRestore is used by drill data restore; dataBackup is rejected by the server API.
	// +optional
	VeleroHooks *DisasterVeleroHooks `json:"veleroHooks,omitempty"`

	// CleanUp 触发演练资源清理
	// +optional
	CleanUp bool `json:"cleanup,omitempty"`
}

// DisasterDrillStatus 定义演练状态
type DisasterDrillStatus struct {
	// State 当前状态
	// +kubebuilder:validation:Enum=Pending;Ready;Executing;Completed;CleaningUp;CleanedUp;Failed
	State DrillState `json:"state,omitempty"`

	// Reason 机器可读错误码
	// +optional
	Reason string `json:"reason,omitempty"`

	// OperationName 关联的 DisasterOperation 名称
	OperationName string `json:"operationName,omitempty"`

	// TargetCluster 实际使用的目标集群
	TargetCluster string `json:"targetCluster,omitempty"`

	// RestoreMode 恢复模式
	// +kubebuilder:validation:Enum=Reuse;FullRestore
	RestoreMode RestoreMode `json:"restoreMode,omitempty"`

	// RestoreName 演练创建的 AppRestore 名称 (仅完整恢复模式)
	RestoreName string `json:"restoreName,omitempty"`

	// CurrentStep 当前执行步骤
	CurrentStep string `json:"currentStep,omitempty"`

	// Steps 步骤详情
	Steps []StepStatus `json:"steps,omitempty"`

	// StartTime 创建时间
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// ReadyTime 校验完成时间
	ReadyTime *metav1.Time `json:"readyTime,omitempty"`

	// ExecutionTime 用户确认执行时间
	ExecutionTime *metav1.Time `json:"executionTime,omitempty"`

	// CompletionTime 完成时间
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`

	// Message 状态消息
	Message string `json:"message,omitempty"`

	// ValidationResults 校验结果
	ValidationResults *DrillValidationResults `json:"validationResults,omitempty"`

	// GroupProgress 容灾组演练进度
	// +optional
	GroupProgress *GroupStatus `json:"groupProgress,omitempty"`
}

// DrillValidationResults 校验结果
type DrillValidationResults struct {
	// InstanceValid Instance 状态是否有效
	InstanceValid bool `json:"instanceValid,omitempty"`
	// ClusterReachable 目标集群是否可达
	ClusterReachable bool `json:"clusterReachable,omitempty"`
	// BackupAvailable 备份是否可用
	BackupAvailable bool `json:"backupAvailable,omitempty"`
	// LastDataSyncTime 最近数据同步时间
	LastDataSyncTime *metav1.Time `json:"lastDataSyncTime,omitempty"`
	// LastResourceSyncTime 最近资源同步时间
	LastResourceSyncTime *metav1.Time `json:"lastResourceSyncTime,omitempty"`
}

// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=drill;drills
// +kubebuilder:printcolumn:name="Instance",type=string,JSONPath=`.spec.instanceName`
// +kubebuilder:printcolumn:name="Group",type=string,JSONPath=`.spec.groupName`
// +kubebuilder:printcolumn:name="State",type=string,JSONPath=`.status.state`
// +kubebuilder:printcolumn:name="RestoreMode",type=string,JSONPath=`.status.restoreMode`
// +kubebuilder:printcolumn:name="TargetCluster",type=string,JSONPath=`.status.targetCluster`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// DisasterDrill 容灾演练资源
type DisasterDrill struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DisasterDrillSpec   `json:"spec,omitempty"`
	Status DisasterDrillStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// DisasterDrillList 包含 DisasterDrill 列表
type DisasterDrillList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DisasterDrill `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DisasterDrill{}, &DisasterDrillList{})
}
