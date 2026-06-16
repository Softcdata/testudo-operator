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

// OperationType 定义容灾操作类型
type OperationType string

const (
	// OperationTypeFailover 从主集群切换到备集群
	OperationTypeFailover OperationType = "failover"
	// OperationTypeReprotect 反向保护：确认故障切换，将原备集群提升为新主集群，原主集群降级为新备集群，并建立反向同步
	OperationTypeReprotect OperationType = "reprotect"
	// OperationTypePause 暂停所有同步
	OperationTypePause OperationType = "pause"
	// OperationTypeResume 恢复同步
	OperationTypeResume OperationType = "resume"
	// OperationTypeSyncOnce 触发立即同步(所有)
	OperationTypeSyncOnce OperationType = "synconce"
	// OperationTypeSyncData 只触发数据同步
	OperationTypeSyncData OperationType = "syncdata"
	// OperationTypeSyncResource 只触发资源同步
	OperationTypeSyncResource OperationType = "syncresource"
	// OperationTypeUndo 撤销切换：放弃当前的故障切换，缩容当前运行的备集群，重新拉起原主集群，并恢复原有的同步关系
	OperationTypeUndo OperationType = "undo"
	// OperationTypeCancel 取消切换：中止故障切换并复原
	OperationTypeCancel OperationType = "cancel"
	// OperationTypeDrill 容灾演练：恢复+扩容目标集群进行测试，不影响源集群
	OperationTypeDrill OperationType = "drill"
	// OperationTypeDrillCleanup 容灾演练清理：根据是否配置了Namespace映射来决定是缩容目标集群负载还是删除演练命名空间/资源
	OperationTypeDrillCleanup OperationType = "drill-cleanup"
)

// OperationState 定义操作的状态
type OperationState string

const (
	OperationStatePending   OperationState = "Pending"   // 待执行
	OperationStateRunning   OperationState = "Running"   // 执行中
	OperationStateCompleted OperationState = "Completed" // 已完成
	OperationStateFailed    OperationState = "Failed"    // 失败
)

// OperationAutoCancelStatus 定义 failover 失败后自动补偿的执行状态
type OperationAutoCancelStatus string

const (
	OperationAutoCancelStatusNotTriggered OperationAutoCancelStatus = "NotTriggered"
	OperationAutoCancelStatusRunning      OperationAutoCancelStatus = "Running"
	OperationAutoCancelStatusSucceeded    OperationAutoCancelStatus = "Succeeded"
	OperationAutoCancelStatusFailed       OperationAutoCancelStatus = "Failed"
)

// OperationAutoCancelMode 定义 failover 失败后自动补偿的分流模式
type OperationAutoCancelMode string

const (
	OperationAutoCancelModeDirectRollback OperationAutoCancelMode = "DirectRollback"
	OperationAutoCancelModeCancelPath     OperationAutoCancelMode = "CancelPath"
	OperationAutoCancelModeNoAutoCancel   OperationAutoCancelMode = "NoAutoCancel"
)

// FailoverStep 定义故障切换操作的步骤
type FailoverStep string

const (
	FailoverStepPreCheck        FailoverStep = "PreCheck"        // 预检查
	FailoverStepPauseSchedules  FailoverStep = "PauseSchedules"  // 暂停调度
	FailoverStepScaleDownSource FailoverStep = "ScaleDownSource" // 缩容源集群
	FailoverStepFinalSync       FailoverStep = "FinalSync"       // 最终同步
	FailoverStepScaleUpTarget   FailoverStep = "ScaleUpTarget"   // 扩容目标集群
	FailoverStepCheckReplicas   FailoverStep = "CheckReplicas"   // 检查副本数（是否达到期望值）
	FailoverStepSwitchRoles     FailoverStep = "SwitchRoles"     // 切换角色
	FailoverStepResumeSchedules FailoverStep = "ResumeSchedules" // 恢复调度（Reprotect 使用）
)

// UndoStep 定义撤销操作的步骤
type UndoStep string

