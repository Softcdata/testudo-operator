package restore

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	networkingv1 "k8s.io/api/networking/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
)

const (
	// AnnotationRestorePolicySource records restore policy source layer.
	AnnotationRestorePolicySource = "testudo.softcdata.com/restore-policy-source"
	// AnnotationRestorePolicySummary records restore policy summary as JSON.
	AnnotationRestorePolicySummary = "testudo.softcdata.com/restore-policy-summary"
	// AnnotationModifierSource records whether modifier input comes from bulk actions.
	AnnotationModifierSource = "testudo.softcdata.com/modifier-source"
	// AnnotationModifierBulkActionCount records enabled bulk action count.
	AnnotationModifierBulkActionCount = "testudo.softcdata.com/modifier-bulk-action-count"
	// AnnotationModifierSnapshotHash records final snapshot hash.
	AnnotationModifierSnapshotHash = "testudo.softcdata.com/modifier-snapshot-hash"
	// AnnotationModifierFlow records resolved modifier flow.
	AnnotationModifierFlow = "testudo.softcdata.com/modifier-flow"
	// AnnotationModifierDirectionSource records direction resolver source.
	AnnotationModifierDirectionSource = "testudo.softcdata.com/modifier-direction-source"
	// AnnotationModifierSummary records modifier compile summary as JSON.
	AnnotationModifierSummary = "testudo.softcdata.com/modifier-summary"
	// AnnotationResourceSelectionMode records the effective resource-selection mode (old/scoped).
	AnnotationResourceSelectionMode = "testudo.softcdata.com/resource-selection-mode"

	resourceSelectionModeOld    = "old"
	resourceSelectionModeScoped = "scoped"
)

// PolicySummary provides a compact observability payload for policy application.
type PolicySummary struct {
	Source                   string `json:"source"`
	StorageClassRuleCount    int    `json:"storageClassRuleCount"`
	IngressClassRuleCount    int    `json:"ingressClassRuleCount"`
	StorageClassMappingCount int    `json:"storageClassMappingCount"`
	IngressClassMappingCount int    `json:"ingressClassMappingCount"`
	StorageUnmatchedPolicy   string `json:"storageUnmatchedPolicy"`
	IngressUnmatchedPolicy   string `json:"ingressUnmatchedPolicy"`
	StorageStrictValidation  bool   `json:"storageStrictValidation"`
	IngressStrictValidation  bool   `json:"ingressStrictValidation"`
	Flow                     string `json:"flow,omitempty"`
	DirectionSource          string `json:"directionSource,omitempty"`
	AppliedRuleCount         int    `json:"appliedRuleCount,omitempty"`
	SkippedRuleCount         int    `json:"skippedRuleCount,omitempty"`
	RejectedRuleCount        int    `json:"rejectedRuleCount,omitempty"`
	ConflictCount            int    `json:"conflictCount,omitempty"`
	ModifierSource           string `json:"modifierSource,omitempty"`
	ModifierBulkActionCount  int    `json:"modifierBulkActionCount,omitempty"`
	ModifierSnapshotHash     string `json:"modifierSnapshotHash,omitempty"`
	ResourceSelectionMode    string `json:"resourceSelectionMode,omitempty"`
}

// RequiresTargetClassValidation reports whether strict class existence validation is required.
func RequiresTargetClassValidation(instance *disasterv1.DisasterInstance) bool {
	return RequiresTargetClassValidationWithOverride(instance, nil)
}

// RequiresTargetClassValidationWithOverride reports whether strict class validation is required
// after applying an optional drill-scoped restorePolicy override.
func RequiresTargetClassValidationWithOverride(
	instance *disasterv1.DisasterInstance,
	override *disasterv1.RestorePolicy,
) bool {
	policy := EffectiveRestorePolicy(instance, override)
	if policy == nil {
		return false
	}
	return (policy.StorageClassMapping != nil && policy.StorageClassMapping.StrictTargetValidation) ||
		(policy.IngressClassMapping != nil && policy.IngressClassMapping.StrictTargetValidation)
}

