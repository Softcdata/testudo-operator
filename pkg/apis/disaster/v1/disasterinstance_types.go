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
	"strings"

	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DisasterInstance 状态机状态常量
const (
	// FsmStatePending 初始状态，DisasterInstance 刚创建
	FsmStatePending = "Pending"
	// FsmStateInitializing 正在执行首次同步
	FsmStateInitializing = "Initializing"
	// FsmStateProtected 正常运行状态，同步正在进行
	FsmStateProtected = "Protected"
	// FsmStatePaused 同步已暂停
	FsmStatePaused = "Paused"
	// FsmStateFailingOver 正在执行故障切换
	FsmStateFailingOver = "FailingOver"
	// FsmStateActive 故障切换成功后的状态（目标集群已激活）
	FsmStateActive = "Active"
	// FsmStateFailingBack 正在执行故障回切
	FsmStateFailingBack = "FailingBack"
	// FsmStateConfigError 配置错误状态（DisasterConfig 不健康）
	FsmStateConfigError = "ConfigError"
	// FsmStateFailed 错误状态
	FsmStateFailed = "Failed"
)

// DisasterCurrentState defines unified external aggregate state derived from fsmState.
type DisasterCurrentState string

const (
	// CurrentStateRunning means baseline protected execution is healthy/running.
	CurrentStateRunning DisasterCurrentState = "Running"
	// CurrentStateActive means failover has switched active side successfully.
	CurrentStateActive DisasterCurrentState = "Active"
	// CurrentStatePaused means sync loops are paused by control-plane action.
	CurrentStatePaused DisasterCurrentState = "Paused"
	// CurrentStateTransitioning means instance is currently switching roles/directions.
	CurrentStateTransitioning DisasterCurrentState = "Transitioning"
	// CurrentStateFailed means instance is in failed/config-error terminal error semantics.
	CurrentStateFailed DisasterCurrentState = "Failed"
	// CurrentStateUnknown means fsmState is empty/unknown and cannot be mapped safely.
	CurrentStateUnknown DisasterCurrentState = "Unknown"
)

// CurrentStateFromFSM maps controller fsmState into externally consumed aggregate state.
func CurrentStateFromFSM(fsmState string) DisasterCurrentState {
	switch strings.TrimSpace(fsmState) {
	case FsmStatePending, FsmStateInitializing, FsmStateProtected:
		return CurrentStateRunning
	case FsmStatePaused:
		return CurrentStatePaused
	case FsmStateFailingOver, FsmStateFailingBack:
		return CurrentStateTransitioning
	case FsmStateActive:
		return CurrentStateActive
	case FsmStateConfigError, FsmStateFailed:
		return CurrentStateFailed
	default:
		return CurrentStateUnknown
	}
}

// IsCurrentStateConsistent checks whether external currentState matches mapping contract.
func IsCurrentStateConsistent(fsmState, currentState string) bool {
	return string(CurrentStateFromFSM(fsmState)) == strings.TrimSpace(currentState)
}

// Pod 恢复方法常量
const (
	// PodRestoreMethodReplica 使用副本数缩放实现 Standby 模式
	PodRestoreMethodReplica = "replica"
	// PodRestoreMethodInitContainer 使用 initContainer 实现 Standby 模式
	PodRestoreMethodInitContainer = "initContainer"
)

// ImageRewriteApplyTarget defines where image rewrite rules should take effect.
type ImageRewriteApplyTarget string

const (
	// ImageRewriteApplyResourceSync applies rules when ResourceSync creates AppRestore.
	ImageRewriteApplyResourceSync ImageRewriteApplyTarget = "resourceSync"
	// ImageRewriteApplyDrill applies rules when Drill performs resource restore.
	ImageRewriteApplyDrill ImageRewriteApplyTarget = "drill"
)

// ImageRewriteUnmatchedPolicy defines behavior when an image does not match any mapping.
type ImageRewriteUnmatchedPolicy string

const (
	// ImageRewriteUnmatchedPolicyFail fails restore preparation on unmatched image.
	ImageRewriteUnmatchedPolicyFail ImageRewriteUnmatchedPolicy = "Fail"
	// ImageRewriteUnmatchedPolicyKeep keeps original image when unmatched.
	ImageRewriteUnmatchedPolicyKeep ImageRewriteUnmatchedPolicy = "Keep"
)