const (
	UndoStepScaleDownTarget UndoStep = "ScaleDownTarget" // 缩容当前目标集群(B)
	UndoStepSwitchRoles     UndoStep = "SwitchRoles"     // 恢复角色(A主B备)
	UndoStepScaleUpSource   UndoStep = "ScaleUpSource"   // 扩容原源集群(A)
	UndoStepResumeSchedules UndoStep = "ResumeSchedules" // 恢复调度
)

// CancelStep 定义取消操作的步骤
type CancelStep string

const (
	CancelStepScaleDownTarget CancelStep = "ScaleDownTarget"
	CancelStepScaleUpSource   CancelStep = "ScaleUpSource"
	CancelStepResumeSchedules CancelStep = "ResumeSchedules"
)

// DrillStep 定义演练操作的步骤
type DrillOperationStep string

const (
	// DrillOperationStepValidation 校验阶段
	DrillOperationStepValidation DrillOperationStep = "Validation"
	// DrillOperationStepRestoreResource 恢复资源阶段 (从 ResourceSync 备份恢复 K8s 资源)
	DrillOperationStepRestoreResource DrillOperationStep = "RestoreResource"
	// DrillOperationStepRestoreData 恢复数据阶段 (从 DataSync 备份恢复 PVC 数据)
	DrillOperationStepRestoreData DrillOperationStep = "RestoreData"
	// DrillOperationStepScaleUp 扩容阶段
	DrillOperationStepScaleUp DrillOperationStep = "ScaleUp"
)

// DrillConfig 演练配置
type DrillConfig struct {
	// TargetCluster 演练目标集群 (可选，不指定则使用 Instance 的 secondaryCluster)
	// +optional
	TargetCluster string `json:"targetCluster,omitempty"`

	// NamespaceMapping 命名空间映射 (可选)
	// 格式: 源命名空间 -> 目标命名空间
	// +optional
	NamespaceMapping map[string]string `json:"namespaceMapping,omitempty"`

	// SkipValidation 跳过前置校验
	// +optional
	SkipValidation bool `json:"skipValidation,omitempty"`

	// RestorePolicy 演练级 restorePolicy 覆盖配置。
	// 仅 drill/drill-cleanup 相关编排使用，未提供时继承实例 restorePolicy。
	// +optional
	RestorePolicy *RestorePolicy `json:"restorePolicy,omitempty"`

	// VeleroHooks carries drill-level Velero hook overrides into operation execution.
	// Only dataRestore is used by drill data restore.
	// +optional
	VeleroHooks *DisasterVeleroHooks `json:"veleroHooks,omitempty"`
}

// OperationDirective 运行时指令
type OperationDirective struct {
	// Confirmed 用户确认执行 (用于 drill 等需要二次确认的操作)
	// +optional
	Confirmed bool `json:"confirmed,omitempty"`
}

// StepStatus 表示单个步骤的状态
type StepStatus struct {
	// Name 步骤名称
	Name string `json:"name"`

	// State 步骤状态 (Pending/Running/Completed/Failed/Skipped)
	State string `json:"state"`

	// StartTime 步骤开始时间
	// +optional
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// CompletionTime 步骤完成时间
	// +optional
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`

	// Message 附加信息
	// +optional
	Message string `json:"message,omitempty"`
}

// RoleStatus 表示操作后的角色分配
type RoleStatus struct {
	// PrimaryCluster 新的主集群
	PrimaryCluster string `json:"primaryCluster"`

	// SecondaryCluster 新的备集群
	SecondaryCluster string `json:"secondaryCluster"`
}

// LevelStatus 记录 Group 操作中每一层的执行状态
type LevelStatus struct {
	// Index 层级索引 (0-based)
	Index int `json:"index"`
	// Instances 该层包含的实例列表
	Instances []string `json:"instances"`
	// State 状态 (Pending, Running, Completed, Failed, TimedOut)
	State string `json:"state"`
	// StartTime 开始时间
	StartTime *metav1.Time `json:"startTime,omitempty"`
	// CompletionTime 完成时间
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`
	// FailedInstances 执行失败的实例
	FailedInstances []string `json:"failedInstances,omitempty"`
}

