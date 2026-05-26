package restore

import (
	"fmt"
	"sort"
	"strings"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var defaultMigrationApplyTargets = []disasterv1.RestoreModifierApplyTarget{
	disasterv1.RestoreModifierApplyDataSync,
	disasterv1.RestoreModifierApplyResourceSync,
	disasterv1.RestoreModifierApplyDrill,
}

// VeleroRuleImportOptions controls import behavior from legacy AppRestore resourceModifierRules.
type VeleroRuleImportOptions struct {
	ApplyTo  []disasterv1.RestoreModifierApplyTarget
	Priority int32
	IDPrefix string
}

// LegacyRestorePolicyConversionResult is the converter output for legacy restore policy fields.
type LegacyRestorePolicyConversionResult struct {
	Rules    []disasterv1.RestoreModifierRule
	Warnings []string
}

// ImportVeleroResourceModifierRules converts existing Velero-native rules into modifier DSL rules.
func ImportVeleroResourceModifierRules(
	rules []disasterv1.ResourceModifierRule,
	options VeleroRuleImportOptions,
) ([]disasterv1.RestoreModifierRule, error) {
	if len(rules) == 0 {
		return nil, nil
	}
	idPrefix := strings.TrimSpace(options.IDPrefix)
	if idPrefix == "" {
		idPrefix = "import-velero"
	}
	applyTo := options.ApplyTo
	if len(applyTo) == 0 {
		applyTo = append([]disasterv1.RestoreModifierApplyTarget{}, defaultMigrationApplyTargets...)
	}

	out := make([]disasterv1.RestoreModifierRule, 0, len(rules))
	for idx := range rules {
		rule := rules[idx]
		ruleID := fmt.Sprintf("%s-%04d", idPrefix, idx)
		if err := validateResourceModifierRuleComplexity(ruleID, rule); err != nil {
			return nil, err
		}
		if len(rule.Patches) == 0 {
			return nil, fmt.Errorf("%s: rule=%s patches cannot be empty", ModifierErrorRuleRejected, ruleID)
		}

		patches := make([]disasterv1.JSONPatch, 0, len(rule.Patches))
		for _, patch := range rule.Patches {
			patches = append(patches, patch)
		}

		out = append(out, disasterv1.RestoreModifierRule{
			ID:       ruleID,
			Mode:     disasterv1.RestoreModifierModeVeleroNative,
			ApplyTo:  append([]disasterv1.RestoreModifierApplyTarget{}, applyTo...),
			Priority: options.Priority,
			Conditions: disasterv1.Conditions{
				GroupResource:     rule.Conditions.GroupResource,
				ResourceNameRegex: rule.Conditions.ResourceNameRegex,
				Namespaces:        cloneStringSlice(rule.Conditions.Namespaces),
				LabelSelector:     cloneLabelSelector(rule.Conditions.LabelSelector),
			},
			VeleroRule: &disasterv1.RestoreModifierVeleroRule{
				Patches: patches,
			},
		})
	}
	return out, nil
}

// ConvertLegacyRestorePolicyToDSL converts legacy mapping fields into modifier DSL rules.
func ConvertLegacyRestorePolicyToDSL(
	policy *disasterv1.RestorePolicy,
	defaultNamespaces []string,
) (LegacyRestorePolicyConversionResult, error) {
	result := LegacyRestorePolicyConversionResult{}
	if policy == nil {
		return result, nil
	}

	if len(policy.ModifierRules) > 0 {
		copied := make([]disasterv1.RestoreModifierRule, 0, len(policy.ModifierRules))
		for idx := range policy.ModifierRules {
			rule := policy.ModifierRules[idx].DeepCopy()
			if rule != nil {
				copied = append(copied, *rule)
			}
		}
		result.Rules = append(result.Rules, copied...)
	}

	appendRule := func(rule disasterv1.RestoreModifierRule) {
		rule.ApplyTo = append([]disasterv1.RestoreModifierApplyTarget{}, defaultMigrationApplyTargets...)
		if rule.Priority == 0 {
			rule.Priority = 200
		}
		result.Rules = append(result.Rules, rule)
	}

	if policy.StorageClassMapping != nil {
		mappings := cloneClassMappings(policy.StorageClassMapping.Mappings)
		if err := validateClassMappings(mappings); err != nil {
			return result, err
		}

		targetClasses := make(map[string]struct{}, len(mappings))
		sourceByTarget := make(map[string]string, len(mappings))
		for idx, mapping := range mappings {
			namespaces := cloneStringSlice(mapping.Namespaces)
			if len(namespaces) == 0 {
				namespaces = cloneStringSlice(defaultNamespaces)
			}
			appendRule(disasterv1.RestoreModifierRule{
				ID:   fmt.Sprintf("legacy-storage-pvc-%03d", idx),
				Mode: disasterv1.RestoreModifierModeReversible,
				Conditions: disasterv1.Conditions{
					GroupResource: "persistentvolumeclaims",
					Namespaces:    namespaces,
				},
				Pair: &disasterv1.RestoreModifierPair{
					Path:        "/spec/storageClassName",
					SourceValue: mapping.SourceClass,
					TargetValue: mapping.TargetClass,
				},
			})
			targetClasses[mapping.TargetClass] = struct{}{}
			sourceByTarget[mapping.TargetClass] = mapping.SourceClass
		}

		targetList := make([]string, 0, len(targetClasses))
		for target := range targetClasses {
			targetList = append(targetList, target)
		}
		sort.Strings(targetList)
		if len(targetList) > 1 {
			return result, fmt.Errorf(
				"ClassMappingInvalid: storageClassMapping generates multiple target classes for PV: %s",
				strings.Join(targetList, ","),
			)
		}
		if len(targetList) == 1 {
			target := targetList[0]
			appendRule(disasterv1.RestoreModifierRule{
				ID:   "legacy-storage-pv",
				Mode: disasterv1.RestoreModifierModeReversible,
				Conditions: disasterv1.Conditions{
					GroupResource: "persistentvolumes",
				},
				Pair: &disasterv1.RestoreModifierPair{
					Path:        "/spec/storageClassName",
					SourceValue: sourceByTarget[target],
					TargetValue: target,
				},
			})
		}

		if policy.StorageClassMapping.StrictTargetValidation {
			result.Warnings = append(result.Warnings, "legacy storageClassMapping.strictTargetValidation requires runtime target validation and is not encoded in DSL rules")
		}
		if policy.StorageClassMapping.UnmatchedPolicy != "" {
			result.Warnings = append(result.Warnings, "legacy storageClassMapping.unmatchedPolicy is preserved at policy-level and not encoded in DSL rules")
		}
	}

	if policy.IngressClassMapping != nil {
		mappings := cloneClassMappings(policy.IngressClassMapping.Mappings)
		if err := validateClassMappings(mappings); err != nil {
			return result, err
		}
		for idx, mapping := range mappings {
			namespaces := cloneStringSlice(mapping.Namespaces)
			if len(namespaces) == 0 {
				namespaces = cloneStringSlice(defaultNamespaces)
			}
			appendRule(disasterv1.RestoreModifierRule{
				ID:   fmt.Sprintf("legacy-ingress-%03d", idx),
				Mode: disasterv1.RestoreModifierModeReversible,
				Conditions: disasterv1.Conditions{
					GroupResource: "ingresses.networking.k8s.io",
					Namespaces:    namespaces,
				},
				Pair: &disasterv1.RestoreModifierPair{
					Path:        "/spec/ingressClassName",
					SourceValue: mapping.SourceClass,
					TargetValue: mapping.TargetClass,
				},
			})
		}
		if policy.IngressClassMapping.StrictTargetValidation {
			result.Warnings = append(result.Warnings, "legacy ingressClassMapping.strictTargetValidation requires runtime target validation and is not encoded in DSL rules")
		}
		if policy.IngressClassMapping.UnmatchedPolicy != "" {
			result.Warnings = append(result.Warnings, "legacy ingressClassMapping.unmatchedPolicy is preserved at policy-level and not encoded in DSL rules")
		}
	}

	return result, nil
}

func cloneLabelSelector(in *metav1.LabelSelector) *metav1.LabelSelector {
	if in == nil {
		return nil
	}
	return in.DeepCopy()
}
