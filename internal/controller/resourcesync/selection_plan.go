package resourcesync

import (
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
)

const resourceSyncSelectionModeScoped = "scoped"

var resourceSyncMandatoryExcludedResources = []string{
	"pods",
	"persistentvolumeclaims",
	"persistentvolumes",
}

type resourceSyncSelectionPlan struct {
	mode string

	includedNamespaces []string
	excludedNamespaces []string
	labelSelector      *metav1.LabelSelector

	includedNamespaceScopedResources []string
	excludedNamespaceScopedResources []string
	includedClusterScopedResources   []string
	excludedClusterScopedResources   []string
}

func resolveResourceSyncSelectionPlan(instance *disasterv1.DisasterInstance) resourceSyncSelectionPlan {
	out := resourceSyncSelectionPlan{}
	if instance == nil || instance.Spec.RestorePolicy == nil || instance.Spec.RestorePolicy.ResourceSelection == nil {
		return out
	}

	p := instance.Spec.RestorePolicy.ResourceSelection
	if p.IncludeClusterResources != nil && *p.IncludeClusterResources {
		return out
	}
	if !hasResourceSyncScopedFilters(p) {
		return out
	}

	out.mode = resourceSyncSelectionModeScoped
	out.includedNamespaces = normalizeResourceSyncFilterValues(p.IncludedNamespaces)
	out.excludedNamespaces = normalizeResourceSyncFilterValues(p.ExcludedNamespaces)
	out.includedNamespaceScopedResources = normalizeResourceSyncFilterValues(p.IncludedNamespaceScopedResources)
	out.excludedNamespaceScopedResources = normalizeResourceSyncFilterValues(p.ExcludedNamespaceScopedResources)
	out.includedClusterScopedResources = normalizeResourceSyncFilterValues(p.IncludedClusterScopedResources)
	out.excludedClusterScopedResources = normalizeResourceSyncFilterValues(p.ExcludedClusterScopedResources)
	if p.LabelSelector != nil {
		out.labelSelector = p.LabelSelector.DeepCopy()
	}
	return out
}

func hasResourceSyncScopedFilters(p *disasterv1.RestoreResourceSelectionPolicy) bool {
	if p == nil {
		return false
	}
	return len(p.IncludedNamespaceScopedResources) > 0 ||
		len(p.ExcludedNamespaceScopedResources) > 0 ||
		len(p.IncludedClusterScopedResources) > 0 ||
		len(p.ExcludedClusterScopedResources) > 0
}

func (p resourceSyncSelectionPlan) scopedMode() bool {
	return p.mode == resourceSyncSelectionModeScoped
}

func (p resourceSyncSelectionPlan) clusterPhaseEnabled() bool {
	return p.scopedMode() && len(p.includedClusterScopedResources) > 0
}

func (p resourceSyncSelectionPlan) namespacePhaseEnabled() bool {
	if !p.scopedMode() {
		return true
	}
	if len(p.includedNamespaceScopedResources) == 0 {
		return true
	}
	return len(p.effectiveNamespacedIncludedResources()) > 0
}

func (p resourceSyncSelectionPlan) effectiveNamespacedIncludedResources() []string {
	return filterOutValues(p.includedNamespaceScopedResources, resourceSyncMandatoryExcludedResources...)
}

func (p resourceSyncSelectionPlan) effectiveNamespacedExcludedResources() []string {
	return mergeUniqueResourceSyncValues(p.excludedNamespaceScopedResources, resourceSyncMandatoryExcludedResources)
}