// GroupStatus 记录 Group 操作的整体状态
type GroupStatus struct {
	// TotalLevels 总层数
	TotalLevels int `json:"totalLevels"`
	// CurrentLevelIndex 当前执行的层级 (0-based)
	CurrentLevelIndex int `json:"currentLevelIndex"`
	// LevelStatuses 各层详细状态
	LevelStatuses []LevelStatus `json:"levelStatuses,omitempty"`
}

// DisasterOperationSpec 定义 DisasterOperation 的期望状态
type DisasterOperationSpec struct {
	// InstanceName 要操作的 DisasterInstance 名称 (与 GroupName 二选一)
	// +optional
	InstanceName string `json:"instanceName,omitempty"`

	// GroupName 要操作的 DisasterGroup 名称 (与 InstanceName 二选一)
	// +optional
	GroupName string `json:"groupName,omitempty"`

	// OperationType 要执行的操作类型
	// +required
	// +kubebuilder:validation:Enum=failover;reprotect;pause;resume;synconce;syncdata;syncresource;undo;cancel;drill;drill-cleanup
	OperationType OperationType `json:"operationType"`

	// DrillConfig 演练配置 (仅当 operationType=drill 时有效)
	// +optional
	DrillConfig *DrillConfig `json:"drillConfig,omitempty"`

	// Directive 运行时指令
	// +optional
	Directive *OperationDirective `json:"directive,omitempty"`

	// Force 是否强制执行操作（跳过不可达的源集群步骤）
	// +optional
	Force bool `json:"force,omitempty"`

	// SkipFinalSync 是否跳过故障切换中的最终同步步骤
	// +optional
	SkipFinalSync bool `json:"skipFinalSync,omitempty"`

	// SkipScaleDownSource 是否跳过故障切换中的源集群缩零步骤
	// 仅对 operationType=failover 生效
	// +optional
	SkipScaleDownSource bool `json:"skipScaleDownSource,omitempty"`

	// TimeoutMinutes 本次操作的超时时间(分钟)。
	// 如果未指定，则继承 DisasterInstance 的 OperationTimeoutMinutes。
	// +optional
	TimeoutMinutes int32 `json:"timeoutMinutes,omitempty"`

	// SkipPodReadyCheck 是否跳过本次操作中的 Pod 就绪校验。
	// 该字段用于覆盖 DisasterInstance.spec.skipPodReadyCheck 默认策略。
	// +optional
	SkipPodReadyCheck *bool `json:"skipPodReadyCheck,omitempty"`

	// WaitUntilReady 在故障切换时，是否等待所有 Pod 就绪才认为切换成功。
	// 如果为 false，仅检查期望副本数是否下发。如果为 true，会检查 .status.readyReplicas。
	// 建议优先使用 SkipPodReadyCheck 进行显式覆盖。
	// +optional
	WaitUntilReady bool `json:"waitUntilReady,omitempty"`

	// RetryPolicy 重试策略
	// +optional
	RetryPolicy *RetryPolicy `json:"retryPolicy,omitempty"`
}

// RetryPolicy 定义重试策略
type RetryPolicy struct {
	// MaxRetries 最大重试次数，默认为 0（不重试）
	// +kubebuilder:default=0
	MaxRetries int32 `json:"maxRetries,omitempty"`

	// RetryIntervalSeconds 重试间隔秒数，默认为 5
	// +kubebuilder:default=5
	RetryIntervalSeconds int32 `json:"retryIntervalSeconds,omitempty"`
}