// ImageSourceMapping defines source->target image source alias mapping.
type ImageSourceMapping struct {
	// SourceImageSource references Cluster.spec.imageSources[].name on source cluster.
	SourceImageSource string `json:"sourceImageSource"`
	// TargetImageSource references Cluster.spec.imageSources[].name on target cluster.
	TargetImageSource string `json:"targetImageSource"`
}

// ImageRewriteConfig defines image rewrite strategy for restore paths.
type ImageRewriteConfig struct {
	// Enabled toggles image rewrite behavior.
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// ApplyTo controls which restore paths apply image rewrite.
	// Supported values: resourceSync, drill.
	// +optional
	ApplyTo []ImageRewriteApplyTarget `json:"applyTo,omitempty"`

	// UnmatchedPolicy controls behavior when image is not matched by any mapping.
	// Supported values: Fail, Keep. Default is Fail.
	// +optional
	// +kubebuilder:validation:Enum=Fail;Keep
	UnmatchedPolicy ImageRewriteUnmatchedPolicy `json:"unmatchedPolicy,omitempty"`

	// Mappings defines source image source alias -> target image source alias rules.
	// +optional
	Mappings []ImageSourceMapping `json:"mappings,omitempty"`
}

// RestoreClassUnmatchedPolicy defines behavior when class mapping is not matched.
type RestoreClassUnmatchedPolicy string

const (
	// RestoreClassUnmatchedPolicyKeep keeps original class when no mapping is matched.
	RestoreClassUnmatchedPolicyKeep RestoreClassUnmatchedPolicy = "Keep"
	// RestoreClassUnmatchedPolicyFail fails restore preparation when no mapping is matched.
	RestoreClassUnmatchedPolicyFail RestoreClassUnmatchedPolicy = "Fail"
)

// RestoreClassMapping defines source->target class mapping for restore.
type RestoreClassMapping struct {
	// SourceClass is the class name in source cluster.
	SourceClass string `json:"sourceClass"`
	// TargetClass is the class name to use in target cluster.
	TargetClass string `json:"targetClass"`
	// Namespaces optionally scopes this mapping to specific namespaces.
	// +optional
	Namespaces []string `json:"namespaces,omitempty"`
}

// RestoreClassMappingPolicy defines class mapping rules and behavior.
type RestoreClassMappingPolicy struct {
	// Mappings defines source->target mapping rules.
	// +optional
	Mappings []RestoreClassMapping `json:"mappings,omitempty"`
	// UnmatchedPolicy controls behavior when mapping is not matched.
	// Supported values: Keep, Fail. Default is Keep.
	// +optional
	// +kubebuilder:validation:Enum=Keep;Fail
	UnmatchedPolicy RestoreClassUnmatchedPolicy `json:"unmatchedPolicy,omitempty"`
	// StrictTargetValidation checks whether target classes exist before restore starts.
	// +optional
	StrictTargetValidation bool `json:"strictTargetValidation,omitempty"`
}

// RestoreResourceSelectionPolicy controls which resources should be restored.
type RestoreResourceSelectionPolicy struct {
	// IncludedNamespaces specifies namespaces to include.
	// +optional
	IncludedNamespaces []string `json:"includedNamespaces,omitempty"`
	// ExcludedNamespaces specifies namespaces to exclude.
	// +optional
	ExcludedNamespaces []string `json:"excludedNamespaces,omitempty"`
	// IncludedResources specifies resources to include.
	// +optional
	IncludedResources []string `json:"includedResources,omitempty"`
	// ExcludedResources specifies resources to exclude.
	// +optional
	ExcludedResources []string `json:"excludedResources,omitempty"`
	// IncludedNamespaceScopedResources specifies namespace-scoped resources to include.
	// +optional
	IncludedNamespaceScopedResources []string `json:"includedNamespaceScopedResources,omitempty"`
	// ExcludedNamespaceScopedResources specifies namespace-scoped resources to exclude.
	// +optional
	ExcludedNamespaceScopedResources []string `json:"excludedNamespaceScopedResources,omitempty"`
	// IncludedClusterScopedResources specifies cluster-scoped resources to include.
	// +optional
	IncludedClusterScopedResources []string `json:"includedClusterScopedResources,omitempty"`
	// ExcludedClusterScopedResources specifies cluster-scoped resources to exclude.
	// +optional
	ExcludedClusterScopedResources []string `json:"excludedClusterScopedResources,omitempty"`
	// LabelSelector filters resources by labels.
	// +optional
	LabelSelector *metav1.LabelSelector `json:"labelSelector,omitempty"`
	// IncludeClusterResources controls whether cluster-scoped resources are included.
	// +optional
	IncludeClusterResources *bool `json:"includeClusterResources,omitempty"`
}

