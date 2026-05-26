package restore

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
)

const (
	// ModifierErrorDirectionResolveFailed indicates flow direction cannot be inferred.
	ModifierErrorDirectionResolveFailed = "DirectionResolveFailed"
	// ModifierErrorRuleRejected indicates governance/schema rejection.
	ModifierErrorRuleRejected = "ModifierRuleRejected"
	// ModifierErrorRuleNotReversible indicates transform is not reversible in Phase 1.
	ModifierErrorRuleNotReversible = "ModifierRuleNotReversible"
	// ModifierErrorRuleConflict indicates conflict key has different values.
	ModifierErrorRuleConflict = "ModifierRuleConflict"
	// ModifierErrorFeatureDisabled indicates new DSL is disabled by migration gate.
	ModifierErrorFeatureDisabled = "ModifierFeatureDisabled"
)

var pairPlaceholderPattern = regexp.MustCompile(`{{\s*\.(SourceCluster|TargetCluster|Flow)\s*}}`)

type modifierFlow string

const (
	modifierFlowForward modifierFlow = "forward"
	modifierFlowReverse modifierFlow = "reverse"
)

type directionSource string

const (
	directionSourceRuntimeStatus    directionSource = "runtimeStatus"
	directionSourceBaselineFallback directionSource = "baselineFallback"
)

// ModifierCompileSummary captures compiler-level observability stats.
type ModifierCompileSummary struct {
	Flow              string
	DirectionSource   string
	AppliedRuleCount  int
	SkippedRuleCount  int
	RejectedRuleCount int
	ConflictCount     int
}

// ApplyInstanceRestorePolicyOptions controls baseline/apply-target context.
type ApplyInstanceRestorePolicyOptions struct {
	BaselineSourceCluster string
	BaselineTargetCluster string
	ApplyTarget           disasterv1.RestoreModifierApplyTarget
	SystemRules           []disasterv1.ResourceModifierRule
	SystemProtectRules    []disasterv1.ResourceModifierRule
	RestorePolicyOverride *disasterv1.RestorePolicy
}

// ApplyInstanceRestorePolicyOption mutates policy apply options.
type ApplyInstanceRestorePolicyOption func(*ApplyInstanceRestorePolicyOptions)

// WithBaselineClusters configures baseline source/target used by direction resolver.
func WithBaselineClusters(sourceCluster, targetCluster string) ApplyInstanceRestorePolicyOption {
	return func(o *ApplyInstanceRestorePolicyOptions) {
		o.BaselineSourceCluster = strings.TrimSpace(sourceCluster)
		o.BaselineTargetCluster = strings.TrimSpace(targetCluster)
	}
}

// WithApplyTarget sets the current restore path (dataSync/resourceSync/drill).
func WithApplyTarget(target disasterv1.RestoreModifierApplyTarget) ApplyInstanceRestorePolicyOption {
	return func(o *ApplyInstanceRestorePolicyOptions) {
		o.ApplyTarget = target
	}
}

// WithSystemRules injects system-built rules into unified merge pipeline.
func WithSystemRules(rules []disasterv1.ResourceModifierRule) ApplyInstanceRestorePolicyOption {
	return func(o *ApplyInstanceRestorePolicyOptions) {
		o.SystemRules = cloneResourceModifierRules(rules)
	}
}

// WithSystemProtectRules injects highest-precedence system-protect rules.
func WithSystemProtectRules(rules []disasterv1.ResourceModifierRule) ApplyInstanceRestorePolicyOption {
	return func(o *ApplyInstanceRestorePolicyOptions) {
		o.SystemProtectRules = cloneResourceModifierRules(rules)
	}
}

// WithRestorePolicyOverride overlays drill-scoped restorePolicy on top of the instance policy.
func WithRestorePolicyOverride(policy *disasterv1.RestorePolicy) ApplyInstanceRestorePolicyOption {
	return func(o *ApplyInstanceRestorePolicyOptions) {
		if policy == nil {
			o.RestorePolicyOverride = nil
			return
		}
		o.RestorePolicyOverride = policy.DeepCopy()
	}
}

func cloneResourceModifierRules(rules []disasterv1.ResourceModifierRule) []disasterv1.ResourceModifierRule {
	if len(rules) == 0 {
		return nil
	}
	out := make([]disasterv1.ResourceModifierRule, 0, len(rules))
	for idx := range rules {
		copied := rules[idx].DeepCopy()
		if copied == nil {
			continue
		}
		out = append(out, *copied)
	}
	return out
}

