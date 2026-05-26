package restore

import (
	"fmt"
	"regexp"
	"regexp/syntax"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
)

const (
	// Phase 1 complexity guardrails.
	maxModifierRulesPerInstance     = 200
	maxPatchesPerRule               = 50
	maxJSONPointerDepth             = 32
	maxResourceNameRegexLength      = 256
	maxResourceNameRegexProgramSize = 1024
)

func validateRuleCountLimit(total int) error {
	if total > maxModifierRulesPerInstance {
		return fmt.Errorf(
			"%s: modifier rule count %d exceeds limit %d",
			ModifierErrorRuleRejected,
			total,
			maxModifierRulesPerInstance,
		)
	}
	return nil
}

func validateRestoreModifierRuleComplexity(ruleID string, rule disasterv1.RestoreModifierRule) error {
	if err := validateConditionsGovernance(rule.Conditions); err != nil {
		return fmt.Errorf("%s: rule=%s %v", ModifierErrorRuleRejected, ruleID, err)
	}
	if normalizeMode(rule.Mode) == disasterv1.RestoreModifierModeVeleroNative && rule.VeleroRule != nil {
		if len(rule.VeleroRule.Patches) > maxPatchesPerRule {
			return fmt.Errorf(
				"%s: rule=%s patch count %d exceeds limit %d",
				ModifierErrorRuleRejected,
				ruleID,
				len(rule.VeleroRule.Patches),
				maxPatchesPerRule,
			)
		}
	}
	return nil
}

func validateResourceModifierRuleComplexity(ruleID string, rule disasterv1.ResourceModifierRule) error {
	if err := validateConditionsGovernance(rule.Conditions); err != nil {
		return fmt.Errorf("%s: rule=%s %v", ModifierErrorRuleRejected, ruleID, err)
	}
	if len(rule.Patches) > maxPatchesPerRule {
		return fmt.Errorf(
			"%s: rule=%s patch count %d exceeds limit %d",
			ModifierErrorRuleRejected,
			ruleID,
			len(rule.Patches),
			maxPatchesPerRule,
		)
	}
	return nil
}

func validateConditionsGovernance(conditions disasterv1.Conditions) error {
	if _, err := parseGroupResource(conditions.GroupResource); err != nil {
		return err
	}
	if conditions.LabelSelector != nil {
		if _, err := metav1.LabelSelectorAsSelector(conditions.LabelSelector); err != nil {
			return fmt.Errorf("invalid labelSelector: %v", err)
		}
	}
	if _, err := compileResourceNameRegex(conditions.ResourceNameRegex); err != nil {
		return err
	}
	return nil
}

func compileResourceNameRegex(raw string) (*regexp.Regexp, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if len(raw) > maxResourceNameRegexLength {
		return nil, fmt.Errorf(
			"%s: resourceNameRegex length %d exceeds limit %d",
			ModifierErrorRuleRejected,
			len(raw),
			maxResourceNameRegexLength,
		)
	}

	compiled, err := regexp.Compile(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: invalid resourceNameRegex: %v", ModifierErrorRuleRejected, err)
	}

	ast, err := syntax.Parse(raw, syntax.Perl)
	if err != nil {
		return nil, fmt.Errorf("%s: invalid resourceNameRegex: %v", ModifierErrorRuleRejected, err)
	}
	ast = ast.Simplify()
	prog, err := syntax.Compile(ast)
	if err != nil {
		return nil, fmt.Errorf("%s: invalid resourceNameRegex: %v", ModifierErrorRuleRejected, err)
	}
	if len(prog.Inst) > maxResourceNameRegexProgramSize {
		return nil, fmt.Errorf(
			"%s: resourceNameRegex program size %d exceeds limit %d",
			ModifierErrorRuleRejected,
			len(prog.Inst),
			maxResourceNameRegexProgramSize,
		)
	}
	return compiled, nil
}

func jsonPointerDepth(path string) int {
	path = strings.TrimSpace(path)
	if path == "" || path == "/" {
		return 0
	}
	if !strings.HasPrefix(path, "/") {
		return 0
	}
	depth := 0
	for _, token := range strings.Split(path[1:], "/") {
		if token == "" {
			continue
		}
		depth++
	}
	return depth
}