// DisasterOperationStatus 定义 DisasterOperation 的观测状态
type DisasterOperationStatus struct {
	// State 当前操作状态
	// +optional
	State OperationState `json:"state,omitempty"`

	// Reason 机器可读错误码
	// +optional
	Reason string `json:"reason,omitempty"`

	// StartTime 操作开始时间
	// +optional
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// CompletionTime 操作完成时间
	// +optional
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`

	// CurrentStep 当前执行的步骤
	// +optional
	CurrentStep string `json:"currentStep,omitempty"`

	// Steps 各步骤的状态
	// +optional
	Steps []StepStatus `json:"steps,omitempty"`

	// AutoCancelTriggered 标识 failover 失败后是否触发了自动补偿
	// +optional
	AutoCancelTriggered bool `json:"autoCancelTriggered,omitempty"`

	// AutoCancelStatus 自动补偿执行状态
	// +optional
	AutoCancelStatus OperationAutoCancelStatus `json:"autoCancelStatus,omitempty"`

	// AutoCancelMode 自动补偿分流模式
	// +optional
	AutoCancelMode OperationAutoCancelMode `json:"autoCancelMode,omitempty"`

	// AutoCancelReason 自动补偿触发原因或最终失败原因
	// +optional
	AutoCancelReason string `json:"autoCancelReason,omitempty"`

	// AutoCancelTriggerStep 触发自动补偿的 failover 步骤
	// +optional
	AutoCancelTriggerStep string `json:"autoCancelTriggerStep,omitempty"`

	// AutoCancelCurrentStep 当前执行中的自动补偿步骤
	// +optional
	AutoCancelCurrentStep string `json:"autoCancelCurrentStep,omitempty"`

	// AutoCancelSteps 自动补偿步骤状态
	// +optional
	AutoCancelSteps []StepStatus `json:"autoCancelSteps,omitempty"`

	// AutoCancelTriggeredAt 自动补偿触发时间
	// +optional
	AutoCancelTriggeredAt *metav1.Time `json:"autoCancelTriggeredAt,omitempty"`

	// AutoCancelCompletionTime 自动补偿完成时间
	// +optional
	AutoCancelCompletionTime *metav1.Time `json:"autoCancelCompletionTime,omitempty"`

	// ManualInterventionRequired 标识是否仍需人工介入
	// +optional
	ManualInterventionRequired bool `json:"manualInterventionRequired,omitempty"`

	// RoleStatus 操作后的角色分配
	// +optional
	RoleStatus *RoleStatus `json:"roleStatus,omitempty"`

	// GroupStatus 容灾组操作状态详情
	// +optional
	GroupStatus *GroupStatus `json:"groupStatus,omitempty"`

	// RetryCount 当前已重试次数
	// +optional
	RetryCount int32 `json:"retryCount,omitempty"`

	// NextRetryTime 下次重试时间
	// +optional
	NextRetryTime *metav1.Time `json:"nextRetryTime,omitempty"`

	// Message 操作的附加信息
	// +optional
	Message string `json:"message,omitempty"`

	// DataRestoreName 演练创建的数据恢复 AppRestore 名称 (仅 Drill 操作使用)
	// +optional
	DataRestoreName string `json:"dataRestoreName,omitempty"`

	// ResourceRestoreName 演练创建的资源恢复 AppRestore 名称 (仅 Drill 操作使用)
	// +optional
	ResourceRestoreName string `json:"resourceRestoreName,omitempty"`

	// Conditions 最新的观测状态
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=do
// +kubebuilder:printcolumn:name="Instance",type="string",JSONPath=".spec.instanceName",description="目标 DisasterInstance"
// +kubebuilder:printcolumn:name="Group",type="string",JSONPath=".spec.groupName",description="目标 DisasterGroup"
// +kubebuilder:printcolumn:name="Type",type="string",JSONPath=".spec.operationType",description="操作类型"
// +kubebuilder:printcolumn:name="State",type="string",JSONPath=".status.state",description="操作状态"
// +kubebuilder:printcolumn:name="CurrentStep",type="string",JSONPath=".status.currentStep",description="当前步骤"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// DisasterOperation 是容灾操作的 API Schema
// 表示一次性操作，如故障切换、故障回切、暂停或恢复
type DisasterOperation struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DisasterOperationSpec   `json:"spec,omitempty"`
	Status DisasterOperationStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// DisasterOperationList 包含 DisasterOperation 列表
type DisasterOperationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DisasterOperation `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DisasterOperation{}, &DisasterOperationList{})
}