func defaultApplyInstanceRestorePolicyOptions() ApplyInstanceRestorePolicyOptions {
	return ApplyInstanceRestorePolicyOptions{}
}

func applyInstanceRestorePolicyOptions(opts ...ApplyInstanceRestorePolicyOption) ApplyInstanceRestorePolicyOptions {
	out := defaultApplyInstanceRestorePolicyOptions()
	for _, opt := range opts {
		if opt != nil {
			opt(&out)
		}
	}
	return out
}

func isUnifiedDirectionResolverEnabled(policy *disasterv1.RestorePolicy) bool {
	if policy == nil || policy.UseUnifiedDirectionResolver == nil {
		return false
	}
	return *policy.UseUnifiedDirectionResolver
}

func hasNewDSLRules(policy *disasterv1.RestorePolicy) bool {
	if policy == nil {
		return false
	}
	if hasEffectiveBulkModifierActions(policy) {
		return true
	}
	return len(policy.ModifierRules) > 0
}

func compileModifierRulesForInstance(
	instance *disasterv1.DisasterInstance,
	options ApplyInstanceRestorePolicyOptions,
) ([]disasterv1.ResourceModifierRule, ModifierCompileSummary, error) {
	summary := ModifierCompileSummary{}
	totalRuleCount := len(options.SystemRules) + len(options.SystemProtectRules)

	rules, err := compileSystemRuleCandidates(options)
	if err != nil {
		summary.RejectedRuleCount++
		return nil, summary, err
	}

	if instance == nil || instance.Spec.RestorePolicy == nil {
		compiled, mergeErr := mergeCompiledCandidates(rules, &summary)
		return compiled, summary, mergeErr
	}
	policy := instance.Spec.RestorePolicy
	modifierInput, usingSnapshot, inputErr := effectiveModifierRuleInput(policy)
	if inputErr != nil {
		summary.RejectedRuleCount++
		return nil, summary, inputErr
	}
	totalRuleCount += len(modifierInput)
	if err := validateRuleCountLimit(totalRuleCount); err != nil {
		summary.RejectedRuleCount++
		return nil, summary, err
	}
	if !isUnifiedDirectionResolverEnabled(policy) {
		if hasNewDSLRules(policy) {
			return nil, summary, fmt.Errorf("%s: unified direction resolver is disabled for modifierRules", ModifierErrorFeatureDisabled)
		}
		compiled, mergeErr := mergeCompiledCandidates(rules, &summary)
		return compiled, summary, mergeErr
	}

	if len(modifierInput) > 0 {
		flow, source, resolveErr := resolveModifierFlow(
			options.BaselineSourceCluster,
			options.BaselineTargetCluster,
			instance.Status.PrimaryCluster,
			instance.Status.SecondaryCluster,
		)
		if resolveErr != nil {
			return nil, summary, resolveErr
		}
		summary.Flow = string(flow)
		summary.DirectionSource = string(source)

		for idx := range modifierInput {
			rule := modifierInput[idx]
			if !modifierRuleEnabled(rule) {
				summary.SkippedRuleCount++
				continue
			}
			if !modifierRuleAppliesToTarget(rule, options.ApplyTarget) {
				summary.SkippedRuleCount++
				continue
			}
			if !directionPolicyAllows(rule.DirectionPolicy, flow) {
				summary.SkippedRuleCount++
				continue
			}
			candidate, cErr := compileOneModifierRule(rule, flow, options)
			if cErr != nil {
				summary.RejectedRuleCount++
				return nil, summary, cErr
			}
			candidate.priority = rule.Priority
			if usingSnapshot {
				candidate.ruleID = normalizedRuleID(rule, idx)
			} else {
				candidate.ruleID = normalizedRuleID(rule, idx)
			}
			candidate.onConflict = normalizeOnConflict(rule.OnConflict)
			rules = append(rules, candidate)
		}
	}

	compiled, mergeErr := mergeCompiledCandidates(rules, &summary)
	return compiled, summary, mergeErr
}

