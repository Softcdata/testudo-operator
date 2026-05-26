package apprestore

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	restorebuilder "github.com/softcdata/testudo-operator/internal/controller/restore"
	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	modifierEffectReasonVerifyFailed = "ModifierEffectVerifyFailed"
	modifierEffectReasonNoEffect     = "ModifierNoEffectDetected"
	maxNoEffectDetails               = 10
)

type modifierEffectAudit struct {
	EffectiveRuleCount int
	NoEffectRuleCount  int
	Details            []string
}

type pvcStorageClassRuleCandidate struct {
	RuleRef     string
	Conditions  disasterv1.Conditions
	Expected    string
	Path        string
	GroupSource string
}

func (r *AppRestoreReconciler) verifyStorageClassRuleEffects(
	ctx context.Context,
	cli client.Client,
	appRestore *disasterv1.AppRestore,
) (modifierEffectAudit, error) {
	audit := modifierEffectAudit{}
	if appRestore == nil {
		return audit, nil
	}
	candidates := collectPVCStorageClassRuleCandidates(appRestore.Spec.ResourceModifierRules)
	if len(candidates) == 0 {
		return audit, nil
	}

	for _, candidate := range candidates {
		pvcs, err := listPVCsForConditions(ctx, cli, appRestore, candidate.Conditions)
		if err != nil {
			return audit, fmt.Errorf("verify %s %s failed: %w", candidate.RuleRef, candidate.Path, err)
		}
		if len(pvcs) == 0 {
			audit.NoEffectRuleCount++
			audit.appendDetail(fmt.Sprintf(
				"rule=%s groupResource=%s path=%s matched zero pvc",
				candidate.RuleRef,
				candidate.GroupSource,
				candidate.Path,
			))
			continue
		}

		hasMismatch := false
		for idx := range pvcs {
			pvc := &pvcs[idx]
			actual := ""
			if pvc.Spec.StorageClassName != nil {
				actual = strings.TrimSpace(*pvc.Spec.StorageClassName)
			}
			expected := strings.TrimSpace(candidate.Expected)
			if actual == expected {
				continue
			}
			hasMismatch = true
			audit.appendDetail(fmt.Sprintf(
				"rule=%s resource=%s/%s path=%s expected=%s actual=%s",
				candidate.RuleRef,
				pvc.Namespace,
				pvc.Name,
				candidate.Path,
				expected,
				actual,
			))
		}

		if hasMismatch {
			audit.NoEffectRuleCount++
			continue
		}
		audit.EffectiveRuleCount++
	}

	return audit, nil
}

func collectPVCStorageClassRuleCandidates(rules []disasterv1.ResourceModifierRule) []pvcStorageClassRuleCandidate {
	if len(rules) == 0 {
		return nil
	}
	out := make([]pvcStorageClassRuleCandidate, 0)
	for i := range rules {
		rule := rules[i]
		if !isPersistentVolumeClaimGroupResource(rule.Conditions.GroupResource) {
			continue
		}
		ruleRef := fmt.Sprintf("rule-%04d", i)
		groupSource := strings.TrimSpace(rule.Conditions.GroupResource)
		for _, patch := range rule.Patches {
			path := strings.TrimSpace(patch.Path)
			if path != "/spec/storageClassName" {
				continue
			}
			op := strings.ToLower(strings.TrimSpace(patch.Operation))
			if op != "add" && op != "replace" {
				continue
			}
			out = append(out, pvcStorageClassRuleCandidate{
				RuleRef:     ruleRef,
				Conditions:  rule.Conditions,
				Expected:    patch.Value,
				Path:        path,
				GroupSource: groupSource,
			})
		}
	}
	return out
}

func isPersistentVolumeClaimGroupResource(raw string) bool {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return false
	}
	parts := strings.Split(raw, ".")
	return len(parts) > 0 && parts[0] == "persistentvolumeclaims"
}