// EffectiveRestorePolicy returns the effective restorePolicy for a concrete restore path.
// The override layer is drill-scoped: modifier/bulk related fields replace instance values,
// while omitted pointer sub-policies continue to inherit from the instance.
func EffectiveRestorePolicy(
	instance *disasterv1.DisasterInstance,
	override *disasterv1.RestorePolicy,
) *disasterv1.RestorePolicy {
	if instance == nil || instance.Spec.RestorePolicy == nil {
		if override == nil {
			return nil
		}
		return override.DeepCopy()
	}
	if override == nil {
		return instance.Spec.RestorePolicy.DeepCopy()
	}

	effective := instance.Spec.RestorePolicy.DeepCopy()
	if effective == nil {
		effective = &disasterv1.RestorePolicy{}
	}

	if override.ResourceSelection != nil {
		effective.ResourceSelection = override.ResourceSelection.DeepCopy()
	}
	if override.Execution != nil {
		effective.Execution = override.Execution.DeepCopy()
	}
	if override.StorageClassMapping != nil {
		effective.StorageClassMapping = override.StorageClassMapping.DeepCopy()
	}
	if override.IngressClassMapping != nil {
		effective.IngressClassMapping = override.IngressClassMapping.DeepCopy()
	}
	if override.UseUnifiedDirectionResolver != nil {
		effective.UseUnifiedDirectionResolver = cloneBoolPtr(override.UseUnifiedDirectionResolver)
	}

	// Drill override explicitly replaces modifier/bulk intent so drills can diverge from the instance.
	effective.BulkModifierActions = cloneBulkModifierActions(override.BulkModifierActions)
	effective.ModifierRules = cloneRestoreModifierRules(override.ModifierRules)
	effective.ModifierRuleSnapshot = cloneRestoreModifierRules(override.ModifierRuleSnapshot)
	effective.ModifierRuleSnapshotHash = strings.TrimSpace(override.ModifierRuleSnapshotHash)

	return effective
}

// ApplyPolicySummaryAnnotations writes policy source and summary annotations.
func ApplyPolicySummaryAnnotations(meta *metav1.ObjectMeta, summary PolicySummary) {
	if meta == nil {
		return
	}
	if meta.Annotations == nil {
		meta.Annotations = make(map[string]string)
	}
	meta.Annotations[AnnotationRestorePolicySource] = summary.Source
	meta.Annotations[AnnotationRestorePolicySummary] = summary.annotationValue()
	if summary.Flow != "" {
		meta.Annotations[AnnotationModifierFlow] = summary.Flow
	}
	if summary.DirectionSource != "" {
		meta.Annotations[AnnotationModifierDirectionSource] = summary.DirectionSource
	}
	if summary.ModifierSource != "" {
		meta.Annotations[AnnotationModifierSource] = summary.ModifierSource
	}
	if summary.ModifierBulkActionCount > 0 {
		meta.Annotations[AnnotationModifierBulkActionCount] = fmt.Sprintf("%d", summary.ModifierBulkActionCount)
	}
	if summary.ModifierSnapshotHash != "" {
		meta.Annotations[AnnotationModifierSnapshotHash] = summary.ModifierSnapshotHash
	}
	if summary.ResourceSelectionMode != "" {
		meta.Annotations[AnnotationResourceSelectionMode] = summary.ResourceSelectionMode
	}
	meta.Annotations[AnnotationModifierSummary] = summary.modifierAnnotationValue()
}