func compileSystemRuleCandidates(options ApplyInstanceRestorePolicyOptions) ([]compiledCandidate, error) {
	total := len(options.SystemRules) + len(options.SystemProtectRules)
	if total == 0 {
		return nil, nil
	}
	out := make([]compiledCandidate, 0, total)
	compile := func(
		rules []disasterv1.ResourceModifierRule,
		idPrefix string,
		isProtect bool,
	) error {
		for idx := range rules {
			rule := rules[idx]
			ruleID := fmt.Sprintf("%s-%04d", idPrefix, idx)
			if err := validateResourceModifierRuleComplexity(ruleID, rule); err != nil {
				return err
			}
			if len(rule.Patches) == 0 {
				return fmt.Errorf("%s: %s rule[%d] patches cannot be empty", ModifierErrorRuleRejected, idPrefix, idx)
			}
			patches := make([]disasterv1.JSONPatch, 0, len(rule.Patches))
			for _, patch := range rule.Patches {
				if err := validatePatchGovernance(
					rule.Conditions,
					patch,
					patchGovernanceOptions{fromSystemRule: true},
				); err != nil {
					return err
				}
				patches = append(patches, patch)
			}
			out = append(out, compiledCandidate{
				rule: disasterv1.ResourceModifierRule{
					Conditions: rule.Conditions,
					Patches:    patches,
				},
				ruleID:          ruleID,
				onConflict:      disasterv1.RestoreModifierConflictPolicyFail,
				isSystem:        true,
				isSystemProtect: isProtect,
			})
		}
		return nil
	}
	if err := compile(options.SystemRules, "system", false); err != nil {
		return nil, err
	}
	if err := compile(options.SystemProtectRules, "system-protect", true); err != nil {
		return nil, err
	}
	return out, nil
}

func mergeCompiledCandidates(rules []compiledCandidate, summary *ModifierCompileSummary) ([]disasterv1.ResourceModifierRule, error) {
	if len(rules) == 0 {
		return nil, nil
	}
	sort.SliceStable(rules, func(i, j int) bool {
		if rules[i].priority == rules[j].priority {
			return rules[i].ruleID < rules[j].ruleID
		}
		return rules[i].priority > rules[j].priority
	})

	selected := make(map[string]selectedPatch, len(rules))
	order := make([]string, 0, len(rules))
	for _, rule := range rules {
		for _, patch := range rule.rule.Patches {
			key := makeConflictKey(rule.rule.Conditions, patch)
			existing, ok := selected[key]
			if !ok {
				selected[key] = selectedPatch{
					candidate: rule,
					patch:     patch,
				}
				order = append(order, key)
				continue
			}

			if existing.patch.Value == patch.Value {
				// Idempotent duplicate candidate.
				if summary != nil {
					summary.SkippedRuleCount++
				}
				continue
			}

			if existing.candidate.isSystemProtect && !rule.isSystemProtect {
				if summary != nil {
					summary.SkippedRuleCount++
				}
				continue
			}
			if rule.isSystemProtect && !existing.candidate.isSystemProtect {
				selected[key] = selectedPatch{
					candidate: rule,
					patch:     patch,
				}
				continue
			}

			if rule.priority == existing.candidate.priority {
				if rule.onConflict == disasterv1.RestoreModifierConflictPolicySkip {
					if summary != nil {
						summary.SkippedRuleCount++
					}
					continue
				}
				if summary != nil {
					summary.ConflictCount++
				}
				return nil, fmt.Errorf(
					"%s: conflict key=%s existingRule=%s currentRule=%s",
					ModifierErrorRuleConflict,
					key,
					existing.candidate.ruleID,
					rule.ruleID,
				)
			}

			if rule.priority > existing.candidate.priority {
				selected[key] = selectedPatch{
					candidate: rule,
					patch:     patch,
				}
				continue
			}
			if summary != nil {
				summary.SkippedRuleCount++
			}
		}
	}

	compiled := make([]disasterv1.ResourceModifierRule, 0, len(order))
	for _, key := range order {
		patch := selected[key]
		compiled = append(compiled, disasterv1.ResourceModifierRule{
			Conditions: patch.candidate.rule.Conditions,
			Patches:    []disasterv1.JSONPatch{patch.patch},
		})
	}
	if summary != nil {
		summary.AppliedRuleCount = len(compiled)
	}
	return compiled, nil
}

type compiledCandidate struct {
	rule            disasterv1.ResourceModifierRule
	ruleID          string
	priority        int32
	onConflict      disasterv1.RestoreModifierConflictPolicy
	isSystem        bool
	isSystemProtect bool
}

type selectedPatch struct {
	candidate compiledCandidate
	patch     disasterv1.JSONPatch
}