// RestoreExecutionPolicy controls restore execution behavior.
type RestoreExecutionPolicy struct {
	// ExistingResourcePolicy controls how to handle existing resources.
	// Supported values: none, update.
	// +optional
	// +kubebuilder:validation:Enum=none;update;None;Update
	ExistingResourcePolicy string `json:"existingResourcePolicy,omitempty"`
	// RestorePVs controls whether to restore PersistentVolumes.
	// +optional
	RestorePVs *bool `json:"restorePVs,omitempty"`
	// ItemOperationTimeout configures velero restore item operation timeout.
	// +optional
	ItemOperationTimeout *metav1.Duration `json:"itemOperationTimeout,omitempty"`
}

// RestoreModifierMode defines rule mode in unified modifier DSL.
type RestoreModifierMode string

const (
	// RestoreModifierModeVeleroNative passes Velero-native rule through.
	RestoreModifierModeVeleroNative RestoreModifierMode = "veleroNative"
	// RestoreModifierModeReversible compiles direction-aware reversible transform.
	RestoreModifierModeReversible RestoreModifierMode = "reversible"
)

// RestoreModifierApplyTarget defines which restore path a rule applies to.
type RestoreModifierApplyTarget string

const (
	RestoreModifierApplyDataSync     RestoreModifierApplyTarget = "dataSync"
	RestoreModifierApplyResourceSync RestoreModifierApplyTarget = "resourceSync"
	RestoreModifierApplyDrill        RestoreModifierApplyTarget = "drill"
)

// RestoreModifierDirectionPolicy defines direction gate for modifier rule.
type RestoreModifierDirectionPolicy string

const (
	RestoreModifierDirectionPolicyAuto        RestoreModifierDirectionPolicy = "Auto"
	RestoreModifierDirectionPolicyForwardOnly RestoreModifierDirectionPolicy = "ForwardOnly"
	RestoreModifierDirectionPolicyReverseOnly RestoreModifierDirectionPolicy = "ReverseOnly"
)

// RestoreModifierConflictPolicy defines how to resolve same-key conflict.
type RestoreModifierConflictPolicy string

const (
	RestoreModifierConflictPolicyFail RestoreModifierConflictPolicy = "Fail"
	RestoreModifierConflictPolicySkip RestoreModifierConflictPolicy = "Skip"
)

// RestoreModifierVeleroRule contains Velero-compatible patch structures.
type RestoreModifierVeleroRule struct {
	// Patches is Velero JSONPatch list (Phase 1 supported).
	// +optional
	Patches []JSONPatch `json:"patches,omitempty"`
	// MergePatches is reserved for Phase 2.
	// +optional
	MergePatches []string `json:"mergePatches,omitempty"`
	// StrategicPatches is reserved for Phase 2.
	// +optional
	StrategicPatches []string `json:"strategicPatches,omitempty"`
}

// RestoreModifierPair defines canonical reversible pair payload.
type RestoreModifierPair struct {
	// Path is JSON Pointer target path.
	// +kubebuilder:validation:MinLength=1
	Path string `json:"path"`
	// SourceValue is the value for the baseline source side.
	// The selected value may contain restricted placeholders.
	// +kubebuilder:validation:MinLength=1
	SourceValue string `json:"sourceValue"`
	// TargetValue is the value for the baseline target side.
	// The selected value may contain restricted placeholders.
	// +kubebuilder:validation:MinLength=1
	TargetValue string `json:"targetValue"`
}

