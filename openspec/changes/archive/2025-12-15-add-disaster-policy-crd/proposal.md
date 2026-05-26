# 变更：添加 DisasterPolicy CRD

## 背景
为了统一管理灾备系统中的各种策略（如自动备份、数据同步、资源同步），需要引入一个新的 CRD `DisasterPolicy`。该 CRD 将作为策略配置的统一入口，允许用户定义策略的类型、执行周期、状态等信息，并通过标签机制方便查询和管理。

## 变更内容
- 引入 `DisasterPolicy` CRD。
- 定义策略类型：自动备份策略、数据同步策略、资源同步策略。
- 支持 Cron 表达式定义的执行周期。
- 支持策略的启用/禁用状态管理。
- 实现控制器逻辑，将关键元数据（类型、名称、状态）同步到资源标签中。

## 设计

### CRD 定义
在 `pkg/apis/disaster/v1/` 中定义 `DisasterPolicy`。

#### 策略类型枚举
```go
// PolicyType defines the type of the policy
// +kubebuilder:validation:Enum=AutoBackup;DataSync;ResourceSync
type PolicyType string

const (
	PolicyTypeAutoBackup   PolicyType = "AutoBackup"
	PolicyTypeDataSync     PolicyType = "DataSync"
	PolicyTypeResourceSync PolicyType = "ResourceSync"
)
```

#### 策略状态枚举
```go
// PolicyState defines the state of the policy
// +kubebuilder:validation:Enum=Enabled;Disabled
// +kubebuilder:default=Enabled
type PolicyState string

const (
	PolicyStateEnabled  PolicyState = "Enabled"
	PolicyStateDisabled PolicyState = "Disabled"
)
```

#### Spec 定义
```go
type DisasterPolicySpec struct {
	// Type specifies the type of the policy
	// +required
	Type PolicyType `json:"type"`

	// Schedule is a Cron expression defining when to run the policy
	// +required
	Schedule string `json:"schedule"`

	// StartTime specifies when the policy should start taking effect
	// +optional
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// Description provides a human-readable description of the policy
	// +optional
	Description string `json:"description,omitempty"`

	// State specifies whether the policy is enabled or disabled
	// +required
	State PolicyState `json:"state"`
}
```

#### Status 定义
```go
type DisasterPolicyStatus struct {
	// LastExecutionTime records the last time the policy was executed
	LastExecutionTime *metav1.Time `json:"lastExecutionTime,omitempty"`
    
    // NextExecutionTime records the next scheduled execution time
    NextExecutionTime *metav1.Time `json:"nextExecutionTime,omitempty"`
}
```

### 标签同步
控制器需要确保以下标签始终与 Spec 中的配置保持一致：
- `testudo.softcdata.com/policy-type`: 对应 `Spec.Type`
- `testudo.softcdata.com/policy-name`: 对应 `Metadata.Name`
- `testudo.softcdata.com/policy-state`: 对应 `Spec.State`

### 控制器逻辑
- 监听 `DisasterPolicy` 的创建和更新事件。
- 验证 Cron 表达式的有效性。
- 自动同步 Spec 中的字段到 Metadata Labels。
- (未来扩展) 根据策略类型和时间调度触发相应的操作（如创建 Backup CR 等）。

## 影响范围
- 新增 `DisasterPolicy` CRD。
- 新增 `DisasterPolicy` 控制器。
- 不影响现有 CRD。