func compileOneModifierRule(
	rule disasterv1.RestoreModifierRule,
	flow modifierFlow,
	options ApplyInstanceRestorePolicyOptions,
) (compiledCandidate, error) {
	if err := validateConditionsGovernance(rule.Conditions); err != nil {
		return compiledCandidate{}, fmt.Errorf("%s: %v", ModifierErrorRuleRejected, err)
	}
	switch normalizeMode(rule.Mode) {
	case disasterv1.RestoreModifierModeVeleroNative:
		return compileVeleroNativeRule(rule)
	case disasterv1.RestoreModifierModeReversible:
		return compileReversibleRule(rule, flow, options)
	default:
		return compiledCandidate{}, fmt.Errorf("%s: unsupported mode=%s", ModifierErrorRuleRejected, rule.Mode)
	}
}

func compileVeleroNativeRule(rule disasterv1.RestoreModifierRule) (compiledCandidate, error) {
	if rule.VeleroRule == nil {
		return compiledCandidate{}, fmt.Errorf("%s: veleroNative rule missing veleroRule", ModifierErrorRuleRejected)
	}
	if len(rule.VeleroRule.MergePatches) > 0 || len(rule.VeleroRule.StrategicPatches) > 0 {
		return compiledCandidate{}, fmt.Errorf("%s: mergePatches/strategicPatches are not supported in phase 1", ModifierErrorRuleRejected)
	}
	if len(rule.VeleroRule.Patches) == 0 {
		return compiledCandidate{}, fmt.Errorf("%s: veleroNative rule patches cannot be empty", ModifierErrorRuleRejected)
	}
	patches := make([]disasterv1.JSONPatch, 0, len(rule.VeleroRule.Patches))
	for _, patch := range rule.VeleroRule.Patches {
		if err := validatePatchGovernance(rule.Conditions, patch, patchGovernanceOptions{}); err != nil {
			return compiledCandidate{}, err
		}
		patches = append(patches, patch)
	}
	return compiledCandidate{
		rule: disasterv1.ResourceModifierRule{
			Conditions: rule.Conditions,
			Patches:    patches,
		},
	}, nil
}

func compileReversibleRule(
	rule disasterv1.RestoreModifierRule,
	flow modifierFlow,
	options ApplyInstanceRestorePolicyOptions,
) (compiledCandidate, error) {
	if rule.Pair == nil {
		return compiledCandidate{}, fmt.Errorf(
			"%s: reversible rule must use pair canonical form (pair.path, pair.sourceValue, pair.targetValue)",
			ModifierErrorRuleNotReversible,
		)
	}
	path := strings.TrimSpace(rule.Pair.Path)
	if path == "" {
		return compiledCandidate{}, fmt.Errorf("%s: reversible rule missing pair.path", ModifierErrorRuleNotReversible)
	}
	if strings.HasSuffix(path, "/-") {
		return compiledCandidate{}, fmt.Errorf("%s: reversible rule does not support append path /-", ModifierErrorRuleNotReversible)
	}

	value, err := resolvePairValue(rule.Pair, flow, options)
	if err != nil {
		return compiledCandidate{}, err
	}
	value = encodePairValueForVeleroPath(path, value)
	patch := disasterv1.JSONPatch{
		Operation: "add",
		Path:      path,
		Value:     value,
	}
	if err := validatePatchGovernance(rule.Conditions, patch, patchGovernanceOptions{}); err != nil {
		return compiledCandidate{}, err
	}
	return compiledCandidate{
		rule: disasterv1.ResourceModifierRule{
			Conditions: rule.Conditions,
			Patches:    []disasterv1.JSONPatch{patch},
		},
	}, nil
}

func resolvePairValue(
	pair *disasterv1.RestoreModifierPair,
	flow modifierFlow,
	options ApplyInstanceRestorePolicyOptions,
) (string, error) {
	if pair == nil {
		return "", fmt.Errorf(
			"%s: reversible rule must use pair canonical form (pair.path, pair.sourceValue, pair.targetValue)",
			ModifierErrorRuleNotReversible,
		)
	}

	rawValue := strings.TrimSpace(pair.TargetValue)
	if flow == modifierFlowReverse {
		rawValue = strings.TrimSpace(pair.SourceValue)
	}
	if flow == modifierFlowForward && rawValue == "" {
		return "", fmt.Errorf("%s: reversible rule missing pair.targetValue", ModifierErrorRuleNotReversible)
	}
	if flow == modifierFlowReverse && rawValue == "" {
		return "", fmt.Errorf("%s: reversible rule missing pair.sourceValue", ModifierErrorRuleNotReversible)
	}
	return renderRestrictedPairValue(rawValue, flow, options)
}

