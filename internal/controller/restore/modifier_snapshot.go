package restore

import (
	"fmt"
	"strings"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
)

func bulkModifierActionEnabled(action disasterv1.BulkModifierAction) bool {
	if action.Enabled == nil {
		return true
	}
	return *action.Enabled
}

func effectiveBulkModifierActions(policy *disasterv1.RestorePolicy) []disasterv1.BulkModifierAction {
	if policy == nil || len(policy.BulkModifierActions) == 0 {
		return nil
	}
	out := make([]disasterv1.BulkModifierAction, 0, len(policy.BulkModifierActions))
	for idx := range policy.BulkModifierActions {
		action := policy.BulkModifierActions[idx]
		if !bulkModifierActionEnabled(action) {
			continue
		}
		copied := action.DeepCopy()
		if copied == nil {
			continue
		}
		out = append(out, *copied)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func hasEffectiveBulkModifierActions(policy *disasterv1.RestorePolicy) bool {
	return len(effectiveBulkModifierActions(policy)) > 0
}

// HasEffectiveBulkModifierActions reports whether policy contains at least one enabled bulk action.
func HasEffectiveBulkModifierActions(policy *disasterv1.RestorePolicy) bool {
	return hasEffectiveBulkModifierActions(policy)
}

func effectiveModifierRuleInput(policy *disasterv1.RestorePolicy) ([]disasterv1.RestoreModifierRule, bool, error) {
	if policy == nil {
		return nil, false, nil
	}
	if !hasEffectiveBulkModifierActions(policy) {
		return cloneRestoreModifierRules(policy.ModifierRules), false, nil
	}
	if len(policy.ModifierRuleSnapshot) == 0 {
		return nil, true, fmt.Errorf("%s: bulk modifier snapshot is required when enabled bulkModifierActions exist", ModifierErrorRuleRejected)
	}
	if strings.TrimSpace(policy.ModifierRuleSnapshotHash) == "" {
		return nil, true, fmt.Errorf("%s: modifierRuleSnapshotHash is required when enabled bulkModifierActions exist", ModifierErrorRuleRejected)
	}
	return cloneRestoreModifierRules(policy.ModifierRuleSnapshot), true, nil
}

// EffectiveModifierRuleInput returns the final modifier rule input selected by bulk snapshot contract.
func EffectiveModifierRuleInput(policy *disasterv1.RestorePolicy) ([]disasterv1.RestoreModifierRule, bool, error) {
	return effectiveModifierRuleInput(policy)
}

func cloneRestoreModifierRules(in []disasterv1.RestoreModifierRule) []disasterv1.RestoreModifierRule {
	if len(in) == 0 {
		return nil
	}
	out := make([]disasterv1.RestoreModifierRule, len(in))
	for i := range in {
		in[i].DeepCopyInto(&out[i])
	}
	return out
}

func cloneBulkModifierActions(in []disasterv1.BulkModifierAction) []disasterv1.BulkModifierAction {
	if len(in) == 0 {
		return nil
	}
	out := make([]disasterv1.BulkModifierAction, len(in))
	for i := range in {
		in[i].DeepCopyInto(&out[i])
	}
	return out
}
