package restore

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
)

var submissionValidationApplyTargets = []disasterv1.RestoreModifierApplyTarget{
	disasterv1.RestoreModifierApplyDataSync,
	disasterv1.RestoreModifierApplyResourceSync,
	disasterv1.RestoreModifierApplyDrill,
}

// ModifierRuleResourceLocator abstracts live resource lookup for submission-time validation.
type ModifierRuleResourceLocator interface {
	ListMatchingResources(
		ctx context.Context,
		conditions disasterv1.Conditions,
		defaultNamespaces []string,
	) ([]unstructured.Unstructured, error)
}

// ValidateModifierRulesAtSubmission validates restorePolicy.modifierRules at submission time.
// Validation includes:
// 1) DSL compile validation (forward/reverse, all apply targets)
// 2) live object selection validation (conditions must match at least one object)
// 3) JSON Pointer locatability validation (including escaped tokens and array indexes)
func ValidateModifierRulesAtSubmission(
	ctx context.Context,
	instance *disasterv1.DisasterInstance,
	baselineSource string,
	baselineTarget string,
	locator ModifierRuleResourceLocator,
) error {
	if instance == nil || instance.Spec.RestorePolicy == nil {
		return nil
	}
	policy := instance.Spec.RestorePolicy
	if len(policy.ModifierRules) == 0 {
		return nil
	}

	baselineSource = strings.TrimSpace(baselineSource)
	baselineTarget = strings.TrimSpace(baselineTarget)
	if isUnifiedDirectionResolverEnabled(policy) && (baselineSource == "" || baselineTarget == "") {
		return fmt.Errorf(
			"%s: baseline source/target cluster is required for submission validation",
			ModifierErrorRuleRejected,
		)
	}

	if err := validateModifierCompileAtSubmission(instance, baselineSource, baselineTarget); err != nil {
		return err
	}
	if locator == nil {
		return fmt.Errorf("%s: resource locator is required for submission validation", ModifierErrorRuleRejected)
	}

	for idx := range policy.ModifierRules {
		rule := policy.ModifierRules[idx]
		if !modifierRuleEnabled(rule) {
			continue
		}
		if err := validateSingleRuleTargetsAndPaths(
			ctx,
			normalizedRuleID(rule, idx),
			rule,
			instance.Spec.Namespaces,
			ApplyInstanceRestorePolicyOptions{
				BaselineSourceCluster: baselineSource,
				BaselineTargetCluster: baselineTarget,
			},
			locator,
		); err != nil {
			return err
		}
	}
	return nil
}