func renderRestrictedPairValue(
	rawValue string,
	flow modifierFlow,
	options ApplyInstanceRestorePolicyOptions,
) (string, error) {
	if !strings.Contains(rawValue, "{{") {
		return rawValue, nil
	}
	if strings.TrimSpace(options.BaselineSourceCluster) == "" || strings.TrimSpace(options.BaselineTargetCluster) == "" {
		return "", fmt.Errorf("%s: pair placeholders require baseline source/target clusters", ModifierErrorDirectionResolveFailed)
	}

	rendered := pairPlaceholderPattern.ReplaceAllStringFunc(rawValue, func(token string) string {
		match := pairPlaceholderPattern.FindStringSubmatch(token)
		if len(match) < 2 {
			return token
		}
		switch match[1] {
		case "SourceCluster":
			return options.BaselineSourceCluster
		case "TargetCluster":
			return options.BaselineTargetCluster
		case "Flow":
			return string(flow)
		default:
			return token
		}
	})
	if strings.Contains(rendered, "{{") || strings.Contains(rendered, "}}") {
		return "", fmt.Errorf(
			"%s: pair value contains unsupported placeholder; only {{ .SourceCluster }}, {{ .TargetCluster }}, {{ .Flow }} are allowed",
			ModifierErrorRuleRejected,
		)
	}
	return rendered, nil
}

type patchGovernanceOptions struct {
	fromSystemRule bool
}

func validatePatchGovernance(
	conditions disasterv1.Conditions,
	patch disasterv1.JSONPatch,
	options patchGovernanceOptions,
) error {
	path := strings.TrimSpace(patch.Path)
	if path == "" {
		return fmt.Errorf("%s: patch path cannot be empty", ModifierErrorRuleRejected)
	}
	if depth := jsonPointerDepth(path); depth > maxJSONPointerDepth {
		return fmt.Errorf("%s: patch path depth %d exceeds limit %d", ModifierErrorRuleRejected, depth, maxJSONPointerDepth)
	}
	if strings.HasPrefix(path, "/status/") || path == "/status" {
		return fmt.Errorf("%s: patch path %s is forbidden", ModifierErrorRuleRejected, path)
	}
	if path == "/metadata/finalizers" || strings.HasPrefix(path, "/metadata/finalizers/") {
		return fmt.Errorf("%s: patch path %s is forbidden", ModifierErrorRuleRejected, path)
	}
	if path == "/metadata/ownerReferences" || strings.HasPrefix(path, "/metadata/ownerReferences/") {
		if options.fromSystemRule && isTrafficlessOwnerReferenceException(conditions, patch) {
			return nil
		}
		return fmt.Errorf("%s: patch path %s is forbidden", ModifierErrorRuleRejected, path)
	}
	return nil
}

func isTrafficlessOwnerReferenceException(
	conditions disasterv1.Conditions,
	patch disasterv1.JSONPatch,
) bool {
	if strings.TrimSpace(patch.Path) != "/metadata/ownerReferences" {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(conditions.GroupResource), "pods") {
		return false
	}

	op := strings.ToLower(strings.TrimSpace(patch.Operation))
	switch op {
	case "remove":
		return true
	case "add":
		return isEmptyJSONArrayLiteral(patch.Value)
	default:
		return false
	}
}

func isEmptyJSONArrayLiteral(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	var items []any
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return false
	}
	return len(items) == 0
}

func modifierRuleEnabled(rule disasterv1.RestoreModifierRule) bool {
	if rule.Enabled == nil {
		return true
	}
	return *rule.Enabled
}

func modifierRuleAppliesToTarget(rule disasterv1.RestoreModifierRule, target disasterv1.RestoreModifierApplyTarget) bool {
	if target == "" || len(rule.ApplyTo) == 0 {
		return true
	}
	for _, t := range rule.ApplyTo {
		if t == target {
			return true
		}
	}
	return false
}

func directionPolicyAllows(policy disasterv1.RestoreModifierDirectionPolicy, flow modifierFlow) bool {
	switch normalizeDirectionPolicy(policy) {
	case disasterv1.RestoreModifierDirectionPolicyForwardOnly:
		return flow == modifierFlowForward
	case disasterv1.RestoreModifierDirectionPolicyReverseOnly:
		return flow == modifierFlowReverse
	default:
		return true
	}
}