// ApplyInstanceRestorePolicy applies instance-level restore policy into AppRestore spec.
func ApplyInstanceRestorePolicy(
	ctx context.Context,
	spec *disasterv1.AppRestoreSpec,
	instance *disasterv1.DisasterInstance,
	targetClient client.Client,
	opts ...ApplyInstanceRestorePolicyOption,
) (PolicySummary, error) {
	summary := PolicySummary{Source: "default"}
	if spec == nil {
		return summary, fmt.Errorf("ClassMappingInvalid: nil app restore spec")
	}
	applyOptions := applyInstanceRestorePolicyOptions(opts...)
	policy := EffectiveRestorePolicy(instance, applyOptions.RestorePolicyOverride)
	if policy == nil {
		return summary, nil
	}
	if applyOptions.RestorePolicyOverride != nil {
		summary.Source = "drillOverride"
	} else {
		summary.Source = "instance"
	}
	if actions := effectiveBulkModifierActions(policy); len(actions) > 0 {
		summary.ModifierSource = "bulkActions"
		summary.ModifierBulkActionCount = len(actions)
		summary.ModifierSnapshotHash = strings.TrimSpace(policy.ModifierRuleSnapshotHash)
	}

	summary.ResourceSelectionMode = applyResourceSelectionPolicy(spec, policy.ResourceSelection)
	if err := applyExecutionPolicy(spec, policy.Execution); err != nil {
		return summary, err
	}

	gateEnabled := isUnifiedDirectionResolverEnabled(policy)
	legacyMappingConfigured := policy.StorageClassMapping != nil || policy.IngressClassMapping != nil
	mappingFlow := modifierFlowForward
	if gateEnabled && legacyMappingConfigured {
		flow, source, err := resolveModifierFlow(
			applyOptions.BaselineSourceCluster,
			applyOptions.BaselineTargetCluster,
			instance.Status.PrimaryCluster,
			instance.Status.SecondaryCluster,
		)
		if err != nil {
			return summary, err
		}
		mappingFlow = flow
		summary.Flow = string(flow)
		summary.DirectionSource = string(source)
	}

	storageRules, storageSummary, err := buildStorageClassRules(
		ctx,
		targetClient,
		policy.StorageClassMapping,
		instance.Spec.Namespaces,
		mappingFlow,
		!gateEnabled,
	)
	if err != nil {
		return summary, err
	}
	ingressRules, ingressSummary, err := buildIngressClassRules(
		ctx,
		targetClient,
		policy.IngressClassMapping,
		instance.Spec.Namespaces,
		mappingFlow,
		!gateEnabled,
	)
	if err != nil {
		return summary, err
	}

	summary.StorageClassRuleCount = len(storageRules)
	summary.StorageClassMappingCount = storageSummary.mappingCount
	summary.StorageUnmatchedPolicy = storageSummary.unmatchedPolicy
	summary.StorageStrictValidation = storageSummary.strictValidation
	summary.IngressClassRuleCount = len(ingressRules)
	summary.IngressClassMappingCount = ingressSummary.mappingCount
	summary.IngressUnmatchedPolicy = ingressSummary.unmatchedPolicy
	summary.IngressStrictValidation = ingressSummary.strictValidation

	systemRules := make([]disasterv1.ResourceModifierRule, 0, len(spec.ResourceModifierRules)+len(storageRules)+len(ingressRules)+len(applyOptions.SystemRules))
	if len(spec.ResourceModifierRules) > 0 {
		systemRules = append(systemRules, cloneResourceModifierRules(spec.ResourceModifierRules)...)
	}
	if len(storageRules) > 0 {
		systemRules = append(systemRules, storageRules...)
	}
	if len(ingressRules) > 0 {
		systemRules = append(systemRules, ingressRules...)
	}
	if len(applyOptions.SystemRules) > 0 {
		systemRules = append(systemRules, cloneResourceModifierRules(applyOptions.SystemRules)...)
	}
	applyOptions.SystemRules = systemRules

	effectiveInstance := instance
	if instance != nil {
		effectiveInstance = instance.DeepCopy()
		effectiveInstance.Spec.RestorePolicy = policy.DeepCopy()
	}
	compiledRules, compileSummary, err := compileModifierRulesForInstance(effectiveInstance, applyOptions)
	if err != nil {
		return summary, err
	}
	spec.ResourceModifierRules = compiledRules
	if compileSummary.Flow != "" {
		summary.Flow = compileSummary.Flow
	}
	if compileSummary.DirectionSource != "" {
		summary.DirectionSource = compileSummary.DirectionSource
	}
	summary.AppliedRuleCount = compileSummary.AppliedRuleCount
	summary.SkippedRuleCount = compileSummary.SkippedRuleCount
	summary.RejectedRuleCount = compileSummary.RejectedRuleCount
	summary.ConflictCount = compileSummary.ConflictCount

	return summary, nil
}

type mappingSummary struct {
	mappingCount     int
	unmatchedPolicy  string
	strictValidation bool
}