// RestoreModifierRule defines unified modifier DSL rule.
type RestoreModifierRule struct {
	// ID is optional user-facing rule id for diagnostics.
	// +optional
	ID string `json:"id,omitempty"`
	// Mode controls compilation behavior.
	// Supported values: veleroNative, reversible.
	// +kubebuilder:validation:Enum=veleroNative;reversible
	Mode RestoreModifierMode `json:"mode"`
	// Enabled gates whether rule participates in compilation. Default true.
	// +optional
	Enabled *bool `json:"enabled,omitempty"`
	// ApplyTo scopes rule to dataSync/resourceSync/drill paths.
	// +optional
	ApplyTo []RestoreModifierApplyTarget `json:"applyTo,omitempty"`
	// Priority decides conflict precedence, larger wins.
	// +optional
	Priority int32 `json:"priority,omitempty"`
	// Conditions reuses Velero condition structure.
	Conditions Conditions `json:"conditions"`
	// VeleroRule is required when mode=veleroNative.
	// +optional
	VeleroRule *RestoreModifierVeleroRule `json:"veleroRule,omitempty"`
	// Pair is required when mode=reversible.
	// Pair values may contain restricted placeholders.
	// +optional
	Pair *RestoreModifierPair `json:"pair,omitempty"`
	// DirectionPolicy gates rule by resolved flow. Default Auto.
	// +optional
	// +kubebuilder:validation:Enum=Auto;ForwardOnly;ReverseOnly
	DirectionPolicy RestoreModifierDirectionPolicy `json:"directionPolicy,omitempty"`
	// OnConflict controls same-priority same-key conflict handling. Default Fail.
	// +optional
	// +kubebuilder:validation:Enum=Fail;Skip
	OnConflict RestoreModifierConflictPolicy `json:"onConflict,omitempty"`
}

// BulkModifierActionType defines supported instance-level bulk modifier actions.
type BulkModifierActionType string

const (
	// BulkModifierActionReplaceExactValue replaces exact string leaf values in instance scope.
	BulkModifierActionReplaceExactValue BulkModifierActionType = "replaceExactValue"
	// BulkModifierActionRemoveKey removes exact object/map keys in instance scope.
	BulkModifierActionRemoveKey BulkModifierActionType = "removeKey"
	// BulkModifierActionRewriteImage rewrites source images dynamically at restore build time.
	BulkModifierActionRewriteImage BulkModifierActionType = "rewriteImage"
)

// ImageRewriteDigestPolicy defines behavior for digest images.
type ImageRewriteDigestPolicy string

const (
	// ImageRewriteDigestPolicyPreserve keeps the original tag/digest suffix during rewrite.
	ImageRewriteDigestPolicyPreserve ImageRewriteDigestPolicy = "Preserve"
)

// DynamicImageRewriteConfig defines stable runtime image rewrite intent for rewriteImage actions.
type DynamicImageRewriteConfig struct {
	// SourcePrefix is the image prefix expected on the baseline source side.
	// +kubebuilder:validation:MinLength=1
	SourcePrefix string `json:"sourcePrefix"`
	// TargetPrefix is the image prefix expected on the baseline target side.
	// +kubebuilder:validation:MinLength=1
	TargetPrefix string `json:"targetPrefix"`
	// UnmatchedPolicy controls behavior when an image does not match any rewrite action.
	// Supported values: Keep, Fail. Default is Keep.
	// +optional
	// +kubebuilder:validation:Enum=Keep;Fail
	UnmatchedPolicy ImageRewriteUnmatchedPolicy `json:"unmatchedPolicy,omitempty"`
	// DigestPolicy controls digest image handling. Phase 1 supports Preserve.
	// +optional
	// +kubebuilder:validation:Enum=Preserve
	DigestPolicy ImageRewriteDigestPolicy `json:"digestPolicy,omitempty"`
}