func applyScopedSelectionToResourceSyncBackupSpec(spec *velerov1.BackupSpec, plan resourceSyncSelectionPlan) {
	if spec == nil || !plan.scopedMode() {
		return
	}

	if len(plan.includedNamespaces) > 0 {
		spec.IncludedNamespaces = append([]string{}, plan.includedNamespaces...)
	}
	if len(plan.excludedNamespaces) > 0 {
		spec.ExcludedNamespaces = mergeUniqueResourceSyncValues(spec.ExcludedNamespaces, plan.excludedNamespaces)
	}
	if plan.labelSelector != nil {
		spec.LabelSelector = plan.labelSelector.DeepCopy()
	}

	spec.IncludedResources = nil
	spec.ExcludedResources = nil
	spec.IncludeClusterResources = nil

	includedNamespaced := plan.effectiveNamespacedIncludedResources()
	if len(plan.includedNamespaceScopedResources) > 0 && len(includedNamespaced) == 0 {
		spec.IncludedNamespaceScopedResources = nil
		spec.ExcludedNamespaceScopedResources = []string{"*"}
	} else {
		spec.IncludedNamespaceScopedResources = cloneStringSlice(includedNamespaced)
		spec.ExcludedNamespaceScopedResources = cloneStringSlice(plan.effectiveNamespacedExcludedResources())
	}

	if plan.clusterPhaseEnabled() {
		spec.IncludedClusterScopedResources = cloneStringSlice(plan.includedClusterScopedResources)
		spec.ExcludedClusterScopedResources = cloneStringSlice(plan.excludedClusterScopedResources)
		return
	}

	spec.IncludedClusterScopedResources = nil
	spec.ExcludedClusterScopedResources = []string{"*"}
}

func applyResourceSyncNamespacedRestoreSelection(spec *disasterv1.AppRestoreSpec, plan resourceSyncSelectionPlan) {
	if spec == nil {
		return
	}

	if plan.scopedMode() {
		spec.Template.IncludedResources = cloneStringSlice(plan.effectiveNamespacedIncludedResources())
		spec.Template.ExcludedResources = cloneStringSlice(plan.effectiveNamespacedExcludedResources())
		includeCluster := false
		spec.Template.IncludeClusterResources = &includeCluster
	}

	enforceResourceSyncMandatoryRestoreExclusions(&spec.Template)
	spec.Template.ExistingResourcePolicy = velerov1.PolicyTypeUpdate
}

func applyResourceSyncClusterRestoreSelection(spec *disasterv1.AppRestoreSpec, plan resourceSyncSelectionPlan) {
	if spec == nil {
		return
	}

	spec.Template.IncludedResources = cloneStringSlice(plan.includedClusterScopedResources)
	spec.Template.ExcludedResources = cloneStringSlice(plan.excludedClusterScopedResources)
	includeCluster := true
	spec.Template.IncludeClusterResources = &includeCluster
	spec.Template.ExistingResourcePolicy = velerov1.PolicyTypeNone
}

func enforceResourceSyncMandatoryRestoreExclusions(spec *velerov1.RestoreSpec) {
	if spec == nil {
		return
	}

	spec.IncludedResources = filterOutValues(spec.IncludedResources, resourceSyncMandatoryExcludedResources...)
	spec.ExcludedResources = mergeUniqueResourceSyncValues(spec.ExcludedResources, resourceSyncMandatoryExcludedResources)
}

func normalizeResourceSyncFilterValues(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		v := strings.TrimSpace(value)
		if v == "" {
			continue
		}
		out = append(out, v)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func mergeUniqueResourceSyncValues(parts ...[]string) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0)
	for _, part := range parts {
		for _, item := range part {
			v := strings.TrimSpace(item)
			if v == "" {
				continue
			}
			if _, exists := seen[v]; exists {
				continue
			}
			seen[v] = struct{}{}
			out = append(out, v)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func filterOutValues(values []string, blocked ...string) []string {
	if len(values) == 0 {
		return nil
	}
	blockedSet := make(map[string]struct{}, len(blocked))
	for _, item := range blocked {
		blockedSet[strings.TrimSpace(item)] = struct{}{}
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		v := strings.TrimSpace(value)
		if v == "" {
			continue
		}
		if _, blocked := blockedSet[v]; blocked {
			continue
		}
		out = append(out, v)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func cloneStringSlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	return append([]string{}, values...)
}