func applyResourceSelectionPolicy(spec *disasterv1.AppRestoreSpec, p *disasterv1.RestoreResourceSelectionPolicy) string {
	if p == nil {
		return ""
	}
	if len(p.IncludedNamespaces) > 0 {
		spec.Template.IncludedNamespaces = append([]string{}, p.IncludedNamespaces...)
	}
	if len(p.ExcludedNamespaces) > 0 {
		spec.Template.ExcludedNamespaces = append([]string{}, p.ExcludedNamespaces...)
	}
	if p.LabelSelector != nil {
		spec.Template.LabelSelector = p.LabelSelector.DeepCopy()
	}

	if includeClusterResourcesIsTrue(p.IncludeClusterResources) {
		if len(p.IncludedResources) > 0 {
			spec.Template.IncludedResources = append([]string{}, p.IncludedResources...)
		}
		if len(p.ExcludedResources) > 0 {
			spec.Template.ExcludedResources = append([]string{}, p.ExcludedResources...)
		}
		spec.Template.IncludeClusterResources = cloneBoolPtr(p.IncludeClusterResources)
		return resourceSelectionModeOld
	}

	if hasScopedResourceFilters(p) {
		spec.Template.IncludedResources = mergeUniqueStringsInOrder(
			p.IncludedNamespaceScopedResources,
			p.IncludedClusterScopedResources,
		)
		spec.Template.ExcludedResources = mergeUniqueStringsInOrder(
			p.ExcludedNamespaceScopedResources,
			p.ExcludedClusterScopedResources,
		)
		spec.Template.IncludeClusterResources = resolveScopedIncludeClusterResources(
			p.IncludedClusterScopedResources,
			p.ExcludedClusterScopedResources,
		)
		return resourceSelectionModeScoped
	}

	if len(p.IncludedResources) > 0 {
		spec.Template.IncludedResources = append([]string{}, p.IncludedResources...)
	}
	if len(p.ExcludedResources) > 0 {
		spec.Template.ExcludedResources = append([]string{}, p.ExcludedResources...)
	}
	if p.IncludeClusterResources != nil {
		spec.Template.IncludeClusterResources = cloneBoolPtr(p.IncludeClusterResources)
	}
	if len(p.IncludedResources) > 0 || len(p.ExcludedResources) > 0 || p.IncludeClusterResources != nil {
		return resourceSelectionModeOld
	}
	return ""
}

func includeClusterResourcesIsTrue(v *bool) bool {
	return v != nil && *v
}

func hasScopedResourceFilters(p *disasterv1.RestoreResourceSelectionPolicy) bool {
	if p == nil {
		return false
	}
	return len(p.IncludedNamespaceScopedResources) > 0 ||
		len(p.ExcludedNamespaceScopedResources) > 0 ||
		len(p.IncludedClusterScopedResources) > 0 ||
		len(p.ExcludedClusterScopedResources) > 0
}