// BulkModifierAction defines instance-level bulk modifier input.
type BulkModifierAction struct {
	// ID is optional user-facing action id for diagnostics and deterministic rule IDs.
	// +optional
	ID string `json:"id,omitempty"`
	// Action controls bulk expansion behavior.
	// Supported values: replaceExactValue, removeKey, rewriteImage.
	// +kubebuilder:validation:Enum=replaceExactValue;removeKey;rewriteImage
	Action BulkModifierActionType `json:"action"`
	// Enabled gates whether action participates in snapshot generation. Default true.
	// +optional
	Enabled *bool `json:"enabled,omitempty"`
	// ApplyTo scopes action to resourceSync/drill restore paths.
	// +optional
	ApplyTo []RestoreModifierApplyTarget `json:"applyTo,omitempty"`
	// DirectionPolicy controls generated rule direction semantics.
	// +optional
	// +kubebuilder:validation:Enum=Auto;ForwardOnly;ReverseOnly
	DirectionPolicy RestoreModifierDirectionPolicy `json:"directionPolicy,omitempty"`
	// SourceValue is required when action=replaceExactValue.
	// +optional
	SourceValue string `json:"sourceValue,omitempty"`
	// TargetValue is required when action=replaceExactValue.
	// +optional
	TargetValue string `json:"targetValue,omitempty"`
	// Key is required when action=removeKey.
	// +optional
	Key string `json:"key,omitempty"`
	// ImageRewrite is required when action=rewriteImage.
	// +optional
	ImageRewrite *DynamicImageRewriteConfig `json:"imageRewrite,omitempty"`
}

// RestorePolicy defines instance-level restore policy for automated restore paths.
type RestorePolicy struct {
	// ResourceSelection controls restore include/exclude scope.
	// +optional
	ResourceSelection *RestoreResourceSelectionPolicy `json:"resourceSelection,omitempty"`
	// Execution controls restore execution behavior.
	// +optional
	Execution *RestoreExecutionPolicy `json:"execution,omitempty"`
	// StorageClassMapping defines StorageClass mapping behavior.
	// +optional
	StorageClassMapping *RestoreClassMappingPolicy `json:"storageClassMapping,omitempty"`
	// IngressClassMapping defines IngressClass mapping behavior.
	// +optional
	IngressClassMapping *RestoreClassMappingPolicy `json:"ingressClassMapping,omitempty"`
	// BulkModifierActions defines instance-level bulk modifier intent.
	// +optional
	BulkModifierActions []BulkModifierAction `json:"bulkModifierActions,omitempty"`
	// ModifierRules defines unified reversible/veleroNative rule set.
	// +optional
	ModifierRules []RestoreModifierRule `json:"modifierRules,omitempty"`
	// ModifierRuleSnapshot defines server-generated final executable modifier rule set.
	// +optional
	ModifierRuleSnapshot []RestoreModifierRule `json:"modifierRuleSnapshot,omitempty"`
	// ModifierRuleSnapshotHash is SHA256 over final modifierRuleSnapshot JSON.
	// +optional
	ModifierRuleSnapshotHash string `json:"modifierRuleSnapshotHash,omitempty"`
	// UseUnifiedDirectionResolver toggles unified direction resolver + DSL compiler.
	// Default false in Phase 1 for smooth migration.
	// +optional
	UseUnifiedDirectionResolver *bool `json:"useUnifiedDirectionResolver,omitempty"`
}

// DisasterVeleroHooks defines Velero-native hooks used by disaster orchestration.
type DisasterVeleroHooks struct {
	// DataBackup applies only to DataSync-created AppBackup.
	// +optional
	DataBackup *velerov1.BackupHooks `json:"dataBackup,omitempty"`

	// DataRestore applies only to DataSync-created AppRestore.
	// +optional
	DataRestore *velerov1.RestoreHooks `json:"dataRestore,omitempty"`
}