func normalizeMode(mode disasterv1.RestoreModifierMode) disasterv1.RestoreModifierMode {
	switch strings.TrimSpace(string(mode)) {
	case string(disasterv1.RestoreModifierModeVeleroNative):
		return disasterv1.RestoreModifierModeVeleroNative
	case string(disasterv1.RestoreModifierModeReversible):
		return disasterv1.RestoreModifierModeReversible
	default:
		return mode
	}
}

func normalizeDirectionPolicy(policy disasterv1.RestoreModifierDirectionPolicy) disasterv1.RestoreModifierDirectionPolicy {
	switch strings.TrimSpace(string(policy)) {
	case string(disasterv1.RestoreModifierDirectionPolicyForwardOnly):
		return disasterv1.RestoreModifierDirectionPolicyForwardOnly
	case string(disasterv1.RestoreModifierDirectionPolicyReverseOnly):
		return disasterv1.RestoreModifierDirectionPolicyReverseOnly
	default:
		return disasterv1.RestoreModifierDirectionPolicyAuto
	}
}

func normalizeOnConflict(policy disasterv1.RestoreModifierConflictPolicy) disasterv1.RestoreModifierConflictPolicy {
	switch strings.TrimSpace(string(policy)) {
	case string(disasterv1.RestoreModifierConflictPolicySkip):
		return disasterv1.RestoreModifierConflictPolicySkip
	default:
		return disasterv1.RestoreModifierConflictPolicyFail
	}
}

func normalizedRuleID(rule disasterv1.RestoreModifierRule, idx int) string {
	id := strings.TrimSpace(rule.ID)
	if id != "" {
		return id
	}
	return fmt.Sprintf("rule-%04d", idx)
}

func makeConflictKey(conditions disasterv1.Conditions, patch disasterv1.JSONPatch) string {
	return fmt.Sprintf(
		"%s|%s|%s",
		normalizedConditions(conditions),
		strings.ToLower(strings.TrimSpace(patch.Operation)),
		strings.TrimSpace(patch.Path),
	)
}

func normalizedConditions(conditions disasterv1.Conditions) string {
	namespaces := append([]string{}, conditions.Namespaces...)
	sort.Strings(namespaces)
	labelSelector := ""
	if conditions.LabelSelector != nil {
		selector, err := metav1.LabelSelectorAsSelector(conditions.LabelSelector)
		if err == nil {
			labelSelector = selector.String()
		}
	}
	return fmt.Sprintf(
		"groupResource=%s|resourceNameRegex=%s|namespaces=%s|labelSelector=%s",
		strings.TrimSpace(conditions.GroupResource),
		strings.TrimSpace(conditions.ResourceNameRegex),
		strings.Join(namespaces, ","),
		labelSelector,
	)
}

func resolveModifierFlow(
	baselineSource string,
	baselineTarget string,
	runtimePrimary string,
	runtimeSecondary string,
) (modifierFlow, directionSource, error) {
	baselineSource = strings.TrimSpace(baselineSource)
	baselineTarget = strings.TrimSpace(baselineTarget)
	runtimePrimary = strings.TrimSpace(runtimePrimary)
	runtimeSecondary = strings.TrimSpace(runtimeSecondary)

	if runtimePrimary == "" && runtimeSecondary == "" {
		return modifierFlowForward, directionSourceBaselineFallback, nil
	}
	if runtimePrimary == "" || runtimeSecondary == "" {
		return "", "", fmt.Errorf("%s: runtime primary/secondary is incomplete", ModifierErrorDirectionResolveFailed)
	}
	if baselineSource == "" || baselineTarget == "" {
		return "", "", fmt.Errorf("%s: baseline source/target is required when runtime role exists", ModifierErrorDirectionResolveFailed)
	}

	if runtimePrimary == baselineSource && runtimeSecondary == baselineTarget {
		return modifierFlowForward, directionSourceRuntimeStatus, nil
	}
	if runtimePrimary == baselineTarget && runtimeSecondary == baselineSource {
		return modifierFlowReverse, directionSourceRuntimeStatus, nil
	}
	return "", "", fmt.Errorf("%s: runtime role does not match baseline", ModifierErrorDirectionResolveFailed)
}
