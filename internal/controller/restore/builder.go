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

// Package restore 提供 AppRestore 构建工具，供 ResourceSync、DataSync 和 DisasterOperation (Drill) 复用
package restore

import (
	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	"k8s.io/utils/ptr"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
)

// RestoreType 定义恢复类型
type RestoreType string

const (
	// RestoreTypeResource 资源恢复 (从 ResourceSync 备份恢复 K8s 资源)
	RestoreTypeResource RestoreType = "resource"
	// RestoreTypeData 数据恢复 (从 DataSync 备份恢复 PVC 数据)
	RestoreTypeData RestoreType = "data"
)

// BuilderConfig 定义 AppRestore 构建配置
type BuilderConfig struct {
	// RestoreType 恢复类型 (resource 或 data)
	RestoreType RestoreType

	// BackupSource AppBackup 名称或备份源标识
	BackupSource string

	// BackupName Velero Backup 名称
	BackupName string

	// TargetCluster 目标集群
	TargetCluster string

	// SourceCluster 源集群 (跨集群恢复时需要)
	SourceCluster string

	// StorageRepository 存储仓库名称
	StorageRepository string

	// IncludedNamespaces 要恢复的命名空间列表
	IncludedNamespaces []string

	// NamespaceMapping 命名空间映射 (可选，用于演练)
	NamespaceMapping map[string]string

	// IsForDrill 是否用于演练 (影响资源恢复时的 modifier)
	IsForDrill bool

	// ExtraResourceModifierRules is appended in resource restore mode.
	// Used for instance-level image rewrite patches.
	ExtraResourceModifierRules []disasterv1.ResourceModifierRule

	// DataResourceModifierRules overrides default trafficless modifiers in data restore mode.
	// When empty, builder falls back to default trafficless modifiers.
	DataResourceModifierRules []disasterv1.ResourceModifierRule
}

// BuildAppRestoreSpec 根据配置构建 AppRestoreSpec
func BuildAppRestoreSpec(cfg BuilderConfig) disasterv1.AppRestoreSpec {
	switch cfg.RestoreType {
	case RestoreTypeResource:
		return buildResourceRestoreSpec(cfg)
	case RestoreTypeData:
		return buildDataRestoreSpec(cfg)
	default:
		// 默认使用资源恢复
		return buildResourceRestoreSpec(cfg)
	}
}

// buildResourceRestoreSpec 构建资源恢复 Spec (从 ResourceSync 备份恢复 K8s 资源)
func buildResourceRestoreSpec(cfg BuilderConfig) disasterv1.AppRestoreSpec {
	modifiers := makeSkeletonModifiers()
	if len(cfg.ExtraResourceModifierRules) > 0 {
		modifiers = append(modifiers, cfg.ExtraResourceModifierRules...)
	}

	return disasterv1.AppRestoreSpec{
		BackupSource:      cfg.BackupSource,
		Cluster:           cfg.TargetCluster,
		SourceCluster:     cfg.SourceCluster,
		StorageRepository: cfg.StorageRepository,
		Template: velerov1.RestoreSpec{
			BackupName:             cfg.BackupName,
			IncludedNamespaces:     cfg.IncludedNamespaces,
			NamespaceMapping:       cfg.NamespaceMapping,
			RestorePVs:             ptr.To(false), // 资源恢复不恢复 PV
			ExcludedResources:      []string{"pods", "persistentvolumeclaims", "persistentvolumes"},
			ExistingResourcePolicy: velerov1.PolicyTypeUpdate, // 更新已存在的资源
		},
		ResourceModifierRules: modifiers,
	}
}

// buildDataRestoreSpec 构建数据恢复 Spec (从 DataSync 备份恢复 PVC 数据)
func buildDataRestoreSpec(cfg BuilderConfig) disasterv1.AppRestoreSpec {
	var policy velerov1.PolicyType
	if cfg.IsForDrill {
		// 演练时使用 Update 策略，覆盖已存在的资源
		policy = velerov1.PolicyTypeUpdate
	} else {
		// 正常同步时使用 None 策略
		policy = velerov1.PolicyTypeNone
	}

	dataModifiers := cfg.DataResourceModifierRules
	if len(dataModifiers) == 0 {
		dataModifiers = makeTrafficlessModifiers()
	}

	spec := disasterv1.AppRestoreSpec{
		BackupSource:      cfg.BackupSource,
		Cluster:           cfg.TargetCluster,
		SourceCluster:     cfg.SourceCluster,
		StorageRepository: cfg.StorageRepository,
		Template: velerov1.RestoreSpec{
			BackupName:             cfg.BackupName,
			IncludedNamespaces:     cfg.IncludedNamespaces,
			NamespaceMapping:       cfg.NamespaceMapping,
			IncludedResources:      []string{"pods", "persistentvolumeclaims", "persistentvolumes"},
			RestorePVs:             ptr.To(true), // 数据恢复需要恢复 PV
			PreserveNodePorts:      ptr.To(true),
			ExistingResourcePolicy: policy,
		},
		ResourceModifierRules: dataModifiers,
	}

	return spec
}

// makeSkeletonModifiers 生成将 replicas 设置为 0 的规则 (用于资源恢复)
func makeSkeletonModifiers() []disasterv1.ResourceModifierRule {
	patches := []disasterv1.JSONPatch{
		{
			Operation: "add", // 使用 add 操作强制覆盖或创建，比 replace 更稳健
			Path:      "/spec/replicas",
			Value:     "0",
		},
	}

	// 针对 Deployments 和 StatefulSets
	return []disasterv1.ResourceModifierRule{
		{
			Conditions: disasterv1.Conditions{GroupResource: "deployments.apps"},
			Patches:    patches,
		},
		{
			Conditions: disasterv1.Conditions{GroupResource: "statefulsets.apps"},
			Patches:    patches,
		},
	}
}

// makeTrafficlessModifiers 生成 Trafficless Restore 规则 (用于数据恢复)
// 恢复时 ResourceModifier 替换 Image 为 busybox，移除所有 Labels，确保 Service 不导流
func makeTrafficlessModifiers() []disasterv1.ResourceModifierRule {
	podPatches := []disasterv1.JSONPatch{
		// 清除所有原有标签 - 替换为只包含 trafficless 的 map
		{
			Operation: "add",
			Path:      "/metadata/labels",
			Value:     `{"trafficless": "true"}`,
		},
		// 将 ownerReferences 置空，避免临时 Pod 被控制器/GC 回收。
		// 使用 add + [] 保持幂等：字段不存在时不会触发 "expected one matching path ... got 0"。
		{
			Operation: "add",
			Path:      "/metadata/ownerReferences",
			Value:     "[]",
		},
		// 替换容器镜像为 busybox
		{
			Operation: "replace",
			Path:      "/spec/containers/0/image",
			Value:     "busybox:1.36",
		},
	}

	return []disasterv1.ResourceModifierRule{
		{
			Conditions: disasterv1.Conditions{
				GroupResource: "pods",
			},
			Patches: podPatches,
		},
	}
}

// MakePVCVolumeNameCleanupRule builds a system-level PVC rule that removes spec.volumeName.
// This is used for first-time data restore to avoid restoring with stale PV bindings.
func MakePVCVolumeNameCleanupRule(namespaces []string) disasterv1.ResourceModifierRule {
	return disasterv1.ResourceModifierRule{
		Conditions: disasterv1.Conditions{
			GroupResource: "persistentvolumeclaims",
			Namespaces:    append([]string(nil), namespaces...),
		},
		Patches: []disasterv1.JSONPatch{{
			Operation: "remove",
			Path:      "/spec/volumeName",
		}},
	}
}