// DisasterInstanceSpec 定义 DisasterInstance 的期望状态
type DisasterInstanceSpec struct {
	// Config 关联的 DisasterConfig 名称
	// +required
	Config string `json:"config"`

	// DataSyncPolicy 定义实例级数据同步策略 override。
	// 为空时继承关联 DisasterConfig.spec.dataSyncPolicy。
	// +optional
	DataSyncPolicy string `json:"dataSyncPolicy,omitempty"`

	// ResourceSyncPolicy 定义实例级资源同步策略 override。
	// 为空时继承关联 DisasterConfig.spec.resourceSyncPolicy。
	// +optional
	ResourceSyncPolicy string `json:"resourceSyncPolicy,omitempty"`

	// Namespaces 需要保护的命名空间列表
	// +optional
	Namespaces []string `json:"namespaces,omitempty"`

	// LabelSelector 用于选择需要保护的资源
	// +optional
	LabelSelector *metav1.LabelSelector `json:"labelSelector,omitempty"`

	// PodRestoreMethod 指定 Standby 模式下的 Pod 处理方式
	// 可选值: "replica" (默认), "initContainer"
	// +optional
	// +kubebuilder:default="replica"
	PodRestoreMethod string `json:"podRestoreMethod,omitempty"`

	// OperationTimeoutMinutes 定义该实例执行容灾操作的默认超时时间(分钟)
	// 默认值: 60 (1小时)
	// +optional
	// +kubebuilder:default=60
	OperationTimeoutMinutes int32 `json:"operationTimeoutMinutes,omitempty"`

	// RestorePolicy 定义实例级恢复策略。
	// DataSync/ResourceSync/Drill 在构建 AppRestore 时会应用该策略。
	// +optional
	RestorePolicy *RestorePolicy `json:"restorePolicy,omitempty"`

	// VeleroHooks defines Velero-native hooks for DataSync data backup and restore.
	// +optional
	VeleroHooks *DisasterVeleroHooks `json:"veleroHooks,omitempty"`

	// SkipPodReadyCheck 定义该实例执行容灾切换时是否默认跳过 Pod 就绪校验。
	// true: 默认跳过容器就绪验证（仅校验副本配置已下发）
	// false: 默认执行容器就绪验证（检查 readyReplicas）
	// 该默认值可被 DisasterOperation 入参覆盖。
	// +optional
	SkipPodReadyCheck *bool `json:"skipPodReadyCheck,omitempty"`
}

// DisasterInstanceStatus 定义 DisasterInstance 的观测状态
type DisasterInstanceStatus struct {
	// FsmState 当前状态机状态
	// +optional
	FsmState string `json:"fsmState,omitempty"`

	// Reason 机器可读错误码
	// +optional
	Reason string `json:"reason,omitempty"`

	// Message 人类可读错误描述
	// +optional
	Message string `json:"message,omitempty"`

	// LastStableFsmState 进入 ConfigError 前的原始状态，用于配置恢复时回放
	// +optional
	LastStableFsmState string `json:"lastStableFsmState,omitempty"`

	// PrimaryCluster 当前主集群（活跃集群）名称
	// +optional
	PrimaryCluster string `json:"primaryCluster,omitempty"`

	// SecondaryCluster 当前备集群（待命集群）名称
	// +optional
	SecondaryCluster string `json:"secondaryCluster,omitempty"`

	// LastDataSyncTime 上次数据同步时间
	// +optional
	LastDataSyncTime *metav1.Time `json:"lastDataSyncTime,omitempty"`

	// LastResourceSyncTime 上次资源同步时间
	// +optional
	LastResourceSyncTime *metav1.Time `json:"lastResourceSyncTime,omitempty"`

	// AvailableOperations 当前状态下可执行的操作列表
	// +optional
	AvailableOperations []string `json:"availableOperations,omitempty"`

	// DataSyncName 管理的 DataSync 资源名称
	// +optional
	DataSyncName string `json:"dataSyncName,omitempty"`

	// ResourceSyncName 管理的 ResourceSync 资源名称
	// +optional
	ResourceSyncName string `json:"resourceSyncName,omitempty"`

	// Conditions 实例状态的最新观测
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=di
// +kubebuilder:printcolumn:name="Config",type="string",JSONPath=".spec.config",description="关联的 DisasterConfig"
// +kubebuilder:printcolumn:name="State",type="string",JSONPath=".status.fsmState",description="当前状态机状态"
// +kubebuilder:printcolumn:name="Primary",type="string",JSONPath=".status.primaryCluster",description="主集群"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// DisasterInstance 是容灾实例的 API Schema
// 它表示两个集群之间针对一组工作负载的容灾关系
type DisasterInstance struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DisasterInstanceSpec   `json:"spec,omitempty"`
	Status DisasterInstanceStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// DisasterInstanceList 包含 DisasterInstance 列表
type DisasterInstanceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DisasterInstance `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DisasterInstance{}, &DisasterInstanceList{})
}