func validateModifierCompileAtSubmission(
	instance *disasterv1.DisasterInstance,
	baselineSource string,
	baselineTarget string,
) error {
	for _, flow := range [][2]string{
		{baselineSource, baselineTarget},
		{baselineTarget, baselineSource},
	} {
		shadow := instance.DeepCopy()
		shadow.Status.PrimaryCluster = flow[0]
		shadow.Status.SecondaryCluster = flow[1]

		for _, target := range submissionValidationApplyTargets {
			_, _, err := compileModifierRulesForInstance(
				shadow,
				ApplyInstanceRestorePolicyOptions{
					BaselineSourceCluster: baselineSource,
					BaselineTargetCluster: baselineTarget,
					ApplyTarget:           target,
				},
			)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

type submissionRulePatch struct {
	operation string
	path      string
}

func validateSingleRuleTargetsAndPaths(
	ctx context.Context,
	ruleID string,
	rule disasterv1.RestoreModifierRule,
	defaultNamespaces []string,
	options ApplyInstanceRestorePolicyOptions,
	locator ModifierRuleResourceLocator,
) error {
	if err := validateRuleNamespaceScope(ruleID, rule.Conditions.Namespaces, defaultNamespaces); err != nil {
		return err
	}

	patches, err := extractSubmissionRulePatches(rule)
	if err != nil {
		return err
	}
	if len(patches) == 0 {
		return fmt.Errorf("%s: rule=%s has no patch path for validation", ModifierErrorRuleRejected, ruleID)
	}

	objects, err := locator.ListMatchingResources(ctx, rule.Conditions, defaultNamespaces)
	if err != nil {
		return fmt.Errorf(
			"%s: rule=%s groupResource=%s list resources failed: %w",
			ModifierErrorRuleRejected,
			ruleID,
			strings.TrimSpace(rule.Conditions.GroupResource),
			err,
		)
	}
	if len(objects) == 0 {
		return fmt.Errorf(
			"%s: rule=%s groupResource=%s matched zero resources",
			ModifierErrorRuleRejected,
			ruleID,
			strings.TrimSpace(rule.Conditions.GroupResource),
		)
	}

	for _, obj := range objects {
		resourceRef := obj.GetName()
		if ns := strings.TrimSpace(obj.GetNamespace()); ns != "" {
			resourceRef = ns + "/" + resourceRef
		}

		for _, patch := range patches {
			if err := ensureJSONPointerLocatable(obj.Object, patch.path, patch.operation); err != nil {
				return fmt.Errorf(
					"%s: rule=%s groupResource=%s resource=%s path=%s: %v",
					ModifierErrorRuleRejected,
					ruleID,
					strings.TrimSpace(rule.Conditions.GroupResource),
					resourceRef,
					patch.path,
					err,
				)
			}
			if normalizeMode(rule.Mode) == disasterv1.RestoreModifierModeReversible {
				if err := validatePairValueCompatibility(patch.path, rule.Pair, obj.Object, options); err != nil {
					return fmt.Errorf(
						"%s: rule=%s groupResource=%s resource=%s path=%s: %v",
						ModifierErrorRuleRejected,
						ruleID,
						strings.TrimSpace(rule.Conditions.GroupResource),
						resourceRef,
						patch.path,
						err,
					)
				}
			}
		}
	}
	return nil
}

func validateRuleNamespaceScope(ruleID string, ruleNamespaces []string, instanceNamespaces []string) error {
	if len(ruleNamespaces) == 0 || len(instanceNamespaces) == 0 {
		return nil
	}

	allowed := make(map[string]struct{}, len(instanceNamespaces))
	for _, ns := range instanceNamespaces {
		trimmed := strings.TrimSpace(ns)
		if trimmed == "" {
			continue
		}
		allowed[trimmed] = struct{}{}
	}
	if len(allowed) == 0 {
		return nil
	}

	for _, ns := range ruleNamespaces {
		trimmed := strings.TrimSpace(ns)
		if trimmed == "" {
			continue
		}
		if _, ok := allowed[trimmed]; ok {
			continue
		}
		return fmt.Errorf(
			"%s: rule=%s namespace=%s is outside instance namespaces",
			ModifierErrorRuleRejected,
			ruleID,
			trimmed,
		)
	}

	return nil
}

func extractSubmissionRulePatches(rule disasterv1.RestoreModifierRule) ([]submissionRulePatch, error) {
	switch normalizeMode(rule.Mode) {
	case disasterv1.RestoreModifierModeVeleroNative:
		if rule.VeleroRule == nil {
			return nil, fmt.Errorf("%s: veleroNative rule missing veleroRule", ModifierErrorRuleRejected)
		}
		patches := make([]submissionRulePatch, 0, len(rule.VeleroRule.Patches))
		for _, p := range rule.VeleroRule.Patches {
			patches = append(patches, submissionRulePatch{
				operation: strings.TrimSpace(p.Operation),
				path:      strings.TrimSpace(p.Path),
			})
		}
		return patches, nil
	case disasterv1.RestoreModifierModeReversible:
		if rule.Pair == nil {
			return nil, fmt.Errorf(
				"%s: reversible rule must use pair canonical form (pair.path, pair.sourceValue, pair.targetValue)",
				ModifierErrorRuleNotReversible,
			)
		}
		return []submissionRulePatch{{
			operation: "add",
			path:      strings.TrimSpace(rule.Pair.Path),
		}}, nil
	default:
		return nil, fmt.Errorf("%s: unsupported mode=%s", ModifierErrorRuleRejected, rule.Mode)
	}
}

func decodeJSONPointerToken(token string) (string, error) {
	if token == "" {
		return "", nil
	}
	var b strings.Builder
	b.Grow(len(token))
	for i := 0; i < len(token); i++ {
		if token[i] != '~' {
			b.WriteByte(token[i])
			continue
		}
		if i+1 >= len(token) {
			return "", fmt.Errorf("invalid JSON Pointer escape at end")
		}
		switch token[i+1] {
		case '0':
			b.WriteByte('~')
		case '1':
			b.WriteByte('/')
		default:
			return "", fmt.Errorf("invalid JSON Pointer escape ~%c", token[i+1])
		}
		i++
	}
	return b.String(), nil
}

// DynamicModifierRuleResourceLocator resolves live resources using dynamic/discovery clients.
type DynamicModifierRuleResourceLocator struct {
	dynamicClient dynamic.Interface
	restMapper    meta.RESTMapper
}

// NewDynamicModifierRuleResourceLocator builds a locator from cluster rest config.
func NewDynamicModifierRuleResourceLocator(restConfig *rest.Config) (*DynamicModifierRuleResourceLocator, error) {
	if restConfig == nil {
		return nil, fmt.Errorf("nil rest config")
	}
	dyn, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, err
	}
	disco, err := discovery.NewDiscoveryClientForConfig(restConfig)
	if err != nil {
		return nil, err
	}
	groupResources, err := restmapper.GetAPIGroupResources(disco)
	if err != nil {
		return nil, err
	}
	return &DynamicModifierRuleResourceLocator{
		dynamicClient: dyn,
		restMapper:    restmapper.NewDiscoveryRESTMapper(groupResources),
	}, nil
}

// ListMatchingResources finds live resources by Velero-like conditions.
func (l *DynamicModifierRuleResourceLocator) ListMatchingResources(
	ctx context.Context,
	conditions disasterv1.Conditions,
	defaultNamespaces []string,
) ([]unstructured.Unstructured, error) {
	if l == nil || l.dynamicClient == nil || l.restMapper == nil {
		return nil, fmt.Errorf("resource locator is not initialized")
	}
	groupResource, err := parseGroupResource(conditions.GroupResource)
	if err != nil {
		return nil, err
	}
	gvr, err := l.restMapper.ResourceFor(schema.GroupVersionResource{
		Group:    groupResource.Group,
		Resource: groupResource.Resource,
	})
	if err != nil {
		return nil, fmt.Errorf("resolve groupResource=%s failed: %w", strings.TrimSpace(conditions.GroupResource), err)
	}

	gvk, err := l.restMapper.KindFor(gvr)
	if err != nil {
		return nil, fmt.Errorf("resolve kind for groupResource=%s failed: %w", strings.TrimSpace(conditions.GroupResource), err)
	}
	mapping, err := l.restMapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return nil, fmt.Errorf("resolve scope for groupResource=%s failed: %w", strings.TrimSpace(conditions.GroupResource), err)
	}

	labelSelector := labels.Everything()
	labelSelectorText := ""
	if conditions.LabelSelector != nil {
		selector, selectorErr := metav1.LabelSelectorAsSelector(conditions.LabelSelector)
		if selectorErr != nil {
			return nil, fmt.Errorf("%s: invalid labelSelector: %v", ModifierErrorRuleRejected, selectorErr)
		}
		labelSelector = selector
		if !selector.Empty() {
			labelSelectorText = selector.String()
		}
	}

	var nameRegex *regexp.Regexp
	if rawRegex := strings.TrimSpace(conditions.ResourceNameRegex); rawRegex != "" {
		compiledRegex, regexErr := compileResourceNameRegex(rawRegex)
		if regexErr != nil {
			return nil, regexErr
		}
		nameRegex = compiledRegex
	}

	matches := make([]unstructured.Unstructured, 0)
	dynResource := l.dynamicClient.Resource(gvr)
	listOptions := metav1.ListOptions{LabelSelector: labelSelectorText}

	scopeName := mapping.Scope.Name()
	if scopeName == meta.RESTScopeNameNamespace {
		namespaces := effectiveRuleNamespaces(conditions.Namespaces, defaultNamespaces)
		if len(namespaces) == 0 {
			namespaces = []string{metav1.NamespaceAll}
		}
		for _, namespace := range namespaces {
			items, listErr := dynResource.Namespace(namespace).List(ctx, listOptions)
			if listErr != nil {
				return nil, listErr
			}
			matches = append(matches, filterMatchedItems(items.Items, labelSelector, nameRegex)...)
		}
		return matches, nil
	}

	items, listErr := dynResource.List(ctx, listOptions)
	if listErr != nil {
		return nil, listErr
	}
	matches = append(matches, filterMatchedItems(items.Items, labelSelector, nameRegex)...)
	return matches, nil
}

func parseGroupResource(raw string) (schema.GroupResource, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return schema.GroupResource{}, fmt.Errorf("%s: conditions.groupResource is required", ModifierErrorRuleRejected)
	}

	parts := strings.Split(raw, ".")
	resource := strings.TrimSpace(parts[0])
	if resource == "" {
		return schema.GroupResource{}, fmt.Errorf("%s: invalid groupResource=%s", ModifierErrorRuleRejected, raw)
	}
	group := ""
	if len(parts) > 1 {
		group = strings.TrimSpace(strings.Join(parts[1:], "."))
	}
	return schema.GroupResource{Group: group, Resource: resource}, nil
}

func effectiveRuleNamespaces(ruleNamespaces []string, defaultNamespaces []string) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, len(ruleNamespaces)+len(defaultNamespaces))

	appendAll := func(items []string) {
		for _, item := range items {
			ns := strings.TrimSpace(item)
			if ns == "" {
				continue
			}
			if _, ok := seen[ns]; ok {
				continue
			}
			seen[ns] = struct{}{}
			out = append(out, ns)
		}
	}

	appendAll(ruleNamespaces)
	if len(out) > 0 {
		return out
	}
	appendAll(defaultNamespaces)
	return out
}

func filterMatchedItems(
	items []unstructured.Unstructured,
	selector labels.Selector,
	nameRegex *regexp.Regexp,
) []unstructured.Unstructured {
	matches := make([]unstructured.Unstructured, 0, len(items))
	for _, item := range items {
		if selector != nil && !selector.Empty() && !selector.Matches(labels.Set(item.GetLabels())) {
			continue
		}
		if nameRegex != nil && !nameRegex.MatchString(item.GetName()) {
			continue
		}
		matches = append(matches, item)
	}
	return matches
}