func mergeUniqueStringsInOrder(parts ...[]string) []string {
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

func resolveScopedIncludeClusterResources(includedCluster []string, excludedCluster []string) *bool {
	if len(excludedCluster) == 1 && strings.TrimSpace(excludedCluster[0]) == "*" {
		v := false
		return &v
	}
	if len(includedCluster) > 0 || len(excludedCluster) > 0 {
		v := true
		return &v
	}
	return nil
}

func applyExecutionPolicy(spec *disasterv1.AppRestoreSpec, p *disasterv1.RestoreExecutionPolicy) error {
	if p == nil {
		return nil
	}
	if p.ExistingResourcePolicy != "" {
		normalized := strings.ToLower(strings.TrimSpace(p.ExistingResourcePolicy))
		switch normalized {
		case string(velerov1.PolicyTypeNone):
			spec.Template.ExistingResourcePolicy = velerov1.PolicyTypeNone
		case string(velerov1.PolicyTypeUpdate):
			spec.Template.ExistingResourcePolicy = velerov1.PolicyTypeUpdate
		default:
			return fmt.Errorf("ClassMappingInvalid: invalid existingResourcePolicy=%s", p.ExistingResourcePolicy)
		}
	}
	if p.RestorePVs != nil {
		spec.Template.RestorePVs = cloneBoolPtr(p.RestorePVs)
	}
	if p.ItemOperationTimeout != nil {
		spec.Template.ItemOperationTimeout = *p.ItemOperationTimeout
	}
	return nil
}

func buildStorageClassRules(
	ctx context.Context,
	targetClient client.Client,
	p *disasterv1.RestoreClassMappingPolicy,
	defaultNamespaces []string,
	flow modifierFlow,
	allowLegacyStrictReverseFallback bool,
) ([]disasterv1.ResourceModifierRule, mappingSummary, error) {
	unmatchedPolicy := normalizedUnmatchedPolicy(disasterv1.RestoreClassUnmatchedPolicyKeep)
	summary := mappingSummary{unmatchedPolicy: string(unmatchedPolicy)}
	if p == nil {
		return nil, summary, nil
	}

	unmatchedPolicy = normalizedUnmatchedPolicy(p.UnmatchedPolicy)
	summary.unmatchedPolicy = string(unmatchedPolicy)
	summary.strictValidation = p.StrictTargetValidation
	summary.mappingCount = len(p.Mappings)

	if unmatchedPolicy == disasterv1.RestoreClassUnmatchedPolicyFail && len(p.Mappings) == 0 {
		return nil, summary, fmt.Errorf("ClassMappingInvalid: storageClassMapping.mappings is required when unmatchedPolicy=Fail")
	}
	if err := validateClassMappings(p.Mappings); err != nil {
		return nil, summary, err
	}
	effectiveMappings := cloneClassMappings(p.Mappings)
	if flow == modifierFlowReverse {
		effectiveMappings = reverseClassMappings(effectiveMappings)
		if err := validateClassMappings(effectiveMappings); err != nil {
			return nil, summary, err
		}
	}
	if p.StrictTargetValidation {
		if targetClient == nil {
			return nil, summary, fmt.Errorf("ClassMappingInvalid: strictTargetValidation requires target cluster client")
		}
		legacyFallbackEnabled := allowLegacyStrictReverseFallback && !p.StrictTargetValidation
		if err := validateStorageClassTargets(ctx, targetClient, effectiveMappings); err != nil {
			if !legacyFallbackEnabled {
				return nil, summary, err
			}
			reversed := reverseClassMappings(effectiveMappings)
			if revErr := validateClassMappings(reversed); revErr == nil {
				if validateStorageClassTargets(ctx, targetClient, reversed) == nil {
					effectiveMappings = reversed
				} else {
					return nil, summary, err
				}
			} else {
				return nil, summary, err
			}
		}
	}

	rules := make([]disasterv1.ResourceModifierRule, 0, len(effectiveMappings)+1)
	allTargets := make(map[string]struct{})
	for _, m := range effectiveMappings {
		namespaces := m.Namespaces
		if len(namespaces) == 0 {
			namespaces = defaultNamespaces
		}
		rules = append(rules, disasterv1.ResourceModifierRule{
			Conditions: disasterv1.Conditions{
				GroupResource: "persistentvolumeclaims",
				Namespaces:    cloneStringSlice(namespaces),
			},
			Patches: []disasterv1.JSONPatch{{
				Operation: "add",
				Path:      "/spec/storageClassName",
				Value:     m.TargetClass,
			}},
		})
		allTargets[m.TargetClass] = struct{}{}
	}

	// PV is cluster-scoped and cannot be namespace-filtered in current modifier schema.
	// Enforce a deterministic single-target behavior for PV patch.
	targetList := make([]string, 0, len(allTargets))
	for t := range allTargets {
		targetList = append(targetList, t)
	}
	sort.Strings(targetList)
	if len(targetList) > 1 {
		return nil, summary, fmt.Errorf("ClassMappingInvalid: storageClassMapping generates multiple target classes for PV: %s", strings.Join(targetList, ","))
	}
	if len(targetList) == 1 {
		rules = append(rules, disasterv1.ResourceModifierRule{
			Conditions: disasterv1.Conditions{GroupResource: "persistentvolumes"},
			Patches: []disasterv1.JSONPatch{{
				Operation: "add",
				Path:      "/spec/storageClassName",
				Value:     targetList[0],
			}},
		})
	}

	return rules, summary, nil
}

func buildIngressClassRules(
	ctx context.Context,
	targetClient client.Client,
	p *disasterv1.RestoreClassMappingPolicy,
	defaultNamespaces []string,
	flow modifierFlow,
	allowLegacyStrictReverseFallback bool,
) ([]disasterv1.ResourceModifierRule, mappingSummary, error) {
	unmatchedPolicy := normalizedUnmatchedPolicy(disasterv1.RestoreClassUnmatchedPolicyKeep)
	summary := mappingSummary{unmatchedPolicy: string(unmatchedPolicy)}
	if p == nil {
		return nil, summary, nil
	}

	unmatchedPolicy = normalizedUnmatchedPolicy(p.UnmatchedPolicy)
	summary.unmatchedPolicy = string(unmatchedPolicy)
	summary.strictValidation = p.StrictTargetValidation
	summary.mappingCount = len(p.Mappings)

	if unmatchedPolicy == disasterv1.RestoreClassUnmatchedPolicyFail && len(p.Mappings) == 0 {
		return nil, summary, fmt.Errorf("ClassMappingInvalid: ingressClassMapping.mappings is required when unmatchedPolicy=Fail")
	}
	if err := validateClassMappings(p.Mappings); err != nil {
		return nil, summary, err
	}
	effectiveMappings := cloneClassMappings(p.Mappings)
	if flow == modifierFlowReverse {
		effectiveMappings = reverseClassMappings(effectiveMappings)
		if err := validateClassMappings(effectiveMappings); err != nil {
			return nil, summary, err
		}
	}
	if p.StrictTargetValidation {
		if targetClient == nil {
			return nil, summary, fmt.Errorf("ClassMappingInvalid: strictTargetValidation requires target cluster client")
		}
		legacyFallbackEnabled := allowLegacyStrictReverseFallback && !p.StrictTargetValidation
		if err := validateIngressClassTargets(ctx, targetClient, effectiveMappings); err != nil {
			if !legacyFallbackEnabled {
				return nil, summary, err
			}
			reversed := reverseClassMappings(effectiveMappings)
			if revErr := validateClassMappings(reversed); revErr == nil {
				if validateIngressClassTargets(ctx, targetClient, reversed) == nil {
					effectiveMappings = reversed
				} else {
					return nil, summary, err
				}
			} else {
				return nil, summary, err
			}
		}
	}

	rules := make([]disasterv1.ResourceModifierRule, 0, len(effectiveMappings))
	for _, m := range effectiveMappings {
		namespaces := m.Namespaces
		if len(namespaces) == 0 {
			namespaces = defaultNamespaces
		}
		rules = append(rules, disasterv1.ResourceModifierRule{
			Conditions: disasterv1.Conditions{
				GroupResource: "ingresses.networking.k8s.io",
				Namespaces:    cloneStringSlice(namespaces),
			},
			Patches: []disasterv1.JSONPatch{{
				Operation: "add",
				Path:      "/spec/ingressClassName",
				Value:     m.TargetClass,
			}},
		})
	}
	return rules, summary, nil
}

func validateStorageClassTargets(ctx context.Context, targetClient client.Client, mappings []disasterv1.RestoreClassMapping) error {
	list := &storagev1.StorageClassList{}
	if err := targetClient.List(ctx, list); err != nil {
		return fmt.Errorf("StorageClassTargetNotFound: list storageclasses failed: %w", err)
	}
	available := make(map[string]struct{}, len(list.Items))
	for _, sc := range list.Items {
		available[sc.Name] = struct{}{}
	}
	for _, m := range mappings {
		if _, ok := available[m.TargetClass]; !ok {
			return fmt.Errorf("StorageClassTargetNotFound: %s", m.TargetClass)
		}
	}
	return nil
}

func validateIngressClassTargets(ctx context.Context, targetClient client.Client, mappings []disasterv1.RestoreClassMapping) error {
	list := &networkingv1.IngressClassList{}
	if err := targetClient.List(ctx, list); err != nil {
		return fmt.Errorf("IngressClassTargetNotFound: list ingressclasses failed: %w", err)
	}
	available := make(map[string]struct{}, len(list.Items))
	for _, ic := range list.Items {
		available[ic.Name] = struct{}{}
	}
	for _, m := range mappings {
		if _, ok := available[m.TargetClass]; !ok {
			return fmt.Errorf("IngressClassTargetNotFound: %s", m.TargetClass)
		}
	}
	return nil
}

func validateClassMappings(mappings []disasterv1.RestoreClassMapping) error {
	seen := make(map[string]string)
	for _, m := range mappings {
		source := strings.TrimSpace(m.SourceClass)
		target := strings.TrimSpace(m.TargetClass)
		if source == "" || target == "" {
			return fmt.Errorf("ClassMappingInvalid: sourceClass/targetClass must be non-empty")
		}
		ns := cloneStringSlice(m.Namespaces)
		sort.Strings(ns)
		key := source + "|" + strings.Join(ns, ",")
		if prev, ok := seen[key]; ok && prev != target {
			return fmt.Errorf("ClassMappingInvalid: duplicate sourceClass=%s with conflicting targetClass (%s,%s)", source, prev, target)
		}
		seen[key] = target
	}
	return nil
}

func normalizedUnmatchedPolicy(p disasterv1.RestoreClassUnmatchedPolicy) disasterv1.RestoreClassUnmatchedPolicy {
	switch p {
	case disasterv1.RestoreClassUnmatchedPolicyFail:
		return disasterv1.RestoreClassUnmatchedPolicyFail
	default:
		return disasterv1.RestoreClassUnmatchedPolicyKeep
	}
}

func cloneStringSlice(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func cloneBoolPtr(in *bool) *bool {
	if in == nil {
		return nil
	}
	v := *in
	return &v
}

func cloneClassMappings(in []disasterv1.RestoreClassMapping) []disasterv1.RestoreClassMapping {
	if len(in) == 0 {
		return nil
	}
	out := make([]disasterv1.RestoreClassMapping, 0, len(in))
	for _, m := range in {
		out = append(out, disasterv1.RestoreClassMapping{
			SourceClass: m.SourceClass,
			TargetClass: m.TargetClass,
			Namespaces:  cloneStringSlice(m.Namespaces),
		})
	}
	return out
}

func reverseClassMappings(in []disasterv1.RestoreClassMapping) []disasterv1.RestoreClassMapping {
	if len(in) == 0 {
		return nil
	}
	out := make([]disasterv1.RestoreClassMapping, 0, len(in))
	for _, m := range in {
		out = append(out, disasterv1.RestoreClassMapping{
			SourceClass: m.TargetClass,
			TargetClass: m.SourceClass,
			Namespaces:  cloneStringSlice(m.Namespaces),
		})
	}
	return out
}

func (s PolicySummary) annotationValue() string {
	payload, err := json.Marshal(s)
	if err != nil {
		return "{}"
	}
	return string(payload)
}

func (s PolicySummary) modifierAnnotationValue() string {
	payload, err := json.Marshal(map[string]any{
		"flow":                 s.Flow,
		"directionSource":      s.DirectionSource,
		"appliedRuleCount":     s.AppliedRuleCount,
		"skippedRuleCount":     s.SkippedRuleCount,
		"rejectedRuleCount":    s.RejectedRuleCount,
		"conflictCount":        s.ConflictCount,
		"modifierSource":       s.ModifierSource,
		"modifierBulkCount":    s.ModifierBulkActionCount,
		"modifierSnapshotHash": s.ModifierSnapshotHash,
	})
	if err != nil {
		return "{}"
	}
	return string(payload)
}

// ModifierAuditMessage renders a compact summary for structured progress events.
func ModifierAuditMessage(summary PolicySummary) string {
	return fmt.Sprintf(
		"restore policy compiled: flow=%s directionSource=%s applied=%d skipped=%d rejected=%d conflict=%d",
		summary.Flow,
		summary.DirectionSource,
		summary.AppliedRuleCount,
		summary.SkippedRuleCount,
		summary.RejectedRuleCount,
		summary.ConflictCount,
	)
}