func listPVCsForConditions(
	ctx context.Context,
	cli client.Client,
	appRestore *disasterv1.AppRestore,
	conditions disasterv1.Conditions,
) ([]corev1.PersistentVolumeClaim, error) {
	labelSelector, err := buildLabelSelector(conditions.LabelSelector)
	if err != nil {
		return nil, err
	}

	nameRegex, err := buildNameRegex(conditions.ResourceNameRegex)
	if err != nil {
		return nil, err
	}

	namespaces := resolveNamespacesForPVCVerification(appRestore, conditions.Namespaces)
	result := make([]corev1.PersistentVolumeClaim, 0)
	seen := make(map[string]struct{})

	listOne := func(namespace string) error {
		list := &corev1.PersistentVolumeClaimList{}
		opts := make([]client.ListOption, 0, 2)
		if strings.TrimSpace(namespace) != "" {
			opts = append(opts, client.InNamespace(namespace))
		}
		if labelSelector != nil {
			opts = append(opts, client.MatchingLabelsSelector{Selector: labelSelector})
		}
		if err := cli.List(ctx, list, opts...); err != nil {
			if namespace == "" {
				return fmt.Errorf("list pvc failed: %w", err)
			}
			return fmt.Errorf("list pvc namespace=%s failed: %w", namespace, err)
		}
		for i := range list.Items {
			item := list.Items[i]
			if nameRegex != nil && !nameRegex.MatchString(item.Name) {
				continue
			}
			key := item.Namespace + "/" + item.Name
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			result = append(result, item)
		}
		return nil
	}

	if len(namespaces) == 0 {
		if err := listOne(""); err != nil {
			return nil, err
		}
		return result, nil
	}

	for _, ns := range namespaces {
		if err := listOne(ns); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func resolveNamespacesForPVCVerification(appRestore *disasterv1.AppRestore, conditionNamespaces []string) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0)
	appendUnique := func(list []string) {
		for _, ns := range list {
			ns = strings.TrimSpace(ns)
			if ns == "" {
				continue
			}
			if _, ok := seen[ns]; ok {
				continue
			}
			seen[ns] = struct{}{}
			result = append(result, ns)
		}
	}

	appendUnique(conditionNamespaces)
	if len(result) > 0 {
		return result
	}
	if appRestore == nil {
		return nil
	}
	appendUnique(appRestore.Status.TargetNamespaces)
	if len(result) > 0 {
		return result
	}
	appendUnique(appRestore.Spec.Template.IncludedNamespaces)
	return result
}

func buildLabelSelector(selector *metav1.LabelSelector) (labels.Selector, error) {
	if selector == nil {
		return nil, nil
	}
	parsed, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		return nil, fmt.Errorf("invalid labelSelector: %w", err)
	}
	return parsed, nil
}

func buildNameRegex(raw string) (*regexp.Regexp, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	re, err := regexp.Compile(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid resourceNameRegex: %w", err)
	}
	return re, nil
}

func (a *modifierEffectAudit) appendDetail(detail string) {
	if a == nil || strings.TrimSpace(detail) == "" {
		return
	}
	if len(a.Details) >= maxNoEffectDetails {
		return
	}
	a.Details = append(a.Details, detail)
}

func applyModifierEffectAuditAnnotations(meta *metav1.ObjectMeta, audit modifierEffectAudit) {
	if meta == nil {
		return
	}
	if meta.Annotations == nil {
		meta.Annotations = map[string]string{}
	}

	summary := map[string]any{}
	if raw := strings.TrimSpace(meta.Annotations[restorebuilder.AnnotationModifierSummary]); raw != "" {
		_ = json.Unmarshal([]byte(raw), &summary)
	}
	summary["effectiveRuleCount"] = audit.EffectiveRuleCount
	summary["noEffectRuleCount"] = audit.NoEffectRuleCount
	if audit.NoEffectRuleCount > 0 && len(audit.Details) > 0 {
		summary["noEffectReason"] = audit.Details[0]
	} else {
		delete(summary, "noEffectReason")
	}

	payload, err := json.Marshal(summary)
	if err != nil {
		meta.Annotations[restorebuilder.AnnotationModifierSummary] = "{}"
		return
	}
	meta.Annotations[restorebuilder.AnnotationModifierSummary] = string(payload)
}

func buildModifierNoEffectMessage(audit modifierEffectAudit) string {
	if len(audit.Details) == 0 {
		return "modifier effect verification failed"
	}
	return "modifier effect verification failed: " + strings.Join(audit.Details, "; ")
}
